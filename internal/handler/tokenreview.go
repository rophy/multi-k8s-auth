package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/rophy/kube-federated-auth/internal/config"
	"github.com/rophy/kube-federated-auth/internal/credentials"
	"github.com/rophy/kube-federated-auth/internal/oidc"
)

// ExtraKeyClusterName is the key used in TokenReview response extra field
// to indicate which cluster the token was validated against.
const ExtraKeyClusterName = "authentication.kubernetes.io/cluster-name"

// TokenVerifier verifies tokens against a specific cluster's JWKS.
type TokenVerifier interface {
	Verify(ctx context.Context, clusterName, rawToken string) (*oidc.Claims, error)
}

type TokenReviewHandler struct {
	verifier  TokenVerifier
	config    *config.Config
	credStore *credentials.Store
}

func NewTokenReviewHandler(v TokenVerifier, cfg *config.Config, store *credentials.Store) *TokenReviewHandler {
	return &TokenReviewHandler{
		verifier:  v,
		config:    cfg,
		credStore: store,
	}
}

func (h *TokenReviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Step 0: Authenticate the caller via their own SA token
	if h.config != nil && len(h.config.AuthorizedClients) > 0 {
		if err := h.authenticateCaller(r); err != nil {
			h.writeError(w, err.code, err.message)
			return
		}
	}

	// Parse TokenReview request
	var tr authv1.TokenReview
	if err := json.NewDecoder(r.Body).Decode(&tr); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if tr.Spec.Token == "" {
		h.writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	if h.verifier == nil || h.config == nil {
		h.writeUnauthenticated(w, &tr, "server not configured")
		return
	}

	// Step 1: Detect cluster via JWKS (local, no token leakage)
	cluster, err := h.detectCluster(r.Context(), tr.Spec.Token)
	if err != nil {
		log.Printf("Cluster detection failed: %v", err)
		h.writeUnauthenticated(w, &tr, "token not valid for any configured cluster")
		return
	}

	log.Printf("Detected cluster: %s", cluster)

	// Step 2: Forward TokenReview to detected cluster
	result, err := h.forwardTokenReview(r.Context(), cluster, &tr)
	if err != nil {
		log.Printf("TokenReview forwarding failed for cluster %s: %v", cluster, err)
		h.writeUnauthenticated(w, &tr, fmt.Sprintf("failed to validate token: %v", err))
		return
	}

	// Add cluster name to extra field for client awareness
	if result.Status.Authenticated {
		if result.Status.User.Extra == nil {
			result.Status.User.Extra = make(map[string]authv1.ExtraValue)
		}
		result.Status.User.Extra[ExtraKeyClusterName] = authv1.ExtraValue{cluster}
	}

	// Return the response from the remote cluster
	json.NewEncoder(w).Encode(result)
}

type authError struct {
	code    int
	message string
}

func (e *authError) Error() string {
	return e.message
}

// authenticateCaller verifies the caller's own ServiceAccount token from the Authorization header.
// Returns nil if the caller is authorized, or an authError with appropriate HTTP status.
func (h *TokenReviewHandler) authenticateCaller(r *http.Request) *authError {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return &authError{http.StatusUnauthorized, "Authorization header required"}
	}

	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return &authError{http.StatusUnauthorized, "Authorization header must use Bearer scheme"}
	}

	callerToken := strings.TrimPrefix(authHeader, bearerPrefix)
	if callerToken == "" {
		return &authError{http.StatusUnauthorized, "bearer token is empty"}
	}

	if h.verifier == nil {
		return &authError{http.StatusInternalServerError, "server not configured for authentication"}
	}

	// Verify caller's token via JWKS to find the source cluster
	var callerCluster string
	var callerClaims *oidc.Claims
	for clusterName := range h.config.Clusters {
		claims, err := h.verifier.Verify(r.Context(), clusterName, callerToken)
		if err == nil {
			callerCluster = clusterName
			callerClaims = claims
			break
		}
	}

	if callerClaims == nil {
		return &authError{http.StatusUnauthorized, "caller token not valid for any configured cluster"}
	}

	// Extract namespace and service account from claims
	namespace, saName := extractIdentity(callerClaims)
	if namespace == "" || saName == "" {
		return &authError{http.StatusUnauthorized, "caller token missing identity claims"}
	}

	// Check against authorized_clients whitelist
	if !h.config.IsAuthorizedClient(callerCluster, namespace, saName) {
		log.Printf("Unauthorized caller: %s/%s/%s", callerCluster, namespace, saName)
		return &authError{http.StatusForbidden, fmt.Sprintf("caller %s/%s/%s is not authorized", callerCluster, namespace, saName)}
	}

	log.Printf("Authorized caller: %s/%s/%s", callerCluster, namespace, saName)
	return nil
}

// extractIdentity extracts namespace and service account name from OIDC claims.
func extractIdentity(claims *oidc.Claims) (namespace, serviceAccount string) {
	if claims.Kubernetes == nil {
		return "", ""
	}

	if ns, ok := claims.Kubernetes["namespace"].(string); ok {
		namespace = ns
	}

	if sa, ok := claims.Kubernetes["serviceaccount"].(map[string]any); ok {
		if name, ok := sa["name"].(string); ok {
			serviceAccount = name
		}
	}

	return namespace, serviceAccount
}

// detectCluster tries to verify the token against all configured clusters using JWKS.
// This is done locally without sending the token anywhere.
// Returns the cluster name that successfully verified the token signature.
func (h *TokenReviewHandler) detectCluster(ctx context.Context, token string) (string, error) {
	for clusterName := range h.config.Clusters {
		_, err := h.verifier.Verify(ctx, clusterName, token)
		if err == nil {
			return clusterName, nil
		}
		// Signature didn't match - try next cluster
		log.Printf("Token not valid for cluster %s: %v", clusterName, err)
	}
	return "", fmt.Errorf("token signature does not match any configured cluster")
}

// forwardTokenReview sends the TokenReview request to the detected cluster's API server.
func (h *TokenReviewHandler) forwardTokenReview(ctx context.Context, clusterName string, tr *authv1.TokenReview) (*authv1.TokenReview, error) {
	clusterCfg, ok := h.config.Clusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("cluster not found: %s", clusterName)
	}

	// Build REST config for the target cluster
	restConfig, err := h.buildRESTConfig(clusterName, clusterCfg)
	if err != nil {
		return nil, fmt.Errorf("building REST config: %w", err)
	}

	// Create Kubernetes client
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	// Forward TokenReview request
	result, err := client.AuthenticationV1().TokenReviews().Create(ctx, tr, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("calling TokenReview API: %w", err)
	}

	// Ensure TypeMeta is set (k8s client doesn't populate this on responses)
	result.APIVersion = "authentication.k8s.io/v1"
	result.Kind = "TokenReview"

	return result, nil
}

// buildRESTConfig creates a REST config for the target cluster
func (h *TokenReviewHandler) buildRESTConfig(clusterName string, clusterCfg config.ClusterConfig) (*rest.Config, error) {
	// For clusters with api_server, use remote credentials
	if clusterCfg.APIServer != "" {
		var bearerToken string
		var caCert []byte

		if h.credStore != nil {
			if creds, ok := h.credStore.Get(clusterName); ok {
				bearerToken = creds.Token
				caCert = creds.CACert
			}
		}

		return &rest.Config{
			Host:        clusterCfg.APIServer,
			BearerToken: bearerToken,
			TLSClientConfig: rest.TLSClientConfig{
				CAData: caCert,
			},
		}, nil
	}

	// For local clusters, try in-cluster config first
	inClusterConfig, err := rest.InClusterConfig()
	if err == nil {
		return inClusterConfig, nil
	}

	// Fallback: use issuer as host (for testing)
	return &rest.Config{
		Host: clusterCfg.Issuer,
	}, nil
}

func (h *TokenReviewHandler) writeUnauthenticated(w http.ResponseWriter, req *authv1.TokenReview, errMsg string) {
	resp := &authv1.TokenReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "authentication.k8s.io/v1",
			Kind:       "TokenReview",
		},
		Status: authv1.TokenReviewStatus{
			Authenticated: false,
			Error:         errMsg,
		},
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *TokenReviewHandler) writeError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	resp := &authv1.TokenReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "authentication.k8s.io/v1",
			Kind:       "TokenReview",
		},
		Status: authv1.TokenReviewStatus{
			Authenticated: false,
			Error:         msg,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

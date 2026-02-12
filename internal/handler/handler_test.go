package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authv1 "k8s.io/api/authentication/v1"

	"github.com/rophy/kube-federated-auth/internal/config"
	"github.com/rophy/kube-federated-auth/internal/oidc"
)

// mockVerifier implements TokenVerifier for testing.
type mockVerifier struct {
	// claims maps token -> Claims. If token is not in the map, Verify returns error.
	claims map[string]*oidc.Claims
}

func (m *mockVerifier) Verify(ctx context.Context, clusterName, rawToken string) (*oidc.Claims, error) {
	if c, ok := m.claims[rawToken]; ok {
		if c.Cluster == "" || c.Cluster == clusterName {
			return &oidc.Claims{
				Cluster:    clusterName,
				Issuer:     c.Issuer,
				Subject:    c.Subject,
				Kubernetes: c.Kubernetes,
			}, nil
		}
	}
	return nil, fmt.Errorf("token not valid for cluster %s", clusterName)
}

func TestHealth(t *testing.T) {
	handler := NewHealthHandler("v1.2.3")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}

	if resp.Version != "v1.2.3" {
		t.Errorf("version = %q, want %q", resp.Version, "v1.2.3")
	}
}

func TestClusters(t *testing.T) {
	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"cluster-a": {Issuer: "https://a.example.com"},
			"cluster-b": {Issuer: "https://b.example.com", APIServer: "https://192.168.1.100:6443"},
		},
	}

	handler := NewClustersHandler(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/clusters", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp ClustersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Clusters) != 2 {
		t.Errorf("clusters count = %d, want %d", len(resp.Clusters), 2)
	}

	// Verify cluster info includes issuer and api_server
	for _, c := range resp.Clusters {
		if c.Name == "cluster-b" {
			if c.APIServer != "https://192.168.1.100:6443" {
				t.Errorf("cluster-b api_server = %q, want %q", c.APIServer, "https://192.168.1.100:6443")
			}
		}
		if c.Issuer == "" {
			t.Errorf("cluster %s issuer is empty", c.Name)
		}
	}
}

func TestTokenReview_InvalidJSON(t *testing.T) {
	handler := NewTokenReviewHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp authv1.TokenReview
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status.Authenticated {
		t.Error("expected authenticated = false")
	}
	if resp.Status.Error == "" {
		t.Error("expected error message")
	}
}

func TestTokenReview_MissingToken(t *testing.T) {
	handler := NewTokenReviewHandler(nil, nil, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp authv1.TokenReview
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status.Authenticated {
		t.Error("expected authenticated = false")
	}
	if resp.Status.Error != "token is required" {
		t.Errorf("error = %q, want %q", resp.Status.Error, "token is required")
	}
}

func TestTokenReview_NotConfigured(t *testing.T) {
	handler := NewTokenReviewHandler(nil, nil, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"test-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should return 200 with unauthenticated status (not 500)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp authv1.TokenReview
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status.Authenticated {
		t.Error("expected authenticated = false")
	}
	if resp.Status.Error != "server not configured" {
		t.Errorf("error = %q, want %q", resp.Status.Error, "server not configured")
	}
}

func TestTokenReview_ResponseFormat(t *testing.T) {
	handler := NewTokenReviewHandler(nil, nil, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"invalid-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp authv1.TokenReview
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify response has correct TypeMeta
	if resp.APIVersion != "authentication.k8s.io/v1" {
		t.Errorf("apiVersion = %q, want %q", resp.APIVersion, "authentication.k8s.io/v1")
	}
	if resp.Kind != "TokenReview" {
		t.Errorf("kind = %q, want %q", resp.Kind, "TokenReview")
	}
}

func TestExtraKeyClusterName(t *testing.T) {
	// Verify the constant follows Kubernetes naming convention
	expected := "authentication.kubernetes.io/cluster-name"
	if ExtraKeyClusterName != expected {
		t.Errorf("ExtraKeyClusterName = %q, want %q", ExtraKeyClusterName, expected)
	}
}

func TestTokenReview_NoAuthHeader_WithAuthorizedClients(t *testing.T) {
	cfg := &config.Config{
		AuthorizedClients: []string{"cluster-a/default/my-app"},
		Clusters: map[string]config.ClusterConfig{
			"cluster-a": {Issuer: "https://a.example.com"},
		},
	}
	verifier := &mockVerifier{claims: map[string]*oidc.Claims{}}
	handler := NewTokenReviewHandler(verifier, cfg, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"some-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestTokenReview_InvalidCallerToken(t *testing.T) {
	cfg := &config.Config{
		AuthorizedClients: []string{"cluster-a/default/my-app"},
		Clusters: map[string]config.ClusterConfig{
			"cluster-a": {Issuer: "https://a.example.com"},
		},
	}
	verifier := &mockVerifier{claims: map[string]*oidc.Claims{}}
	handler := NewTokenReviewHandler(verifier, cfg, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"some-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestTokenReview_ValidCallerButNotWhitelisted(t *testing.T) {
	cfg := &config.Config{
		AuthorizedClients: []string{"cluster-a/default/allowed-app"},
		Clusters: map[string]config.ClusterConfig{
			"cluster-a": {Issuer: "https://a.example.com"},
		},
	}
	verifier := &mockVerifier{
		claims: map[string]*oidc.Claims{
			"caller-token": {
				Subject: "system:serviceaccount:default:not-allowed",
				Kubernetes: map[string]any{
					"namespace": "default",
					"serviceaccount": map[string]any{
						"name": "not-allowed",
					},
				},
			},
		},
	}
	handler := NewTokenReviewHandler(verifier, cfg, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"some-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer caller-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestTokenReview_NoAuthorizedClients_SkipsAuth(t *testing.T) {
	// When authorized_clients is empty, auth should be skipped entirely
	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"cluster-a": {Issuer: "https://a.example.com"},
		},
	}
	handler := NewTokenReviewHandler(nil, cfg, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"some-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	// No Authorization header - should still work because no authorized_clients configured
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should reach the "server not configured" path (nil verifier), not 401
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp authv1.TokenReview
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status.Error != "server not configured" {
		t.Errorf("error = %q, want %q", resp.Status.Error, "server not configured")
	}
}

func TestTokenReview_AuthHeaderNotBearer(t *testing.T) {
	cfg := &config.Config{
		AuthorizedClients: []string{"*/*/*"},
		Clusters: map[string]config.ClusterConfig{
			"cluster-a": {Issuer: "https://a.example.com"},
		},
	}
	handler := NewTokenReviewHandler(&mockVerifier{}, cfg, nil)

	body := `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","spec":{"token":"some-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", strings.NewReader(body))
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestExtractIdentity(t *testing.T) {
	claims := &oidc.Claims{
		Kubernetes: map[string]any{
			"namespace": "kube-system",
			"serviceaccount": map[string]any{
				"name": "my-sa",
				"uid":  "abc-123",
			},
		},
	}

	ns, sa := extractIdentity(claims)
	if ns != "kube-system" {
		t.Errorf("namespace = %q, want %q", ns, "kube-system")
	}
	if sa != "my-sa" {
		t.Errorf("serviceAccount = %q, want %q", sa, "my-sa")
	}
}

func TestExtractIdentity_NilKubernetes(t *testing.T) {
	claims := &oidc.Claims{}
	ns, sa := extractIdentity(claims)
	if ns != "" || sa != "" {
		t.Errorf("expected empty, got ns=%q sa=%q", ns, sa)
	}
}

func TestExtractIdentity_MissingFields(t *testing.T) {
	claims := &oidc.Claims{
		Kubernetes: map[string]any{
			"namespace": "default",
			// no serviceaccount
		},
	}
	ns, sa := extractIdentity(claims)
	if ns != "default" {
		t.Errorf("namespace = %q, want %q", ns, "default")
	}
	if sa != "" {
		t.Errorf("serviceAccount = %q, want empty", sa)
	}
}

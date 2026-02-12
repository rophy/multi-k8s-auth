package proxy

import (
	"log"
	"net/http"
	"strings"
)

// Response headers for auth_request mode (nginx auth_request / traefik ForwardAuth / istio ext_authz)
const (
	HeaderAuthRequestUser         = "X-Auth-Request-User"
	HeaderAuthRequestGroups       = "X-Auth-Request-Groups"
	HeaderAuthRequestExtraCluster = "X-Auth-Request-Extra-Cluster-Name"
)

// Request headers for reverse proxy mode (forwarded to upstream)
const (
	HeaderForwardedUser         = "X-Forwarded-User"
	HeaderForwardedGroups       = "X-Forwarded-Groups"
	HeaderForwardedExtraCluster = "X-Forwarded-Extra-Cluster-Name"
)

const ExtraKeyClusterName = "authentication.kubernetes.io/cluster-name"

type AuthHandler struct {
	reviewer TokenReviewer
}

func NewAuthHandler(reviewer TokenReviewer) *AuthHandler {
	return &AuthHandler{reviewer: reviewer}
}

func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	result, err := h.reviewer.Review(r.Context(), token)
	if err != nil {
		log.Printf("TokenReview error: %v", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if !result.Status.Authenticated {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	w.Header().Set(HeaderAuthRequestUser, result.Status.User.Username)
	if len(result.Status.User.Groups) > 0 {
		w.Header().Set(HeaderAuthRequestGroups, strings.Join(result.Status.User.Groups, ","))
	}
	if clusterNames, ok := result.Status.User.Extra[ExtraKeyClusterName]; ok && len(clusterNames) > 0 {
		w.Header().Set(HeaderAuthRequestExtraCluster, string(clusterNames[0]))
	}

	w.WriteHeader(http.StatusOK)
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimPrefix(auth, prefix)
}

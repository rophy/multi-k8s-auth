package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockReviewer implements TokenReviewer for testing.
type mockReviewer struct {
	result *TokenReviewResponse
	err    error
}

func (m *mockReviewer) Review(ctx context.Context, token string) (*TokenReviewResponse, error) {
	return m.result, m.err
}

func authenticatedReviewer(username string, groups []string, extra map[string][]string) *mockReviewer {
	return &mockReviewer{
		result: &TokenReviewResponse{
			APIVersion: "authentication.k8s.io/v1",
			Kind:       "TokenReview",
			Status: TokenReviewStatus{
				Authenticated: true,
				User: UserInfo{
					Username: username,
					Groups:   groups,
					Extra:    extra,
				},
			},
		},
	}
}

func unauthenticatedReviewer() *mockReviewer {
	return &mockReviewer{
		result: &TokenReviewResponse{
			Status: TokenReviewStatus{
				Authenticated: false,
				Error:         "token not valid",
			},
		},
	}
}

func errorReviewer() *mockReviewer {
	return &mockReviewer{err: fmt.Errorf("connection refused")}
}

func TestAuthHandler_NoToken(t *testing.T) {
	handler := NewAuthHandler(authenticatedReviewer("user", nil, nil))
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_InvalidBearerPrefix(t *testing.T) {
	handler := NewAuthHandler(authenticatedReviewer("user", nil, nil))
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_Authenticated(t *testing.T) {
	extra := map[string][]string{
		ExtraKeyClusterName: {"cluster-b"},
	}
	handler := NewAuthHandler(authenticatedReviewer("system:serviceaccount:default:test", []string{"system:serviceaccounts", "system:serviceaccounts:default"}, extra))
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	if user := w.Header().Get(HeaderAuthRequestUser); user != "system:serviceaccount:default:test" {
		t.Errorf("X-Auth-Request-User = %q, want %q", user, "system:serviceaccount:default:test")
	}
	if groups := w.Header().Get(HeaderAuthRequestGroups); groups != "system:serviceaccounts,system:serviceaccounts:default" {
		t.Errorf("X-Auth-Request-Groups = %q, want %q", groups, "system:serviceaccounts,system:serviceaccounts:default")
	}
	if cluster := w.Header().Get(HeaderAuthRequestExtraCluster); cluster != "cluster-b" {
		t.Errorf("X-Auth-Request-Extra-Cluster-Name = %q, want %q", cluster, "cluster-b")
	}
}

func TestAuthHandler_Unauthenticated(t *testing.T) {
	handler := NewAuthHandler(unauthenticatedReviewer())
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	if user := w.Header().Get(HeaderAuthRequestUser); user != "" {
		t.Errorf("X-Auth-Request-User should be empty, got %q", user)
	}
}

func TestAuthHandler_BackendUnreachable(t *testing.T) {
	handler := NewAuthHandler(errorReviewer())
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

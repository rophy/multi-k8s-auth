package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReverseProxy_Authenticated(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get(HeaderForwardedUser)
		if user != "system:serviceaccount:default:test" {
			t.Errorf("upstream got X-Forwarded-User = %q, want %q", user, "system:serviceaccount:default:test")
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Error("upstream should not receive Authorization header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	extra := map[string][]string{
		ExtraKeyClusterName: {"cluster-b"},
	}
	reviewer := authenticatedReviewer("system:serviceaccount:default:test", []string{"system:serviceaccounts"}, extra)

	handler, err := NewReverseProxyHandler(reviewer, upstream.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(w.Result().Body)
	if string(body) != "upstream response" {
		t.Errorf("body = %q, want %q", string(body), "upstream response")
	}
}

func TestReverseProxy_NoToken(t *testing.T) {
	reviewer := authenticatedReviewer("user", nil, nil)

	handler, err := NewReverseProxyHandler(reviewer, "http://localhost:9999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestReverseProxy_Unauthenticated(t *testing.T) {
	reviewer := unauthenticatedReviewer()

	handler, err := NewReverseProxyHandler(reviewer, "http://localhost:9999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

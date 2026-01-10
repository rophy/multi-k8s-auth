package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	authv1 "k8s.io/api/authentication/v1"
)

// These tests can run in two modes:
// 1. In-cluster: as a pod with SERVICE_HOST and TOKEN_PATH env vars
// 2. Local: with kubectl port-forward (SERVICE_HOST=localhost:8080)

var (
	serviceHost = getEnv("SERVICE_HOST", "localhost:8080")
	tokenPath   = getEnv("TOKEN_PATH", "")
	clusterName = getEnv("CLUSTER_NAME", "cluster-b")
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// buildBaseURL constructs URL without cluster routing (for health/clusters endpoints)
func buildBaseURL() string {
	return fmt.Sprintf("http://%s", serviceHost)
}

// buildClusterHost returns the Host header value for cluster routing
func buildClusterHost(cluster string) string {
	return fmt.Sprintf("api.%s.kube-fed", cluster)
}

func TestMain(m *testing.M) {
	if os.Getenv("E2E_TEST") != "true" {
		fmt.Println("Skipping e2e tests. Set E2E_TEST=true to run.")
		os.Exit(0)
	}

	// Wait for service to be ready
	waitForService(30 * time.Second)

	os.Exit(m.Run())
}

func TestHealth(t *testing.T) {
	resp, err := http.Get(buildBaseURL() + "/health")
	if err != nil {
		t.Fatalf("failed to call /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestClusters(t *testing.T) {
	resp, err := http.Get(buildBaseURL() + "/clusters")
	if err != nil {
		t.Fatalf("failed to call /clusters: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body struct {
		Clusters []struct {
			Name string `json:"name"`
		} `json:"clusters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	found := false
	for _, c := range body.Clusters {
		if c.Name == clusterName {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(body.Clusters))
		for i, c := range body.Clusters {
			names[i] = c.Name
		}
		t.Errorf("cluster %q not found in %v", clusterName, names)
	}
}

func TestTokenReview_Success(t *testing.T) {
	token := getTestToken(t)

	tr := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: token,
		},
	}
	tr.APIVersion = "authentication.k8s.io/v1"
	tr.Kind = "TokenReview"

	reqBody, _ := json.Marshal(tr)

	url := buildBaseURL() + "/apis/authentication.k8s.io/v1/tokenreviews"
	req, _ := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Host = buildClusterHost(clusterName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to call tokenreviews: %v", err)
	}
	defer resp.Body.Close()

	var result authv1.TokenReview
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, error: %s", resp.StatusCode, http.StatusOK, result.Status.Error)
	}

	if !result.Status.Authenticated {
		t.Fatalf("expected authenticated = true, got error: %s", result.Status.Error)
	}

	// Verify expected fields
	if !strings.HasPrefix(result.Status.User.Username, "system:serviceaccount:") {
		t.Errorf("username = %v, want system:serviceaccount:...", result.Status.User.Username)
	}

	if result.APIVersion != "authentication.k8s.io/v1" {
		t.Errorf("apiVersion = %q, want %q", result.APIVersion, "authentication.k8s.io/v1")
	}

	if result.Kind != "TokenReview" {
		t.Errorf("kind = %q, want %q", result.Kind, "TokenReview")
	}
}

func TestTokenReview_InvalidToken(t *testing.T) {
	tr := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: "invalid.token.here",
		},
	}
	tr.APIVersion = "authentication.k8s.io/v1"
	tr.Kind = "TokenReview"

	reqBody, _ := json.Marshal(tr)

	url := buildBaseURL() + "/apis/authentication.k8s.io/v1/tokenreviews"
	req, _ := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Host = buildClusterHost(clusterName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to call tokenreviews: %v", err)
	}
	defer resp.Body.Close()

	var result authv1.TokenReview
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Status.Authenticated {
		t.Error("expected authenticated = false for invalid token")
	}

	if result.Status.Error == "" {
		t.Error("expected error message for invalid token")
	}
}

func TestTokenReview_InvalidHost(t *testing.T) {
	tr := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: "some-token",
		},
	}
	tr.APIVersion = "authentication.k8s.io/v1"
	tr.Kind = "TokenReview"

	reqBody, _ := json.Marshal(tr)

	url := buildBaseURL() + "/apis/authentication.k8s.io/v1/tokenreviews"
	req, _ := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "invalid.host.name"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to call tokenreviews: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var result authv1.TokenReview
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Status.Authenticated {
		t.Error("expected authenticated = false for invalid host")
	}

	if !strings.Contains(result.Status.Error, "unable to determine cluster") {
		t.Errorf("error = %q, expected to contain 'unable to determine cluster'", result.Status.Error)
	}
}

// getTestToken reads the token from TOKEN_PATH env var (in-cluster)
// or returns empty string to skip token-based tests (local without token)
func getTestToken(t *testing.T) string {
	t.Helper()

	if tokenPath == "" {
		t.Skip("TOKEN_PATH not set, skipping token validation test")
	}

	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("failed to read token from %s: %v", tokenPath, err)
	}

	return strings.TrimSpace(string(token))
}

// waitForService waits for the service to be ready
func waitForService(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(buildBaseURL() + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			fmt.Printf("Service ready at %s\n", buildBaseURL())
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(time.Second)
	}
	fmt.Printf("Warning: service at %s may not be ready\n", buildBaseURL())
}

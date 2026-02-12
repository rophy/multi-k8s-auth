package proxy

import "testing"

func TestConfig_Validate_NoTokenReviewURL(t *testing.T) {
	cfg := &Config{
		TokenReviewURL: "",
		Port:           4180,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error when token-review-url is empty (defaults to in-cluster), got: %v", err)
	}
}

func TestConfig_Validate_WithTokenReviewURL(t *testing.T) {
	cfg := &Config{
		TokenReviewURL: "http://kube-federated-auth:8080",
		Port:           4180,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestConfig_RESTConfig_WithURL(t *testing.T) {
	cfg := &Config{
		TokenReviewURL: "http://kube-federated-auth:8080",
	}
	restConfig, err := cfg.RESTConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restConfig.Host != "http://kube-federated-auth:8080" {
		t.Errorf("host = %q, want %q", restConfig.Host, "http://kube-federated-auth:8080")
	}
	if restConfig.ContentConfig.ContentType != "application/json" {
		t.Errorf("content type = %q, want %q", restConfig.ContentConfig.ContentType, "application/json")
	}
}

package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const ExtraKeyClusterName = "authentication.kubernetes.io/cluster-name"

// TokenReview types — lightweight replacements for k8s.io/api/authentication/v1.
// Only the fields we need for JSON serialization.

type TokenReviewRequest struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Spec       TokenReviewSpec `json:"spec"`
}

type TokenReviewSpec struct {
	Token string `json:"token"`
}

type TokenReviewResponse struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Status     TokenReviewStatus `json:"status"`
}

type TokenReviewStatus struct {
	Authenticated bool     `json:"authenticated"`
	User          UserInfo `json:"user,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type UserInfo struct {
	Username string              `json:"username"`
	Groups   []string            `json:"groups,omitempty"`
	Extra    map[string][]string `json:"extra,omitempty"`
}

// TokenReviewer validates tokens via the TokenReview API.
type TokenReviewer interface {
	Review(ctx context.Context, token string) (*TokenReviewResponse, error)
}

// HTTPTokenReviewer calls the TokenReview API using a plain HTTP client.
type HTTPTokenReviewer struct {
	url        string // full URL to POST TokenReview requests
	client     *http.Client
	bearerToken string // optional: for authenticating to the API server
}

func (r *HTTPTokenReviewer) Review(ctx context.Context, token string) (*TokenReviewResponse, error) {
	reqBody := TokenReviewRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
		Spec:       TokenReviewSpec{Token: token},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling token review request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearerToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling token review: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("token review returned status %d", resp.StatusCode)
	}

	var result TokenReviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding token review response: %w", err)
	}

	return &result, nil
}

package proxy

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// TokenReviewer validates tokens via the TokenReview API.
type TokenReviewer interface {
	Review(ctx context.Context, token string) (*authv1.TokenReview, error)
}

// KubeTokenReviewer calls the TokenReview API using a Kubernetes client.
// Works with any endpoint that speaks the TokenReview API:
// - Kubernetes API server (in-cluster or via kubeconfig)
// - kube-federated-auth (via --token-review-url)
type KubeTokenReviewer struct {
	client kubernetes.Interface
}

// NewKubeTokenReviewer creates a TokenReviewer from a rest.Config.
func NewKubeTokenReviewer(config *rest.Config) (*KubeTokenReviewer, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return &KubeTokenReviewer{client: clientset}, nil
}

func (c *KubeTokenReviewer) Review(ctx context.Context, token string) (*authv1.TokenReview, error) {
	tr := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: token,
		},
	}

	result, err := c.client.AuthenticationV1().TokenReviews().Create(ctx, tr, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("calling token review: %w", err)
	}

	result.APIVersion = "authentication.k8s.io/v1"
	result.Kind = "TokenReview"

	return result, nil
}

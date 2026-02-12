package proxy

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestKubeTokenReviewer_Authenticated(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenReview{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "authentication.k8s.io/v1",
				Kind:       "TokenReview",
			},
			Status: authv1.TokenReviewStatus{
				Authenticated: true,
				User: authv1.UserInfo{
					Username: "system:serviceaccount:default:test",
					Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:default"},
				},
			},
		}, nil
	})

	reviewer := &KubeTokenReviewer{client: client}
	result, err := reviewer.Review(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Status.Authenticated {
		t.Error("expected authenticated = true")
	}
	if result.Status.User.Username != "system:serviceaccount:default:test" {
		t.Errorf("username = %q, want %q", result.Status.User.Username, "system:serviceaccount:default:test")
	}
}

func TestKubeTokenReviewer_Unauthenticated(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenReview{
			Status: authv1.TokenReviewStatus{
				Authenticated: false,
				Error:         "token not valid",
			},
		}, nil
	})

	reviewer := &KubeTokenReviewer{client: client}
	result, err := reviewer.Review(context.Background(), "bad-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status.Authenticated {
		t.Error("expected authenticated = false")
	}
	if result.Status.Error != "token not valid" {
		t.Errorf("error = %q, want %q", result.Status.Error, "token not valid")
	}
}

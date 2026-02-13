package credentials

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/rophy/kube-federated-auth/internal/config"
)

// makeJWT creates a minimal JWT with the given claims for testing.
func makeJWT(sub string, exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	claims := map[string]interface{}{
		"sub": sub,
		"exp": exp.Unix(),
	}
	claimsJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	return header + "." + payload + ".sig"
}

// fakeClientFactory returns a clientFactory that always returns the given fake client.
func fakeClientFactory(client *kubefake.Clientset) clientFactory {
	return func(_ config.ClusterConfig, _ *Credentials) (kubernetes.Interface, error) {
		return client, nil
	}
}

func generateCACert(notBefore, notAfter time.Time) []byte {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		IsCA:         true,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

// --- CA cert expiration tests ---

func TestCheckCACertExpiration_WarnsWhenExpiringSoon(t *testing.T) {
	// 10-year cert with 30 days remaining → 20% threshold is 2 years → should warn
	notBefore := time.Now().Add(-10*365*24*time.Hour + 30*24*time.Hour)
	cert := generateCACert(notBefore, time.Now().Add(30*24*time.Hour))

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	checkCACertExpiration("test-cluster", cert)

	output := buf.String()
	if !strings.Contains(output, "WARNING") {
		t.Errorf("expected warning log, got: %s", output)
	}
	if !strings.Contains(output, "test-cluster") {
		t.Errorf("expected cluster name in log, got: %s", output)
	}
}

func TestCheckCACertExpiration_NoWarningWhenFarFromExpiry(t *testing.T) {
	// 10-year cert issued now → 20% threshold is 2 years, remaining ~10 years → no warning
	cert := generateCACert(time.Now(), time.Now().Add(10*365*24*time.Hour))

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	checkCACertExpiration("test-cluster", cert)

	if buf.Len() > 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}

func TestCheckCACertExpiration_InvalidPEM(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	checkCACertExpiration("test-cluster", []byte("not a pem"))

	output := buf.String()
	if !strings.Contains(output, "failed to decode") {
		t.Errorf("expected decode error log, got: %s", output)
	}
}

func TestCheckCACertExpiration_EmptyCert(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	checkCACertExpiration("test-cluster", nil)

	if buf.Len() > 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}

// --- Token renewal tests ---

func setupFakeClient(t *testing.T, responseToken string) *kubefake.Clientset {
	t.Helper()
	fakeClient := kubefake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "serviceaccounts/token", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenRequest{
			Status: authv1.TokenRequestStatus{
				Token:               responseToken,
				ExpirationTimestamp: metav1.NewTime(time.Now().Add(168 * time.Hour)),
			},
		}, nil
	})
	return fakeClient
}

func setupFailingClient(t *testing.T) *kubefake.Clientset {
	t.Helper()
	fakeClient := kubefake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "serviceaccounts/token", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("Unauthorized")
	})
	return fakeClient
}

func defaultConfig() *config.Config {
	return &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"cluster-b": {
				Issuer:    "https://kubernetes.default.svc.cluster.local",
				APIServer: "https://10.0.0.1:6443",
			},
		},
	}
}

func TestRenew_SuccessWithValidToken(t *testing.T) {
	storedToken := makeJWT("system:serviceaccount:kube-federated-auth:reader", time.Now().Add(1*time.Hour))
	renewedToken := makeJWT("system:serviceaccount:kube-federated-auth:reader", time.Now().Add(168*time.Hour))

	store := newTestStore()
	store.credentials["cluster-b"] = &Credentials{Token: storedToken, CACert: []byte("ca")}

	fakeClient := setupFakeClient(t, renewedToken)
	cfg := defaultConfig()

	r := NewRenewer(cfg, store, nil)
	r.clientFactory = fakeClientFactory(fakeClient)

	// Force renew_before to be very large so renewal is triggered
	cfg.Renewal = &config.RenewalSettings{RenewBefore: 8760 * time.Hour}

	err := r.renew(context.Background(), "cluster-b", cfg.Clusters["cluster-b"])
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	creds, _ := store.Get("cluster-b")
	if creds.Token != renewedToken {
		t.Errorf("expected renewed token, got: %s", creds.Token[:50])
	}
}

func TestRenew_SkipsWhenTokenNotExpiring(t *testing.T) {
	// Token expires in 168h, renew_before defaults to 48h → should skip
	storedToken := makeJWT("system:serviceaccount:kube-federated-auth:reader", time.Now().Add(168*time.Hour))

	store := newTestStore()
	store.credentials["cluster-b"] = &Credentials{Token: storedToken, CACert: []byte("ca")}

	fakeClient := setupFakeClient(t, "should-not-be-used")
	cfg := defaultConfig()

	r := NewRenewer(cfg, store, nil)
	r.clientFactory = fakeClientFactory(fakeClient)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	err := r.renew(context.Background(), "cluster-b", cfg.Clusters["cluster-b"])
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buf.String(), "Skipping renewal") {
		t.Errorf("expected 'Skipping renewal' log, got: %s", buf.String())
	}

	// Token should NOT have been replaced
	creds, _ := store.Get("cluster-b")
	if creds.Token != storedToken {
		t.Error("token should not have been replaced")
	}
}

func TestRenew_FallsBackToBootstrapOnFailure(t *testing.T) {
	// Stored token is invalid, bootstrap file has a valid token
	invalidToken := "invalid.token.here"
	bootstrapToken := makeJWT("system:serviceaccount:kube-federated-auth:reader", time.Now().Add(1*time.Hour))
	renewedToken := makeJWT("system:serviceaccount:kube-federated-auth:reader", time.Now().Add(168*time.Hour))

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	caPath := filepath.Join(dir, "ca.crt")
	os.WriteFile(tokenPath, []byte(bootstrapToken), 0644)
	os.WriteFile(caPath, []byte("ca-data"), 0644)

	store := newTestStore()
	store.credentials["cluster-b"] = &Credentials{Token: invalidToken, CACert: []byte("ca")}

	// The stored token is invalid, so requestNewToken fails at parseServiceAccountFromToken
	// before the client is ever called. The bootstrap token is valid, so the retry succeeds.
	fakeClient := setupFakeClient(t, renewedToken)

	cfg := defaultConfig()
	cfg.Clusters["cluster-b"] = config.ClusterConfig{
		Issuer:    "https://kubernetes.default.svc.cluster.local",
		APIServer: "https://10.0.0.1:6443",
		TokenPath: tokenPath,
		CACert:    caPath,
	}
	cfg.Renewal = &config.RenewalSettings{RenewBefore: 8760 * time.Hour}

	r := NewRenewer(cfg, store, nil)
	r.clientFactory = fakeClientFactory(fakeClient)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	err := r.renew(context.Background(), "cluster-b", cfg.Clusters["cluster-b"])
	if err != nil {
		t.Fatalf("expected successful fallback, got error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "retrying with bootstrap credentials") {
		t.Errorf("expected bootstrap fallback log, got: %s", output)
	}

	creds, _ := store.Get("cluster-b")
	if creds.Token != renewedToken {
		t.Error("expected renewed token from bootstrap fallback")
	}
}

func TestRenew_BothStoredAndBootstrapFail(t *testing.T) {
	invalidToken := "invalid.token.here"
	bootstrapToken := makeJWT("system:serviceaccount:kube-federated-auth:reader", time.Now().Add(1*time.Hour))

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	caPath := filepath.Join(dir, "ca.crt")
	os.WriteFile(tokenPath, []byte(bootstrapToken), 0644)
	os.WriteFile(caPath, []byte("ca-data"), 0644)

	store := newTestStore()
	store.credentials["cluster-b"] = &Credentials{Token: invalidToken, CACert: []byte("ca")}

	fakeClient := setupFailingClient(t) // always fails

	cfg := defaultConfig()
	cfg.Clusters["cluster-b"] = config.ClusterConfig{
		Issuer:    "https://kubernetes.default.svc.cluster.local",
		APIServer: "https://10.0.0.1:6443",
		TokenPath: tokenPath,
		CACert:    caPath,
	}
	cfg.Renewal = &config.RenewalSettings{RenewBefore: 8760 * time.Hour}

	r := NewRenewer(cfg, store, nil)
	r.clientFactory = fakeClientFactory(fakeClient)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	err := r.renew(context.Background(), "cluster-b", cfg.Clusters["cluster-b"])
	if err == nil {
		t.Fatal("expected error when both stored and bootstrap tokens fail")
	}

	output := buf.String()
	if !strings.Contains(output, "bootstrap token at "+tokenPath+" is invalid or expired") {
		t.Errorf("expected 'bootstrap token is invalid or expired' log, got: %s", output)
	}
	if !strings.Contains(output, "Please mount a new bootstrap token at "+tokenPath) {
		t.Errorf("expected 'Please mount a new bootstrap token' log, got: %s", output)
	}
}

func TestRenew_NoTokenPathConfigured(t *testing.T) {
	invalidToken := "invalid.token.here"

	store := newTestStore()
	store.credentials["cluster-b"] = &Credentials{Token: invalidToken, CACert: []byte("ca")}

	fakeClient := setupFailingClient(t)
	cfg := defaultConfig() // no TokenPath set
	cfg.Renewal = &config.RenewalSettings{RenewBefore: 8760 * time.Hour}

	r := NewRenewer(cfg, store, nil)
	r.clientFactory = fakeClientFactory(fakeClient)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	err := r.renew(context.Background(), "cluster-b", cfg.Clusters["cluster-b"])
	if err == nil {
		t.Fatal("expected error when no token_path configured")
	}

	output := buf.String()
	if !strings.Contains(output, "Please mount a new bootstrap token and set token_path") {
		t.Errorf("expected 'set token_path' log, got: %s", output)
	}
}

func TestRenew_BootstrapFileNotReadable(t *testing.T) {
	invalidToken := "invalid.token.here"

	store := newTestStore()
	store.credentials["cluster-b"] = &Credentials{Token: invalidToken, CACert: []byte("ca")}

	fakeClient := setupFailingClient(t)
	cfg := defaultConfig()
	cfg.Clusters["cluster-b"] = config.ClusterConfig{
		Issuer:    "https://kubernetes.default.svc.cluster.local",
		APIServer: "https://10.0.0.1:6443",
		TokenPath: "/nonexistent/token",
		CACert:    "/nonexistent/ca.crt",
	}
	cfg.Renewal = &config.RenewalSettings{RenewBefore: 8760 * time.Hour}

	r := NewRenewer(cfg, store, nil)
	r.clientFactory = fakeClientFactory(fakeClient)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	err := r.renew(context.Background(), "cluster-b", cfg.Clusters["cluster-b"])
	if err == nil {
		t.Fatal("expected error when bootstrap file not readable")
	}

	output := buf.String()
	if !strings.Contains(output, "failed to read bootstrap token from /nonexistent/token") {
		t.Errorf("expected 'failed to read' log, got: %s", output)
	}
}

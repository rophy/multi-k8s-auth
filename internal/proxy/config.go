package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
)

const (
	inClusterTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	inClusterCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	tokenReviewPath    = "/apis/authentication.k8s.io/v1/tokenreviews"
)

type Config struct {
	TokenReviewURL string
	Upstream       string
	Port           int
	AuthPrefix     string
}

func ParseFlags() *Config {
	cfg := &Config{}
	flag.StringVar(&cfg.TokenReviewURL, "token-review-url", getEnv("TOKEN_REVIEW_URL", ""), "URL of TokenReview endpoint (default: in-cluster Kubernetes API)")
	flag.StringVar(&cfg.Upstream, "upstream", getEnv("UPSTREAM", ""), "upstream URL for reverse proxy mode")
	flag.IntVar(&cfg.Port, "port", getEnvInt("PORT", 4180), "listen port")
	flag.StringVar(&cfg.AuthPrefix, "auth-prefix", getEnv("AUTH_PREFIX", "/auth"), "path for auth subrequest endpoint")
	flag.Parse()
	return cfg
}

func (c *Config) Validate() error {
	return nil
}

// NewTokenReviewer creates a TokenReviewer based on the configuration.
// If TokenReviewURL is set, it targets that URL with plain HTTP.
// Otherwise, it uses in-cluster Kubernetes API with TLS and bearer token.
func (c *Config) NewTokenReviewer() (*HTTPTokenReviewer, error) {
	if c.TokenReviewURL != "" {
		return &HTTPTokenReviewer{
			url:    c.TokenReviewURL + tokenReviewPath,
			client: http.DefaultClient,
		}, nil
	}

	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("not running in-cluster: KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT not set (use --token-review-url for out-of-cluster usage)")
	}

	token, err := os.ReadFile(inClusterTokenFile)
	if err != nil {
		return nil, fmt.Errorf("reading service account token: %w", err)
	}

	caCert, err := os.ReadFile(inClusterCAFile)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate from %s", inClusterCAFile)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}

	apiServerURL := "https://" + net.JoinHostPort(host, port)
	return &HTTPTokenReviewer{
		url:         apiServerURL + tokenReviewPath,
		client:      client,
		bearerToken: string(token),
	}, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

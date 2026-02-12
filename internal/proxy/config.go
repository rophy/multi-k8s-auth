package proxy

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"k8s.io/client-go/rest"
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

// RESTConfig builds a rest.Config based on the configuration.
// If TokenReviewURL is set, it targets that URL with JSON content type.
// Otherwise, it uses in-cluster Kubernetes config.
func (c *Config) RESTConfig() (*rest.Config, error) {
	if c.TokenReviewURL != "" {
		return &rest.Config{
			Host: c.TokenReviewURL,
			ContentConfig: rest.ContentConfig{
				ContentType: "application/json",
			},
		}, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w (set --token-review-url for out-of-cluster usage)", err)
	}
	return config, nil
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

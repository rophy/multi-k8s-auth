package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/rophy/kube-federated-auth/internal/proxy"
)

var Version = "dev"

func main() {
	cfg := proxy.ParseFlags()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	log.Printf("kube-auth-proxy version %s", Version)
	if cfg.TokenReviewURL != "" {
		log.Printf("TokenReview endpoint: %s", cfg.TokenReviewURL)
	} else {
		log.Printf("TokenReview endpoint: in-cluster Kubernetes API")
	}
	if cfg.Upstream != "" {
		log.Printf("Reverse proxy mode: upstream=%s", cfg.Upstream)
	} else {
		log.Printf("Auth subrequest mode only")
	}

	reviewer, err := cfg.NewTokenReviewer()
	if err != nil {
		log.Fatalf("Failed to create token reviewer: %v", err)
	}

	handler, err := proxy.NewServer(cfg, reviewer, Version)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

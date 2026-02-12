package proxy

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewServer(cfg *Config, reviewer TokenReviewer, version string) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/healthz", NewHealthHandler(version).ServeHTTP)
	r.Get(cfg.AuthPrefix, NewAuthHandler(reviewer).ServeHTTP)

	if cfg.Upstream != "" {
		rp, err := NewReverseProxyHandler(reviewer, cfg.Upstream)
		if err != nil {
			return nil, err
		}
		r.Handle("/*", rp)
	}

	return r, nil
}

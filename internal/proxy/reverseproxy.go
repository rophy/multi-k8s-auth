package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type ReverseProxyHandler struct {
	reviewer TokenReviewer
	proxy    *httputil.ReverseProxy
}

func NewReverseProxyHandler(reviewer TokenReviewer, upstreamURL string) (*ReverseProxyHandler, error) {
	target, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("parsing upstream URL: %w", err)
	}

	return &ReverseProxyHandler{
		reviewer: reviewer,
		proxy:    httputil.NewSingleHostReverseProxy(target),
	}, nil
}

func (h *ReverseProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	result, err := h.reviewer.Review(r.Context(), token)
	if err != nil {
		log.Printf("TokenReview error: %v", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if !result.Status.Authenticated {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	r.Header.Set(HeaderForwardedUser, result.Status.User.Username)
	if len(result.Status.User.Groups) > 0 {
		r.Header.Set(HeaderForwardedGroups, strings.Join(result.Status.User.Groups, ","))
	}
	if clusterNames, ok := result.Status.User.Extra[ExtraKeyClusterName]; ok && len(clusterNames) > 0 {
		r.Header.Set(HeaderForwardedExtraCluster, clusterNames[0])
	}

	r.Header.Del("Authorization")

	h.proxy.ServeHTTP(w, r)
}

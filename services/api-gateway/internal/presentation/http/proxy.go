package http

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewReverseProxy routes requests to target microservices while preserving tracing and tenant headers.
func NewReverseProxy(targetURL *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}

	return proxy
}
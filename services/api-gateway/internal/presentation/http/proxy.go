package http

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	appIdentity "marketmind/services/api-gateway/internal/application/identity"
	domainIdentity "marketmind/services/api-gateway/internal/domain/identity"
)

// NewReverseProxy routes requests to target microservices while preserving tracing and tenant headers.
func NewReverseProxy(targetURL *url.URL, logger *slog.Logger) http.Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 5 * time.Second
	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(targetURL)
			request.Out.Host = targetURL.Host
			request.SetXForwarded()
			request.Out.Header.Del("Forwarded")
			// The sample has no service credentials; do not leak the API bearer token.
			request.Out.Header.Del("Authorization")
			claims, _ := request.In.Context().Value(UserClaimsKey).(*domainIdentity.UserClaims)
			// Rewrite runs after hop-by-hop stripping, so Connection cannot remove
			// the trusted identity/tracing headers we send to the upstream.
			propagateIdentity(request.Out.Header, claims)
			for key, header := range map[contextKey]string{RequestIDKey: RequestIDHeader, CorrelationIDKey: CorrelationIDHeader} {
				if value, ok := request.In.Context().Value(key).(string); ok {
					request.Out.Header.Set(header, value)
				}
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.ErrorContext(r.Context(), "downstream request failed",
				slog.Any("error", err), slog.String("request_id", r.Header.Get(RequestIDHeader)))
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "downstream service unavailable"})
		},
	}
}

// RegisterProxyRoutes is used by the executable and the integration tests. Every
// registered proxy path has authentication and a method-specific permission gate.
func RegisterProxyRoutes(mux *http.ServeMux, useCase *appIdentity.UseCase, target *url.URL, logger *slog.Logger) {
	authenticate := AuthenticateMiddleware(useCase)
	proxy := NewReverseProxy(target, logger)
	sample := http.StripPrefix("/api/v1/sample", proxy)
	readSample := requireMethodPermissions(map[string]string{
		http.MethodGet: domainIdentity.PermissionSampleRead, http.MethodHead: domainIdentity.PermissionSampleRead,
	}, sample)
	for _, path := range []string{"/api/v1/sample/health", "/api/v1/sample/health/live", "/api/v1/sample/health/ready"} {
		mux.Handle(path, authenticate(readSample))
	}
	market := requireMethodPermissions(map[string]string{
		http.MethodGet: domainIdentity.PermissionMarketRead, http.MethodHead: domainIdentity.PermissionMarketRead,
	}, proxy)
	research := requireMethodPermissions(map[string]string{
		http.MethodGet: domainIdentity.PermissionResearchRead, http.MethodHead: domainIdentity.PermissionResearchRead,
		http.MethodPost: domainIdentity.PermissionResearchWrite,
	}, proxy)
	// The root paths are also protected instead of exposing ServeMux redirects.
	mux.Handle("/api/v1/market", authenticate(market))
	mux.Handle("/api/v1/market/", authenticate(market))
	mux.Handle("/api/v1/research", authenticate(research))
	mux.Handle("/api/v1/research/", authenticate(research))
}

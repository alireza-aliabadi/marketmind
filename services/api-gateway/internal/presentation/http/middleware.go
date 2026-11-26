package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const (
	RequestIDHeader                = "X-Request-ID"
	CorrelationIDHeader            = "X-Correlation-ID"
	RequestIDKey        contextKey = "request_id"
	CorrelationIDKey    contextKey = "correlation_id"
)

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-id"
	}
	return hex.EncodeToString(bytes)
}

func validTracingID(value string) bool {
	return value != "" && len(value) <= 128 && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r))
	}) == -1
}

// TracingMiddleware injects request ID and correlation ID into headers and context.
func TracingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			reqID := r.Header.Get(RequestIDHeader)
			if len(r.Header.Values(RequestIDHeader)) != 1 || !validTracingID(reqID) {
				reqID = generateID()
			}

			corrID := r.Header.Get(CorrelationIDHeader)
			if len(r.Header.Values(CorrelationIDHeader)) != 1 || !validTracingID(corrID) {
				corrID = reqID
			}

			w.Header().Set(RequestIDHeader, reqID)
			w.Header().Set(CorrelationIDHeader, corrID)

			ctx := context.WithValue(r.Context(), RequestIDKey, reqID)
			ctx = context.WithValue(ctx, CorrelationIDKey, corrID)

			reqWithCtx := r.Clone(ctx)
			reqWithCtx.Header.Set(RequestIDHeader, reqID)
			reqWithCtx.Header.Set(CorrelationIDHeader, corrID)

			logger.InfoContext(ctx, "incoming HTTP request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("request_id", reqID),
				slog.String("correlation_id", corrID),
			)

			next.ServeHTTP(w, reqWithCtx)

			logger.InfoContext(ctx, "completed HTTP request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("request_id", reqID),
				slog.Duration("duration_ms", time.Since(start)),
			)
		})
	}
}

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	appHealth "marketmind/services/api-gateway/internal/application/health"
	appIdentity "marketmind/services/api-gateway/internal/application/identity"
	infraAuth "marketmind/services/api-gateway/internal/infrastructure/auth"
	infraHealth "marketmind/services/api-gateway/internal/infrastructure/health"
	presHTTP "marketmind/services/api-gateway/internal/presentation/http"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	appName := getEnv("APP_NAME", "marketmind-gateway")
	appEnv := getEnv("APP_ENV", "local")
	port := getEnv("PORT", "8080")
	jwtSecret := getEnv("JWT_SECRET", "marketmind-dev-jwt-secret-change-in-prod-32b")
	downstreamServiceURL := getEnv("DOWNSTREAM_URL", "http://localhost:8000")
	version := "0.1.0"

	var logHandler slog.Handler
	if appEnv == "prod" || appEnv == "production" {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	logger := slog.New(logHandler)

	logger.Info("starting API gateway",
		slog.String("service", appName),
		slog.String("env", appEnv),
		slog.String("version", version),
		slog.String("port", port),
	)

	// Composition Root
	checker := infraHealth.NewSystemChecker(appName, version, appEnv)
	healthUseCase := appHealth.NewUseCase(checker)
	healthHandler := presHTTP.NewHandler(healthUseCase)

	jwtVerifier := infraAuth.NewJWTVerifier(jwtSecret)
	identityUseCase := appIdentity.NewUseCase(jwtVerifier)
	authMiddleware := presHTTP.AuthenticateMiddleware(identityUseCase)

	mux := http.NewServeMux()

	// 1. Unauthenticated routes (Health / Liveness)
	healthHandler.RegisterRoutes(mux)

	// 2. Protected downstream reverse proxy
	targetURL, err := url.Parse(downstreamServiceURL)
	if err != nil {
		logger.Error("invalid downstream target URL", slog.Any("error", err))
		os.Exit(1)
	}
	proxyHandler := presHTTP.NewReverseProxy(targetURL)
	mux.Handle("/api/v1/research/", authMiddleware(proxyHandler))
	mux.Handle("/api/v1/market/", authMiddleware(proxyHandler))

	// Tracing middleware wraps entire mux
	tracedHandler := presHTTP.TracingMiddleware(logger)(mux)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      tracedHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("gateway listening", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server fatal error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down gateway...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	} else {
		logger.Info("gateway exited cleanly")
	}
}
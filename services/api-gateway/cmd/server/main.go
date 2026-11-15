package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	appHealth "marketmind/services/api-gateway/internal/application/health"
	appIdentity "marketmind/services/api-gateway/internal/application/identity"
	infraAuth "marketmind/services/api-gateway/internal/infrastructure/auth"
	infraConfig "marketmind/services/api-gateway/internal/infrastructure/config"
	infraHealth "marketmind/services/api-gateway/internal/infrastructure/health"
	presHTTP "marketmind/services/api-gateway/internal/presentation/http"
)

func main() {
	config, err := infraConfig.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid gateway configuration", slog.Any("error", err))
		os.Exit(1)
	}
	appName, appEnv := config.AppName, config.AppEnv
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
		slog.String("address", config.Address),
	)

	// Composition Root
	checker := infraHealth.NewSystemChecker(appName, version, appEnv)
	healthUseCase := appHealth.NewUseCase(checker)
	healthHandler := presHTTP.NewHandler(healthUseCase)

	discoveryContext, cancelDiscovery := context.WithTimeout(context.Background(), 10*time.Second)
	verifier, err := infraAuth.NewOIDCVerifier(discoveryContext, config.OIDC)
	cancelDiscovery()
	if err != nil {
		logger.Error("cannot initialize OIDC authentication", slog.Any("error", err))
		os.Exit(1)
	}
	identityUseCase := appIdentity.NewUseCase(verifier)

	mux := http.NewServeMux()

	// 1. Unauthenticated routes (Health / Liveness)
	healthHandler.RegisterRoutes(mux)

	// 2. Protected downstream reverse proxy
	presHTTP.RegisterProxyRoutes(mux, identityUseCase, config.DownstreamURL, logger)

	// Tracing middleware wraps entire mux
	tracedHandler := presHTTP.TracingMiddleware(logger)(mux)

	server := &http.Server{
		Addr:              config.Address,
		Handler:           tracedHandler,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

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

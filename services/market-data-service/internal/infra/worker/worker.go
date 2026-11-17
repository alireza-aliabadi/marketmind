// Package worker supervises private ingestion roles and exposes aggregate
// operational health only. It is not a public business API or AI tool surface.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	app "marketmind/services/market-data-service/internal/application/ingestion"
	"marketmind/services/market-data-service/internal/infra/events"
	"marketmind/services/market-data-service/internal/infra/kafka"
	"marketmind/services/market-data-service/internal/infra/postgres"
)

type Config struct {
	Role, Group, Address string
	OperationTimeout     time.Duration
	Policy               app.SourcePolicy
}
type State struct {
	Ready                     atomic.Bool
	Succeeded, Failed         atomic.Int64
	Pending, Parked, OpenRuns atomic.Int64
	OldestSeconds             atomic.Int64
}

func (s *State) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !s.Ready.Load() {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"status":"not_ready"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		ready := 0
		if s.Ready.Load() {
			ready = 1
		}
		_, _ = fmt.Fprintf(w, "market_ingestion_ready %d\nmarket_ingestion_cycles_total %d\nmarket_ingestion_failures_total %d\nmarket_outbox_pending %d\nmarket_outbox_parked %d\nmarket_outbox_oldest_seconds %d\nmarket_provider_runs_open %d\n", ready, s.Succeeded.Load(), s.Failed.Load(), s.Pending.Load(), s.Parked.Load(), s.OldestSeconds.Load(), s.OpenRuns.Load())
	})
	return mux
}

func Run(parent context.Context, cfg Config, repository *postgres.Repository, client *kafka.Client) error {
	if cfg.Role != "relay" && cfg.Role != "consumer" {
		return fmt.Errorf("worker role must be relay or consumer")
	}
	if cfg.OperationTimeout <= 0 || cfg.OperationTimeout > time.Minute {
		return fmt.Errorf("worker operation deadline must be 1ns..1m")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	state := &State{}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("operational listener unavailable")
	}
	server := &http.Server{Handler: state.Handler(), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 4096}
	serverErrors := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer stop()
		_ = server.Shutdown(cleanup)
	}()
	var next func(context.Context) error
	if cfg.Role == "consumer" {
		consumer, err := client.Consumer(cfg.Group, app.Processor{Repository: repository, Decoder: events.Codec{}, Retry: app.DefaultRetryPolicy(), Policy: cfg.Policy})
		if err != nil {
			return err
		}
		defer consumer.Close()
		next = consumer.Next
	} else {
		next = func(ctx context.Context) error {
			_, err := (app.Relay{Repository: repository, Publisher: client, Retry: app.DefaultRetryPolicy()}).Drain(ctx, 100)
			return err
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		delay := 500 * time.Millisecond
		for ctx.Err() == nil {
			operation, stop := context.WithTimeout(ctx, cfg.OperationTimeout)
			err := next(operation)
			stop()
			if ctx.Err() != nil {
				return
			}
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				state.Failed.Add(1)
				state.Ready.Store(false)
				slog.Error("ingestion worker cycle failed", "role", cfg.Role, "error_class", "operation_failed")
				delay = min(delay*2, 15*time.Second)
			} else {
				if err == nil {
					state.Succeeded.Add(1)
				}
				delay = 500 * time.Millisecond
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	check := func() {
		probe, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		if policy, ok := cfg.Policy.(interface{ Ready() error }); ok && policy.Ready() != nil {
			state.Ready.Store(false)
			return
		}
		if repository.Ping(probe) != nil || client.Check(probe) != nil {
			state.Ready.Store(false)
			return
		}
		backlog, err := repository.Backlog(probe)
		if err != nil {
			state.Ready.Store(false)
			return
		}
		state.Pending.Store(backlog.Pending)
		state.Parked.Store(backlog.Failed)
		state.OpenRuns.Store(backlog.OpenRuns)
		state.OldestSeconds.Store(int64(backlog.OldestSeconds))
		state.Ready.Store(true)
	}
	check()
	for {
		select {
		case <-parent.Done():
			state.Ready.Store(false)
			cancel()
			<-done
			return nil
		case err := <-serverErrors:
			cancel()
			<-done
			return fmt.Errorf("operational server failed: %w", err)
		case <-ticker.C:
			check()
		}
	}
}

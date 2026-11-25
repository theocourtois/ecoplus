package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/cloud/hetzner"
	"github.com/theocourtois/hetzner-ecoplus/internal/config"
	"github.com/theocourtois/hetzner-ecoplus/internal/engine"
	"github.com/theocourtois/hetzner-ecoplus/internal/metrics"
	"github.com/theocourtois/hetzner-ecoplus/internal/server"
	"k8s.io/utils/clock"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("ecoplus-server failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	clk := clock.RealClock{}
	cloudClient := hetzner.New(hetzner.Options{
		Token: cfg.HCloudToken, Clock: clk, Version: version,
		Limit: hetzner.LimitConfig{Rate: cfg.APIRate, Burst: cfg.APIBurst, LowWatermark: cfg.LowWatermark},
	})
	source := &lateSource{}
	meter := metrics.New(source, cloudClient.Remaining)
	eng := engine.New(engine.Options{
		Cloud: cloudClient, Clock: clk, Fleet: cfg.Fleet, Workers: cfg.Workers, Resync: cfg.Resync, Logger: logger,
		LeaseAlert: cfg.LeaseAlert, Observer: meter,
	})
	source.eng = eng
	guard := engine.NewGuard(engine.GuardOptions{
		Store: cloudClient, Clock: clk, InstanceID: cfg.InstanceID, Period: cfg.Heartbeat, Halt: eng.Halt, Logger: logger,
	})
	if err := guard.Check(ctx); err != nil {
		return fmt.Errorf("single-writer check: %w", err)
	}
	if err := eng.Start(ctx); err != nil {
		return err
	}
	defer eng.Stop()
	go guard.Run(ctx)
	srv := server.New(server.Options{Engine: eng, Token: cfg.Token, Logger: logger})
	return serve(ctx, logger, cfg, srv, meter.Handler())
}

type lateSource struct {
	eng *engine.Engine
}

func (l *lateSource) Stats() engine.Stats {
	if l.eng == nil {
		return engine.Stats{}
	}
	return l.eng.Stats()
}

func serve(ctx context.Context, logger *slog.Logger, cfg config.Config, srv *server.Server, metricsHandler http.Handler) error {
	apiServer := &http.Server{Addr: cfg.Addr, Handler: srv.APIHandler(), ReadHeaderTimeout: 10 * time.Second}
	adminServer := &http.Server{Addr: cfg.AdminAddr, Handler: srv.AdminHandler(metricsHandler), ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 2)
	for _, s := range []*http.Server{apiServer, adminServer} {
		go func() {
			logger.Info("listening", "addr", s.Addr)
			errs <- s.ListenAndServe()
		}()
	}
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var failures []error
	for _, s := range []*http.Server{apiServer, adminServer} {
		if err := s.Shutdown(shutdownCtx); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

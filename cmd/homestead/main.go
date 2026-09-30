// Command homestead runs the household budget and property management web app
// with automatic bank sync via Enable Banking.
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

	"github.com/AQUI74S/homestead/internal/api"
	"github.com/AQUI74S/homestead/internal/config"
	"github.com/AQUI74S/homestead/internal/demo"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
	"github.com/AQUI74S/homestead/internal/syncer"
	"github.com/AQUI74S/homestead/web"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("aborted", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.DB.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	var provider eb.Provider
	if cfg.Demo {
		log.Warn("demo mode: simulated bank, no real account data")
		provider = eb.NewDemo()
	} else {
		c, err := eb.NewClient(cfg.EBAPIBase, cfg.EBAppID, cfg.EBKeyPath)
		if err != nil {
			return err
		}
		provider = c
	}
	if cfg.Password == "" {
		log.Warn("HS_PASSWORD is empty: the web UI is reachable without login. Only run it like this locally!")
	}

	sy := syncer.New(st, provider, log)
	sy.Configure(cfg.BankDailyLimit, cfg.SyncInterval)
	if cfg.Demo {
		if err := demo.Seed(ctx, st, sy, log); err != nil {
			log.Error("could not create demo data", "err", err)
		}
	}
	go sy.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(cfg, st, provider, sy, log, web.FS).Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	log.Info("homestead running", "version", version, "addr", cfg.Addr, "public_url", cfg.PublicURL, "sync_interval", cfg.SyncInterval.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

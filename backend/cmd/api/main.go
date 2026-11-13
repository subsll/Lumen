// Lumen API: case management, evidence registration and chain of custody.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lumenforensics/lumen/internal/api"
	"github.com/lumenforensics/lumen/internal/vaultclient"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := api.Connect(ctx, env("DATABASE_URL", "postgres://lumen:lumen@localhost:5432/lumen"))
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	if err := api.Migrate(ctx, db); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}

	s := &api.Server{
		DB:           db,
		Vault:        vaultclient.New(env("VAULT_URL", "http://localhost:9000"), os.Getenv("VAULT_TOKEN")),
		CaptureURL:   env("CAPTURE_URL", "http://localhost:9100"),
		HTTP:         &http.Client{Timeout: 3 * time.Minute},
		AllowedHosts: map[string]bool{},
	}
	maxMeta, err := strconv.ParseInt(os.Getenv("METADATA_MAX_BYTES"), 10, 64)
	if err != nil || maxMeta <= 0 {
		maxMeta = 1 << 30
	}
	s.Metadata = api.NewMetadata(os.Getenv("METADATA_URL"), os.Getenv("METADATA_TOKEN"), maxMeta)
	go s.RunMetadataWorker(ctx)
	for _, h := range strings.Split(env("LUMEN_ALLOWED_HOSTS", "localhost,127.0.0.1,api"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			s.AllowedHosts[h] = true
		}
	}

	interval, err := time.ParseDuration(env("VERIFY_INTERVAL", "24h"))
	if err != nil || interval < time.Minute {
		interval = 24 * time.Hour
	}
	go func() {
		t := time.NewTicker(min(interval, time.Hour))
		defer t.Stop()
		for {
			if n, err := s.RunScheduledVerification(ctx, interval); err != nil {
				slog.Error("scheduled verification", "err", err)
			} else if n > 0 {
				slog.Info("scheduled verification", "evidence_checked", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	addr := env("LISTEN_ADDR", ":8080")
	srv := &http.Server{Addr: addr, Handler: s.Routes(), ReadHeaderTimeout: 15 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("lumen api listening", "addr", addr, "version", api.Version)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

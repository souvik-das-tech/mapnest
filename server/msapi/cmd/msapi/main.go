// Command msapi is the MapNest REST API, an admin layer over MapServer (see PLAN.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"msapi/internal/api"
	"msapi/internal/config"
	"msapi/internal/store"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	// "msapi healthcheck": Docker HEALTHCHECK without curl in the image.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "msapi:", err)
		os.Exit(1)
	}
}

// healthcheck asks the local API for /health. 200 (all OK) and 503
// (MapServer or a check degraded) both mean msapi itself is up.
func healthcheck() int {
	listen := os.Getenv("MSAPI_LISTEN")
	if listen == "" {
		listen = ":8081"
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 1
	}
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/api/v1/health?stores=false")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable {
		return 0
	}
	return 1
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	if v, err := st.SchemaVersion(ctx); err == nil {
		log.Info("database ready", "path", cfg.DBPath(), "schema_version", v)
	}

	apiSrv, err := api.New(cfg, st, log, version)
	if err != nil {
		return err
	}
	defer apiSrv.Close()

	// The database is the source of truth: regenerate everything MapServer
	// reads. A failure is logged, not fatal, so the API stays up to fix it.
	if err := apiSrv.Rebuild(ctx, "startup"); err != nil {
		log.Error("initial mapfile build failed", "error", err)
	}
	// Finish fast copies of non-COG rasters that were interrupted.
	apiSrv.ResumeRasterCache(ctx)

	// Timeouts against slow clients (slowloris). ReadTimeout covers the
	// largest upload (a 512 MB restore) on a slow link; there is no
	// WriteTimeout because backups stream and previews of big rasters can
	// take minutes. MaxHeaderBytes keeps headers small.
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	// Drop expired and idle sessions now and then (login also does it).
	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := st.PurgeSessions(ctx, now, cfg.SessionIdle); err != nil {
					log.Warn("purge sessions", "error", err)
				}
			}
		}
	}()

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "version", version,
			"session_hours", cfg.SessionTTL.Hours(), "session_idle_minutes", cfg.SessionIdle.Minutes(),
			"login_max_attempts", cfg.LoginMaxAttempts, "public_docs", cfg.PublicDocs)
		if cfg.InsecureDev {
			log.Warn("MSAPI_INSECURE_DEV is on: weak passwords and API keys are accepted. Never use this in production.")
		}
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}

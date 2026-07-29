// Command api is the FlowLens HTTP API server.
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

	"github.com/flowlens/api/internal/config"
	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/gitlab"
	apihttp "github.com/flowlens/api/internal/http"
	"github.com/flowlens/api/internal/issuesync"
	syncpkg "github.com/flowlens/api/internal/sync"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		return err
	}

	server, err := apihttp.NewServer(cfg, database.NewQuerier(pool), pool, database.NewTxRunner(pool), cipher)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Outbound issue sync job handlers (docs/plans/issue-sync.md,
	// "Outbound"); internal/task enqueues the jobs they execute.
	issueSync := issuesync.NewService(database.NewQuerier(pool), cipher, func(baseURL string) gitlab.Client { return gitlab.NewHTTPClient(baseURL) })
	registry := syncpkg.NewRegistry()
	registry.Register(issuesync.KindIssueCreate, issueSync.HandleIssueCreate)
	registry.Register(issuesync.KindIssueUpdate, issueSync.HandleIssueUpdate)
	registry.Register(issuesync.KindIssueClose, issueSync.HandleIssueClose)
	registry.Register(issuesync.KindIssueReopen, issueSync.HandleIssueReopen)
	worker := syncpkg.NewWorker(database.NewQuerier(pool), registry, syncpkg.WithPollInterval(cfg.SyncWorkerPollInterval))
	if cfg.SyncWorkerEnabled {
		go func() {
			slog.Info("sync worker starting", "poll_interval", cfg.SyncWorkerPollInterval)
			if err := worker.Run(context.Background()); err != nil {
				slog.Error("sync worker stopped", "error", err)
			}
		}()
	}

	// Start serving in a goroutine so we can wait for shutdown signals.
	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "port", cfg.Port, "env", cfg.Env)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cfg.SyncWorkerEnabled {
			if err := worker.Stop(shutdownCtx); err != nil {
				slog.Warn("sync worker did not stop cleanly", "error", err)
			}
		}
		return httpServer.Shutdown(shutdownCtx)
	}
}

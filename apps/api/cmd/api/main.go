// Command api is the FlowLens HTTP API server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flowlens/api/internal/commentsync"
	"github.com/flowlens/api/internal/config"
	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/gitlab"
	apihttp "github.com/flowlens/api/internal/http"
	"github.com/flowlens/api/internal/issuesync"
	"github.com/flowlens/api/internal/mrsync"
	"github.com/flowlens/api/internal/notification"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectsync"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/flowlens/api/internal/webhookapply"
	"github.com/flowlens/api/internal/webhookevent"
	"github.com/flowlens/api/migrations"
)

// version is stamped at link time (see the Makefile's -ldflags). "dev" is
// what an un-stamped local `go build` reports.
var version = "dev"

func main() {
	// Two subcommands run before any configuration is loaded, because they
	// exist precisely for an operator who has no configuration yet: the
	// self-hosting quickstart generates ENCRYPTION_KEY with `gen-key`
	// against this same image (docs/self-hosting.md), so requiring a
	// DATABASE_URL to produce a random key would be circular.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "gen-key":
			if err := genKey(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "gen-key:", err)
				os.Exit(1)
			}
			return
		case "version":
			fmt.Println(version)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q (known: gen-key, version)\n", os.Args[1])
			os.Exit(2)
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// genKey prints a ready-to-paste ENCRYPTION_KEY line: a fresh AES-256 key,
// base64-encoded the way config.Load expects to read it back.
func genKey(w io.Writer) error {
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "ENCRYPTION_KEY=%s\n", base64.StdEncoding.EncodeToString(key))
	return err
}

func run() error {
	cfg, err := config.Load(version)
	if err != nil {
		return err
	}
	slog.Info("flowlens api starting", "version", cfg.Version, "env", cfg.Env)

	// Bring the schema up before opening the pool the API serves from, so
	// no handler can observe a half-migrated database.
	if cfg.RunMigrations {
		if err := database.Migrate(cfg.DatabaseURL, migrations.FS); err != nil {
			return err
		}
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

	// One TLS policy, one transport, shared by the request-path clients
	// inside the server and the background sync workers below — otherwise a
	// connection could verify from the browser and fail in the worker.
	gitlabTLS := gitlab.TLSPolicy{
		CACertFile:         cfg.GitlabCACertFile,
		InsecureSkipVerify: cfg.GitlabTLSInsecureSkipVerify,
	}
	clientFactory, err := gitlab.NewClientFactory(gitlabTLS)
	if err != nil {
		return err
	}
	if !gitlabTLS.Verifying() {
		slog.Warn("gitlab TLS certificate verification is disabled",
			"hint", "set GITLAB_CA_CERT_FILE to trust a private CA, or GITLAB_TLS_INSECURE_SKIP_VERIFY=false, to verify certificates")
	}

	server, err := apihttp.NewServer(cfg, database.NewQuerier(pool), pool, database.NewTxRunner(pool), cipher, clientFactory)
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
	issueSync := issuesync.NewService(database.NewQuerier(pool), cipher, clientFactory)
	registry := syncpkg.NewRegistry()
	registry.Register(issuesync.KindIssueCreate, issueSync.HandleIssueCreate)
	registry.Register(issuesync.KindIssueUpdate, issueSync.HandleIssueUpdate)
	registry.Register(issuesync.KindIssueClose, issueSync.HandleIssueClose)
	registry.Register(issuesync.KindIssueReopen, issueSync.HandleIssueReopen)

	// Outbound comment sync job handler (#104): internal/taskcomment
	// enqueues the job it executes, pushing a task comment to its linked
	// GitLab issue as a note.
	commentSync := commentsync.NewService(database.NewQuerier(pool), cipher, clientFactory)
	registry.Register(commentsync.KindCommentCreate, commentSync.HandleCommentCreate)

	// project.import / project.resync job handlers (issue #25): initial
	// import (auto-enqueued by internal/linkedproject.Service.Create) and
	// manual re-sync (POST /linked-gitlab-projects/{linkID}/sync-runs).
	projectSync := projectsync.NewService(database.NewQuerier(pool), database.NewTxRunner(pool), project.NewService(database.NewQuerier(pool)), cipher, clientFactory)
	registry.Register(projectsync.KindProjectImport, projectSync.HandleImport)
	registry.Register(projectsync.KindProjectResync, projectSync.HandleResync)

	// mr.import / mr.resync job handlers (issue #111): read-only merge
	// request + pipeline sync, auto-enqueued by
	// internal/linkedproject.Service.Create alongside project.import.
	mrSync := mrsync.NewService(database.NewQuerier(pool), database.NewTxRunner(pool), cipher, clientFactory)
	registry.Register(mrsync.KindMRImport, mrSync.HandleImport)
	registry.Register(mrsync.KindMRResync, mrSync.HandleResync)

	worker := syncpkg.NewWorker(database.NewQuerier(pool), registry, syncpkg.WithPollInterval(cfg.SyncWorkerPollInterval))
	if cfg.SyncWorkerEnabled {
		go func() {
			slog.Info("sync worker starting", "poll_interval", cfg.SyncWorkerPollInterval)
			if err := worker.Run(context.Background()); err != nil {
				slog.Error("sync worker stopped", "error", err)
			}
		}()
	}

	// Inbound webhook apply pipeline (docs/plans/issue-sync.md, "Inbound"):
	// applies recorded webhook_events to tasks, guarded against stale, echo
	// and out-of-scope deliveries. Runs under the same enable flag as the
	// outbound worker above — together they are "the sync engine".
	webhookApply := webhookapply.NewService(database.NewTxRunner(pool))
	webhookWorker := webhookapply.NewWorker(webhookApply, webhookapply.WithPollInterval(cfg.SyncWorkerPollInterval))
	if cfg.SyncWorkerEnabled {
		go func() {
			slog.Info("webhook apply worker starting", "poll_interval", cfg.SyncWorkerPollInterval)
			if err := webhookWorker.Run(context.Background()); err != nil {
				slog.Error("webhook apply worker stopped", "error", err)
			}
		}()
	}

	// Webhook event retention cleanup (issue #26): sweeps old 'processed'
	// webhook_events rows on its own schedule, independent of the sync
	// engine's poll interval. Runs under the same enable flag as the other
	// background workers since it is likewise background housekeeping, not
	// something a request waits on.
	webhookCleanup := webhookevent.NewCleanupWorker(webhookevent.NewService(database.NewQuerier(pool), cipher))
	if cfg.SyncWorkerEnabled {
		go func() {
			slog.Info("webhook event cleanup worker starting", "interval", webhookevent.DefaultCleanupInterval, "retention", webhookevent.DefaultRetentionPeriod)
			if err := webhookCleanup.Run(context.Background()); err != nil {
				slog.Error("webhook event cleanup worker stopped", "error", err)
			}
		}()
	}

	// Daily digest notifications (issue #109): sweeps every project with
	// notifications enabled and sends its digest once past its configured
	// send_hour. Runs under the same enable flag as the other background
	// workers for the same reason webhookCleanup does.
	notificationWorker := notification.NewWorker(database.NewQuerier(pool), notification.NewHTTPSender())
	if cfg.SyncWorkerEnabled {
		go func() {
			slog.Info("notification digest worker starting", "interval", notification.DefaultSweepInterval)
			if err := notificationWorker.Run(context.Background()); err != nil {
				slog.Error("notification digest worker stopped", "error", err)
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
			if err := webhookWorker.Stop(shutdownCtx); err != nil {
				slog.Warn("webhook apply worker did not stop cleanly", "error", err)
			}
			if err := webhookCleanup.Stop(shutdownCtx); err != nil {
				slog.Warn("webhook event cleanup worker did not stop cleanly", "error", err)
			}
			if err := notificationWorker.Stop(shutdownCtx); err != nil {
				slog.Warn("notification digest worker did not stop cleanly", "error", err)
			}
		}
		return httpServer.Shutdown(shutdownCtx)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pkm-sync/internal/config"
	"pkm-sync/internal/embeddings"
	"pkm-sync/internal/server"

	"github.com/spf13/cobra"
)

var serveAddr string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the pkm-sync HTTP API server",
	Long: `Run a long-lived HTTP server exposing read-only access to the data
pkm-sync maintains: semantic search (vectors.db), Gmail full-text search
(archive.db), Slack message search (slack.db), pipeline status, and
Prometheus metrics.

Endpoints:
  GET /api/search          semantic vector search (q, source_type, source_name, limit, min_score)
  GET /api/emails          Gmail FTS search (q, from, since, limit, body)
  GET /api/slack/messages  Slack keyword search (q, channel, author, since, limit)
  GET /api/status          pipeline-status JSON written by sync jobs
  GET /metrics             Prometheus metrics (unauthenticated)
  GET /healthz             liveness probe (unauthenticated)

Authentication: set PKM_API_TOKEN to require "Authorization: Bearer <token>"
on /api/* routes. When unset, the API is open (intended for local use only).

Data paths come from config.yaml (vectordb.db_path, archive.db_path,
slack.db_path) with env overrides PKM_ARCHIVE_DB, PKM_SLACK_DB,
PKM_SLACK_USER_CACHE, PKM_STATUS_DIR, and PKM_SYNC_METRICS_DB.`,
	RunE: runServeCommand,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "Listen address (host:port)")
}

func runServeCommand(cmd *cobra.Command, _ []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	provider, err := embeddings.NewProvider(cfg.Embeddings)
	if err != nil {
		return fmt.Errorf("failed to create embedding provider: %w", err)
	}

	if provider == nil {
		slog.Warn("No embedding provider configured; /api/search will return 503")
	} else {
		defer provider.Close()
	}

	vectorDBPath, err := resolveVectorDBPath(cfg)
	if err != nil {
		return fmt.Errorf("failed to resolve vectors.db path: %w", err)
	}

	cfgDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	srvCfg := server.Config{
		Token:         os.Getenv("PKM_API_TOKEN"),
		StatusDir:     envOrDefault("PKM_STATUS_DIR", "/data/pipeline-status"),
		MetricsDBPath: envOrDefault("PKM_SYNC_METRICS_DB", "/data/agent_metrics.db"),
		VectorDBPath:  vectorDBPath,
		ArchiveDBPath: firstNonEmpty(os.Getenv("PKM_ARCHIVE_DB"), cfg.Archive.DBPath, filepath.Join(cfgDir, "archive.db")),
		SlackDBPath:   firstNonEmpty(os.Getenv("PKM_SLACK_DB"), cfg.Slack.DBPath, filepath.Join(cfgDir, "slack.db")),
		UserCachePath: firstNonEmpty(os.Getenv("PKM_SLACK_USER_CACHE"), filepath.Join(cfgDir, "slack-user-cache.json")),
		Dimensions:    cfg.Embeddings.Dimensions,
	}

	if srvCfg.Token == "" {
		slog.Warn("PKM_API_TOKEN not set; API routes are unauthenticated")
	}

	httpServer := &http.Server{
		Addr:              serveAddr,
		Handler:           server.New(srvCfg, provider).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut down gracefully on SIGINT/SIGTERM so in-flight requests finish.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = httpServer.Shutdown(shutdownCtx)
	}()

	slog.Info("pkm-sync API server listening",
		"addr", serveAddr,
		"auth", srvCfg.Token != "",
		"vectors_db", srvCfg.VectorDBPath,
		"archive_db", srvCfg.ArchiveDBPath,
		"slack_db", srvCfg.SlackDBPath)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// envOrDefault returns the environment variable's value, or def when unset.
func envOrDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}

	return def
}

// firstNonEmpty returns the first non-empty string from the arguments.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

package main

import (
	"context"
	"fmt"
	"path/filepath"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sinks"
	"pkm-sync/internal/sources/google/auth"
	syncer "pkm-sync/internal/sync"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var (
	indexSourceName    string
	indexSince         string
	indexLimit         int
	indexReindex       bool
	indexDelay         int
	indexMaxContentLen int
)

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index Gmail messages into vector database for semantic search",
	Long: `Index Gmail messages into a vector database for semantic search.
Messages are grouped by thread and embedded together for better context.

Examples:
  pkm-sync index --source gmail_work --since 30d
  pkm-sync index --source gmail_work --since 7d --limit 500
  pkm-sync index --reindex  # Re-index all messages from all sources`,
	RunE: runIndexCommand,
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().StringVar(&indexSourceName, "source", "", "Gmail source to index (gmail_work, gmail_personal, etc.)")
	indexCmd.Flags().StringVar(&indexSince, "since", "30d", "Index emails since (7d, 2006-01-02, today)")
	indexCmd.Flags().IntVar(&indexLimit, "limit", 1000, "Maximum number of emails to fetch per source")
	indexCmd.Flags().BoolVar(&indexReindex, "reindex", false, "Re-index already indexed threads")
	indexCmd.Flags().IntVar(&indexDelay, "delay", 200, "Delay between embeddings in milliseconds (prevents Ollama overload)")
	indexCmd.Flags().IntVar(&indexMaxContentLen, "max-content-length", 30000, "Truncate email content to this many characters (0 = no limit)")
}

func runIndexCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine which Gmail sources to index
	var sourcesToIndex []string
	if indexSourceName != "" {
		sourcesToIndex = []string{indexSourceName}
	} else {
		sourcesToIndex = getEnabledGmailSources(cfg)
	}

	if len(sourcesToIndex) == 0 {
		return fmt.Errorf("no Gmail sources configured. Please configure Gmail sources in your config file or use --source flag")
	}

	sinceTime, err := parseSinceTime(indexSince)
	if err != nil {
		return fmt.Errorf("failed to parse --since: %w", err)
	}

	// Resolve vector DB path
	dbPath := cfg.VectorDB.DBPath
	if dbPath == "" {
		configDir, err := config.GetConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		dbPath = filepath.Join(configDir, "vectors.db")
	}

	fmt.Printf("Using embedding provider: %s (%s, %d dimensions)\n",
		cfg.Embeddings.Provider, cfg.Embeddings.Model, cfg.Embeddings.Dimensions)
	fmt.Printf("Using vector database: %s\n", dbPath)

	// Create vector sink
	vectorSink, err := sinks.NewVectorSink(sinks.VectorSinkConfig{
		DBPath:        dbPath,
		Reindex:       indexReindex,
		Delay:         indexDelay,
		MaxContentLen: indexMaxContentLen,
		EmbeddingsCfg: cfg.Embeddings,
	})
	if err != nil {
		return fmt.Errorf("failed to create vector sink: %w", err)
	}
	defer vectorSink.Close()

	// Build source entries (force ExtractRecipients for richer embeddings)
	entries := make([]syncer.SourceEntry, 0, len(sourcesToIndex))

	for _, sourceName := range sourcesToIndex {
		sourceConfig, exists := cfg.Sources[sourceName]
		if !exists {
			fmt.Printf("Warning: Source '%s' not found in config, skipping\n", sourceName)

			continue
		}

		if sourceConfig.Type != "gmail" {
			fmt.Printf("Warning: Source '%s' is not a Gmail source (type: %s), skipping\n", sourceName, sourceConfig.Type)

			continue
		}

		// Force ExtractRecipients for richer embedding metadata
		sourceConfig.Gmail.ExtractRecipients = true

		client, err := auth.GetClient()
		if err != nil {
			return fmt.Errorf("failed to create authenticated client: %w", err)
		}

		src, err := createSourceWithConfig(sourceName, sourceConfig, client)
		if err != nil {
			return fmt.Errorf("failed to configure source '%s': %w", sourceName, err)
		}

		entries = append(entries, syncer.SourceEntry{
			Name:  sourceName,
			Src:   src,
			Since: sinceTime,
			Limit: indexLimit,
		})
	}

	if len(entries) == 0 {
		return fmt.Errorf("no valid Gmail sources to index")
	}

	// Run sync pipeline: fetch → (no transform) → vector sink
	// Source tags are required so VectorSink can extract source names
	s := syncer.NewMultiSyncer(nil) // no transformer pipeline for indexing

	_, err = s.SyncAll(
		ctx,
		entries,
		[]interfaces.Sink{vectorSink},
		syncer.MultiSyncOptions{
			DefaultLimit: indexLimit,
			SourceTags:   true, // VectorSink needs "source:<name>" tags for dedup
			TransformCfg: models.TransformConfig{Enabled: false},
		},
	)
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Print database stats
	stats, err := vectorSink.Stats()
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	fmt.Printf("\n=== Vector Database Stats ===\n")
	fmt.Printf("Total documents: %d\n", stats.TotalDocuments)
	fmt.Printf("Total threads: %d\n", stats.TotalThreads)
	fmt.Printf("Average messages per thread: %.1f\n", stats.AverageMessageCount)

	if len(stats.DocumentsBySource) > 0 {
		fmt.Printf("\nDocuments by source:\n")

		for source, count := range stats.DocumentsBySource {
			fmt.Printf("  %s: %d\n", source, count)
		}
	}

	if !stats.OldestDocument.IsZero() && !stats.NewestDocument.IsZero() {
		fmt.Printf("\nDate range: %s to %s\n",
			stats.OldestDocument.Format("2006-01-02"),
			stats.NewestDocument.Format("2006-01-02"))
	}

	return nil
}

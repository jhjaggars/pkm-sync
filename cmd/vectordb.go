package main

import (
	"fmt"

	"pkm-sync/internal/config"
	"pkm-sync/internal/vectorstore"

	"github.com/spf13/cobra"
)

var (
	normalizeTimestampsDBPath string
	normalizeTimestampsDryRun bool
)

// vectordbCmd groups maintenance subcommands that operate directly on
// vectors.db, outside the normal index/search pipeline.
var vectordbCmd = &cobra.Command{
	Use:   "vectordb",
	Short: "Maintenance commands for vectors.db",
}

// normalizeTimestampsCmd backfills documents.created_at/updated_at rows that
// were written before UpsertDocument called .UTC() before formatting (see
// internal/vectorstore/store.go). See NormalizeTimestamps for the rationale
// and idempotency guarantee.
var normalizeTimestampsCmd = &cobra.Command{
	Use:   "normalize-timestamps",
	Short: "Rewrite documents.created_at/updated_at to Z-suffixed UTC RFC3339",
	Long: `A past bug in UpsertDocument formatted created_at/updated_at with
time.Time.Format(time.RFC3339) without first calling .UTC(). Rows written
while the process's local timezone was non-UTC therefore got a local-offset
suffix (e.g. "2026-07-11T15:30:13-04:00") instead of "Z". Because these
columns are compared lexicographically — SQL MIN()/MAX() in Stats() and the
/metrics freshness gauges, and inferLastSynced's incremental "since" query —
a mix of offset- and Z-suffixed strings sorts incorrectly.

This command scans every row in the documents table, reparses created_at and
updated_at as RFC3339, and rewrites only the values that do not already
round-trip to their canonical UTC Z-suffixed form via
time.Time.UTC().Format(time.RFC3339). Rows that are already correct — and any
row with a value that fails to parse — are left untouched, so the command is
idempotent: running it again after a successful pass reports zero rows fixed.

Use --dry-run to only count and print affected rows without writing anything.

Examples:
  # Preview what would change, using the configured/default vectors.db path.
  pkm-sync vectordb normalize-timestamps --dry-run

  # Apply the fix to a specific database file.
  pkm-sync vectordb normalize-timestamps --db /path/to/vectors.db

Always back up vectors.db (or test against a copy) before running without
--dry-run against data you care about.`,
	RunE: runNormalizeTimestamps,
}

func init() {
	rootCmd.AddCommand(vectordbCmd)
	vectordbCmd.AddCommand(normalizeTimestampsCmd)

	normalizeTimestampsCmd.Flags().StringVar(&normalizeTimestampsDBPath, "db", "",
		"Path to vectors.db (defaults to the configured/standard location)")
	normalizeTimestampsCmd.Flags().BoolVar(&normalizeTimestampsDryRun, "dry-run", false,
		"Only count and print affected rows; make no writes")
}

func runNormalizeTimestamps(_ *cobra.Command, _ []string) error {
	dbPath := normalizeTimestampsDBPath
	if dbPath == "" {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		dbPath, err = resolveVectorDBPath(cfg)
		if err != nil {
			return fmt.Errorf("failed to resolve vectors.db path: %w", err)
		}
	}

	// NewQueryStore requires the file to already exist and skips schema
	// creation — this command must never create a fresh, empty vectors.db.
	store, err := vectorstore.NewQueryStore(dbPath, 0)
	if err != nil {
		return fmt.Errorf("failed to open vectors.db at %s: %w", dbPath, err)
	}
	defer store.Close()

	result, err := store.NormalizeTimestamps(normalizeTimestampsDryRun)
	if err != nil {
		return fmt.Errorf("failed to normalize timestamps: %w", err)
	}

	verb := "fixed"
	if normalizeTimestampsDryRun {
		verb = "would fix"
	}

	fmt.Printf("scanned %d document row(s); %s %d created_at and %d updated_at value(s)\n",
		result.RowsScanned, verb, result.CreatedAtFixed, result.UpdatedAtFixed)

	return nil
}

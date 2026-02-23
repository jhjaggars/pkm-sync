package main

import (
	"context"
	"fmt"
	"strings"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sinks"
	syncer "pkm-sync/internal/sync"
	"pkm-sync/internal/transform"
	"pkm-sync/pkg/interfaces"

	"github.com/spf13/cobra"
)

var (
	syncSourceName   string
	syncTargetName   string
	syncOutputDir    string
	syncSince        string
	syncDryRun       bool
	syncLimit        int
	syncOutputFormat string
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync all enabled sources to PKM systems",
	Long: `Sync all enabled sources (Gmail, Google Calendar, etc.) to PKM targets in a single operation.

Examples:
  pkm-sync sync
  pkm-sync sync --source gmail_work
  pkm-sync sync --target obsidian --output ./vault
  pkm-sync sync --since 7d --dry-run
  pkm-sync sync --source gmail_work --dry-run --format json`,
	RunE: runSyncCommand,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().StringVar(&syncSourceName, "source", "", "Filter to a specific source by name")
	syncCmd.Flags().StringVar(&syncTargetName, "target", "", "PKM target (obsidian, logseq)")
	syncCmd.Flags().StringVarP(&syncOutputDir, "output", "o", "", "Output directory")
	syncCmd.Flags().StringVar(&syncSince, "since", "", "Sync items since (7d, 2006-01-02, today)")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "Show what would be synced without making changes")
	syncCmd.Flags().IntVar(&syncLimit, "limit", 1000, "Maximum number of items per source")
	syncCmd.Flags().StringVar(&syncOutputFormat, "format", "summary", "Output format for dry-run (summary, json)")
}

func runSyncCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	// Determine which sources to sync
	var sourcesToSync []string
	if syncSourceName != "" {
		sourcesToSync = []string{syncSourceName}
	} else {
		sourcesToSync = getEnabledSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no enabled sources found. Configure sources in your config file or use --source flag")
	}

	// Resolve target, output, since from CLI flags with config fallbacks
	finalTargetName := cfg.Sync.DefaultTarget
	if syncTargetName != "" {
		finalTargetName = syncTargetName
	}

	finalOutputDir := cfg.Sync.DefaultOutputDir
	if syncOutputDir != "" {
		finalOutputDir = syncOutputDir
	}

	finalSince := cfg.Sync.DefaultSince
	if syncSince != "" {
		finalSince = syncSince
	}

	defaultSinceTime, err := parseSinceTime(finalSince)
	if err != nil {
		return fmt.Errorf("invalid since parameter: %w", err)
	}

	fmt.Printf("Syncing sources [%s] to %s (output: %s, since: %s)\n",
		strings.Join(sourcesToSync, ", "), finalTargetName, finalOutputDir, finalSince)

	// Build source entries (pre-create sources with per-source options)
	entries := make([]syncer.SourceEntry, 0, len(sourcesToSync))

	for _, srcName := range sourcesToSync {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: source '%s' not configured, skipping\n", srcName)

			continue
		}

		if !sourceConfig.Enabled {
			fmt.Printf("Source '%s' is disabled, skipping\n", srcName)

			continue
		}

		if sourceConfig.Type != "gmail" && sourceConfig.Type != "google_calendar" && sourceConfig.Type != "google_drive" {
			fmt.Printf("Warning: source '%s' has unsupported type '%s', skipping\n", srcName, sourceConfig.Type)

			continue
		}

		src, err := createSourceWithConfig(srcName, sourceConfig, nil)
		if err != nil {
			fmt.Printf("Warning: failed to create source '%s': %v, skipping\n", srcName, err)

			continue
		}

		entry := syncer.SourceEntry{Name: srcName, Src: src}

		// Per-source since: config overrides default, but CLI flag takes precedence
		if sourceConfig.Since != "" && syncSince == "" {
			t, err := parseSinceTime(sourceConfig.Since)
			if err != nil {
				fmt.Printf("Warning: invalid since time for source '%s': %v, using default\n", srcName, err)
			} else {
				entry.Since = t
			}
		}

		// Per-source limit (cap at 2500)
		if sourceConfig.Google.MaxResults > 0 {
			if sourceConfig.Google.MaxResults > 2500 {
				fmt.Printf("Warning: max_results for source '%s' is %d (maximum: 2500), capping\n", srcName, sourceConfig.Google.MaxResults)

				entry.Limit = 2500
			} else {
				entry.Limit = sourceConfig.Google.MaxResults
			}
		}

		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return fmt.Errorf("no valid sources could be initialized")
	}

	// Create target and file sink
	target, err := createTargetWithConfig(finalTargetName, cfg)
	if err != nil {
		return fmt.Errorf("failed to create target: %w", err)
	}

	fileSink := sinks.NewFileSink(target, finalOutputDir)

	// Build transformer pipeline with all transformers
	pipeline := transform.NewPipeline()
	for _, t := range transform.GetAllContentProcessingTransformers() {
		if err := pipeline.AddTransformer(t); err != nil {
			return fmt.Errorf("failed to add transformer %s: %w", t.Name(), err)
		}
	}

	// Run the full sync pipeline
	s := syncer.NewMultiSyncer(pipeline)

	syncResult, err := s.SyncAll(
		context.Background(),
		entries,
		[]interfaces.Sink{fileSink},
		syncer.MultiSyncOptions{
			DefaultSince: defaultSinceTime,
			DefaultLimit: syncLimit,
			SourceTags:   cfg.Sync.SourceTags,
			TransformCfg: cfg.Transformers,
			DryRun:       syncDryRun,
		},
	)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	if syncDryRun {
		previews, err := target.Preview(syncResult.Items, finalOutputDir)
		if err != nil {
			return fmt.Errorf("failed to generate preview: %w", err)
		}

		switch syncOutputFormat {
		case "json":
			return outputDryRunJSON(syncResult.Items, previews, finalTargetName, finalOutputDir, sourcesToSync)
		case "summary":
			return outputDryRunSummary(syncResult.Items, previews, finalTargetName, finalOutputDir, sourcesToSync)
		default:
			return fmt.Errorf("unknown format '%s': supported formats are 'summary' and 'json'", syncOutputFormat)
		}
	}

	printSyncSummary(syncResult.SourceResults, len(syncResult.Items))

	return nil
}

func printSyncSummary(results []syncer.SourceResult, totalItems int) {
	attempted := len(results)
	succeeded := 0
	failed := 0

	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			succeeded++
		}
	}

	fmt.Printf("\nSync complete: %d sources attempted, %d succeeded, %d failed\n", attempted, succeeded, failed)
	fmt.Printf("Total items exported: %d\n", totalItems)

	if failed > 0 {
		fmt.Printf("\nFailed sources:\n")

		for _, r := range results {
			if r.Err != nil {
				fmt.Printf("  - %s: %v\n", r.Name, r.Err)
			}
		}
	}
}

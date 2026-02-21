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
	driveSourceName   string
	driveTargetName   string
	driveOutputDir    string
	driveSince        string
	driveDryRun       bool
	driveLimit        int
	driveOutputFormat string
)

var driveCmd = &cobra.Command{
	Use:   "drive",
	Short: "Sync Google Drive documents to PKM systems",
	Long: `Sync Google Drive documents (Google Docs, Sheets, Slides) to PKM targets.

Configure google_drive sources in your config file to specify folders,
shared drives, and export formats.

Use the fetch subcommand to fetch a single document by URL:
  pkm-sync drive fetch <URL>

Examples:
  pkm-sync drive --source my_drive --target obsidian --output ./vault
  pkm-sync drive --since 7d --dry-run
  pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit"`,
	RunE: runDriveCommand,
}

func init() {
	rootCmd.AddCommand(driveCmd)
	driveCmd.Flags().StringVar(&driveSourceName, "source", "", "Drive source name (as configured in config file)")
	driveCmd.Flags().StringVar(&driveTargetName, "target", "", "PKM target (obsidian, logseq)")
	driveCmd.Flags().StringVarP(&driveOutputDir, "output", "o", "", "Output directory")
	driveCmd.Flags().StringVar(&driveSince, "since", "", "Sync documents modified since (7d, 2006-01-02, today)")
	driveCmd.Flags().BoolVar(&driveDryRun, "dry-run", false, "Show what would be synced without making changes")
	driveCmd.Flags().IntVar(&driveLimit, "limit", 100, "Maximum number of documents to fetch")
	driveCmd.Flags().StringVar(&driveOutputFormat, "format", "summary", "Output format for dry-run (summary, json)")
}

func runDriveCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	// Determine which Drive sources to sync
	var sourcesToSync []string
	if driveSourceName != "" {
		sourcesToSync = []string{driveSourceName}
	} else {
		sourcesToSync = getEnabledDriveSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Drive sources configured. Configure google_drive sources in your config file or use --source flag")
	}

	// Apply config defaults, then CLI overrides
	finalTargetName := cfg.Sync.DefaultTarget
	if driveTargetName != "" {
		finalTargetName = driveTargetName
	}

	finalOutputDir := cfg.Sync.DefaultOutputDir
	if driveOutputDir != "" {
		finalOutputDir = driveOutputDir
	}

	finalSince := cfg.Sync.DefaultSince
	if driveSince != "" {
		finalSince = driveSince
	}

	defaultSinceTime, err := parseSinceTime(finalSince)
	if err != nil {
		return fmt.Errorf("invalid since parameter: %w", err)
	}

	fmt.Printf("Syncing Drive from sources [%s] to %s (output: %s, since: %s)\n",
		strings.Join(sourcesToSync, ", "), finalTargetName, finalOutputDir, finalSince)

	// Build source entries (pre-create sources with per-source options)
	var entries []syncer.SourceEntry

	for _, srcName := range sourcesToSync {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: Drive source '%s' not configured, skipping\n", srcName)

			continue
		}

		if !sourceConfig.Enabled {
			fmt.Printf("Drive source '%s' is disabled, skipping\n", srcName)

			continue
		}

		if sourceConfig.Type != "google_drive" {
			fmt.Printf("Warning: source '%s' is not a Drive source (type: %s), skipping\n", srcName, sourceConfig.Type)

			continue
		}

		src, err := createSourceWithConfig(srcName, sourceConfig, nil)
		if err != nil {
			fmt.Printf("Warning: failed to create Drive source '%s': %v, skipping\n", srcName, err)

			continue
		}

		entry := syncer.SourceEntry{Name: srcName, Src: src}

		// Per-source since: config overrides default, CLI flag takes precedence
		if sourceConfig.Since != "" && driveSince == "" {
			t, err := parseSinceTime(sourceConfig.Since)
			if err != nil {
				fmt.Printf("Warning: invalid since time for Drive source '%s': %v, using default\n", srcName, err)
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
		return fmt.Errorf("no valid Drive sources could be initialized")
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
			DefaultLimit: driveLimit,
			SourceTags:   cfg.Sync.SourceTags,
			TransformCfg: cfg.Transformers,
			DryRun:       driveDryRun,
		},
	)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	if driveDryRun {
		previews, err := target.Preview(syncResult.Items, finalOutputDir)
		if err != nil {
			return fmt.Errorf("failed to generate preview: %w", err)
		}

		switch driveOutputFormat {
		case "json":
			return outputDryRunJSON(syncResult.Items, previews, finalTargetName, finalOutputDir, sourcesToSync)
		case "summary":
			return outputDryRunSummary(syncResult.Items, previews, finalTargetName, finalOutputDir, sourcesToSync)
		default:
			return fmt.Errorf("unknown format '%s': supported formats are 'summary' and 'json'", driveOutputFormat)
		}
	}

	fmt.Printf("Successfully exported %d documents\n", len(syncResult.Items))

	return nil
}

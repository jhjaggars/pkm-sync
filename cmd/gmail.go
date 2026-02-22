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
	gmailSourceName   string
	gmailTargetName   string
	gmailOutputDir    string
	gmailSince        string
	gmailDryRun       bool
	gmailLimit        int
	gmailOutputFormat string
)

var gmailCmd = &cobra.Command{
	Use:   "gmail",
	Short: "Sync Gmail emails to PKM systems",
	Long: `Sync Gmail emails to PKM targets (obsidian, logseq, etc.)

Examples:
  pkm-sync gmail --source gmail_work --target obsidian --output ./vault
  pkm-sync gmail --source gmail_personal --target logseq --output ./graph --since 7d
  pkm-sync gmail --source gmail_work --target obsidian --dry-run`,
	RunE: runGmailCommand,
}

func init() {
	rootCmd.AddCommand(gmailCmd)
	gmailCmd.Flags().StringVar(&gmailSourceName, "source", "", "Gmail source (gmail_work, gmail_personal, etc.)")
	gmailCmd.Flags().StringVar(&gmailTargetName, "target", "", "PKM target (obsidian, logseq)")
	gmailCmd.Flags().StringVarP(&gmailOutputDir, "output", "o", "", "Output directory")
	gmailCmd.Flags().StringVar(&gmailSince, "since", "", "Sync emails since (7d, 2006-01-02, today)")
	gmailCmd.Flags().BoolVar(&gmailDryRun, "dry-run", false, "Show what would be synced without making changes")
	gmailCmd.Flags().IntVar(&gmailLimit, "limit", 1000, "Maximum number of emails to fetch (default: 1000)")
	gmailCmd.Flags().StringVar(&gmailOutputFormat, "format", "summary", "Output format for dry-run (summary, json)")
}

func runGmailCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	// Determine Gmail sources to sync
	var sourcesToSync []string
	if gmailSourceName != "" {
		sourcesToSync = []string{gmailSourceName}
	} else {
		sourcesToSync = getEnabledGmailSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Gmail sources configured. Please configure Gmail sources in your config file or use --source flag")
	}

	// Resolve target, output, since from CLI flags with config fallbacks
	finalTargetName := cfg.Sync.DefaultTarget
	if gmailTargetName != "" {
		finalTargetName = gmailTargetName
	}

	finalOutputDir := cfg.Sync.DefaultOutputDir
	if gmailOutputDir != "" {
		finalOutputDir = gmailOutputDir
	}

	finalSince := cfg.Sync.DefaultSince
	if gmailSince != "" {
		finalSince = gmailSince
	}

	defaultSinceTime, err := parseSinceTime(finalSince)
	if err != nil {
		return fmt.Errorf("invalid since parameter: %w", err)
	}

	fmt.Printf("Syncing Gmail from sources [%s] to %s (output: %s, since: %s)\n",
		strings.Join(sourcesToSync, ", "), finalTargetName, finalOutputDir, finalSince)

	// Build source entries (pre-create sources with per-source options)
	var entries []syncer.SourceEntry

	for _, srcName := range sourcesToSync {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: Gmail source '%s' not configured, skipping\n", srcName)

			continue
		}

		if !sourceConfig.Enabled {
			fmt.Printf("Gmail source '%s' is disabled, skipping\n", srcName)

			continue
		}

		if sourceConfig.Type != "gmail" {
			fmt.Printf("Warning: source '%s' is not a Gmail source (type: %s), skipping\n", srcName, sourceConfig.Type)

			continue
		}

		src, err := createSourceWithConfig(srcName, sourceConfig, nil)
		if err != nil {
			fmt.Printf("Warning: failed to create Gmail source '%s': %v, skipping\n", srcName, err)

			continue
		}

		// Per-source since: config overrides default, but CLI flag takes precedence
		entry := syncer.SourceEntry{Name: srcName, Src: src}

		if sourceConfig.Since != "" && gmailSince == "" {
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
		return fmt.Errorf("no valid Gmail sources could be initialized")
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
			DefaultLimit: gmailLimit,
			SourceTags:   cfg.Sync.SourceTags,
			TransformCfg: cfg.Transformers,
			DryRun:       gmailDryRun,
		},
	)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	if gmailDryRun {
		previews, err := target.Preview(syncResult.Items, finalOutputDir)
		if err != nil {
			return fmt.Errorf("failed to generate preview: %w", err)
		}

		switch gmailOutputFormat {
		case "json":
			return outputDryRunJSON(syncResult.Items, previews, finalTargetName, finalOutputDir, sourcesToSync)
		case "summary":
			return outputDryRunSummary(syncResult.Items, previews, finalTargetName, finalOutputDir, sourcesToSync)
		default:
			return fmt.Errorf("unknown format '%s': supported formats are 'summary' and 'json'", gmailOutputFormat)
		}
	}

	fmt.Printf("Successfully exported %d emails\n", len(syncResult.Items))

	return nil
}

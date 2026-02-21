package main

import (
	"fmt"
	"strings"

	"pkm-sync/internal/config"
	"pkm-sync/internal/transform"
	"pkm-sync/pkg/models"

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

// sourceResult tracks the outcome of syncing a single source.
type sourceResult struct {
	Name      string
	ItemCount int
	Err       error
}

func runSyncCommand(cmd *cobra.Command, args []string) error {
	// Load configuration
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

	// Resolve target, output, and since from CLI flags with config fallbacks
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

	// Parse the default since time (used as fallback for per-source since)
	defaultSinceTime, err := parseSinceTime(finalSince)
	if err != nil {
		return fmt.Errorf("invalid since parameter: %w", err)
	}

	fmt.Printf("Syncing sources [%s] to %s (output: %s, since: %s)\n",
		strings.Join(sourcesToSync, ", "), finalTargetName, finalOutputDir, finalSince)

	// Create target — fatal on failure since we need somewhere to write
	target, err := createTargetWithConfig(finalTargetName, cfg)
	if err != nil {
		return fmt.Errorf("failed to create target: %w", err)
	}

	// Collect all items across all sources
	var allItems []models.ItemInterface

	var results []sourceResult

	for _, srcName := range sourcesToSync {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: source '%s' not configured, skipping\n", srcName)
			results = append(results, sourceResult{Name: srcName, Err: fmt.Errorf("not configured")})

			continue
		}

		if !sourceConfig.Enabled {
			fmt.Printf("Source '%s' is disabled, skipping\n", srcName)

			continue
		}

		// Only support source types that implement the Source interface
		if sourceConfig.Type != "gmail" && sourceConfig.Type != "google_calendar" {
			fmt.Printf("Warning: source '%s' has unsupported type '%s', skipping\n", srcName, sourceConfig.Type)
			results = append(results, sourceResult{Name: srcName, Err: fmt.Errorf("unsupported type '%s'", sourceConfig.Type)})

			continue
		}

		source, err := createSourceWithConfig(srcName, sourceConfig, nil)
		if err != nil {
			fmt.Printf("Warning: failed to create source '%s': %v, skipping\n", srcName, err)
			results = append(results, sourceResult{Name: srcName, Err: err})

			continue
		}

		// Per-source since: config overrides default, but CLI flag takes precedence over both
		sourceSince := finalSince
		if sourceConfig.Since != "" && syncSince == "" {
			sourceSince = sourceConfig.Since
		}

		sourceSinceTime, err := parseSinceTime(sourceSince)
		if err != nil {
			fmt.Printf("Warning: invalid since time for source '%s': %v, using default\n", srcName, err)

			sourceSinceTime = defaultSinceTime
		}

		// Per-source max results with a cap at 2500
		maxResults := syncLimit

		if sourceConfig.Google.MaxResults > 0 {
			if sourceConfig.Google.MaxResults > 2500 {
				fmt.Printf("Warning: max_results for source '%s' is %d (maximum: 2500), capping\n", srcName, sourceConfig.Google.MaxResults)

				maxResults = 2500
			} else {
				maxResults = sourceConfig.Google.MaxResults
			}
		}

		fmt.Printf("Fetching items from %s...\n", srcName)

		items, err := source.Fetch(sourceSinceTime, maxResults)
		if err != nil {
			fmt.Printf("Warning: failed to fetch from source '%s': %v, skipping\n", srcName, err)
			results = append(results, sourceResult{Name: srcName, Err: err})

			continue
		}

		// Apply source tags if enabled
		if cfg.Sync.SourceTags {
			for _, item := range items {
				currentTags := item.GetTags()
				item.SetTags(append(currentTags, "source:"+srcName))
			}
		}

		fmt.Printf("Found %d items from %s\n", len(items), srcName)
		results = append(results, sourceResult{Name: srcName, ItemCount: len(items)})
		allItems = append(allItems, items...)
	}

	fmt.Printf("Total items collected: %d\n", len(allItems))

	// Apply transformer pipeline if configured
	if cfg.Transformers.Enabled {
		pipeline := transform.NewPipeline()

		for _, t := range transform.GetAllExampleTransformers() {
			if err := pipeline.AddTransformer(t); err != nil {
				return fmt.Errorf("failed to add transformer %s: %w", t.Name(), err)
			}
		}

		if err := pipeline.Configure(cfg.Transformers); err != nil {
			return fmt.Errorf("failed to configure transformer pipeline: %w", err)
		}

		transformedItems, err := pipeline.Transform(allItems)
		if err != nil {
			return fmt.Errorf("failed to transform items: %w", err)
		}

		fmt.Printf("Transformed to %d items\n", len(transformedItems))
		allItems = transformedItems
	}

	if syncDryRun {
		previews, err := target.Preview(allItems, finalOutputDir)
		if err != nil {
			return fmt.Errorf("failed to generate preview: %w", err)
		}

		switch syncOutputFormat {
		case "json":
			return outputDryRunJSON(allItems, previews, finalTargetName, finalOutputDir, sourcesToSync)
		case "summary":
			return outputDryRunSummary(allItems, previews, finalTargetName, finalOutputDir, sourcesToSync)
		default:
			return fmt.Errorf("unknown format '%s': supported formats are 'summary' and 'json'", syncOutputFormat)
		}
	}

	if err := target.Export(allItems, finalOutputDir); err != nil {
		return fmt.Errorf("failed to export to target: %w", err)
	}

	// Print summary
	printSyncSummary(results, len(allItems))

	return nil
}

func printSyncSummary(results []sourceResult, totalItems int) {
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

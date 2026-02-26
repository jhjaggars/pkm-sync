package main

import (
	"fmt"
	"strings"

	"pkm-sync/internal/config"

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
	Long: `Sync all enabled sources (Gmail, Google Calendar, Drive, Slack) to PKM targets in a single operation.

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

	// Group enabled sources by type for dispatch to runSourceSync.
	typeGroups := map[string][]string{}

	for _, srcName := range sourcesToSync {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: source '%s' not configured, skipping\n", srcName)

			continue
		}

		switch sourceConfig.Type {
		case "gmail", "google_calendar", "google_drive", "slack":
			typeGroups[sourceConfig.Type] = append(typeGroups[sourceConfig.Type], srcName)
		default:
			fmt.Printf("Warning: source '%s' has unsupported type '%s', skipping\n", srcName, sourceConfig.Type)
		}
	}

	if len(typeGroups) == 0 {
		return fmt.Errorf("no valid sources could be initialized")
	}

	type typeGroupCfg struct {
		sourceType string
		sourceKind string
		itemKind   string
	}

	groups := []typeGroupCfg{
		{"gmail", "Gmail", "emails"},
		{"google_calendar", "Calendar", "events"},
		{"google_drive", "Drive", "documents"},
		{"slack", "Slack", "messages"},
	}

	var failedGroups []string

	for _, g := range groups {
		sources, ok := typeGroups[g.sourceType]
		if !ok || len(sources) == 0 {
			continue
		}

		if err := runSourceSync(cfg, sourceSyncConfig{
			SourceType:   g.sourceType,
			Sources:      sources,
			TargetName:   finalTargetName,
			OutputDir:    finalOutputDir,
			Since:        finalSince,
			SinceFlag:    syncSince,
			DefaultLimit: syncLimit,
			DryRun:       syncDryRun,
			OutputFormat: syncOutputFormat,
			SourceKind:   g.sourceKind,
			ItemKind:     g.itemKind,
		}); err != nil {
			fmt.Printf("Warning: %s sync failed: %v\n", g.sourceKind, err)
			failedGroups = append(failedGroups, g.sourceKind)
		}
	}

	if len(failedGroups) > 0 {
		return fmt.Errorf("sync failed for: %s", strings.Join(failedGroups, ", "))
	}

	return nil
}

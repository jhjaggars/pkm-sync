package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sinks"
	slacksource "pkm-sync/internal/sources/slack"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var (
	slackSourceName string
	slackSince      string
	slackDryRun     bool
	slackLimit      int
	slackDBPath     string
)

var slackCmd = &cobra.Command{
	Use:   "slack",
	Short: "Sync Slack messages to SQLite archive",
	Long: `Sync Slack messages from configured sources into a SQLite archive with FTS5 full-text search.

Examples:
  pkm-sync slack --source slack_work
  pkm-sync slack --source slack_work --since 7d
  pkm-sync slack --source slack_work --dry-run
  pkm-sync slack --db-path /custom/path/slack.db`,
	RunE: runSlackCommand,
}

func init() {
	rootCmd.AddCommand(slackCmd)
	slackCmd.Flags().StringVar(&slackSourceName, "source", "", "Slack source name (e.g. slack_work)")
	slackCmd.Flags().StringVar(&slackSince, "since", "", "Sync messages since (7d, 2006-01-02, today)")
	slackCmd.Flags().BoolVar(&slackDryRun, "dry-run", false, "Show what would be synced without making changes")
	slackCmd.Flags().IntVar(&slackLimit, "limit", 1000, "Maximum number of messages to fetch (default: 1000)")
	slackCmd.Flags().StringVar(&slackDBPath, "db-path", "", "Path to SQLite archive database (default: ~/.config/pkm-sync/slack.db)")
}

func runSlackCommand(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	// Resolve sources to sync.
	var sourcesToSync []string
	if slackSourceName != "" {
		sourcesToSync = []string{slackSourceName}
	} else {
		sourcesToSync = getEnabledSlackSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Slack sources configured. Please configure Slack sources in your config file or use --source flag")
	}

	// Resolve database path.
	dbPath := slackDBPath
	if dbPath == "" {
		configDir, err := config.GetConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		dbPath = filepath.Join(configDir, "slack.db")
	}

	// Resolve since time.
	finalSince := cfg.Sync.DefaultSince
	if slackSince != "" {
		finalSince = slackSince
	}

	sinceTime, err := parseSinceTime(finalSince)
	if err != nil {
		return fmt.Errorf("invalid since parameter: %w", err)
	}

	// Collect all items from all sources.
	var allItems []models.FullItem

	for _, srcName := range sourcesToSync {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: Slack source '%s' not configured, skipping\n", srcName)

			continue
		}

		if !sourceConfig.Enabled {
			fmt.Printf("Slack source '%s' is disabled, skipping\n", srcName)

			continue
		}

		if sourceConfig.Type != "slack" {
			fmt.Printf("Warning: source '%s' is not a Slack source (type: %s), skipping\n", srcName, sourceConfig.Type)

			continue
		}

		src := slacksource.NewSlackSource(srcName, sourceConfig)
		if err := src.Configure(nil, nil); err != nil {
			fmt.Printf("Warning: failed to configure Slack source '%s': %v, skipping\n", srcName, err)

			continue
		}

		items, err := src.Fetch(sinceTime, slackLimit)
		if err != nil {
			fmt.Printf("Warning: failed to fetch from Slack source '%s': %v, skipping\n", srcName, err)

			continue
		}

		allItems = append(allItems, items...)
	}

	if slackDryRun {
		printSlackDryRunSummary(allItems, dbPath)

		return nil
	}

	if len(allItems) == 0 {
		fmt.Println("No Slack messages found to archive.")

		return nil
	}

	// Write to SQLite archive.
	sink, err := sinks.NewSlackArchiveSink(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open slack archive at %s: %w", dbPath, err)
	}

	defer sink.Close()

	if err := sink.Write(context.Background(), allItems); err != nil {
		return fmt.Errorf("failed to write slack messages to archive: %w", err)
	}

	// Optionally write to VectorSink when auto_index is enabled.
	vectorSink, err := maybeCreateVectorSink(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vector sink: %w", err)
	}

	if vectorSink != nil {
		defer vectorSink.Close()

		if err := vectorSink.Write(context.Background(), allItems); err != nil {
			fmt.Printf("Warning: vector sink write failed: %v\n", err)
		}
	}

	fmt.Printf("Successfully wrote %d Slack messages to %s\n", len(allItems), dbPath)

	return nil
}

// printSlackDryRunSummary prints a channel-by-channel count table.
func printSlackDryRunSummary(items []models.FullItem, dbPath string) {
	// Count messages per channel.
	counts := make(map[string]int)

	for _, item := range items {
		meta := item.GetMetadata()
		channelName, _ := meta["channel"].(string)

		if channelName == "" {
			channelName = "(unknown)"
		}

		counts[channelName]++
	}

	// Sort channel names for deterministic output.
	channels := make([]string, 0, len(counts))
	for ch := range counts {
		channels = append(channels, ch)
	}

	sort.Strings(channels)

	fmt.Printf("%-32s %s\n", "Channel", "Messages")
	fmt.Printf("%-32s %s\n", "--------------------------------", "--------")

	for _, ch := range channels {
		fmt.Printf("%-32s %d\n", ch, counts[ch])
	}

	fmt.Printf("\nTotal: %d messages across %d channels\n", len(items), len(counts))
	fmt.Printf("Would write to: %s\n", dbPath)
}

// getEnabledSlackSources returns enabled Slack source names from config.
func getEnabledSlackSources(cfg *models.Config) []string {
	var enabledSources []string

	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled && sourceConfig.Type == "slack" {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled && sourceConfig.Type == "slack" {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

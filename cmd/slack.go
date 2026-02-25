package main

import (
	"fmt"

	"pkm-sync/internal/config"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var (
	slackSourceName   string
	slackTargetName   string
	slackOutputDir    string
	slackSince        string
	slackDryRun       bool
	slackLimit        int
	slackOutputFormat string
)

var slackCmd = &cobra.Command{
	Use:   "slack",
	Short: "Sync Slack messages to PKM systems",
	Long: `Sync Slack messages to PKM targets (obsidian, logseq, etc.)

Examples:
  pkm-sync slack --source slack_work --target obsidian --output ./vault
  pkm-sync slack --source slack_work --since 7d
  pkm-sync slack --source slack_work --dry-run`,
	RunE: runSlackCommand,
}

func init() {
	rootCmd.AddCommand(slackCmd)
	slackCmd.Flags().StringVar(&slackSourceName, "source", "", "Slack source name (e.g. slack_work)")
	slackCmd.Flags().StringVar(&slackTargetName, "target", "", "PKM target (obsidian, logseq)")
	slackCmd.Flags().StringVarP(&slackOutputDir, "output", "o", "", "Output directory")
	slackCmd.Flags().StringVar(&slackSince, "since", "", "Sync messages since (7d, 2006-01-02, today)")
	slackCmd.Flags().BoolVar(&slackDryRun, "dry-run", false, "Show what would be synced without making changes")
	slackCmd.Flags().IntVar(&slackLimit, "limit", 1000, "Maximum number of messages to fetch (default: 1000)")
	slackCmd.Flags().StringVar(&slackOutputFormat, "format", "summary", "Output format for dry-run (summary, json)")
}

func runSlackCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	var sourcesToSync []string
	if slackSourceName != "" {
		sourcesToSync = []string{slackSourceName}
	} else {
		sourcesToSync = getEnabledSlackSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Slack sources configured. Please configure Slack sources in your config file or use --source flag")
	}

	finalTargetName := cfg.Sync.DefaultTarget
	if slackTargetName != "" {
		finalTargetName = slackTargetName
	}

	finalOutputDir := cfg.Sync.DefaultOutputDir
	if slackOutputDir != "" {
		finalOutputDir = slackOutputDir
	}

	finalSince := cfg.Sync.DefaultSince
	if slackSince != "" {
		finalSince = slackSince
	}

	return runSourceSync(cfg, sourceSyncConfig{
		SourceType:   "slack",
		Sources:      sourcesToSync,
		TargetName:   finalTargetName,
		OutputDir:    finalOutputDir,
		Since:        finalSince,
		SinceFlag:    slackSince,
		DefaultLimit: slackLimit,
		DryRun:       slackDryRun,
		OutputFormat: slackOutputFormat,
		SourceKind:   "Slack",
		ItemKind:     "messages",
	})
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

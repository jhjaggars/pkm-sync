package main

import (
	"fmt"

	"pkm-sync/internal/config"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var (
	jiraSourceName string
	jiraSince      string
	jiraDryRun     bool
	jiraLimit      int
)

var jiraCmd = &cobra.Command{
	Use:   "jira",
	Short: "Sync Jira issues to PKM systems",
	Long: `Sync Jira issues from configured sources to PKM targets.

Examples:
  pkm-sync jira --source jira_work
  pkm-sync jira --source jira_work --since 7d
  pkm-sync jira --source jira_work --dry-run
  pkm-sync jira --limit 500`,
	RunE: runJiraCommand,
}

func init() {
	rootCmd.AddCommand(jiraCmd)
	jiraCmd.Flags().StringVar(&jiraSourceName, "source", "", "Jira source name (e.g. jira_work)")
	jiraCmd.Flags().StringVar(&jiraSince, "since", "", "Sync issues since (7d, 2006-01-02, today)")
	jiraCmd.Flags().BoolVar(&jiraDryRun, "dry-run", false, "Show what would be synced without making changes")
	jiraCmd.Flags().IntVar(&jiraLimit, "limit", 1000, "Maximum number of issues to fetch (default: 1000)")
}

func runJiraCommand(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	var sourcesToSync []string
	if jiraSourceName != "" {
		sourcesToSync = []string{jiraSourceName}
	} else {
		sourcesToSync = getEnabledJiraSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Jira sources configured. Please configure Jira sources in your config file or use --source flag")
	}

	finalSince := cfg.Sync.DefaultSince
	if jiraSince != "" {
		finalSince = jiraSince
	}

	return runSourceSync(cfg, sourceSyncConfig{
		SourceType:   "jira",
		Sources:      sourcesToSync,
		TargetName:   cfg.Sync.DefaultTarget,
		OutputDir:    cfg.Sync.DefaultOutputDir,
		Since:        finalSince,
		SinceFlag:    jiraSince,
		DefaultLimit: jiraLimit,
		DryRun:       jiraDryRun,
		OutputFormat: "summary",
		SourceKind:   "Jira",
		ItemKind:     "issues",
	})
}

// getEnabledJiraSources returns enabled Jira source names from config.
func getEnabledJiraSources(cfg *models.Config) []string {
	var enabledSources []string

	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled && sourceConfig.Type == "jira" {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled && sourceConfig.Type == "jira" {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

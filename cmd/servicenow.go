package main

import (
	"fmt"

	"pkm-sync/internal/config"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var (
	servicenowSourceName string
	servicenowSince      string
	servicenowDryRun     bool
	servicenowLimit      int
)

var servicenowCmd = &cobra.Command{
	Use:        "servicenow",
	Short:      "Sync ServiceNow tickets to PKM systems",
	Deprecated: "use 'pkm-sync sync servicenow' or 'pkm-sync sync --source <name>' instead",
	Long: `Sync ServiceNow tickets (RITMs, incidents, etc.) from configured sources to PKM targets.

Examples:
  pkm-sync servicenow --source snow_work
  pkm-sync servicenow --source snow_work --since 7d
  pkm-sync servicenow --source snow_work --dry-run
  pkm-sync servicenow --limit 500`,
	RunE: runServiceNowCommand,
}

func init() {
	rootCmd.AddCommand(servicenowCmd)
	servicenowCmd.Flags().StringVar(&servicenowSourceName, "source", "", "ServiceNow source name (e.g. snow_work)")
	servicenowCmd.Flags().StringVar(&servicenowSince, "since", "", "Sync tickets since (7d, 2006-01-02, today)")
	servicenowCmd.Flags().BoolVar(&servicenowDryRun, "dry-run", false, "Show what would be synced without making changes")
	servicenowCmd.Flags().IntVar(&servicenowLimit, "limit", 1000, "Maximum number of tickets to fetch (default: 1000)")
}

func runServiceNowCommand(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	var sourcesToSync []string
	if servicenowSourceName != "" {
		sourcesToSync = []string{servicenowSourceName}
	} else {
		sourcesToSync = getEnabledServiceNowSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no ServiceNow sources configured. Please configure ServiceNow sources in your config file or use --source flag")
	}

	finalSince := cfg.Sync.DefaultSince
	if servicenowSince != "" {
		finalSince = servicenowSince
	}

	return runSourceSync(cfg, sourceSyncConfig{
		SourceType:   "servicenow",
		Sources:      sourcesToSync,
		TargetName:   cfg.Sync.DefaultTarget,
		OutputDir:    cfg.Sync.DefaultOutputDir,
		Since:        finalSince,
		SinceFlag:    servicenowSince,
		DefaultLimit: servicenowLimit,
		DryRun:       servicenowDryRun,
		OutputFormat: "summary",
		SourceKind:   "ServiceNow",
		ItemKind:     "tickets",
	})
}

// getEnabledServiceNowSources returns enabled ServiceNow source names from config.
func getEnabledServiceNowSources(cfg *models.Config) []string {
	var enabledSources []string

	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled && sourceConfig.Type == "servicenow" {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled && sourceConfig.Type == "servicenow" {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

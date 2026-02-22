package main

import (
	"fmt"

	"pkm-sync/internal/config"

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

	var sourcesToSync []string
	if gmailSourceName != "" {
		sourcesToSync = []string{gmailSourceName}
	} else {
		sourcesToSync = getEnabledGmailSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Gmail sources configured. Please configure Gmail sources in your config file or use --source flag")
	}

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

	return runSourceSync(cfg, sourceSyncConfig{
		SourceType:   "gmail",
		Sources:      sourcesToSync,
		TargetName:   finalTargetName,
		OutputDir:    finalOutputDir,
		Since:        finalSince,
		SinceFlag:    gmailSince,
		DefaultLimit: gmailLimit,
		DryRun:       gmailDryRun,
		OutputFormat: gmailOutputFormat,
		SourceKind:   "Gmail",
		ItemKind:     "emails",
	})
}

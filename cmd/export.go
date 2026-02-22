package main

import (
	"fmt"

	"pkm-sync/internal/config"

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

	var sourcesToSync []string
	if driveSourceName != "" {
		sourcesToSync = []string{driveSourceName}
	} else {
		sourcesToSync = getEnabledDriveSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Drive sources configured. Configure google_drive sources in your config file or use --source flag")
	}

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

	return runSourceSync(cfg, sourceSyncConfig{
		SourceType:   "google_drive",
		Sources:      sourcesToSync,
		TargetName:   finalTargetName,
		OutputDir:    finalOutputDir,
		Since:        finalSince,
		SinceFlag:    driveSince,
		DefaultLimit: driveLimit,
		DryRun:       driveDryRun,
		OutputFormat: driveOutputFormat,
		SourceKind:   "Drive",
		ItemKind:     "documents",
	})
}

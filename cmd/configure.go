package main

import (
	"fmt"
	"os"

	"pkm-sync/internal/config"
	"pkm-sync/internal/configure"

	"github.com/spf13/cobra"
)

var configureSourceType string

var configureCmd = &cobra.Command{
	Use:   "configure [source-name]",
	Short: "Interactively configure what to sync from each source",
	Long: `Connect to the source API, see available options with previews,
and pick what to sync. Changes are saved to config.yaml.

Examples:
  pkm-sync configure                    # Pick from configured sources
  pkm-sync configure slack_redhat       # Configure a specific source
  pkm-sync configure --type slack       # Create a new Slack source`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConfigureCommand,
}

func init() {
	rootCmd.AddCommand(configureCmd)
	configureCmd.Flags().StringVar(
		&configureSourceType,
		"type", "",
		"Source type for new source (slack, gmail, google_drive, jira, google_calendar)",
	)
}

func runConfigureCommand(_ *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config (%v); using defaults\n", err)

		cfg = config.GetDefaultConfig()
	}

	sourceID := ""
	if len(args) > 0 {
		sourceID = args[0]
	}

	return configure.RunConfigure(cfg, sourceID, configureSourceType)
}

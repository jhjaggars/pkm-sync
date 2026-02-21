package main

import (
	"fmt"
	"log/slog"
	"os"

	"pkm-sync/internal/config"

	"github.com/spf13/cobra"
)

var (
	credentialsPath string
	configDir       string
	debugMode       bool
	startDate       string
	endDate         string
)

var rootCmd = &cobra.Command{
	Use:   "pkm-sync",
	Short: "Synchronize data between various sources and PKM systems",
	Long: `pkm-sync integrates data sources (Google Calendar, Gmail, Drive, etc.) 
with Personal Knowledge Management systems (Obsidian, Logseq, etc.).

Commands:
  sync      Sync all enabled sources to PKM systems
  gmail     Sync Gmail emails to PKM systems
  drive     Export Google Drive documents to markdown
  calendar  List and sync Google Calendar events
  setup     Verify authentication configuration
  config    Manage configuration files`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Set up logging based on debug flag
		if debugMode {
			// Set debug level logging
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			}))
			slog.SetDefault(logger)
		} else {
			// Set default info level logging
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))
			slog.SetDefault(logger)
		}

		if credentialsPath != "" {
			config.SetCustomCredentialsPath(credentialsPath)
		}

		if configDir != "" {
			config.SetCustomConfigDir(configDir)
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&credentialsPath, "credentials", "c", "", "Path to credentials.json file")
	rootCmd.PersistentFlags().StringVar(&configDir, "config-dir", "", "Custom configuration directory")
	rootCmd.PersistentFlags().BoolVarP(&debugMode, "debug", "d", false, "Enable debug logging")
	rootCmd.PersistentFlags().StringVarP(&startDate, "start", "s", "", "Start date (ISO 8601, relative like '7d', named like 'today', or natural language like 'last week')")
	rootCmd.PersistentFlags().StringVarP(&endDate, "end", "e", "", "End date (ISO 8601, relative like '7d', named like 'today', or natural language like 'last week')")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

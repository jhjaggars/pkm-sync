package main

import (
	"fmt"
	"log/slog"
	"os"

	"pkm-sync/internal/config"
	"pkm-sync/internal/keystore"
	"pkm-sync/internal/sources/google/auth"
	servicenow "pkm-sync/internal/sources/servicenow"
	slack "pkm-sync/internal/sources/slack"

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
with Personal Knowledge Management systems (Obsidian, Logseq, etc.).`,
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

		// Initialize secret store and wire it into auth packages.
		// Determine config directory for file fallback.
		effectiveConfigDir := configDir
		if effectiveConfigDir == "" {
			if d, err := config.GetConfigDir(); err == nil {
				effectiveConfigDir = d
			}
		}

		// Determine storage mode from config if available.
		storageMode := keystore.ModeAuto
		if cfg, err := config.LoadConfig(); err == nil && cfg.Auth.SecretStorage != "" {
			storageMode = cfg.Auth.SecretStorage
		}

		if store, err := keystore.New(storageMode, effectiveConfigDir); err != nil {
			slog.Debug("secret store init failed, secrets will use file fallback", "err", err)
		} else {
			auth.SetStore(store)
			slack.SetStore(store)
			servicenow.SetStore(store)
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

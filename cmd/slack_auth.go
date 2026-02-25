package main

import (
	"fmt"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sources/slack"

	"github.com/spf13/cobra"
)

var slackAuthWorkspace string

var slackAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Slack by extracting a session token",
	Long: `Opens a browser window for you to log in to your Slack workspace.
Once you're logged in, the session token is automatically extracted and saved.

Examples:
  pkm-sync slack auth --workspace https://myworkspace.slack.com`,
	RunE: runSlackAuthCommand,
}

func init() {
	slackCmd.AddCommand(slackAuthCmd)
	slackAuthCmd.Flags().StringVar(&slackAuthWorkspace, "workspace", "", "Slack workspace URL (e.g. https://myworkspace.slack.com)")
}

func runSlackAuthCommand(cmd *cobra.Command, args []string) error {
	workspaceURL := slackAuthWorkspace

	// Fall back to first configured slack source
	if workspaceURL == "" {
		cfg, err := config.LoadConfig()
		if err == nil {
			for _, srcCfg := range cfg.Sources {
				if srcCfg.Type == "slack" && srcCfg.Slack.WorkspaceURL != "" {
					workspaceURL = srcCfg.Slack.WorkspaceURL

					break
				}
			}
		}
	}

	if workspaceURL == "" {
		return fmt.Errorf("workspace URL is required. Use --workspace flag or configure a slack source in config")
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	progress := func(step, message string) {
		fmt.Printf("[%s] %s\n", step, message)
	}

	fmt.Printf("Starting Slack authentication for %s\n", workspaceURL)
	fmt.Printf("A browser window will open. Please complete the login process.\n\n")

	td, err := slack.RunAuth(workspaceURL, configDir, progress)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	tokenDisplay := td.Token
	if len(tokenDisplay) > 20 {
		tokenDisplay = tokenDisplay[:20]
	}

	fmt.Printf("\nAuthentication successful!\n")
	fmt.Printf("Token saved for workspace: %s\n", td.Workspace)
	fmt.Printf("Token: %s...\n", tokenDisplay)

	return nil
}

package main

import (
	"fmt"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sources/servicenow"

	"github.com/spf13/cobra"
)

var servicenowAuthInstance string

var servicenowAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with ServiceNow via browser SSO",
	Long: `Opens a browser window for you to log in to ServiceNow via SSO.
Once you're logged in, the session token is automatically extracted and saved.

Examples:
  pkm-sync servicenow auth --instance https://redhat.service-now.com`,
	RunE: runServiceNowAuthCommand,
}

func init() {
	servicenowCmd.AddCommand(servicenowAuthCmd)
	servicenowAuthCmd.Flags().StringVar(&servicenowAuthInstance, "instance", "", "ServiceNow instance URL (e.g. https://redhat.service-now.com)")
}

func runServiceNowAuthCommand(_ *cobra.Command, _ []string) error {
	instanceURL := servicenowAuthInstance

	// Fall back to first configured ServiceNow source.
	if instanceURL == "" {
		cfg, err := config.LoadConfig()
		if err == nil {
			for _, srcCfg := range cfg.Sources {
				if srcCfg.Type == "servicenow" && srcCfg.ServiceNow.InstanceURL != "" {
					instanceURL = srcCfg.ServiceNow.InstanceURL

					break
				}
			}
		}
	}

	if instanceURL == "" {
		return fmt.Errorf("instance URL is required. Use --instance flag or configure a servicenow source in config")
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	progress := func(step, message string) {
		fmt.Printf("[%s] %s\n", step, message)
	}

	fmt.Printf("Starting ServiceNow authentication for %s\n", instanceURL)
	fmt.Printf("A browser window will open. Please complete the SSO login process.\n\n")

	td, err := servicenow.RunAuth(instanceURL, configDir, progress)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	gckDisplay := td.GCK
	if len(gckDisplay) > 20 {
		gckDisplay = gckDisplay[:20]
	}

	fmt.Printf("\nAuthentication successful!\n")
	fmt.Printf("Token saved for instance: %s\n", td.Instance)
	fmt.Printf("Session token: %s...\n", gckDisplay)

	return nil
}

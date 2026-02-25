package main

import (
	"fmt"
	"sort"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sources/slack"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var slackChannelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "List available Slack channels and debug boot response",
	Long: `List channels visible to the authenticated Slack token.
Also shows raw boot response keys to help diagnose Enterprise Slack layouts.

Examples:
  pkm-sync slack channels --source slack_redhat`,
	RunE: runSlackChannelsCommand,
}

func init() {
	slackCmd.AddCommand(slackChannelsCmd)
}

func runSlackChannelsCommand(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Find the first enabled slack source.
	var (
		sourceName string
		sourceCfg  models.SourceConfig
	)

	if slackSourceName != "" {
		sc, ok := cfg.Sources[slackSourceName]
		if !ok {
			return fmt.Errorf("source %q not found in config", slackSourceName)
		}

		sourceName = slackSourceName
		sourceCfg = sc
	} else {
		for name, sc := range cfg.Sources {
			if sc.Type == "slack" && sc.Enabled {
				sourceName = name
				sourceCfg = sc

				break
			}
		}
	}

	if sourceName == "" {
		return fmt.Errorf("no enabled slack source found; use --source to specify one")
	}

	src := slack.NewSlackSource(sourceName, sourceCfg)
	if err := src.Configure(nil, nil); err != nil {
		return fmt.Errorf("failed to configure source: %w", err)
	}

	client := src.Client()

	// Show boot response keys.
	fmt.Println("=== Boot response keys ===")

	keys, err := client.BootKeys()
	if err != nil {
		return fmt.Errorf("failed to fetch boot keys: %w", err)
	}

	sort.Strings(keys)

	for _, k := range keys {
		fmt.Printf("  %s\n", k)
	}

	// Show a sample of channel objects.
	fmt.Println("\n=== Sample channel objects (up to 5) ===")

	samples, err := client.BootChannelSample(5)
	if err != nil {
		return fmt.Errorf("failed to fetch channel samples: %w", err)
	}

	for i, s := range samples {
		fmt.Printf("  [%d] id=%v name=%v is_im=%v\n", i, s["id"], s["name"], s["is_im"])
	}

	// Show resolved channels.
	fmt.Println("\n=== Resolved channels ===")

	channels, err := client.GetChannels()
	if err != nil {
		return fmt.Errorf("failed to get channels: %w", err)
	}

	for _, ch := range channels {
		fmt.Printf("  #%-30s  id=%s\n", ch.Name, ch.ID)
	}

	fmt.Printf("\nTotal: %d channels\n", len(channels))

	return nil
}

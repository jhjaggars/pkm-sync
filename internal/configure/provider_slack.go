package configure

import (
	"fmt"
	"net/url"
	"strings"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sources/slack"
	"pkm-sync/pkg/models"
)

// SlackProvider implements DiscoveryProvider for Slack workspaces.
type SlackProvider struct {
	client    *slack.Client
	configDir string
}

// SourceType implements DiscoveryProvider.
func (p *SlackProvider) SourceType() string { return sourceTypeSlack }

// Authenticate implements DiscoveryProvider. It loads the stored token for the
// workspace configured under sourceID and initializes the API client.
func (p *SlackProvider) Authenticate(cfg *models.Config, sourceID string) error {
	src, ok := cfg.Sources[sourceID]
	if !ok {
		return fmt.Errorf("source %q not found in configuration", sourceID)
	}

	if src.Slack.WorkspaceURL == "" {
		return fmt.Errorf("source %q has no workspace_url configured", sourceID)
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	p.configDir = configDir

	workspace := slackWorkspaceName(src.Slack.WorkspaceURL)

	td, err := slack.LoadToken(configDir, workspace)
	if err != nil {
		return fmt.Errorf("failed to load Slack token: %w", err)
	}

	if td == nil {
		return fmt.Errorf(
			"no Slack token found for workspace %q\n\n"+
				"To fix this, run: pkm-sync slack auth --workspace %s",
			workspace, src.Slack.WorkspaceURL,
		)
	}

	rateLimitMs := src.Slack.RateLimitMs
	if rateLimitMs <= 0 {
		rateLimitMs = 500
	}

	p.client = slack.NewClient(td.Token, td.CookieHeader, src.Slack.APIUrl, rateLimitMs)

	return nil
}

// DiscoverySections implements DiscoveryProvider. It fetches channels and DMs
// from the Slack API and returns two sections: "Channels" and "Direct Messages".
func (p *SlackProvider) DiscoverySections(currentConfig models.SourceConfig) ([]DiscoverySection, error) {
	if p.client == nil {
		return nil, fmt.Errorf("not authenticated — call Authenticate first")
	}

	// Build a set of currently-configured channel names for pre-selection.
	configuredChannels := make(map[string]bool, len(currentConfig.Slack.Channels))
	for _, ch := range currentConfig.Slack.Channels {
		configuredChannels[ch] = true
	}

	// Fetch all channels (public channels and private groups).
	channels, err := p.client.GetChannels()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Slack channels: %w", err)
	}

	channelOpts := make([]DiscoverableOption, 0, len(channels))
	for _, ch := range channels {
		if ch.Name == "" {
			continue // skip nameless entries
		}

		preview, _ := p.Preview(ch.ID, 3)
		desc := FormatPreviewDescription(preview)

		channelOpts = append(channelOpts, DiscoverableOption{
			// The config stores channel names (not IDs), matching how source.go uses them.
			ID:          ch.Name,
			Name:        "#" + ch.Name,
			Description: desc,
			Selected:    configuredChannels[ch.Name],
		})
	}

	// Fetch DMs.
	dms, err := p.client.GetDMs()
	if err != nil {
		// Non-fatal: log and continue without DMs section.
		dms = nil
	}

	dmOpts := make([]DiscoverableOption, 0, len(dms))
	for _, dm := range dms {
		name := dm.Name
		if name == "" {
			name = dm.User // fall back to user ID
		}

		preview, _ := p.Preview(dm.ID, 3)
		desc := FormatPreviewDescription(preview)

		dmOpts = append(dmOpts, DiscoverableOption{
			ID:          dm.ID,
			Name:        name,
			Description: desc,
			// DMs are toggled separately via IncludeDMs; individual DMs are not listed in Channels.
			Selected: currentConfig.Slack.IncludeDMs,
		})
	}

	sections := []DiscoverySection{
		{
			Name:        "Channels",
			Description: "Select the Slack channels you want to sync",
			Options:     channelOpts,
		},
	}

	if len(dmOpts) > 0 {
		sections = append(sections, DiscoverySection{
			Name:        "Direct Messages",
			Description: "Select direct message conversations to include",
			Options:     dmOpts,
		})
	}

	return sections, nil
}

// ApplySelections implements DiscoveryProvider. It updates the SourceConfig in-place.
//
// For the "Channels" section the selected IDs are channel names (as that is what
// the Slack source's FindChannel function expects). For "Direct Messages" we set
// IncludeDMs to true if any DMs were selected.
func (p *SlackProvider) ApplySelections(cfg *models.SourceConfig, sectionName string, selectedIDs []string) {
	switch sectionName {
	case "Channels":
		cfg.Slack.Channels = selectedIDs
	case "Direct Messages":
		cfg.Slack.IncludeDMs = len(selectedIDs) > 0
	}
}

// Preview implements DiscoveryProvider. It fetches the most recent messages from
// a channel and returns their text (truncated to 80 chars each).
func (p *SlackProvider) Preview(channelID string, limit int) ([]string, error) {
	if p.client == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	msgs, _, err := p.client.GetHistory(channelID, "", "", "", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history for channel %s: %w", channelID, err)
	}

	subjects := make([]string, 0, len(msgs))
	for _, m := range msgs {
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}

		subjects = append(subjects, TruncateString(text, 80))
	}

	return subjects, nil
}

// RequiredFields implements DiscoveryProvider.
func (p *SlackProvider) RequiredFields() []RequiredField {
	return []RequiredField{
		{
			Key:         "workspace_url",
			Prompt:      "Slack workspace URL",
			Placeholder: "https://myorg.slack.com",
			Validate: func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("workspace URL is required")
				}

				return nil
			},
		},
	}
}

// slackWorkspaceName extracts a safe workspace identifier from a workspace URL.
// This replicates the unexported workspaceName() from internal/sources/slack/auth.go —
// if that function changes its normalization logic, this must be updated to match.
func slackWorkspaceName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "slack"
	}

	host := u.Hostname()
	parts := strings.Split(host, ".")

	if len(parts) > 0 {
		return parts[0]
	}

	return host
}

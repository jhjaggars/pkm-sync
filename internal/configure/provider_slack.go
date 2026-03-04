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
// from the Slack API and returns three sections: "Channels", "Channel Groups",
// and "Messaging".
func (p *SlackProvider) DiscoverySections(currentConfig models.SourceConfig) ([]DiscoverySection, error) {
	if p.client == nil {
		return nil, fmt.Errorf("not authenticated — call Authenticate first")
	}

	// Build a set of currently-configured channel names for pre-selection.
	configuredChannels := make(map[string]bool, len(currentConfig.Slack.Channels))
	for _, ch := range currentConfig.Slack.Channels {
		configuredChannels[ch] = true
	}

	// Build a set of currently-configured channel groups for pre-selection.
	configuredGroups := make(map[string]bool, len(currentConfig.Slack.ChannelGroups))
	for _, g := range currentConfig.Slack.ChannelGroups {
		configuredGroups[g] = true
	}

	// Fetch all channels (public channels and private groups).
	channels, err := p.client.GetChannels()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Slack channels: %w", err)
	}

	// "Channels" section: regular channels only (filter out MPDMs).
	channelOpts := make([]DiscoverableOption, 0, len(channels))
	for _, ch := range channels {
		if ch.Name == "" || ch.IsMPIM {
			continue // skip nameless entries and group DMs
		}

		preview, _ := p.Preview(ch.ID, 3)
		desc := FormatPreviewDescription(preview)

		channelOpts = append(channelOpts, DiscoverableOption{
			ID:          ch.Name,
			Name:        "#" + ch.Name,
			Description: desc,
			Selected:    configuredChannels[ch.Name],
		})
	}

	// "Channel Groups" section: starred + custom sidebar sections.
	groupOpts := p.buildChannelGroupOptions(configuredGroups)

	// "Messaging" section: two synthetic toggles.
	// Get a preview from the first DM for the DM toggle description.
	var dmDesc string

	dms, err := p.client.GetDMs()
	if err == nil && len(dms) > 0 {
		preview, _ := p.Preview(dms[0].ID, 1)
		dmDesc = FormatPreviewDescription(preview)
	}

	// Get a preview from the first MPDM for the group DM toggle description.
	var mpdmDesc string

	mpdms, err := p.client.GetMPDMs()
	if err == nil && len(mpdms) > 0 {
		preview, _ := p.Preview(mpdms[0].ID, 1)
		mpdmDesc = FormatPreviewDescription(preview)
	}

	messagingOpts := []DiscoverableOption{
		{
			ID:          "__include_dms__",
			Name:        "Direct messages (1:1)",
			Description: dmDesc,
			Selected:    currentConfig.Slack.IncludeDMs,
		},
		{
			ID:          "__include_group_dms__",
			Name:        "Group messages (multi-party)",
			Description: mpdmDesc,
			Selected:    currentConfig.Slack.IncludeGroupDMs,
		},
	}

	return []DiscoverySection{
		{
			Name:        "Channels",
			Description: "Select the Slack channels you want to sync",
			Options:     channelOpts,
		},
		{
			Name:        "Channel Groups",
			Description: "Sync all channels in a dynamic group (starred or sidebar section)",
			Options:     groupOpts,
		},
		{
			Name:        "Messaging",
			Description: "Toggle direct and group message syncing",
			Options:     messagingOpts,
		},
	}, nil
}

// buildChannelGroupOptions constructs the options for the "Channel Groups" section.
func (p *SlackProvider) buildChannelGroupOptions(configuredGroups map[string]bool) []DiscoverableOption {
	var opts []DiscoverableOption

	// Starred channels option.
	starredChannels, _ := p.client.GetStarredChannels()
	starredDesc := fmt.Sprintf("%d starred channels", len(starredChannels))

	opts = append(opts, DiscoverableOption{
		ID:          "__group_starred__",
		Name:        "Starred channels",
		Description: starredDesc,
		Selected:    configuredGroups["starred"],
	})

	// Custom sidebar sections.
	sections, err := p.client.GetChannelSections()
	if err == nil {
		for name, ids := range sections {
			opts = append(opts, DiscoverableOption{
				ID:          "__group_section__" + name,
				Name:        name,
				Description: fmt.Sprintf("%d channels", len(ids)),
				Selected:    configuredGroups[name],
			})
		}
	}

	return opts
}

// ApplySelections implements DiscoveryProvider. It updates the SourceConfig in-place.
//
// For the "Channels" section the selected IDs are channel names (as that is what
// the Slack source's FindChannel function expects). For "Messaging" we set the
// appropriate toggle flags based on which synthetic IDs were selected.
func (p *SlackProvider) ApplySelections(cfg *models.SourceConfig, sectionName string, selectedIDs []string) {
	switch sectionName {
	case "Channels":
		// Filter out any mpdm-* names that may have been stored from old configs.
		filtered := make([]string, 0, len(selectedIDs))

		for _, id := range selectedIDs {
			if !strings.HasPrefix(id, "mpdm-") {
				filtered = append(filtered, id)
			}
		}

		cfg.Slack.Channels = filtered
	case "Channel Groups":
		cfg.Slack.ChannelGroups = nil

		for _, id := range selectedIDs {
			switch {
			case id == "__group_starred__":
				cfg.Slack.ChannelGroups = append(cfg.Slack.ChannelGroups, "starred")
			case strings.HasPrefix(id, "__group_section__"):
				name := strings.TrimPrefix(id, "__group_section__")
				cfg.Slack.ChannelGroups = append(cfg.Slack.ChannelGroups, name)
			}
		}
	case "Messaging":
		cfg.Slack.IncludeDMs = false
		cfg.Slack.IncludeGroupDMs = false

		for _, id := range selectedIDs {
			switch id {
			case "__include_dms__":
				cfg.Slack.IncludeDMs = true
			case "__include_group_dms__":
				cfg.Slack.IncludeGroupDMs = true
			}
		}
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

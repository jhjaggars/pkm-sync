package configure

import (
	"fmt"
	"strings"

	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/gmail"
	"pkm-sync/pkg/models"
)

// GmailProvider implements DiscoveryProvider for Gmail accounts.
type GmailProvider struct {
	svc *gmail.Service
}

// SourceType implements DiscoveryProvider.
func (p *GmailProvider) SourceType() string { return sourceTypeGmail }

// Authenticate implements DiscoveryProvider. It uses the stored OAuth token to
// create an authenticated Gmail service.
func (p *GmailProvider) Authenticate(cfg *models.Config, sourceID string) error {
	src, ok := cfg.Sources[sourceID]
	if !ok {
		return fmt.Errorf("source %q not found in configuration", sourceID)
	}

	client, err := auth.GetClient()
	if err != nil {
		return fmt.Errorf(
			"failed to get Google OAuth client: %w\n\n"+
				"To fix this, run: pkm-sync setup",
			err,
		)
	}

	svc, err := gmail.NewService(client, src.Gmail, sourceID)
	if err != nil {
		return fmt.Errorf("failed to create Gmail service: %w", err)
	}

	p.svc = svc

	return nil
}

// DiscoverySections implements DiscoveryProvider. It fetches all Gmail labels
// and returns one section with labels as selectable options, pre-checked when
// they are already in currentConfig.Gmail.Labels.
func (p *GmailProvider) DiscoverySections(currentConfig models.SourceConfig) ([]DiscoverySection, error) {
	if p.svc == nil {
		return nil, fmt.Errorf("not authenticated — call Authenticate first")
	}

	labels, err := p.svc.GetLabels()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Gmail labels: %w", err)
	}

	// Build a set of currently-configured labels for pre-selection.
	configured := make(map[string]bool, len(currentConfig.Gmail.Labels))
	for _, l := range currentConfig.Gmail.Labels {
		configured[l] = true
	}

	opts := make([]DiscoverableOption, 0, len(labels))
	for _, label := range labels {
		// Skip system labels that start with "CATEGORY_" — they clutter the list.
		if strings.HasPrefix(label.Id, "CATEGORY_") {
			continue
		}

		preview, _ := p.Preview(label.Id, 3)
		desc := FormatPreviewDescription(preview)

		opts = append(opts, DiscoverableOption{
			ID:          label.Id,
			Name:        label.Name,
			Description: desc,
			Selected:    configured[label.Id] || configured[label.Name],
		})
	}

	return []DiscoverySection{
		{
			Name:        "Labels",
			Description: "Select Gmail labels to include in syncs",
			Options:     opts,
		},
	}, nil
}

// ApplySelections implements DiscoveryProvider. It sets cfg.Gmail.Labels to the
// list of selected label IDs.
func (p *GmailProvider) ApplySelections(cfg *models.SourceConfig, sectionName string, selectedIDs []string) {
	if sectionName == "Labels" {
		cfg.Gmail.Labels = selectedIDs
	}
}

// Preview implements DiscoveryProvider. It returns up to limit recent email subjects
// matching the given label ID using the GetRecentSubjects method added to Service.
func (p *GmailProvider) Preview(labelID string, limit int) ([]string, error) {
	if p.svc == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	query := "label:" + labelID

	subjects, err := p.svc.GetRecentSubjects(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recent subjects for label %s: %w", labelID, err)
	}

	return subjects, nil
}

// RequiredFields implements DiscoveryProvider.
func (p *GmailProvider) RequiredFields() []RequiredField {
	return []RequiredField{
		{
			Key:         "name",
			Prompt:      "Gmail instance name",
			Placeholder: "Work Emails",
			Validate: func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("name is required")
				}

				return nil
			},
		},
	}
}

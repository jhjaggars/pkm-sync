package configure

import (
	"fmt"

	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/calendar"
	"pkm-sync/pkg/models"
)

// CalendarProvider implements DiscoveryProvider for Google Calendar.
type CalendarProvider struct {
	svc *calendar.Service
}

// SourceType implements DiscoveryProvider.
func (p *CalendarProvider) SourceType() string { return sourceTypeCalendar }

// Authenticate implements DiscoveryProvider. It obtains a Google OAuth client and
// initializes the Calendar service client.
func (p *CalendarProvider) Authenticate(_ *models.Config, _ string) error {
	httpClient, err := auth.GetClient()
	if err != nil {
		return fmt.Errorf(
			"failed to get Google OAuth client: %w\n\n"+
				"Run 'pkm-sync setup' to complete the OAuth flow",
			err,
		)
	}

	svc, err := calendar.NewService(httpClient)
	if err != nil {
		return fmt.Errorf("failed to create Calendar service: %w", err)
	}

	p.svc = svc

	return nil
}

// DiscoverySections implements DiscoveryProvider. It returns two sections:
//   - "Calendar" — all calendars the user has access to (single-selection semantics)
//   - "Options" — boolean flags: include_declined, include_private
func (p *CalendarProvider) DiscoverySections(currentConfig models.SourceConfig) ([]DiscoverySection, error) {
	if p.svc == nil {
		return nil, fmt.Errorf("not authenticated — call Authenticate first")
	}

	calendars, err := p.svc.ListCalendars()
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars: %w", err)
	}

	calendarOpts := make([]DiscoverableOption, 0, len(calendars))
	for _, cal := range calendars {
		name := cal.Summary
		if cal.Primary {
			name += " (primary)"
		}

		calendarOpts = append(calendarOpts, DiscoverableOption{
			ID:       cal.ID,
			Name:     name,
			Selected: currentConfig.Google.CalendarID == cal.ID,
		})
	}

	optionOpts := []DiscoverableOption{
		{
			ID:       "include_declined",
			Name:     "Include declined events",
			Selected: currentConfig.Google.IncludeDeclined,
		},
		{
			ID:       "include_private",
			Name:     "Include private events",
			Selected: currentConfig.Google.IncludePrivate,
		},
	}

	return []DiscoverySection{
		{
			Name:        "Calendar",
			Description: "Select the calendar to sync (choose one)",
			Options:     calendarOpts,
		},
		{
			Name:        "Options",
			Description: "Select additional event types to include",
			Options:     optionOpts,
		},
	}, nil
}

// ApplySelections implements DiscoveryProvider. It updates the SourceConfig in-place.
//
// For "Calendar", the first selected ID is used as CalendarID (single-select semantics).
// For "Options", selected strings are mapped to boolean flags.
func (p *CalendarProvider) ApplySelections(cfg *models.SourceConfig, sectionName string, selectedIDs []string) {
	switch sectionName {
	case "Calendar":
		if len(selectedIDs) > 0 {
			cfg.Google.CalendarID = selectedIDs[0]
		} else {
			cfg.Google.CalendarID = ""
		}
	case "Options":
		cfg.Google.IncludeDeclined = false
		cfg.Google.IncludePrivate = false

		for _, id := range selectedIDs {
			switch id {
			case "include_declined":
				cfg.Google.IncludeDeclined = true
			case "include_private":
				cfg.Google.IncludePrivate = true
			}
		}
	}
}

// Preview implements DiscoveryProvider. Calendar events are time-bound and not
// meaningful to preview out of context, so this returns nil without error.
func (p *CalendarProvider) Preview(_ string, _ int) ([]string, error) {
	return nil, nil
}

// RequiredFields implements DiscoveryProvider.
func (p *CalendarProvider) RequiredFields() []RequiredField {
	return []RequiredField{
		{Key: "calendar_id", Prompt: "Calendar ID", Placeholder: "primary"},
	}
}

package calendar

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type Service struct {
	calendarService          *calendar.Service
	attendeeAllowList        []string
	requireMultipleAttendees bool
	includeSelfOnlyEvents    bool
}

func NewService(client *http.Client) (*Service, error) {
	ctx := context.Background()

	calendarService, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Google Calendar service: %w. "+
			"Ensure credentials are valid and Calendar API is enabled", err)
	}

	return &Service{
		calendarService:          calendarService,
		requireMultipleAttendees: true,  // Default: filter out 0-1 attendee events
		includeSelfOnlyEvents:    false, // Default: don't include solo events
	}, nil
}

// SetAttendeeAllowList configures the allow list for attendee filtering.
func (s *Service) SetAttendeeAllowList(allowList []string) {
	s.attendeeAllowList = allowList
}

// SetRequireMultipleAttendees configures whether to require multiple attendees.
func (s *Service) SetRequireMultipleAttendees(require bool) {
	s.requireMultipleAttendees = require
}

// SetIncludeSelfOnlyEvents configures whether to include events where you're the only attendee.
func (s *Service) SetIncludeSelfOnlyEvents(include bool) {
	s.includeSelfOnlyEvents = include
}

// shouldIncludeEvent applies two-step filtering: 1) attendee allow list, 2) self-only rules.
func (s *Service) shouldIncludeEvent(event *calendar.Event) bool {
	// Step 1: Apply attendee allow list filtering
	if !s.passesAttendeeAllowListFilter(event) {
		return false
	}

	// Step 2: Apply self-only event filtering
	return s.passesSelfOnlyEventFilter(event)
}

// passesAttendeeAllowListFilter checks if event passes the attendee allow list filter.
func (s *Service) passesAttendeeAllowListFilter(event *calendar.Event) bool {
	// If no allow list is configured, all events pass this filter
	if len(s.attendeeAllowList) == 0 {
		return true
	}

	// Check if at least one attendee matches the allow list
	for _, attendee := range event.Attendees {
		if attendee.Email != "" {
			attendeeEmail := strings.ToLower(strings.TrimSpace(attendee.Email))
			for _, allowedEmail := range s.attendeeAllowList {
				if strings.ToLower(strings.TrimSpace(allowedEmail)) == attendeeEmail {
					return true // Found at least one matching attendee
				}
			}
		}
	}

	// No attendees matched the allow list
	return false
}

// passesSelfOnlyEventFilter checks if event passes the self-only event filter.
func (s *Service) passesSelfOnlyEventFilter(event *calendar.Event) bool {
	// If we don't require multiple attendees, all events pass this filter
	if !s.requireMultipleAttendees {
		return true
	}

	totalAttendeeCount := len(event.Attendees)

	// Events with 0 or 1 attendees are considered "self-only" events
	if totalAttendeeCount <= 1 {
		// If includeSelfOnlyEvents is true, include these events
		return s.includeSelfOnlyEvents
	}

	// Events with 2+ attendees always pass (these are meetings with others)
	return true
}

// filterEvents applies the attendee allow list filter to a slice of events.
func (s *Service) filterEvents(events []*calendar.Event) []*calendar.Event {
	// Always apply filtering, even if allow list is empty (for attendee count filtering)
	var filteredEvents []*calendar.Event

	for _, event := range events {
		if s.shouldIncludeEvent(event) {
			filteredEvents = append(filteredEvents, event)
		}
	}

	return filteredEvents
}

func (s *Service) GetUpcomingEvents(maxResults int64) ([]*calendar.Event, error) {
	t := time.Now().Format(time.RFC3339)

	events, err := s.calendarService.Events.List("primary").
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(t).
		MaxResults(maxResults).
		OrderBy("startTime").
		Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve events: %w", err)
	}

	return s.filterEvents(events.Items), nil
}

func (s *Service) GetEventsInRange(start, end time.Time, maxResults int64) ([]*calendar.Event, error) {
	startTime := start.Format(time.RFC3339)
	endTime := end.Format(time.RFC3339)

	events, err := s.calendarService.Events.List("primary").
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(startTime).
		TimeMax(endTime).
		MaxResults(maxResults).
		OrderBy("startTime").
		Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve events in range: %w", err)
	}

	return s.filterEvents(events.Items), nil
}

func (s *Service) ConvertToModel(event *calendar.Event) *models.CalendarEvent {
	modelEvent := &models.CalendarEvent{
		ID:          event.Id,
		Summary:     event.Summary,
		Description: event.Description,
		Location:    event.Location,
	}

	if event.Start.DateTime != "" {
		if startTime, err := time.Parse(time.RFC3339, event.Start.DateTime); err == nil {
			modelEvent.Start = startTime
		}
	}

	if event.End.DateTime != "" {
		if endTime, err := time.Parse(time.RFC3339, event.End.DateTime); err == nil {
			modelEvent.End = endTime
		}
	}

	for _, attendee := range event.Attendees {
		if attendee.Email != "" {
			modelAttendee := models.Attendee{
				Email:          attendee.Email,
				DisplayName:    attendee.DisplayName,
				ResponseStatus: attendee.ResponseStatus,
				Self:           attendee.Self,
			}
			modelEvent.Attendees = append(modelEvent.Attendees, modelAttendee)

			if attendee.Self {
				modelEvent.MyResponseStatus = attendee.ResponseStatus
			}
		}
	}

	if event.ConferenceData != nil && len(event.ConferenceData.EntryPoints) > 0 {
		for _, entryPoint := range event.ConferenceData.EntryPoints {
			if entryPoint.EntryPointType == "video" && entryPoint.Uri != "" {
				modelEvent.MeetingURL = entryPoint.Uri

				break
			}
		}
	}

	// Process native Calendar API attachments
	for _, attachment := range event.Attachments {
		calAttachment := models.CalendarAttachment{
			FileURL:  attachment.FileUrl,
			FileID:   attachment.FileId,
			Title:    attachment.Title,
			MimeType: attachment.MimeType,
			IconLink: attachment.IconLink,
		}
		modelEvent.Attachments = append(modelEvent.Attachments, calAttachment)
	}

	return modelEvent
}

// ConvertToModelWithDrive converts a calendar event to a model with drive file attachments populated.
func (s *Service) ConvertToModelWithDrive(event *calendar.Event) *models.CalendarEvent {
	// Now that we use native Calendar API attachments, just use the base conversion
	return s.ConvertToModel(event)
}

// DriveServiceInterface defines the interface for drive service operations needed by calendar.

// CalendarInfo holds basic calendar metadata for discovery.
type CalendarInfo struct {
	ID      string
	Summary string // human-readable name
	Primary bool
}

// ListCalendars returns all calendars the authenticated user has access to.
func (s *Service) ListCalendars() ([]*CalendarInfo, error) {
	resp, err := s.calendarService.CalendarList.List().Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars: %w", err)
	}

	calendars := make([]*CalendarInfo, 0, len(resp.Items))
	for _, item := range resp.Items {
		calendars = append(calendars, &CalendarInfo{
			ID:      item.Id,
			Summary: item.Summary,
			Primary: item.Primary,
		})
	}

	return calendars, nil
}

package google

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/calendar"
	"pkm-sync/internal/sources/google/drive"
	"pkm-sync/internal/sources/google/gmail"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

const (
	SourceTypeGoogle   = "google"
	SourceTypeGmail    = "gmail"
	SourceTypeCalendar = "google_calendar"
)

type GoogleSource struct {
	calendarService *calendar.Service
	driveService    *drive.Service
	gmailService    *gmail.Service
	httpClient      *http.Client
	config          models.SourceConfig
	sourceID        string
}

func NewGoogleSource() *GoogleSource {
	return &GoogleSource{}
}

func NewGoogleSourceWithConfig(sourceID string, config models.SourceConfig) *GoogleSource {
	return &GoogleSource{
		sourceID: sourceID,
		config:   config,
	}
}

func (g *GoogleSource) Name() string {
	if g.sourceID != "" {
		return g.sourceID
	}

	if g.config.Type == SourceTypeGmail {
		return SourceTypeGmail
	}

	return SourceTypeCalendar
}

func (g *GoogleSource) Configure(config map[string]interface{}, client *http.Client) error {
	var err error
	if client == nil {
		// Use existing auth logic if no client is provided
		client, err = auth.GetClient()
		if err != nil {
			return fmt.Errorf("failed to get authenticated client: %w", err)
		}
	}

	g.httpClient = client

	// Initialize services based on source type
	if g.config.Type == SourceTypeGmail {
		return g.initializeGmailService(client)
	}

	// Default to calendar and drive services
	return g.initializeCalendarAndDriveServices(client, config)
}

// initializeGmailService initializes the Gmail service for Gmail sources.
func (g *GoogleSource) initializeGmailService(client *http.Client) error {
	var err error

	g.gmailService, err = gmail.NewService(client, g.config.Gmail, g.sourceID)
	if err != nil {
		return fmt.Errorf("failed to initialize Gmail service: %w", err)
	}

	return nil
}

// initializeCalendarAndDriveServices initializes calendar and drive services for non-Gmail sources.
func (g *GoogleSource) initializeCalendarAndDriveServices(client *http.Client, config map[string]interface{}) error {
	var err error

	// Initialize calendar service
	g.calendarService, err = calendar.NewService(client)
	if err != nil {
		return fmt.Errorf("failed to initialize Calendar service: %w", err)
	}

	// Configure calendar service options
	g.configureCalendarService(config)

	// Initialize drive service
	g.driveService, err = drive.NewService(client)
	if err != nil {
		return fmt.Errorf("failed to initialize Drive service: %w", err)
	}

	return nil
}

// configureCalendarService applies configuration settings to the calendar service.
func (g *GoogleSource) configureCalendarService(config map[string]interface{}) {
	// Configure attendee allow list if provided
	if allowListInterface, exists := config["attendee_allow_list"]; exists {
		if allowList, ok := allowListInterface.([]interface{}); ok {
			var stringAllowList []string

			for _, item := range allowList {
				if emailStr, ok := item.(string); ok {
					stringAllowList = append(stringAllowList, emailStr)
				}
			}

			g.calendarService.SetAttendeeAllowList(stringAllowList)
		}
	}

	// Configure attendee count filtering options
	if requireMultiple, exists := config["require_multiple_attendees"]; exists {
		if requireBool, ok := requireMultiple.(bool); ok {
			g.calendarService.SetRequireMultipleAttendees(requireBool)
		}
	}

	if includeSelfOnly, exists := config["include_self_only_events"]; exists {
		if includeBool, ok := includeSelfOnly.(bool); ok {
			g.calendarService.SetIncludeSelfOnlyEvents(includeBool)
		}
	}
}

func (g *GoogleSource) Fetch(since time.Time, limit int) ([]models.ItemInterface, error) {
	if g.config.Type == SourceTypeGmail {
		return g.fetchGmail(since, limit)
	}

	// Default: Handle Google Calendar sources
	return g.fetchCalendar(since, limit)
}

func (g *GoogleSource) fetchGmail(since time.Time, limit int) ([]models.ItemInterface, error) {
	if g.gmailService == nil {
		slog.Error("Gmail service not initialized", "source_id", g.sourceID)

		return nil, fmt.Errorf("gmail service not initialized")
	}

	// Get threads using the new Threads API
	query := g.gmailService.BuildQuery(since)
	slog.Info("Fetching Gmail threads",
		"source_id", g.sourceID,
		"query", query,
		"since", since.Format("2006-01-02"),
		"limit", limit)

	threads, err := g.gmailService.GetThreads(query, limit)
	if err != nil {
		slog.Error("Failed to fetch Gmail threads",
			"source_id", g.sourceID,
			"query", query,
			"error", err)

		return nil, fmt.Errorf("failed to fetch Gmail threads: %w", err)
	}

	slog.Info("Gmail threads retrieved successfully",
		"source_id", g.sourceID,
		"threads_count", len(threads))

	items := make([]models.ItemInterface, 0, len(threads))

	var conversionErrors []error

	for i, thread := range threads {
		// Convert thread directly to Item using new converter
		threadItem, err := gmail.FromGmailThread(thread, g.config.Gmail, g.gmailService)
		if err != nil {
			slog.Warn("Failed to convert Gmail thread to item",
				"source_id", g.sourceID,
				"thread_id", thread.Id,
				"thread_index", i,
				"error", err)
			conversionErrors = append(conversionErrors, err)

			continue // Skip this thread but continue with others
		}

		// Convert to ItemInterface
		item := models.AsItemInterface(threadItem)
		items = append(items, item)
	}

	if len(conversionErrors) > 0 {
		slog.Warn("Some Gmail threads could not be converted",
			"source_id", g.sourceID,
			"total_threads", len(threads),
			"successful_conversions", len(items),
			"failed_conversions", len(conversionErrors))

		// Continue with successful conversions rather than failing completely
		// This provides better resilience in production
	}

	slog.Info("Gmail thread processing completed",
		"source_id", g.sourceID,
		"items_created", len(items),
		"conversion_errors", len(conversionErrors))

	return items, nil
}

func (g *GoogleSource) fetchCalendar(since time.Time, limit int) ([]models.ItemInterface, error) {
	if g.calendarService == nil {
		return nil, fmt.Errorf("calendar service not initialized")
	}

	events, err := g.calendarService.GetEventsInRange(since, time.Now().AddDate(0, 1, 0), int64(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch calendar events: %w", err)
	}

	items := make([]models.ItemInterface, 0, len(events))

	for _, event := range events {
		// Convert API event to model, then to legacy item, then to interface
		calEvent := g.calendarService.ConvertToModelWithDrive(event)
		legacyItem := models.FromCalendarEvent(calEvent)
		item := models.AsItemInterface(legacyItem)
		items = append(items, item)
	}

	return items, nil
}

func (g *GoogleSource) SupportsRealtime() bool {
	return false // Future: implement webhooks
}

// Ensure GoogleSource implements Source interface.
var _ interfaces.Source = (*GoogleSource)(nil)

package google

import (
	"fmt"
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
	SourceTypeDrive    = "google_drive"
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

	switch g.config.Type {
	case SourceTypeGmail:
		return SourceTypeGmail
	case SourceTypeDrive:
		return SourceTypeDrive
	default:
		return SourceTypeCalendar
	}
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
	switch g.config.Type {
	case SourceTypeGmail:
		return g.initializeGmailService(client)
	case SourceTypeDrive:
		return g.initializeDriveOnlyService(client)
	default:
		// Default to calendar and drive services
		return g.initializeCalendarAndDriveServices(client, config)
	}
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

func (g *GoogleSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	switch g.config.Type {
	case SourceTypeGmail:
		return g.fetchGmail(since, limit)
	case SourceTypeDrive:
		return g.fetchDrive(since, limit)
	default:
		return g.fetchCalendar(since, limit)
	}
}

func (g *GoogleSource) fetchGmail(since time.Time, limit int) ([]models.FullItem, error) {
	if g.gmailService == nil {
		return nil, fmt.Errorf("gmail service not initialized")
	}

	// Use Threads API when thread grouping is enabled for native thread fetching.
	if g.config.Gmail.IncludeThreads {
		return g.fetchGmailThreads(since, limit)
	}

	return g.fetchGmailMessages(since, limit)
}

// fetchGmailMessages fetches individual messages using the Messages API.
func (g *GoogleSource) fetchGmailMessages(since time.Time, limit int) ([]models.FullItem, error) {
	messages, err := g.gmailService.GetMessages(since, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Gmail messages: %w", err)
	}

	items := make([]models.FullItem, 0, len(messages))

	for _, message := range messages {
		legacyItem, err := gmail.FromGmailMessageWithService(message, g.config.Gmail, g.gmailService)
		if err != nil {
			return nil, fmt.Errorf("failed to convert Gmail message to item: %w", err)
		}

		items = append(items, models.AsFullItem(legacyItem))
	}

	return items, nil
}

// fetchGmailThreads fetches complete threads using the Threads API.
func (g *GoogleSource) fetchGmailThreads(since time.Time, limit int) ([]models.FullItem, error) {
	threads, err := g.gmailService.GetThreads(since, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Gmail threads: %w", err)
	}

	items := make([]models.FullItem, 0, len(threads))

	for _, thread := range threads {
		legacyItem, err := gmail.FromGmailThread(thread, g.config.Gmail, g.gmailService)
		if err != nil {
			return nil, fmt.Errorf("failed to convert Gmail thread to item: %w", err)
		}

		items = append(items, models.AsFullItem(legacyItem))
	}

	return items, nil
}

func (g *GoogleSource) fetchCalendar(since time.Time, limit int) ([]models.FullItem, error) {
	if g.calendarService == nil {
		return nil, fmt.Errorf("calendar service not initialized")
	}

	events, err := g.calendarService.GetEventsInRange(since, time.Now().AddDate(0, 1, 0), int64(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch calendar events: %w", err)
	}

	items := make([]models.FullItem, 0, len(events))

	for _, event := range events {
		// Convert API event to model, then to legacy item, then to interface
		calEvent := g.calendarService.ConvertToModelWithDrive(event)
		legacyItem := models.FromCalendarEvent(calEvent)
		item := models.AsFullItem(legacyItem)
		items = append(items, item)
	}

	return items, nil
}

func (g *GoogleSource) SupportsRealtime() bool {
	return false // Future: implement webhooks
}

// initializeDriveOnlyService initializes only the Drive service for Drive sources.
func (g *GoogleSource) initializeDriveOnlyService(client *http.Client) error {
	var err error

	g.driveService, err = drive.NewService(client)
	if err != nil {
		return fmt.Errorf("failed to initialize Drive service: %w", err)
	}

	return nil
}

// fetchDrive fetches Google Drive documents as items.
func (g *GoogleSource) fetchDrive(since time.Time, limit int) ([]models.FullItem, error) {
	if g.driveService == nil {
		return nil, fmt.Errorf("drive service not initialized")
	}

	cfg := g.config.Drive

	// Build MIME type filter from configured workspace types
	var mimeTypes []string

	if len(cfg.WorkspaceTypes) > 0 {
		for _, wt := range cfg.WorkspaceTypes {
			switch wt {
			case "document":
				mimeTypes = append(mimeTypes, drive.MimeTypeGoogleDoc)
			case "spreadsheet":
				mimeTypes = append(mimeTypes, drive.MimeTypeGoogleSheet)
			case "presentation":
				mimeTypes = append(mimeTypes, drive.MimeTypeGooglePresentation)
			}
		}
	} else {
		// Default: all workspace types
		mimeTypes = []string{
			drive.MimeTypeGoogleDoc,
			drive.MimeTypeGoogleSheet,
			drive.MimeTypeGooglePresentation,
		}
	}

	listOpts := drive.ListFilesOptions{
		MimeTypes:           mimeTypes,
		ModifiedAfter:       since,
		ExtraQuery:          cfg.Query,
		IncludeSharedDrives: cfg.IncludeSharedDrives,
	}

	if limit > 0 {
		listOpts.MaxResults = limit
	}

	// Collect files, deduplicating across folders
	seen := make(map[string]bool)

	var allFiles []*drive.DriveFileInfo

	folderIDs := cfg.FolderIDs
	if len(folderIDs) == 0 {
		folderIDs = []string{"root"}
	}

	for _, folderID := range folderIDs {
		files, err := g.driveService.ListFilesInFolder(folderID, since, cfg.Recursive, listOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to list files in folder %s: %w", folderID, err)
		}

		for _, f := range files {
			if !seen[f.ID] {
				seen[f.ID] = true
				allFiles = append(allFiles, f)
			}
		}
	}

	if cfg.IncludeSharedWithMe {
		sharedFiles, err := g.driveService.ListSharedWithMe(since, listOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to list shared-with-me files: %w", err)
		}

		for _, f := range sharedFiles {
			if !seen[f.ID] {
				seen[f.ID] = true
				allFiles = append(allFiles, f)
			}
		}
	}

	// Apply limit after deduplication
	if limit > 0 && len(allFiles) > limit {
		allFiles = allFiles[:limit]
	}

	items := make([]models.FullItem, 0, len(allFiles))

	for _, f := range allFiles {
		item, err := g.convertDriveFile(f, cfg)
		if err != nil {
			fmt.Printf("Warning: failed to convert drive file '%s': %v\n", f.Name, err)

			continue
		}

		items = append(items, item)
	}

	return items, nil
}

// convertDriveFile converts a DriveFileInfo to a models.FullItem.
func (g *GoogleSource) convertDriveFile(
	file *drive.DriveFileInfo,
	cfg models.DriveSourceConfig,
) (models.FullItem, error) {
	// Determine export format based on file type
	var format string

	switch file.MimeType {
	case drive.MimeTypeGoogleDoc:
		format = cfg.DocExportFormat
		if format == "" {
			format = drive.FormatMD
		}
	case drive.MimeTypeGoogleSheet:
		format = cfg.SheetExportFormat
		if format == "" {
			format = drive.FormatCSV
		}
	case drive.MimeTypeGooglePresentation:
		format = cfg.SlideExportFormat
		if format == "" {
			format = drive.FormatTXT
		}
	default:
		return nil, fmt.Errorf("unsupported MIME type for export: %s", file.MimeType)
	}

	exportMimeType, err := drive.GetExportMimeType(file.MimeType, format)
	if err != nil {
		return nil, err
	}

	convertToMarkdown := (format == drive.FormatMD)

	content, err := g.driveService.ExportAsString(file.ID, exportMimeType, convertToMarkdown)
	if err != nil {
		return nil, fmt.Errorf("failed to export file '%s': %w", file.Name, err)
	}

	// Map MIME type to item type
	var itemType string

	switch file.MimeType {
	case drive.MimeTypeGoogleDoc:
		itemType = "document"
	case drive.MimeTypeGoogleSheet:
		itemType = "spreadsheet"
	case drive.MimeTypeGooglePresentation:
		itemType = "presentation"
	}

	metadata := map[string]interface{}{
		"mime_type":     file.MimeType,
		"web_view_link": file.WebViewLink,
		"owners":        file.Owners,
		"starred":       file.Starred,
	}

	var links []models.Link

	if file.WebViewLink != "" {
		links = append(links, models.Link{
			URL:   file.WebViewLink,
			Title: "View in Drive",
			Type:  "document",
		})
	}

	item := &models.BasicItem{
		ID:         file.ID,
		Title:      file.Name,
		Content:    content,
		SourceType: SourceTypeDrive,
		ItemType:   itemType,
		CreatedAt:  file.CreatedTime,
		UpdatedAt:  file.ModifiedTime,
		Tags:       []string{},
		Metadata:   metadata,
		Links:      links,
	}

	return item, nil
}

// Ensure GoogleSource implements Source interface.
var _ interfaces.Source = (*GoogleSource)(nil)

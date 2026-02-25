package transform

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"pkm-sync/internal/sync"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// MockSource implements interfaces.Source for testing pipeline integration.
type MockSource struct {
	items []models.FullItem
}

func (m *MockSource) Name() string {
	return "mock_source"
}

func (m *MockSource) Configure(config map[string]interface{}, client *http.Client) error {
	return nil
}

func (m *MockSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	return m.items, nil
}

func (m *MockSource) SupportsRealtime() bool {
	return false
}

// MockSink captures written items for assertion.
type MockSink struct {
	writtenItems []models.FullItem
}

func (m *MockSink) Name() string {
	return "mock_sink"
}

func (m *MockSink) Write(_ context.Context, items []models.FullItem) error {
	m.writtenItems = items

	return nil
}

// Ensure MockSink implements Sink.
var _ interfaces.Sink = (*MockSink)(nil)

// TestPipelineIntegrationWithSyncEngine tests the complete flow from source -> pipeline -> sink.
func TestPipelineIntegrationWithSyncEngine(t *testing.T) {
	// Create test items with content that will trigger transformations
	testItems := []models.FullItem{
		func() models.FullItem {
			item := models.NewBasicItem("1", "  Re: Important Meeting  ")
			item.SetContent("  This is about a meeting\n\n\n\nwith urgent details  ")
			item.SetSourceType("test_source")
			item.SetItemType("email")
			item.SetTags([]string{"existing"})

			return item
		}(),
		func() models.FullItem {
			item := models.NewBasicItem("2", "Short note")
			item.SetContent("Too short")
			item.SetSourceType("test_source")
			item.SetItemType("note")
			item.SetTags([]string{})

			return item
		}(),
	}

	// Create mock source and sink
	source := &MockSource{items: testItems}
	sink := &MockSink{}

	// Create and configure the transform pipeline
	pipeline := NewPipeline()

	// Register transformers
	contentCleanup := NewContentCleanupTransformer()
	autoTagging := NewAutoTaggingTransformer()
	filter := NewFilterTransformer()

	pipeline.AddTransformer(contentCleanup)
	pipeline.AddTransformer(autoTagging)
	pipeline.AddTransformer(filter)

	// Configure the pipeline
	transformCfg := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"content_cleanup", "auto_tagging", "filter"},
		ErrorStrategy: "log_and_continue",
		Transformers: map[string]map[string]interface{}{
			"auto_tagging": {
				"rules": []interface{}{
					map[string]interface{}{
						"pattern": "meeting",
						"tags":    []interface{}{"work", "meeting"},
					},
					map[string]interface{}{
						"pattern": "urgent",
						"tags":    []interface{}{"priority"},
					},
				},
			},
			"filter": {
				"min_content_length": 15,
			},
		},
	}

	err := pipeline.Configure(transformCfg)
	if err != nil {
		t.Fatalf("Failed to configure pipeline: %v", err)
	}

	// Create multi-syncer with pipeline and run sync
	ms := sync.NewMultiSyncer(pipeline)

	result, err := ms.SyncAll(
		context.Background(),
		[]sync.SourceEntry{{Name: "test_source", Src: source}},
		[]interfaces.Sink{sink},
		sync.MultiSyncOptions{
			DefaultSince: time.Now().Add(-24 * time.Hour),
			TransformCfg: transformCfg,
		},
	)
	if err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	// Verify the results
	if len(sink.writtenItems) != 1 {
		t.Fatalf("Expected 1 item to be written after filtering, got %d", len(sink.writtenItems))
	}

	_ = result // result.Items also has the filtered list

	exportedItem := sink.writtenItems[0]

	// Verify content cleanup worked
	if exportedItem.GetTitle() != "Important Meeting" {
		t.Errorf("Expected cleaned title 'Important Meeting', got '%s'", exportedItem.GetTitle())
	}

	expectedContent := "This is about a meeting\n\nwith urgent details"
	if exportedItem.GetContent() != expectedContent {
		t.Errorf("Expected cleaned content '%s', got '%s'", expectedContent, exportedItem.GetContent())
	}

	// Verify auto-tagging worked
	tagMap := make(map[string]bool)
	for _, tag := range exportedItem.GetTags() {
		tagMap[tag] = true
	}

	expectedTags := []string{"existing", "work", "meeting", "source:test_source", "type:email"}
	for _, expectedTag := range expectedTags {
		if !tagMap[expectedTag] {
			t.Errorf("Missing expected tag: %s", expectedTag)
		}
	}

	// Verify filter worked (second item should be filtered out due to short content)
	if len(sink.writtenItems) > 1 {
		t.Error("Filter should have removed the short content item")
	}
}

// TestPipelineIntegrationErrorHandling tests that error handling works correctly in the sync engine.
func TestPipelineIntegrationErrorHandling(t *testing.T) {
	testItems := []models.FullItem{
		func() models.FullItem {
			item := models.NewBasicItem("1", "Test Item")
			item.SetContent("Test content")
			item.SetSourceType("test_source")
			item.SetItemType("email")
			item.SetTags([]string{})

			return item
		}(),
	}

	source := &MockSource{items: testItems}
	sink := &MockSink{}

	// Create pipeline with a failing transformer
	pipeline := NewPipeline()
	failingTransformer := &MockTransformer{name: "failing_transformer", shouldFail: true}
	workingTransformer := &MockTransformer{name: "working_transformer"}

	pipeline.AddTransformer(failingTransformer)
	pipeline.AddTransformer(workingTransformer)

	transformCfg := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"failing_transformer", "working_transformer"},
		ErrorStrategy: "log_and_continue",
		Transformers:  make(map[string]map[string]interface{}),
	}

	err := pipeline.Configure(transformCfg)
	if err != nil {
		t.Fatalf("Failed to configure pipeline: %v", err)
	}

	ms := sync.NewMultiSyncer(pipeline)

	// This should not fail despite the failing transformer
	_, err = ms.SyncAll(
		context.Background(),
		[]sync.SourceEntry{{Name: "test_source", Src: source}},
		[]interfaces.Sink{sink},
		sync.MultiSyncOptions{
			DefaultSince: time.Now().Add(-24 * time.Hour),
			TransformCfg: transformCfg,
		},
	)
	if err != nil {
		t.Fatalf("SyncAll should not fail with log_and_continue strategy: %v", err)
	}

	// Verify items were still written
	if len(sink.writtenItems) != 1 {
		t.Fatalf("Expected 1 item to be written, got %d", len(sink.writtenItems))
	}

	// Verify the working transformer processed the item
	writtenItem := sink.writtenItems[0]
	hasWorkingTag := false
	hasFailingTag := false

	for _, tag := range writtenItem.GetTags() {
		if tag == "transformed_by_working_transformer" {
			hasWorkingTag = true
		}

		if tag == "transformed_by_failing_transformer" {
			hasFailingTag = true
		}
	}

	if !hasWorkingTag {
		t.Error("Working transformer should have processed the item")
	}

	if hasFailingTag {
		t.Error("Failing transformer should not have tagged the item")
	}
}

// TestGmailTransformerIntegration tests the integration between Gmail source and transformers.
func TestGmailTransformerIntegration(t *testing.T) {
	// Create test items that simulate Gmail output
	gmailItems := []*models.Item{
		{
			ID:      "gmail_1",
			Title:   "Re: Fwd: Important Project Discussion",
			Content: "<h1>Meeting Notes</h1><p>Please review <a href=\"https://example.com/doc\">this document</a>.</p><p>Check out: https://company.com/wiki</p><p>--</p><p>Best regards,<br>John Doe<br>john@company.com</p>",
			Metadata: map[string]interface{}{
				"thread_id": "thread123",
				"from":      "john@company.com",
			},
		},
		{
			ID:      "gmail_2",
			Title:   "Re: Important Project Discussion",
			Content: "<p>Thanks for sharing. I agree with the proposal completely.</p><p>Let me know when you want to schedule the next review session.</p>",
			Metadata: map[string]interface{}{
				"thread_id": "thread123",
				"from":      "jane@company.com",
			},
		},
		{
			ID:      "gmail_3",
			Title:   "Separate Email",
			Content: "<p>This is a separate discussion.</p>",
			Metadata: map[string]interface{}{
				"thread_id": "thread456",
				"from":      "alice@company.com",
			},
		},
	}

	// Create pipeline with Gmail-specific transformers
	pipeline := NewPipeline()

	// Content cleanup (HTML to Markdown)
	contentCleanup := NewContentCleanupTransformer()
	pipeline.AddTransformer(contentCleanup)

	// Signature removal
	signatureRemoval := NewSignatureRemovalTransformer()
	pipeline.AddTransformer(signatureRemoval)

	// Link extraction
	linkExtraction := NewLinkExtractionTransformer()
	pipeline.AddTransformer(linkExtraction)

	// Thread grouping
	threadGrouping := NewThreadGroupingTransformer()
	pipeline.AddTransformer(threadGrouping)

	// Configure the pipeline
	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"content_cleanup", "signature_removal", "link_extraction", "thread_grouping"},
		ErrorStrategy: "log_and_continue",
		Transformers: map[string]map[string]interface{}{
			"content_cleanup": {
				"html_to_markdown":        true,
				"strip_quoted_text":       true,
				"remove_extra_whitespace": true,
			},
			"signature_removal": {
				"max_signature_lines": 5,
				"trim_empty_lines":    true,
			},
			"link_extraction": {
				"extract_markdown_links": true,
				"extract_plain_urls":     true,
				"deduplicate_links":      true,
			},
			"thread_grouping": {
				"enabled": true,
				"mode":    "consolidated",
			},
		},
	}

	err := pipeline.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure pipeline: %v", err)
	}

	// Apply transformations
	// Convert legacy items to FullItem
	interfaceItems := make([]models.FullItem, len(gmailItems))
	for i, item := range gmailItems {
		interfaceItems[i] = models.AsFullItem(item)
	}

	result, err := pipeline.Transform(interfaceItems)
	if err != nil {
		t.Fatalf("Pipeline transformation failed: %v", err)
	}

	// Verify results
	if len(result) != 2 {
		t.Fatalf("Expected 2 items (1 consolidated thread + 1 individual), got %d", len(result))
	}

	// Find the consolidated thread
	var consolidated models.FullItem

	var individual models.FullItem

	for _, item := range result {
		if strings.Contains(item.GetTitle(), "Thread_") {
			consolidated = item
		} else {
			individual = item
		}
	}

	if consolidated == nil {
		t.Fatal("No consolidated thread found")
	}

	if individual == nil {
		t.Fatal("No individual item found")
	}

	// Debug: Print consolidated content and links
	t.Logf("Consolidated content: %s", consolidated.GetContent())
	t.Logf("Consolidated links count: %d", len(consolidated.GetLinks()))

	for i, link := range consolidated.GetLinks() {
		t.Logf("  Link %d: %s", i, link.URL)
	}

	// Verify consolidated thread properties
	if !strings.Contains(consolidated.GetTitle(), "Important-Project-Discussion") {
		t.Errorf("Expected consolidated title to contain 'Important-Project-Discussion', got: %s", consolidated.GetTitle())
	}

	if !strings.Contains(consolidated.GetContent(), "Meeting Notes") {
		t.Errorf("Expected content to contain 'Meeting Notes'")
	}

	if !strings.Contains(consolidated.GetContent(), "I agree with the proposal completely") {
		t.Errorf("Expected content to contain second email content")
	}

	// Verify signatures were removed
	if strings.Contains(consolidated.GetContent(), "Best regards") {
		t.Errorf("Expected signatures to be removed from consolidated content")
	}

	// Verify HTML was converted to markdown
	if strings.Contains(consolidated.GetContent(), "<h1>") {
		t.Errorf("Expected HTML to be converted to markdown")
	}

	if !strings.Contains(consolidated.GetContent(), "# Meeting Notes") {
		t.Errorf("Expected H1 to be converted to markdown header")
	}

	// Verify links were extracted
	if len(consolidated.GetLinks()) == 0 {
		t.Errorf("Expected links to be extracted")
	}

	// Check for specific URLs
	foundExampleURL := false
	foundCompanyURL := false

	for _, link := range consolidated.GetLinks() {
		if link.URL == "https://example.com/doc" {
			foundExampleURL = true
		}

		if link.URL == "https://company.com/wiki" {
			foundCompanyURL = true
		}
	}

	if !foundExampleURL {
		t.Errorf("Expected to find example.com URL in links")
	}

	if !foundCompanyURL {
		t.Errorf("Expected to find company.com URL in links")
	}

	// Verify individual email
	if individual.GetTitle() != "Separate Email" {
		t.Errorf("Expected individual title 'Separate Email', got: %s", individual.GetTitle())
	}

	if strings.Contains(individual.GetContent(), "<p>") {
		t.Errorf("Expected HTML to be converted in individual item too")
	}
}

// TestTransformersWithCalendarItems tests that transformers work with Google Calendar items.
func TestTransformersWithCalendarItems(t *testing.T) {
	// Create test calendar items
	calendarItems := []*models.Item{
		{
			ID:      "cal_1",
			Title:   "Weekly Team Meeting",
			Content: "Discuss project progress and upcoming deadlines. Meeting location: https://meet.google.com/abc-def-ghi",
			Metadata: map[string]interface{}{
				"event_id":   "event123",
				"location":   "Conference Room A",
				"attendees":  []string{"alice@company.com", "bob@company.com"},
				"start_time": time.Now(),
				"end_time":   time.Now().Add(1 * time.Hour),
			},
			SourceType: "google_calendar",
			ItemType:   "event",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		{
			ID:      "cal_2",
			Title:   "One-on-One with Manager",
			Content: "Career development discussion. Please review performance goals at https://docs.google.com/document/xyz",
			Metadata: map[string]interface{}{
				"event_id":   "event456",
				"attendees":  []string{"employee@company.com", "manager@company.com"},
				"start_time": time.Now().Add(2 * time.Hour),
				"end_time":   time.Now().Add(3 * time.Hour),
			},
			SourceType: "google_calendar",
			ItemType:   "event",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
	}

	// Test content cleanup transformer
	contentCleanup := NewContentCleanupTransformer()
	contentCleanup.Configure(map[string]interface{}{
		"remove_extra_whitespace": true,
	})

	// Convert to FullItem
	interfaceItems := make([]models.FullItem, len(calendarItems))
	for i, item := range calendarItems {
		interfaceItems[i] = models.AsFullItem(item)
	}

	result, err := contentCleanup.Transform(interfaceItems)
	if err != nil {
		t.Fatalf("Content cleanup failed with calendar items: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 items after content cleanup, got %d", len(result))
	}

	// Test link extraction transformer
	linkExtraction := NewLinkExtractionTransformer()
	linkExtraction.Configure(map[string]interface{}{
		"extract_plain_urls": true,
	})

	result, err = linkExtraction.Transform(result)
	if err != nil {
		t.Fatalf("Link extraction failed with calendar items: %v", err)
	}

	// Verify links were extracted
	totalLinks := 0
	for _, item := range result {
		totalLinks += len(item.GetLinks())
	}

	if totalLinks == 0 {
		t.Errorf("Expected links to be extracted from calendar items")
	}

	// Find meet.google.com and docs.google.com links
	foundMeetLink := false
	foundDocsLink := false

	for _, item := range result {
		for _, link := range item.GetLinks() {
			if strings.Contains(link.URL, "meet.google.com") {
				foundMeetLink = true
			}

			if strings.Contains(link.URL, "docs.google.com") {
				foundDocsLink = true
			}
		}
	}

	if !foundMeetLink {
		t.Errorf("Expected to find Google Meet link")
	}

	if !foundDocsLink {
		t.Errorf("Expected to find Google Docs link")
	}
}

// TestTransformersWithDriveItems tests that transformers work with Google Drive items.
func TestTransformersWithDriveItems(t *testing.T) {
	// Create test drive items
	driveItems := []*models.Item{
		{
			ID:      "drive_1",
			Title:   "Project Proposal Draft",
			Content: "# Project Overview\n\nThis document outlines the proposed project. For more details, see: https://drive.google.com/file/d/123/view",
			Metadata: map[string]interface{}{
				"file_id":     "file123",
				"mime_type":   "application/vnd.google-apps.document",
				"size":        1024,
				"modified_by": "author@company.com",
			},
			SourceType: "google_drive",
			ItemType:   "document",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		{
			ID:      "drive_2",
			Title:   "Meeting Notes - Q4 Planning",
			Content: "## Attendees\n- Alice Smith\n- Bob Johnson\n\n## Action Items\n- Review budget: https://sheets.google.com/d/456/edit\n- Update timeline\n\n--\nBest regards,\nTeam Lead",
			Metadata: map[string]interface{}{
				"file_id":     "file456",
				"mime_type":   "application/vnd.google-apps.document",
				"size":        2048,
				"modified_by": "lead@company.com",
			},
			SourceType: "google_drive",
			ItemType:   "document",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
	}

	// Test signature removal with Drive items (should handle "Best regards" at end)
	signatureRemoval := NewSignatureRemovalTransformer()
	signatureRemoval.Configure(map[string]interface{}{
		"max_signature_lines": 3,
	})

	// Convert to FullItem
	interfaceItems := make([]models.FullItem, len(driveItems))
	for i, item := range driveItems {
		interfaceItems[i] = models.AsFullItem(item)
	}

	result, err := signatureRemoval.Transform(interfaceItems)
	if err != nil {
		t.Fatalf("Signature removal failed with drive items: %v", err)
	}

	// Check that signature was removed from second item
	if strings.Contains(result[1].GetContent(), "Best regards") {
		t.Errorf("Expected signature to be removed from Drive document")
	}

	// Test link extraction
	linkExtraction := NewLinkExtractionTransformer()
	linkExtraction.Configure(map[string]interface{}{
		"extract_plain_urls": true,
		"deduplicate_links":  true,
	})

	result, err = linkExtraction.Transform(result)
	if err != nil {
		t.Fatalf("Link extraction failed with drive items: %v", err)
	}

	// Verify Google Drive/Sheets links were extracted
	foundDriveLink := false
	foundSheetsLink := false

	for _, item := range result {
		for _, link := range item.GetLinks() {
			if strings.Contains(link.URL, "drive.google.com") {
				foundDriveLink = true
			}

			if strings.Contains(link.URL, "sheets.google.com") {
				foundSheetsLink = true
			}
		}
	}

	if !foundDriveLink {
		t.Errorf("Expected to find Google Drive link")
	}

	if !foundSheetsLink {
		t.Errorf("Expected to find Google Sheets link")
	}

	// Verify content cleanup works
	contentCleanup := NewContentCleanupTransformer()
	contentCleanup.Configure(map[string]interface{}{
		"remove_extra_whitespace": true,
	})

	result, err = contentCleanup.Transform(result)
	if err != nil {
		t.Fatalf("Content cleanup failed with drive items: %v", err)
	}

	// Drive items should be processed successfully
	if len(result) != 2 {
		t.Errorf("Expected 2 drive items after processing, got %d", len(result))
	}
}

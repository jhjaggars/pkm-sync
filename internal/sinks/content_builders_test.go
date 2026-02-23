package sinks

import (
	"strings"
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

// makeItem creates a BasicItem for testing.
func makeItem(id, title, content, sourceType string, createdAt time.Time, metadata map[string]any) models.FullItem {
	item := models.NewBasicItem(id, title)
	item.SetContent(content)
	item.SetSourceType(sourceType)
	item.SetCreatedAt(createdAt)

	if metadata != nil {
		item.SetMetadata(metadata)
	}

	return item
}

func baseGroup(subject, sourceName string, items []models.FullItem) *itemGroup {
	start := items[0].GetCreatedAt()
	end := items[len(items)-1].GetCreatedAt()

	return &itemGroup{
		threadID:   "test-thread",
		subject:    subject,
		messages:   items,
		startTime:  start,
		endTime:    end,
		sourceName: sourceName,
	}
}

// --- gmailBuilder tests ---

func TestGmailBuilder_SourceType(t *testing.T) {
	b := &gmailBuilder{}

	if b.sourceType() != "gmail" {
		t.Errorf("expected 'gmail', got %q", b.sourceType())
	}
}

func TestGmailBuilder_CleanTitle(t *testing.T) {
	b := &gmailBuilder{}

	cases := []struct {
		input    string
		expected string
	}{
		{"Hello world", "Hello world"},
		{"Re: Hello world", "Hello world"},
		{"RE: Hello world", "Hello world"},
		{"Fwd: Hello world", "Hello world"},
		{"FWD: Hello world", "Hello world"},
		{"Re: Fwd: Re: Hello world", "Hello world"},
		{"Re: Fwd: Hello world", "Hello world"},
		{"  Re: Hello  ", "Hello"},
	}

	for _, tc := range cases {
		item := makeItem("1", tc.input, "", "gmail", time.Now(), nil)
		got := b.cleanTitle(item)

		if got != tc.expected {
			t.Errorf("cleanTitle(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestGmailBuilder_BuildContent(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	item := makeItem("msg1", "Project Update", "Here is the update.", "gmail", ts, map[string]any{
		"from": "alice@example.com",
		"to":   "bob@example.com",
		"cc":   "carol@example.com",
	})

	b := &gmailBuilder{}
	group := baseGroup("Project Update", "gmail_work", []models.FullItem{item})
	content := b.buildContent(group)

	if !strings.Contains(content, "Thread: Project Update") {
		t.Error("content should contain thread header")
	}

	if !strings.Contains(content, "From: alice@example.com") {
		t.Error("content should contain From field")
	}

	if !strings.Contains(content, "To: bob@example.com") {
		t.Error("content should contain To field")
	}

	if !strings.Contains(content, "Cc: carol@example.com") {
		t.Error("content should contain Cc field")
	}

	if !strings.Contains(content, "Here is the update.") {
		t.Error("content should contain message body")
	}
}

func TestGmailBuilder_BuildContent_NoContent(t *testing.T) {
	item := makeItem("msg1", "Empty Email", "", "gmail", time.Now(), nil)

	b := &gmailBuilder{}
	group := baseGroup("Empty Email", "gmail_work", []models.FullItem{item})
	content := b.buildContent(group)

	if !strings.Contains(content, "(no content)") {
		t.Error("should show (no content) when message body is empty")
	}
}

func TestGmailBuilder_BuildMetadata(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	item1 := makeItem("msg1", "Thread", "Body 1", "gmail", ts, map[string]any{
		"from": "alice@example.com",
		"to":   "bob@example.com",
	})
	item2 := makeItem("msg2", "Re: Thread", "Body 2", "gmail", ts.Add(time.Hour), map[string]any{
		"from": "bob@example.com",
		"to":   "alice@example.com",
	})

	b := &gmailBuilder{}
	group := baseGroup("Thread", "gmail_work", []models.FullItem{item1, item2})
	meta := b.buildMetadata(group)

	if meta["message_count"] != 2 {
		t.Errorf("expected message_count=2, got %v", meta["message_count"])
	}

	participants, ok := meta["participants"].([]string)
	if !ok {
		t.Fatal("participants should be a []string")
	}

	participantSet := make(map[string]bool)
	for _, p := range participants {
		participantSet[p] = true
	}

	if !participantSet["alice@example.com"] || !participantSet["bob@example.com"] {
		t.Errorf("expected both alice and bob in participants, got %v", participants)
	}

	if _, ok := meta["date_range"]; !ok {
		t.Error("metadata should contain date_range")
	}
}

// --- calendarBuilder tests ---

func TestCalendarBuilder_SourceType(t *testing.T) {
	b := &calendarBuilder{}

	if b.sourceType() != "google_calendar" {
		t.Errorf("expected 'google_calendar', got %q", b.sourceType())
	}
}

func TestCalendarBuilder_CleanTitle(t *testing.T) {
	b := &calendarBuilder{}

	item := makeItem("1", "  Weekly Sync  ", "", "google_calendar", time.Now(), nil)
	got := b.cleanTitle(item)

	if got != "Weekly Sync" {
		t.Errorf("expected 'Weekly Sync', got %q", got)
	}
}

func TestCalendarBuilder_BuildContent(t *testing.T) {
	start := time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	attendees := []models.Attendee{
		{Email: "alice@example.com", DisplayName: "Alice"},
		{Email: "bob@example.com"},
	}

	item := makeItem("evt1", "Team Standup", "Daily team sync", "google_calendar", start, map[string]any{
		"start_time": start,
		"end_time":   end,
		"location":   "Conference Room A",
		"attendees":  attendees,
	})
	item.SetLinks([]models.Link{{URL: "https://meet.example.com/abc", Type: "meeting_url"}})

	b := &calendarBuilder{}
	group := baseGroup("Team Standup", "calendar", []models.FullItem{item})
	content := b.buildContent(group)

	if !strings.Contains(content, "Event: Team Standup") {
		t.Error("content should contain event header")
	}

	if !strings.Contains(content, "Start: 2025-01-15 09:00") {
		t.Error("content should contain start time")
	}

	if !strings.Contains(content, "End: 2025-01-15 10:00") {
		t.Error("content should contain end time")
	}

	if !strings.Contains(content, "Location: Conference Room A") {
		t.Error("content should contain location")
	}

	if !strings.Contains(content, "Alice") {
		t.Error("content should contain attendee display name")
	}

	if !strings.Contains(content, "bob@example.com") {
		t.Error("content should contain attendee email when no display name")
	}

	if !strings.Contains(content, "Meeting URL: https://meet.example.com/abc") {
		t.Error("content should contain meeting URL")
	}

	if !strings.Contains(content, "Daily team sync") {
		t.Error("content should contain event description")
	}
}

func TestCalendarBuilder_BuildContent_Empty(t *testing.T) {
	b := &calendarBuilder{}
	group := &itemGroup{subject: "Empty", messages: nil}
	content := b.buildContent(group)

	if content != "" {
		t.Errorf("expected empty content for empty group, got %q", content)
	}
}

func TestCalendarBuilder_BuildMetadata(t *testing.T) {
	start := time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	attendees := []models.Attendee{
		{Email: "alice@example.com", DisplayName: "Alice"},
	}

	item := makeItem("evt1", "Meeting", "", "google_calendar", start, map[string]any{
		"start_time": start,
		"end_time":   end,
		"location":   "Room 101",
		"attendees":  attendees,
	})

	b := &calendarBuilder{}
	group := baseGroup("Meeting", "calendar", []models.FullItem{item})
	meta := b.buildMetadata(group)

	if _, ok := meta["start_time"]; !ok {
		t.Error("metadata should contain start_time")
	}

	if _, ok := meta["end_time"]; !ok {
		t.Error("metadata should contain end_time")
	}

	if meta["location"] != "Room 101" {
		t.Errorf("expected location 'Room 101', got %v", meta["location"])
	}

	attendeeNames, ok := meta["attendees"].([]string)
	if !ok {
		t.Fatal("attendees should be []string")
	}

	if len(attendeeNames) != 1 || attendeeNames[0] != "Alice" {
		t.Errorf("expected ['Alice'], got %v", attendeeNames)
	}
}

// --- driveBuilder tests ---

func TestDriveBuilder_SourceType(t *testing.T) {
	b := &driveBuilder{}

	if b.sourceType() != "google_drive" {
		t.Errorf("expected 'google_drive', got %q", b.sourceType())
	}
}

func TestDriveBuilder_CleanTitle(t *testing.T) {
	b := &driveBuilder{}

	item := makeItem("1", "  Q4 Report  ", "", "google_drive", time.Now(), nil)
	got := b.cleanTitle(item)

	if got != "Q4 Report" {
		t.Errorf("expected 'Q4 Report', got %q", got)
	}
}

func TestDriveBuilder_BuildContent(t *testing.T) {
	ts := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	item := makeItem("doc1", "Q4 Report", "## Summary\n\nQ4 was great.", "google_drive", ts, map[string]any{
		"mime_type":     "application/vnd.google-apps.document",
		"owners":        []string{"alice@example.com"},
		"web_view_link": "https://docs.google.com/document/d/abc123",
	})

	b := &driveBuilder{}
	group := baseGroup("Q4 Report", "my_drive", []models.FullItem{item})
	content := b.buildContent(group)

	if !strings.Contains(content, "Document: Q4 Report") {
		t.Error("content should contain document header")
	}

	if !strings.Contains(content, "Type: application/vnd.google-apps.document") {
		t.Error("content should contain mime type")
	}

	if !strings.Contains(content, "Owners: alice@example.com") {
		t.Error("content should contain owners")
	}

	if !strings.Contains(content, "Link: https://docs.google.com/document/d/abc123") {
		t.Error("content should contain web view link")
	}

	if !strings.Contains(content, "Q4 was great.") {
		t.Error("content should contain document body")
	}
}

func TestDriveBuilder_BuildContent_Empty(t *testing.T) {
	b := &driveBuilder{}
	group := &itemGroup{subject: "Empty", messages: nil}
	content := b.buildContent(group)

	if content != "" {
		t.Errorf("expected empty content for empty group, got %q", content)
	}
}

func TestDriveBuilder_BuildMetadata(t *testing.T) {
	ts := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	item := makeItem("doc1", "Report", "", "google_drive", ts, map[string]any{
		"mime_type":     "application/vnd.google-apps.document",
		"owners":        []string{"alice@example.com"},
		"web_view_link": "https://docs.google.com/document/d/abc",
	})

	b := &driveBuilder{}
	group := baseGroup("Report", "my_drive", []models.FullItem{item})
	meta := b.buildMetadata(group)

	if meta["mime_type"] != "application/vnd.google-apps.document" {
		t.Errorf("expected mime_type in metadata, got %v", meta["mime_type"])
	}

	if meta["web_view_link"] != "https://docs.google.com/document/d/abc" {
		t.Errorf("expected web_view_link in metadata, got %v", meta["web_view_link"])
	}

	if _, ok := meta["date_range"]; !ok {
		t.Error("metadata should contain date_range")
	}
}

// --- genericBuilder tests ---

func TestGenericBuilder_SourceType(t *testing.T) {
	b := &genericBuilder{}

	if b.sourceType() != "unknown" {
		t.Errorf("expected 'unknown', got %q", b.sourceType())
	}
}

func TestGenericBuilder_CleanTitle(t *testing.T) {
	b := &genericBuilder{}

	item := makeItem("1", "  Some Item  ", "", "other", time.Now(), nil)
	got := b.cleanTitle(item)

	if got != "Some Item" {
		t.Errorf("expected 'Some Item', got %q", got)
	}
}

func TestGenericBuilder_BuildContent(t *testing.T) {
	item := makeItem("1", "Test Item", "This is the content.", "other", time.Now(), nil)

	b := &genericBuilder{}
	group := baseGroup("Test Item", "other_source", []models.FullItem{item})
	content := b.buildContent(group)

	if !strings.Contains(content, "Item: Test Item") {
		t.Error("content should contain item header")
	}

	if !strings.Contains(content, "This is the content.") {
		t.Error("content should contain item body")
	}
}

func TestGenericBuilder_BuildContent_Empty(t *testing.T) {
	b := &genericBuilder{}
	group := &itemGroup{subject: "Empty", messages: nil}
	content := b.buildContent(group)

	if content != "" {
		t.Errorf("expected empty content for empty group, got %q", content)
	}
}

func TestGenericBuilder_BuildMetadata_PassesThrough(t *testing.T) {
	item := makeItem("1", "Test", "", "other", time.Now(), map[string]any{
		"custom_field": "custom_value",
		"another":      42,
	})

	b := &genericBuilder{}
	group := baseGroup("Test", "source", []models.FullItem{item})
	meta := b.buildMetadata(group)

	if meta["custom_field"] != "custom_value" {
		t.Errorf("expected custom_field to pass through, got %v", meta["custom_field"])
	}

	if meta["another"] != 42 {
		t.Errorf("expected another=42, got %v", meta["another"])
	}
}

// --- getContentBuilder tests ---

func TestGetContentBuilder(t *testing.T) {
	cases := []struct {
		srcType  string
		expected string
	}{
		{"gmail", "gmail"},
		{"google_calendar", "google_calendar"},
		{"google_drive", "google_drive"},
		{"unknown_type", "unknown"},
		{"", "unknown"},
	}

	for _, tc := range cases {
		b := getContentBuilder(tc.srcType)
		got := b.sourceType()

		if got != tc.expected {
			t.Errorf("getContentBuilder(%q).sourceType() = %q, want %q", tc.srcType, got, tc.expected)
		}
	}
}

// --- collapseWhitespace tests ---

func TestCollapseWhitespace(t *testing.T) {
	input := "line1\n\n\n\nline2\n\n\nline3"
	got := collapseWhitespace(input)

	if strings.Count(got, "\n\n\n") > 0 {
		t.Error("collapseWhitespace should not leave triple newlines")
	}

	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") || !strings.Contains(got, "line3") {
		t.Error("collapseWhitespace should preserve content")
	}
}

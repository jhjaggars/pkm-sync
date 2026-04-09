package resolve

import (
	"context"
	"errors"
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

// mockDriveClient implements driveClient for tests.
type mockDriveClient struct {
	metadata    *models.DriveFile
	metadataErr error
	content     string
	contentErr  error
}

func (m *mockDriveClient) GetFileMetadata(_ string) (*models.DriveFile, error) {
	return m.metadata, m.metadataErr
}

func (m *mockDriveClient) ExportAsString(_, _ string, _ bool, _ int64) (string, error) {
	return m.content, m.contentErr
}

func TestDriveResolver_CanResolve(t *testing.T) {
	r := NewDriveResolver(nil, models.DriveSourceConfig{})

	tests := []struct {
		url  string
		want bool
	}{
		{"https://docs.google.com/document/d/abc/edit", true},
		{"https://drive.google.com/file/d/abc/view", true},
		{"https://docs.google.com/spreadsheets/d/abc", true},
		{"https://docs.google.com/presentation/d/abc", true},
		{"https://slack.com/archives/C123", false},
		{"https://example.com", false},
		{"https://jira.example.com/browse/PROJ-1", false},
	}

	for _, tt := range tests {
		got := r.CanResolve(tt.url)
		if got != tt.want {
			t.Errorf("CanResolve(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestDriveResolver_Resolve_GoogleDoc(t *testing.T) {
	modifiedAt := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	mock := &mockDriveClient{
		metadata: &models.DriveFile{
			ID:           "abc123",
			Name:         "My Report",
			MimeType:     "application/vnd.google-apps.document",
			WebViewLink:  "https://docs.google.com/document/d/abc123/edit",
			ModifiedTime: modifiedAt,
		},
		content: "# My Report\n\nContent here.",
	}

	r := NewDriveResolver(mock, models.DriveSourceConfig{})

	item, err := r.Resolve(context.Background(), "https://docs.google.com/document/d/abc123/edit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item == nil {
		t.Fatal("expected a resolved item, got nil")
	}

	if item.GetID() != "abc123" {
		t.Errorf("ID = %q, want %q", item.GetID(), "abc123")
	}

	if item.GetItemType() != "document" {
		t.Errorf("ItemType = %q, want %q", item.GetItemType(), "document")
	}

	if item.GetSourceType() != "google_drive" {
		t.Errorf("SourceType = %q, want %q", item.GetSourceType(), "google_drive")
	}

	if item.GetContent() != mock.content {
		t.Errorf("Content = %q, want %q", item.GetContent(), mock.content)
	}

	if !item.GetUpdatedAt().Equal(modifiedAt) {
		t.Errorf("UpdatedAt = %v, want %v", item.GetUpdatedAt(), modifiedAt)
	}
}

func TestDriveResolver_Resolve_Spreadsheet(t *testing.T) {
	mock := &mockDriveClient{
		metadata: &models.DriveFile{
			ID:       "sheet1",
			Name:     "Budget",
			MimeType: "application/vnd.google-apps.spreadsheet",
		},
		content: "a,b,c\n1,2,3",
	}

	r := NewDriveResolver(mock, models.DriveSourceConfig{})

	item, err := r.Resolve(context.Background(), "https://docs.google.com/spreadsheets/d/sheet1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.GetItemType() != "spreadsheet" {
		t.Errorf("ItemType = %q, want %q", item.GetItemType(), "spreadsheet")
	}
}

func TestDriveResolver_Resolve_UnsupportedMIME(t *testing.T) {
	mock := &mockDriveClient{
		metadata: &models.DriveFile{
			ID:       "pdf1",
			Name:     "doc.pdf",
			MimeType: "application/pdf",
		},
	}

	r := NewDriveResolver(mock, models.DriveSourceConfig{})

	_, err := r.Resolve(context.Background(), "https://drive.google.com/file/d/pdf1/view")
	if err == nil {
		t.Fatal("expected error for unsupported MIME type, got nil")
	}
}

func TestDriveResolver_Resolve_MetadataError(t *testing.T) {
	mock := &mockDriveClient{
		metadataErr: errors.New("not found"),
	}

	r := NewDriveResolver(mock, models.DriveSourceConfig{})

	_, err := r.Resolve(context.Background(), "https://docs.google.com/document/d/missing/edit")
	if err == nil {
		t.Fatal("expected error from metadata failure, got nil")
	}
}

func TestDriveResolver_Resolve_ExportError(t *testing.T) {
	mock := &mockDriveClient{
		metadata: &models.DriveFile{
			ID:       "doc1",
			Name:     "Doc",
			MimeType: "application/vnd.google-apps.document",
		},
		contentErr: errors.New("export failed"),
	}

	r := NewDriveResolver(mock, models.DriveSourceConfig{})

	_, err := r.Resolve(context.Background(), "https://docs.google.com/document/d/doc1/edit")
	if err == nil {
		t.Fatal("expected error from export failure, got nil")
	}
}

func TestDriveResolver_Resolve_BadURL(t *testing.T) {
	r := NewDriveResolver(&mockDriveClient{}, models.DriveSourceConfig{})

	// URL that passes CanResolve but has no extractable file ID.
	_, err := r.Resolve(context.Background(), "https://docs.google.com/")
	if err == nil {
		t.Fatal("expected error for URL with no file ID, got nil")
	}
}

func TestDriveResolver_Resolve_TaggedAsResolved(t *testing.T) {
	mock := &mockDriveClient{
		metadata: &models.DriveFile{
			ID:       "doc2",
			Name:     "Tagged Doc",
			MimeType: "application/vnd.google-apps.document",
		},
		content: "content",
	}

	r := NewDriveResolver(mock, models.DriveSourceConfig{})

	item, err := r.Resolve(context.Background(), "https://docs.google.com/document/d/doc2/edit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tags := item.GetTags()
	found := false

	for _, tag := range tags {
		if tag == "resolved" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("resolved item should have 'resolved' tag, got: %v", tags)
	}
}

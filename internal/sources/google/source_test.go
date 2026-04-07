package google

import (
	"errors"
	"testing"
	"time"

	"pkm-sync/internal/sources/google/drive"
	"pkm-sync/pkg/models"
)

// mockDriveExporter is a test double for driveExporter.
type mockDriveExporter struct {
	// listFiles is called per folderID; returns the configured files or error.
	listFiles       []*drive.DriveFileInfo
	listErr         error
	sharedFiles     []*drive.DriveFileInfo
	sharedErr       error
	exportContent   string
	exportErr       error
	configureCalled bool
}

func (m *mockDriveExporter) Configure(_ models.DriveSourceConfig) {
	m.configureCalled = true
}

func (m *mockDriveExporter) ListFilesInFolder(_ string, _ time.Time, _ bool, _ drive.ListFilesOptions) ([]*drive.DriveFileInfo, error) {
	return m.listFiles, m.listErr
}

func (m *mockDriveExporter) ListSharedWithMe(_ time.Time, _ drive.ListFilesOptions) ([]*drive.DriveFileInfo, error) {
	return m.sharedFiles, m.sharedErr
}

func (m *mockDriveExporter) ExportAsString(_ string, _ string, _ bool, _ int64) (string, error) {
	return m.exportContent, m.exportErr
}

// newTestGoogleDriveSource creates a GoogleSource wired for Drive with the given mock.
func newTestGoogleDriveSource(mock driveExporter, driveCfg models.DriveSourceConfig) *GoogleSource {
	return &GoogleSource{
		driveService: mock,
		config: models.SourceConfig{
			Type:  SourceTypeDrive,
			Drive: driveCfg,
		},
	}
}

// ---- convertDriveFile tests ----

func TestConvertDriveFile_Doc(t *testing.T) {
	mock := &mockDriveExporter{exportContent: "# Hello"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	file := &drive.DriveFileInfo{
		ID:          "doc1",
		Name:        "My Doc",
		MimeType:    drive.MimeTypeGoogleDoc,
		WebViewLink: "https://docs.google.com/...",
		CreatedTime: time.Now(),
		ModifiedTime: time.Now(),
	}

	item, err := src.convertDriveFile(file, models.DriveSourceConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.GetID() != "doc1" {
		t.Errorf("ID = %q, want %q", item.GetID(), "doc1")
	}

	if item.GetTitle() != "My Doc" {
		t.Errorf("Title = %q, want %q", item.GetTitle(), "My Doc")
	}

	if item.GetItemType() != "document" {
		t.Errorf("ItemType = %q, want %q", item.GetItemType(), "document")
	}

	if item.GetContent() != "# Hello" {
		t.Errorf("Content = %q, want %q", item.GetContent(), "# Hello")
	}

	if item.GetSourceType() != SourceTypeDrive {
		t.Errorf("SourceType = %q, want %q", item.GetSourceType(), SourceTypeDrive)
	}
}

func TestConvertDriveFile_Sheet(t *testing.T) {
	mock := &mockDriveExporter{exportContent: "a,b,c"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	file := &drive.DriveFileInfo{
		ID:       "sheet1",
		Name:     "My Sheet",
		MimeType: drive.MimeTypeGoogleSheet,
	}

	item, err := src.convertDriveFile(file, models.DriveSourceConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.GetItemType() != "spreadsheet" {
		t.Errorf("ItemType = %q, want %q", item.GetItemType(), "spreadsheet")
	}
}

func TestConvertDriveFile_Presentation(t *testing.T) {
	mock := &mockDriveExporter{exportContent: "slide text"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	file := &drive.DriveFileInfo{
		ID:       "pres1",
		Name:     "My Slides",
		MimeType: drive.MimeTypeGooglePresentation,
	}

	item, err := src.convertDriveFile(file, models.DriveSourceConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.GetItemType() != "presentation" {
		t.Errorf("ItemType = %q, want %q", item.GetItemType(), "presentation")
	}
}

func TestConvertDriveFile_UnsupportedMIME(t *testing.T) {
	mock := &mockDriveExporter{}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	file := &drive.DriveFileInfo{
		ID:       "pdf1",
		Name:     "some.pdf",
		MimeType: "application/pdf",
	}

	_, err := src.convertDriveFile(file, models.DriveSourceConfig{})
	if err == nil {
		t.Fatal("expected error for unsupported MIME type, got nil")
	}
}

func TestConvertDriveFile_ExportError(t *testing.T) {
	exportErr := errors.New("export failed")
	mock := &mockDriveExporter{exportErr: exportErr}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	file := &drive.DriveFileInfo{
		ID:       "doc2",
		Name:     "Failing Doc",
		MimeType: drive.MimeTypeGoogleDoc,
	}

	_, err := src.convertDriveFile(file, models.DriveSourceConfig{})
	if err == nil {
		t.Fatal("expected error from export failure, got nil")
	}
}

func TestConvertDriveFile_WebViewLink(t *testing.T) {
	mock := &mockDriveExporter{exportContent: "content"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	file := &drive.DriveFileInfo{
		ID:          "doc3",
		Name:        "Linked Doc",
		MimeType:    drive.MimeTypeGoogleDoc,
		WebViewLink: "https://docs.google.com/document/d/abc",
	}

	item, err := src.convertDriveFile(file, models.DriveSourceConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	links := item.GetLinks()
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}

	if links[0].URL != file.WebViewLink {
		t.Errorf("link URL = %q, want %q", links[0].URL, file.WebViewLink)
	}
}

func TestConvertDriveFile_CustomExportFormat(t *testing.T) {
	mock := &mockDriveExporter{exportContent: "plain text"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	file := &drive.DriveFileInfo{
		ID:       "doc4",
		Name:     "Text Doc",
		MimeType: drive.MimeTypeGoogleDoc,
	}

	cfg := models.DriveSourceConfig{DocExportFormat: "txt"}

	item, err := src.convertDriveFile(file, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.GetContent() != "plain text" {
		t.Errorf("Content = %q, want %q", item.GetContent(), "plain text")
	}
}

// ---- fetchDrive tests ----

func TestFetchDrive_NotInitialized(t *testing.T) {
	src := &GoogleSource{}

	_, err := src.fetchDrive(time.Now(), 0)
	if err == nil {
		t.Fatal("expected error when drive service is nil")
	}
}

func TestFetchDrive_AllSucceed(t *testing.T) {
	files := []*drive.DriveFileInfo{
		{ID: "a", Name: "Doc A", MimeType: drive.MimeTypeGoogleDoc},
		{ID: "b", Name: "Doc B", MimeType: drive.MimeTypeGoogleDoc},
	}

	mock := &mockDriveExporter{listFiles: files, exportContent: "content"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	items, err := src.fetchDrive(time.Now(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestFetchDrive_PartialFailure(t *testing.T) {
	files := []*drive.DriveFileInfo{
		{ID: "a", Name: "Good Doc", MimeType: drive.MimeTypeGoogleDoc},
		{ID: "b", Name: "Bad PDF", MimeType: "application/pdf"}, // unsupported → conversion error
	}

	mock := &mockDriveExporter{listFiles: files, exportContent: "ok"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	items, err := src.fetchDrive(time.Now(), 0)
	if err != nil {
		t.Fatalf("expected no fatal error on partial failure, got: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 successful item, got %d", len(items))
	}
}

func TestFetchDrive_AllFail(t *testing.T) {
	files := []*drive.DriveFileInfo{
		{ID: "x", Name: "Bad1", MimeType: "application/pdf"},
		{ID: "y", Name: "Bad2", MimeType: "application/pdf"},
	}

	mock := &mockDriveExporter{listFiles: files}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	items, err := src.fetchDrive(time.Now(), 0)
	if err != nil {
		t.Fatalf("expected nil error even when all conversions fail, got: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestFetchDrive_Deduplication(t *testing.T) {
	// Same file appears in two folders; should only be included once.
	file := &drive.DriveFileInfo{ID: "dup", Name: "Dup Doc", MimeType: drive.MimeTypeGoogleDoc}
	mock := &mockDriveExporter{listFiles: []*drive.DriveFileInfo{file}, exportContent: "x"}
	cfg := models.DriveSourceConfig{
		FolderIDs: []string{"folder1", "folder2"},
	}
	src := newTestGoogleDriveSource(mock, cfg)

	items, err := src.fetchDrive(time.Now(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 deduplicated item, got %d", len(items))
	}
}

func TestFetchDrive_LimitApplied(t *testing.T) {
	files := []*drive.DriveFileInfo{
		{ID: "1", Name: "Doc1", MimeType: drive.MimeTypeGoogleDoc},
		{ID: "2", Name: "Doc2", MimeType: drive.MimeTypeGoogleDoc},
		{ID: "3", Name: "Doc3", MimeType: drive.MimeTypeGoogleDoc},
	}

	mock := &mockDriveExporter{listFiles: files, exportContent: "content"}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	items, err := src.fetchDrive(time.Now(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items (limit applied), got %d", len(items))
	}
}

func TestFetchDrive_SizeFilter(t *testing.T) {
	files := []*drive.DriveFileInfo{
		{ID: "small", Name: "Small", MimeType: drive.MimeTypeGoogleDoc, Size: 100},
		{ID: "large", Name: "Large", MimeType: drive.MimeTypeGoogleDoc, Size: 10_000_000},
	}

	mock := &mockDriveExporter{listFiles: files, exportContent: "content"}
	cfg := models.DriveSourceConfig{MaxFileSizeBytes: 1_000_000}
	src := newTestGoogleDriveSource(mock, cfg)

	items, err := src.fetchDrive(time.Now(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item (large file filtered), got %d", len(items))
	}

	if items[0].GetID() != "small" {
		t.Errorf("expected 'small' item, got %q", items[0].GetID())
	}
}

func TestFetchDrive_ListError(t *testing.T) {
	mock := &mockDriveExporter{listErr: errors.New("API error")}
	src := newTestGoogleDriveSource(mock, models.DriveSourceConfig{})

	_, err := src.fetchDrive(time.Now(), 0)
	if err == nil {
		t.Fatal("expected error from list failure, got nil")
	}
}

func TestFetchDrive_ParallelExports(t *testing.T) {
	files := []*drive.DriveFileInfo{
		{ID: "p1", Name: "P1", MimeType: drive.MimeTypeGoogleDoc},
		{ID: "p2", Name: "P2", MimeType: drive.MimeTypeGoogleDoc},
		{ID: "p3", Name: "P3", MimeType: drive.MimeTypeGoogleDoc},
	}

	mock := &mockDriveExporter{listFiles: files, exportContent: "parallel content"}
	cfg := models.DriveSourceConfig{MaxConcurrentExports: 3}
	src := newTestGoogleDriveSource(mock, cfg)

	items, err := src.fetchDrive(time.Now(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("expected 3 items from parallel export, got %d", len(items))
	}
}

func TestFetchDrive_SharedWithMe(t *testing.T) {
	shared := []*drive.DriveFileInfo{
		{ID: "s1", Name: "Shared Doc", MimeType: drive.MimeTypeGoogleDoc},
	}

	mock := &mockDriveExporter{sharedFiles: shared, exportContent: "shared content"}
	cfg := models.DriveSourceConfig{IncludeSharedWithMe: true}
	src := newTestGoogleDriveSource(mock, cfg)

	items, err := src.fetchDrive(time.Now(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 shared item, got %d", len(items))
	}
}

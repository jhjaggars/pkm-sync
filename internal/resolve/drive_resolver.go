package resolve

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"pkm-sync/internal/sources/google/drive"
	"pkm-sync/pkg/models"
)

// driveClient is the subset of drive.Service used by DriveResolver.
// Defined as an interface so tests can inject a mock without a live Drive API.
type driveClient interface {
	GetFileMetadata(fileID string) (*models.DriveFile, error)
	ExportAsString(fileID, exportMimeType string, convertToMarkdown bool, maxBytes int64) (string, error)
}

// DriveResolver resolves Google Drive and Google Docs URLs to FullItems.
type DriveResolver struct {
	svc     driveClient
	cfg     models.DriveSourceConfig
	pattern *regexp.Regexp
}

// NewDriveResolver creates a DriveResolver backed by the given driveClient.
// Accepts the driveClient interface so tests can inject mocks directly.
func NewDriveResolver(svc driveClient, cfg models.DriveSourceConfig) *DriveResolver {
	return &DriveResolver{
		svc:     svc,
		cfg:     cfg,
		pattern: regexp.MustCompile(`(?i)https?://(?:docs|drive)\.google\.com/`),
	}
}

// Name implements interfaces.Resolver.
func (r *DriveResolver) Name() string { return "drive" }

// CanResolve implements interfaces.Resolver.
func (r *DriveResolver) CanResolve(rawURL string) bool {
	return r.pattern.MatchString(rawURL)
}

// Resolve implements interfaces.Resolver.
func (r *DriveResolver) Resolve(_ context.Context, rawURL string) (models.FullItem, error) {
	fileID, err := drive.ExtractFileID(rawURL)
	if err != nil {
		return nil, fmt.Errorf("drive resolver: cannot extract file ID from %q: %w", rawURL, err)
	}

	meta, err := r.svc.GetFileMetadata(fileID)
	if err != nil {
		return nil, fmt.Errorf("drive resolver: metadata fetch failed for %q: %w", fileID, err)
	}

	// Determine export format based on MIME type and config preferences.
	var format string

	switch meta.MimeType {
	case drive.MimeTypeGoogleDoc:
		format = r.cfg.DocExportFormat
		if format == "" {
			format = drive.FormatMD
		}
	case drive.MimeTypeGoogleSheet:
		format = r.cfg.SheetExportFormat
		if format == "" {
			format = drive.FormatCSV
		}
	case drive.MimeTypeGooglePresentation:
		format = r.cfg.SlideExportFormat
		if format == "" {
			format = drive.FormatTXT
		}
	default:
		return nil, fmt.Errorf("drive resolver: unsupported MIME type %q for file %q", meta.MimeType, fileID)
	}

	exportMIME, err := drive.GetExportMimeType(meta.MimeType, format)
	if err != nil {
		return nil, fmt.Errorf("drive resolver: %w", err)
	}

	convertToMarkdown := format == drive.FormatMD

	content, err := r.svc.ExportAsString(fileID, exportMIME, convertToMarkdown, r.cfg.MaxFileSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("drive resolver: export failed for %q: %w", fileID, err)
	}

	// Map MIME type to item type string.
	var itemType string

	switch meta.MimeType {
	case drive.MimeTypeGoogleDoc:
		itemType = "document"
	case drive.MimeTypeGoogleSheet:
		itemType = "spreadsheet"
	case drive.MimeTypeGooglePresentation:
		itemType = "presentation"
	}

	updatedAt := meta.ModifiedTime
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	item := &models.BasicItem{
		ID:         fileID,
		Title:      meta.Name,
		Content:    content,
		SourceType: "google_drive",
		ItemType:   itemType,
		CreatedAt:  updatedAt,
		UpdatedAt:  updatedAt,
		Tags:       []string{"resolved"},
		Metadata: map[string]interface{}{
			"mime_type":     meta.MimeType,
			"web_view_link": meta.WebViewLink,
			"owners":        meta.Owners,
		},
		Links: []models.Link{
			{URL: rawURL, Title: meta.Name, Type: "document"},
		},
	}

	return item, nil
}

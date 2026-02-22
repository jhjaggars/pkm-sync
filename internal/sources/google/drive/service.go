package drive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	mdconverter "github.com/JohannesKaufmann/html-to-markdown/v2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type Service struct {
	client *drive.Service
}

func NewService(httpClient *http.Client) (*Service, error) {
	driveService, err := drive.NewService(context.Background(), option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Drive client: %w", err)
	}

	return &Service{client: driveService}, nil
}

// GetFileMetadata retrieves metadata for a Google Drive file.
func (s *Service) GetFileMetadata(fileID string) (*models.DriveFile, error) {
	file, err := s.client.Files.Get(fileID).Fields("id,name,mimeType,webViewLink,modifiedTime,owners").Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve file metadata: %w", err)
	}

	driveFile := &models.DriveFile{
		ID:          file.Id,
		Name:        file.Name,
		MimeType:    file.MimeType,
		WebViewLink: file.WebViewLink,
	}

	for _, owner := range file.Owners {
		driveFile.Owners = append(driveFile.Owners, owner.DisplayName)
	}

	driveFile.Shared = len(file.Owners) > 1

	return driveFile, nil
}

// IsGoogleDoc checks if a file is a Google Doc that can be exported to markdown.
func (s *Service) IsGoogleDoc(mimeType string) bool {
	return mimeType == "application/vnd.google-apps.document"
}

// ExportDocAsMarkdown exports a Google Doc as markdown format.
func (s *Service) ExportDocAsMarkdown(fileID string, outputPath string) error {
	if !s.IsGoogleDocByID(fileID) {
		return fmt.Errorf("file %s is not a Google Doc", fileID)
	}

	// Export as plain text first (closest to markdown)
	resp, err := s.client.Files.Export(fileID, "text/plain").Download()
	if err != nil {
		return fmt.Errorf("unable to export document: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("unable to create output directory: %w", err)
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("unable to create output file: %w", err)
	}

	defer func() {
		_ = outFile.Close()
	}()

	// Copy content to file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("unable to write file content: %w", err)
	}

	return nil
}

// IsGoogleDocByID checks if a file ID represents a Google Doc.
func (s *Service) IsGoogleDocByID(fileID string) bool {
	file, err := s.client.Files.Get(fileID).Fields("mimeType").Do()
	if err != nil {
		return false
	}

	return s.IsGoogleDoc(file.MimeType)
}

// GetAttachmentsFromEvent extracts Google Drive file attachments from a calendar event.
func (s *Service) GetAttachmentsFromEvent(eventDescription string) ([]string, error) {
	var fileIDs []string

	// Look for Google Drive links in the event description
	// Google Drive links typically follow patterns like:
	// https://docs.google.com/document/d/FILE_ID/edit
	// https://drive.google.com/file/d/FILE_ID/view

	lines := strings.Split(eventDescription, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "docs.google.com/document/d/") {
			// Extract file ID from Google Docs URL
			if fileID := extractFileIDFromDocsURL(line); fileID != "" {
				fileIDs = append(fileIDs, fileID)
			}
		} else if strings.Contains(line, "drive.google.com/file/d/") {
			// Extract file ID from Google Drive URL
			if fileID := extractFileIDFromDriveURL(line); fileID != "" {
				fileIDs = append(fileIDs, fileID)
			}
		}
	}

	return fileIDs, nil
}

// extractFileIDFromDocsURL extracts file ID from Google Docs URL.
func extractFileIDFromDocsURL(url string) string {
	// Pattern: https://docs.google.com/document/d/FILE_ID/edit
	parts := strings.Split(url, "/")
	for i, part := range parts {
		if part == "d" && i+1 < len(parts) {
			fileID := parts[i+1]
			// Remove any query parameters
			if idx := strings.Index(fileID, "?"); idx != -1 {
				fileID = fileID[:idx]
			}

			return fileID
		}
	}

	return ""
}

// extractFileIDFromDriveURL extracts file ID from Google Drive URL.
func extractFileIDFromDriveURL(url string) string {
	// Pattern: https://drive.google.com/file/d/FILE_ID/view
	parts := strings.Split(url, "/")
	for i, part := range parts {
		if part == "d" && i+1 < len(parts) {
			fileID := parts[i+1]
			// Remove any query parameters
			if idx := strings.Index(fileID, "?"); idx != -1 {
				fileID = fileID[:idx]
			}

			return fileID
		}
	}

	return ""
}

// ExtractFileID extracts file ID from various Google Drive and Docs URL patterns.
// Supports:
// - docs.google.com/document/d/{ID}.
// - docs.google.com/spreadsheets/d/{ID}.
// - docs.google.com/presentation/d/{ID}.
// - drive.google.com/file/d/{ID}.
// - drive.google.com/open?id={ID}.
func ExtractFileID(url string) (string, error) {
	// Try docs pattern (works for documents, spreadsheets, presentations)
	if fileID := extractFileIDFromDocsURL(url); fileID != "" {
		return fileID, nil
	}

	// Try drive file pattern
	if fileID := extractFileIDFromDriveURL(url); fileID != "" {
		return fileID, nil
	}

	// Try drive.google.com/open?id= pattern
	fileID := extractFileIDFromOpenURL(url)
	if fileID != "" {
		return fileID, nil
	}

	return "", fmt.Errorf("unable to extract file ID from URL: %s", url)
}

// extractFileIDFromOpenURL extracts file ID from drive.google.com/open?id= URLs.
func extractFileIDFromOpenURL(url string) string {
	if !strings.Contains(url, "drive.google.com/open") {
		return ""
	}

	if !strings.Contains(url, "id=") {
		return ""
	}

	parts := strings.Split(url, "id=")
	if len(parts) < 2 {
		return ""
	}

	fileID := parts[1]
	// Remove any trailing parameters
	if idx := strings.Index(fileID, "&"); idx != -1 {
		fileID = fileID[:idx]
	}

	return fileID
}

// ExportAttachedDocsFromEvent exports all Google Docs attached to an event.
func (s *Service) ExportAttachedDocsFromEvent(eventDescription, outputDir string) ([]string, error) {
	fileIDs, err := s.GetAttachmentsFromEvent(eventDescription)
	if err != nil {
		return nil, err
	}

	exportedFiles := make([]string, 0, len(fileIDs))

	for _, fileID := range fileIDs {
		// Get file metadata to determine name and type
		metadata, err := s.GetFileMetadata(fileID)
		if err != nil {
			fmt.Printf("Warning: Could not get metadata for file %s: %v\n", fileID, err)

			continue
		}

		// Only export Google Docs
		if !s.IsGoogleDoc(metadata.MimeType) {
			fmt.Printf("Skipping %s: not a Google Doc (type: %s)\n", metadata.Name, metadata.MimeType)

			continue
		}

		// Generate output filename
		filename := sanitizeFilename(metadata.Name)
		if !strings.HasSuffix(filename, ".md") {
			filename += ".md"
		}

		outputPath := filepath.Join(outputDir, filename)

		// Export the document
		if err := s.ExportDocAsMarkdown(fileID, outputPath); err != nil {
			fmt.Printf("Warning: Could not export %s: %v\n", metadata.Name, err)

			continue
		}

		exportedFiles = append(exportedFiles, outputPath)
		fmt.Printf("Exported: %s -> %s\n", metadata.Name, outputPath)
	}

	return exportedFiles, nil
}

// sanitizeFilename removes or replaces characters that are invalid in filenames.
func sanitizeFilename(filename string) string {
	// Replace common problematic characters
	replacements := map[string]string{
		"/":  "-",
		"\\": "-",
		":":  "-",
		"*":  "",
		"?":  "",
		"\"": "",
		"<":  "",
		">":  "",
		"|":  "-",
	}

	for old, new := range replacements {
		filename = strings.ReplaceAll(filename, old, new)
	}

	// Remove multiple consecutive spaces and trim
	filename = strings.TrimSpace(filename)
	for strings.Contains(filename, "  ") {
		filename = strings.ReplaceAll(filename, "  ", " ")
	}

	return filename
}

// Google Workspace MIME types.
const (
	MimeTypeGoogleDoc          = "application/vnd.google-apps.document"
	MimeTypeGoogleSheet        = "application/vnd.google-apps.spreadsheet"
	MimeTypeGooglePresentation = "application/vnd.google-apps.presentation"
)

// Export MIME types.
const (
	MimeTypePlainText = "text/plain"
	MimeTypeHTML      = "text/html"
	MimeTypeCSV       = "text/csv"
)

// Format constants.
const (
	FormatHTML = "html"
	FormatMD   = "md"
	FormatTXT  = "txt"
	FormatCSV  = "csv"
)

// GetExportMimeType returns the appropriate export MIME type for a given file type and format.
func GetExportMimeType(fileMimeType, format string) (string, error) {
	switch fileMimeType {
	case MimeTypeGoogleDoc:
		switch format {
		case FormatTXT:
			return MimeTypePlainText, nil
		case FormatHTML, FormatMD:
			return MimeTypeHTML, nil
		default:
			return "", fmt.Errorf("unsupported format '%s' for Google Docs (supported: txt, html, md)", format)
		}
	case MimeTypeGoogleSheet:
		switch format {
		case FormatCSV:
			return MimeTypeCSV, nil
		case FormatHTML:
			return MimeTypeHTML, nil
		default:
			return "", fmt.Errorf("unsupported format '%s' for Google Sheets (supported: csv, html)", format)
		}
	case MimeTypeGooglePresentation:
		switch format {
		case FormatTXT:
			return MimeTypePlainText, nil
		case FormatHTML:
			return MimeTypeHTML, nil
		default:
			return "", fmt.Errorf("unsupported format '%s' for Google Slides (supported: txt, html)", format)
		}
	default:
		return "", fmt.Errorf("unsupported file type: %s (only Google Docs, Sheets, and Slides are supported)", fileMimeType)
	}
}

// ExportDocument exports a Google Workspace document and returns the content as a ReadCloser.
func (s *Service) ExportDocument(fileID, exportMimeType string) (io.ReadCloser, error) {
	resp, err := s.client.Files.Export(fileID, exportMimeType).Download()
	if err != nil {
		return nil, fmt.Errorf("unable to export document: %w", err)
	}

	return resp.Body, nil
}

// IsGoogleWorkspaceFile returns true if the MIME type is one of the three supported Workspace types.
func IsGoogleWorkspaceFile(mimeType string) bool {
	switch mimeType {
	case MimeTypeGoogleDoc, MimeTypeGoogleSheet, MimeTypeGooglePresentation:
		return true
	}

	return false
}

// buildQuery constructs a Drive API query string from the given options.
// The returned string is suitable for use as the q parameter in Files.List().
func buildQuery(opts ListFilesOptions) string {
	var parts []string

	// Never include trashed files
	parts = append(parts, "trashed = false")

	if opts.FolderID != "" {
		parts = append(parts, fmt.Sprintf("'%s' in parents", opts.FolderID))
	}

	if opts.IncludeSharedWithMe {
		parts = append(parts, "sharedWithMe = true")
	}

	if !opts.ModifiedAfter.IsZero() {
		parts = append(parts, fmt.Sprintf("modifiedTime > '%s'", opts.ModifiedAfter.UTC().Format(time.RFC3339)))
	}

	if len(opts.MimeTypes) == 1 {
		parts = append(parts, fmt.Sprintf("mimeType = '%s'", opts.MimeTypes[0]))
	} else if len(opts.MimeTypes) > 1 {
		mimeFilters := make([]string, len(opts.MimeTypes))
		for i, mt := range opts.MimeTypes {
			mimeFilters[i] = fmt.Sprintf("mimeType = '%s'", mt)
		}

		parts = append(parts, "("+strings.Join(mimeFilters, " or ")+")")
	}

	query := strings.Join(parts, " and ")

	if opts.ExtraQuery != "" {
		if query != "" {
			query += " and " + opts.ExtraQuery
		} else {
			query = opts.ExtraQuery
		}
	}

	return query
}

// ListFiles lists files matching the given options, handling pagination automatically.
func (s *Service) ListFiles(opts ListFilesOptions) ([]*DriveFileInfo, error) {
	pageSize := int64(100)
	if opts.PageSize > 0 {
		pageSize = int64(opts.PageSize)
	}

	query := buildQuery(opts)

	var files []*DriveFileInfo

	pageToken := ""

	for {
		const fields = "nextPageToken, " +
			"files(id,name,mimeType,webViewLink,modifiedTime,createdTime,owners,size,parents,description,starred)"

		req := s.client.Files.List().
			Fields(fields).
			Q(query).
			PageSize(pageSize)

		if opts.IncludeSharedDrives {
			req = req.IncludeItemsFromAllDrives(true).SupportsAllDrives(true)
		}

		if pageToken != "" {
			req = req.PageToken(pageToken)
		}

		result, err := req.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list drive files: %w", err)
		}

		for _, f := range result.Files {
			info := convertFileInfo(f)
			files = append(files, info)

			if opts.MaxResults > 0 && len(files) >= opts.MaxResults {
				return files, nil
			}
		}

		if result.NextPageToken == "" {
			break
		}

		pageToken = result.NextPageToken
	}

	return files, nil
}

// ListFilesInFolder lists files in a specific folder. If recursive is true, subfolders are
// traversed and their contents included. folderID "root" refers to the Drive root.
func (s *Service) ListFilesInFolder(
	folderID string,
	since time.Time,
	recursive bool,
	opts ListFilesOptions,
) ([]*DriveFileInfo, error) {
	folderOpts := opts
	folderOpts.FolderID = folderID
	folderOpts.ModifiedAfter = since
	// Don't use sharedWithMe filter when listing by folder
	folderOpts.IncludeSharedWithMe = false

	files, err := s.ListFiles(folderOpts)
	if err != nil {
		return nil, err
	}

	if !recursive {
		return files, nil
	}

	// Find subfolders
	subfolderOpts := ListFilesOptions{
		FolderID:            folderID,
		MimeTypes:           []string{"application/vnd.google-apps.folder"},
		IncludeSharedDrives: opts.IncludeSharedDrives,
	}

	subfolders, err := s.ListFiles(subfolderOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list subfolders in %s: %w", folderID, err)
	}

	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f.ID] = true
	}

	for _, subfolder := range subfolders {
		subFiles, err := s.ListFilesInFolder(subfolder.ID, since, recursive, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list files in subfolder %s: %w", subfolder.ID, err)
		}

		for _, f := range subFiles {
			if !seen[f.ID] {
				seen[f.ID] = true
				files = append(files, f)
			}
		}
	}

	return files, nil
}

// ListSharedWithMe lists Google Workspace files shared with the authenticated user.
func (s *Service) ListSharedWithMe(since time.Time, opts ListFilesOptions) ([]*DriveFileInfo, error) {
	sharedOpts := opts
	sharedOpts.FolderID = ""
	sharedOpts.IncludeSharedWithMe = true
	sharedOpts.ModifiedAfter = since

	return s.ListFiles(sharedOpts)
}

// ExportAsString exports a Google Workspace file as a string. If convertToMarkdown is true
// and the content is HTML, it will be converted to Markdown.
func (s *Service) ExportAsString(fileID, exportMimeType string, convertToMarkdown bool) (string, error) {
	body, err := s.ExportDocument(fileID, exportMimeType)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = body.Close()
	}()

	data, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("failed to read exported content: %w", err)
	}

	if convertToMarkdown {
		md, err := mdconverter.ConvertString(string(data))
		if err != nil {
			return "", fmt.Errorf("failed to convert HTML to markdown: %w", err)
		}

		return md, nil
	}

	return string(data), nil
}

// convertFileInfo converts a Drive API File object to a DriveFileInfo.
func convertFileInfo(f *drive.File) *DriveFileInfo {
	info := &DriveFileInfo{
		ID:          f.Id,
		Name:        f.Name,
		MimeType:    f.MimeType,
		WebViewLink: f.WebViewLink,
		Description: f.Description,
		Starred:     f.Starred,
		Size:        f.Size,
		Parents:     f.Parents,
	}

	for _, owner := range f.Owners {
		info.Owners = append(info.Owners, owner.DisplayName)
	}

	if f.ModifiedTime != "" {
		if t, err := time.Parse(time.RFC3339, f.ModifiedTime); err == nil {
			info.ModifiedTime = t
		}
	}

	if f.CreatedTime != "" {
		if t, err := time.Parse(time.RFC3339, f.CreatedTime); err == nil {
			info.CreatedTime = t
		}
	}

	return info
}

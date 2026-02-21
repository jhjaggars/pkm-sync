package drive

import "time"

// ListFilesOptions controls how files are listed from Google Drive.
type ListFilesOptions struct {
	// FolderID limits listing to a specific folder; empty means no folder filter.
	FolderID string
	// ModifiedAfter filters files to those modified after this time (zero = no filter).
	ModifiedAfter time.Time
	// MimeTypes restricts results to these MIME types (empty = no filter).
	MimeTypes []string
	// IncludeSharedWithMe adds "sharedWithMe = true" to the query.
	IncludeSharedWithMe bool
	// IncludeSharedDrives includes results from shared drives.
	IncludeSharedDrives bool
	// PageSize is the number of results per page (default 100, max 1000).
	PageSize int
	// MaxResults caps total results; 0 means unlimited.
	MaxResults int
	// ExtraQuery is appended with AND to the generated query.
	ExtraQuery string
}

// DriveFileInfo holds metadata for a Google Drive file.
type DriveFileInfo struct {
	ID           string
	Name         string
	MimeType     string
	WebViewLink  string
	ModifiedTime time.Time
	CreatedTime  time.Time
	Owners       []string
	Size         int64
	Parents      []string
	Description  string
	Starred      bool
}

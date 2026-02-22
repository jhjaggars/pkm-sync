package drive

import (
	"strings"
	"testing"
	"time"
)

func TestIsGoogleWorkspaceFile(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     bool
	}{
		{"google doc", MimeTypeGoogleDoc, true},
		{"google sheet", MimeTypeGoogleSheet, true},
		{"google slides", MimeTypeGooglePresentation, true},
		{"pdf", "application/pdf", false},
		{"plain text", "text/plain", false},
		{"empty", "", false},
		{"folder", "application/vnd.google-apps.folder", false},
		{"jpeg", "image/jpeg", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGoogleWorkspaceFile(tt.mimeType); got != tt.want {
				t.Errorf("IsGoogleWorkspaceFile(%q) = %v, want %v", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestBuildQuery(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		opts     ListFilesOptions
		wantPart string // substring that must appear in result
		notWant  string // substring that must NOT appear in result
	}{
		{
			name:     "always includes trashed filter",
			opts:     ListFilesOptions{},
			wantPart: "trashed = false",
		},
		{
			name:     "folder filter",
			opts:     ListFilesOptions{FolderID: "abc123"},
			wantPart: "'abc123' in parents",
		},
		{
			name:    "no folder filter when empty",
			opts:    ListFilesOptions{},
			notWant: "in parents",
		},
		{
			name:     "modified after filter",
			opts:     ListFilesOptions{ModifiedAfter: now},
			wantPart: "modifiedTime > '2025-06-01T12:00:00Z'",
		},
		{
			name:    "no modified after when zero",
			opts:    ListFilesOptions{},
			notWant: "modifiedTime",
		},
		{
			name:     "single mime type filter",
			opts:     ListFilesOptions{MimeTypes: []string{MimeTypeGoogleDoc}},
			wantPart: "mimeType = 'application/vnd.google-apps.document'",
		},
		{
			name: "multiple mime types use OR",
			opts: ListFilesOptions{MimeTypes: []string{
				MimeTypeGoogleDoc,
				MimeTypeGoogleSheet,
			}},
			wantPart: "mimeType = 'application/vnd.google-apps.document' or mimeType = 'application/vnd.google-apps.spreadsheet'",
		},
		{
			name:     "shared with me filter",
			opts:     ListFilesOptions{IncludeSharedWithMe: true},
			wantPart: "sharedWithMe = true",
		},
		{
			name:     "extra query appended",
			opts:     ListFilesOptions{ExtraQuery: "name contains 'report'"},
			wantPart: "name contains 'report'",
		},
		{
			name:     "extra query not doubled with AND when there are other filters",
			opts:     ListFilesOptions{FolderID: "abc", ExtraQuery: "name contains 'x'"},
			wantPart: "and name contains 'x'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildQuery(tt.opts)

			if tt.wantPart != "" {
				if !strings.Contains(got, tt.wantPart) {
					t.Errorf("buildQuery() = %q, want it to contain %q", got, tt.wantPart)
				}
			}

			if tt.notWant != "" {
				if strings.Contains(got, tt.notWant) {
					t.Errorf("buildQuery() = %q, want it NOT to contain %q", got, tt.notWant)
				}
			}
		})
	}
}

func TestGetExportMimeType(t *testing.T) {
	tests := []struct {
		name     string
		fileMime string
		format   string
		wantMime string
		wantErr  bool
	}{
		{"doc to txt", MimeTypeGoogleDoc, "txt", MimeTypePlainText, false},
		{"doc to md", MimeTypeGoogleDoc, "md", MimeTypeHTML, false},
		{"doc to html", MimeTypeGoogleDoc, "html", MimeTypeHTML, false},
		{"doc to csv invalid", MimeTypeGoogleDoc, "csv", "", true},
		{"sheet to csv", MimeTypeGoogleSheet, "csv", MimeTypeCSV, false},
		{"sheet to html", MimeTypeGoogleSheet, "html", MimeTypeHTML, false},
		{"sheet to md invalid", MimeTypeGoogleSheet, "md", "", true},
		{"slides to txt", MimeTypeGooglePresentation, "txt", MimeTypePlainText, false},
		{"slides to html", MimeTypeGooglePresentation, "html", MimeTypeHTML, false},
		{"slides to csv invalid", MimeTypeGooglePresentation, "csv", "", true},
		{"unsupported type", "application/pdf", "txt", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetExportMimeType(tt.fileMime, tt.format)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetExportMimeType(%q, %q) error = %v, wantErr %v", tt.fileMime, tt.format, err, tt.wantErr)

				return
			}

			if !tt.wantErr && got != tt.wantMime {
				t.Errorf("GetExportMimeType(%q, %q) = %q, want %q", tt.fileMime, tt.format, got, tt.wantMime)
			}
		})
	}
}

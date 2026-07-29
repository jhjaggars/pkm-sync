package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pkm-sync/internal/sinks"
	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/drive"
	"pkm-sync/internal/utils"
	"pkm-sync/pkg/models"

	mdconverter "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/spf13/cobra"
)

var (
	fetchFormat   string
	fetchComments bool
	fetchOutput   string
)

var driveFetchCmd = &cobra.Command{
	Use:        "fetch <URL>",
	Short:      "Fetch a Google Drive document by URL",
	Deprecated: "use 'pkm-sync fetch <URL>' instead",
	Long: `Fetch a Google Drive document by URL and output its content.

By default, content is written to stdout. Use --output to write a markdown
file with YAML frontmatter containing the source URL, title, and fetch
timestamp. This frontmatter enables 'pkm-sync drive refresh' to re-fetch
the document later.

Supports Google Docs, Sheets, and Slides URLs in various formats:
  - docs.google.com/document/d/{ID}/edit
  - docs.google.com/spreadsheets/d/{ID}/edit
  - docs.google.com/presentation/d/{ID}/edit
  - drive.google.com/file/d/{ID}/view
  - drive.google.com/open?id={ID}

Output formats:
  - txt  : Plain text (default for stdout)
  - md   : Markdown (default for --output)
  - html : HTML
  - csv  : CSV (for spreadsheets only)

Use --comments to append document comments as markdown footnotes.

Examples:
  pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit"
  pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit" --format md --comments
  pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit" --output Drive/
  pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit" --output Drive/custom-name.md`,
	Args: cobra.ExactArgs(1),
	RunE: runDriveFetchCommand,
}

func init() {
	driveCmd.AddCommand(driveFetchCmd)
	driveFetchCmd.Flags().StringVar(&fetchFormat, "format", "", "Output format (txt, md, html, csv). Defaults to md with --output, txt otherwise")
	driveFetchCmd.Flags().BoolVar(&fetchComments, "comments", false, "Append document comments as markdown footnotes")
	driveFetchCmd.Flags().StringVarP(&fetchOutput, "output", "o", "", "Write to file/directory with frontmatter (enables re-fetch via 'drive refresh')")
}

func runDriveFetchCommand(_ *cobra.Command, args []string) error {
	docURL := args[0]

	fileID, err := drive.ExtractFileID(docURL)
	if err != nil {
		return err
	}

	client, err := auth.GetClient()
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	driveService, err := drive.NewService(client)
	if err != nil {
		return fmt.Errorf("failed to create drive service: %w", err)
	}

	metadata, err := driveService.GetFileMetadata(fileID)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Default format depends on output mode.
	format := fetchFormat
	if format == "" {
		if fetchOutput != "" {
			format = "md"
		} else {
			format = "txt"
		}
	}

	exportMimeType, err := drive.GetExportMimeType(metadata.MimeType, format)
	if err != nil {
		return err
	}

	content, err := driveService.ExportDocument(fileID, exportMimeType)
	if err != nil {
		return fmt.Errorf("failed to export document: %w", err)
	}

	defer func() { _ = content.Close() }()

	if fetchComments && format != "md" {
		fmt.Fprintln(os.Stderr, "Warning: --comments is only supported with markdown format, ignoring")
	}

	// Non-markdown: write raw content.
	if format != "md" {
		if fetchOutput != "" {
			return writeRawToFile(content, resolveOutputPath(fetchOutput, metadata.Name, format))
		}

		_, err = io.Copy(os.Stdout, content)

		return err
	}

	// Markdown: convert HTML.
	htmlBytes, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("failed to read HTML content: %w", err)
	}

	markdown, err := mdconverter.ConvertString(string(htmlBytes))
	if err != nil {
		return fmt.Errorf("failed to convert HTML to markdown: %w", err)
	}

	if fetchComments {
		markdown, err = appendComments(driveService, fileID, markdown)
		if err != nil {
			return err
		}
	}

	// Write to file with frontmatter, or stdout without.
	if fetchOutput != "" {
		return writeDocToFile(docURL, metadata, markdown, resolveOutputPath(fetchOutput, metadata.Name, "md"))
	}

	_, err = fmt.Fprint(os.Stdout, markdown)

	return err
}

// writeDocToFile writes a markdown document with YAML frontmatter to disk.
// If the file already exists and content is unchanged, the write is skipped
// to avoid bumping mtime (which would cause Obsidian dataview "modified today"
// queries to show false positives on every sync run).
func writeDocToFile(sourceURL string, metadata *models.DriveFile, markdown, filePath string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Read the existing file once — used for both timestamp preservation and
	// content comparison, avoiding a double-read race.
	existingBytes, _ := os.ReadFile(filePath)

	// Preserve the original created timestamp so rendered content stays stable
	// across refreshes. Fall back to now only for new files.
	createdAt := time.Now()

	if len(existingBytes) > 0 {
		if t := extractFrontmatterTimestamp(string(existingBytes), "created"); !t.IsZero() {
			createdAt = t
		}
	}

	// Use Drive's actual modification time for UpdatedAt when available,
	// so the field reflects when the document changed, not when we synced.
	updatedAt := time.Now()
	if !metadata.ModifiedTime.IsZero() {
		updatedAt = metadata.ModifiedTime
	}

	// Build the item so we get consistent frontmatter via the obsidian formatter.
	item := &models.BasicItem{
		ID:         "drive_" + metadata.ID,
		Title:      metadata.Name,
		SourceType: "drive",
		ItemType:   "document",
		Content:    markdown,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Metadata: map[string]any{
			"source_url": sourceURL,
			"title":      metadata.Name,
		},
		Tags: []string{"source:drive"},
	}

	if len(metadata.Owners) > 0 {
		item.Metadata["owners"] = metadata.Owners
	}

	formatter := sinks.NewObsidianFormatterPublic()
	content := formatter.FormatItemContent(item)

	// Skip writing if content is unchanged to avoid bumping mtime.
	if len(existingBytes) > 0 && string(existingBytes) == content {
		return nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Wrote %s\n", filePath)

	return nil
}

// extractFrontmatterTimestamp reads a YAML frontmatter field as a time.Time.
// Returns zero time if the field is absent or unparseable.
//
// Only the leading frontmatter block (between the first and second "---" lines)
// is scanned; "---" horizontal rules in the document body are ignored.
func extractFrontmatterTimestamp(content, field string) time.Time {
	prefix := field + ": "
	seenOpen := false // true after the opening "---"
	inFrontmatter := false

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")

		if line == "---" {
			if !seenOpen {
				// Opening fence — only valid as the very first non-empty line.
				seenOpen = true
				inFrontmatter = true

				continue
			}

			// Closing fence — stop scanning.
			break
		}

		if inFrontmatter && strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			val = strings.Trim(val, "\"")

			// time.RFC3339 == "2006-01-02T15:04:05Z07:00"; only one entry needed.
			for _, layout := range []string{time.RFC3339, "2006-01-02"} {
				if t, err := time.Parse(layout, val); err == nil {
					return t
				}
			}
		}
	}

	return time.Time{}
}

// writeRawToFile writes non-markdown content directly to a file.
func writeRawToFile(content io.Reader, filePath string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	_, err = io.Copy(f, content)

	return err
}

// resolveOutputPath determines the output file path. If outputFlag is a
// directory (ends with / or exists as a dir), the filename is derived from
// the document title. Otherwise it's used as-is.
func resolveOutputPath(outputFlag, docTitle, format string) string {
	ext := "." + format
	if format == "md" {
		ext = ".md"
	}

	info, err := os.Stat(outputFlag)
	if (err == nil && info.IsDir()) || strings.HasSuffix(outputFlag, "/") {
		safe := utils.SanitizeFilename(docTitle)

		return filepath.Join(outputFlag, safe+ext)
	}

	return outputFlag
}

func appendComments(driveService *drive.Service, fileID, markdown string) (string, error) {
	comments, err := driveService.GetComments(fileID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch comments: %w", err)
	}

	if len(comments) == 0 {
		return markdown, nil
	}

	markdown = drive.InsertCommentMarkers(markdown, comments)
	markdown += "\n\n" + drive.FormatCommentsAsFootnotes(comments)

	return markdown, nil
}

// extractSourceURL reads the YAML frontmatter of a markdown file and returns
// the source_url value if present. Used by drive refresh.
func extractSourceURL(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}

	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if inFrontmatter {
				break // end of frontmatter
			}

			inFrontmatter = true

			continue
		}

		if inFrontmatter && strings.HasPrefix(line, "source_url: ") {
			val := strings.TrimPrefix(line, "source_url: ")
			val = strings.Trim(val, "\"")

			return val, nil
		}
	}

	return "", nil
}

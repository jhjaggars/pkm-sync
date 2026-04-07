package main

import (
	"fmt"
	"io"
	"os"

	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/drive"

	mdconverter "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/spf13/cobra"
)

var (
	fetchFormat   string
	fetchComments bool
)

var driveFetchCmd = &cobra.Command{
	Use:   "fetch <URL>",
	Short: "Fetch a Google Drive document by URL and output to stdout",
	Long: `Fetch a Google Drive document by URL and output its content to stdout.

Supports Google Docs, Sheets, and Slides URLs in various formats:
  - docs.google.com/document/d/{ID}/edit
  - docs.google.com/spreadsheets/d/{ID}/edit
  - docs.google.com/presentation/d/{ID}/edit
  - drive.google.com/file/d/{ID}/view
  - drive.google.com/open?id={ID}

Output formats:
  - txt  : Plain text (default)
  - md   : Markdown (converts HTML to markdown)
  - html : HTML
  - csv  : CSV (for spreadsheets only)

Use --comments to append document comments as markdown footnotes (md format only).

Examples:
  pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit"
  pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit" --format md --comments
  pkm-sync drive fetch "https://docs.google.com/spreadsheets/d/xyz789/edit" --format csv`,
	Args: cobra.ExactArgs(1),
	RunE: runDriveFetchCommand,
}

func init() {
	driveCmd.AddCommand(driveFetchCmd)
	driveFetchCmd.Flags().StringVar(&fetchFormat, "format", "txt", "Output format (txt, md, html, csv)")
	driveFetchCmd.Flags().BoolVar(&fetchComments, "comments", false, "Append document comments as markdown footnotes (md format only)")
}

func runDriveFetchCommand(cmd *cobra.Command, args []string) error {
	url := args[0]

	// Extract file ID from URL
	fileID, err := drive.ExtractFileID(url)
	if err != nil {
		return err
	}

	// Get authenticated client
	client, err := auth.GetClient()
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Create drive service
	driveService, err := drive.NewService(client)
	if err != nil {
		return fmt.Errorf("failed to create drive service: %w", err)
	}

	// Get file metadata to determine MIME type
	metadata, err := driveService.GetFileMetadata(fileID)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Determine export MIME type based on format and file type
	exportMimeType, err := drive.GetExportMimeType(metadata.MimeType, fetchFormat)
	if err != nil {
		return err
	}

	// Export document
	content, err := driveService.ExportDocument(fileID, exportMimeType)
	if err != nil {
		return fmt.Errorf("failed to export document: %w", err)
	}

	defer func() {
		_ = content.Close()
	}()

	// Warn if --comments used with non-markdown format
	if fetchComments && fetchFormat != "md" {
		fmt.Fprintln(os.Stderr, "Warning: --comments is only supported with --format md, ignoring")
	}

	// Non-markdown formats: write content directly to stdout
	if fetchFormat != "md" {
		_, err = io.Copy(os.Stdout, content)

		return err
	}

	// Markdown: convert HTML to markdown
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

	_, err = fmt.Fprint(os.Stdout, markdown)

	return err
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

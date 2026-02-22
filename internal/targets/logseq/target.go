package logseq

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

type LogseqTarget struct {
	graphPath   string
	journalPath string
	pagesPath   string
}

func NewLogseqTarget() *LogseqTarget {
	return &LogseqTarget{}
}

func (l *LogseqTarget) Name() string {
	return "logseq"
}

func (l *LogseqTarget) Configure(config map[string]interface{}) error {
	if graphPath, ok := config["graph_path"].(string); ok {
		l.graphPath = graphPath
		l.journalPath = filepath.Join(graphPath, "journals")
		l.pagesPath = filepath.Join(graphPath, "pages")
	}

	return nil
}

func (l *LogseqTarget) Export(items []models.FullItem, outputDir string) error {
	// Use flat structure - all files in outputDir
	for _, item := range items {
		if err := l.exportItem(item, outputDir); err != nil {
			return fmt.Errorf("failed to export item %s: %w", item.GetID(), err)
		}
	}

	return nil
}

func (l *LogseqTarget) exportItem(item models.FullItem, outputDir string) error {
	filename := l.FormatFilename(item.GetTitle())
	filePath := filepath.Join(outputDir, filename)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	content := l.formatContent(item)

	return os.WriteFile(filePath, []byte(content), 0644)
}

func (l *LogseqTarget) formatContent(item models.FullItem) string {
	var sb strings.Builder

	// Properties block (Logseq-specific)
	sb.WriteString("- id:: " + item.GetID() + "\n")
	sb.WriteString("- source:: " + item.GetSourceType() + "\n")
	sb.WriteString("- type:: " + item.GetItemType() + "\n")
	sb.WriteString("- created:: [[" + item.GetCreatedAt().Format("Jan 2nd, 2006") + "]]\n")

	// Add custom metadata
	for key, value := range item.GetMetadata() {
		sb.WriteString(fmt.Sprintf("- %s:: %v\n", key, value))
	}

	// Tags
	if len(item.GetTags()) > 0 {
		sb.WriteString("- tags:: ")

		for i, tag := range item.GetTags() {
			if i > 0 {
				sb.WriteString(", ")
			}

			sb.WriteString("#" + tag)
		}

		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// Title as heading
	sb.WriteString("# " + item.GetTitle() + "\n\n")

	// Content
	if item.GetContent() != "" {
		sb.WriteString(item.GetContent())
		sb.WriteString("\n\n")
	}

	// Attachments as blocks
	if len(item.GetAttachments()) > 0 {
		sb.WriteString("## Attachments\n")

		for _, attachment := range item.GetAttachments() {
			if attachment.URL != "" {
				sb.WriteString("- [" + attachment.Name + "](" + attachment.URL + ")\n")
			} else {
				sb.WriteString("- [[" + attachment.Name + "]]\n")
			}
		}

		sb.WriteString("\n")
	}

	// Links as blocks
	if len(item.GetLinks()) > 0 {
		sb.WriteString("## Links\n")

		for _, link := range item.GetLinks() {
			sb.WriteString("- [" + link.Title + "](" + link.URL + ")\n")
		}
	}

	return sb.String()
}

func (l *LogseqTarget) FormatFilename(title string) string {
	// Logseq prefers page references format
	filename := sanitizeFilename(title)

	return filename + l.GetFileExtension()
}

func (l *LogseqTarget) GetFileExtension() string {
	return ".md"
}

func (l *LogseqTarget) FormatMetadata(metadata map[string]interface{}) string {
	var sb strings.Builder
	for key, value := range metadata {
		sb.WriteString(fmt.Sprintf("- %s:: %v\n", key, value))
	}

	return sb.String()
}

// Preview generates a preview of what files would be created/modified without actually writing them.
func (l *LogseqTarget) Preview(items []models.FullItem, outputDir string) ([]*interfaces.FilePreview, error) {
	previews := make([]*interfaces.FilePreview, 0, len(items))

	for _, item := range items {
		filename := l.FormatFilename(item.GetTitle())
		filePath := filepath.Join(outputDir, filename)
		content := l.formatContent(item)

		action, existingContent, err := determineFileAction(filePath, content)
		if err != nil {
			return nil, fmt.Errorf("could not determine action for %s: %w", filePath, err)
		}

		preview := &interfaces.FilePreview{
			FilePath:        filePath,
			Action:          action,
			Content:         content,
			ExistingContent: existingContent,
			Conflict:        false, // Simplified for Logseq
		}

		previews = append(previews, preview)
	}

	return previews, nil
}

func determineFileAction(filePath, newContent string) (string, string, error) {
	existingData, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "create", "", nil // File doesn't exist
		}

		return "", "", fmt.Errorf("failed to read existing file: %w", err) // Other read error
	}

	existingContent := string(existingData)
	if existingContent == newContent {
		return "skip", existingContent, nil
	}

	return "update", existingContent, nil
}

// sanitizeFilename removes or replaces characters that are invalid in filenames.
func sanitizeFilename(filename string) string {
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

	filename = strings.TrimSpace(filename)
	for strings.Contains(filename, "  ") {
		filename = strings.ReplaceAll(filename, "  ", " ")
	}

	return filename
}

// Ensure LogseqTarget implements Target interface.
var _ interfaces.Target = (*LogseqTarget)(nil)

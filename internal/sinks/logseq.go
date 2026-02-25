package sinks

import (
	"fmt"
	"os"
	"strings"

	"pkm-sync/pkg/models"
)

type logseqFormatter struct {
	graphPath   string
	journalPath string
	pagesPath   string
}

func newLogseqFormatter() *logseqFormatter {
	return &logseqFormatter{}
}

func (l *logseqFormatter) name() string {
	return "logseq"
}

func (l *logseqFormatter) configure(config map[string]any) {
	if config == nil {
		return
	}

	if graphPath, ok := config["graph_path"].(string); ok {
		l.graphPath = graphPath
		l.journalPath = graphPath + "/journals"
		l.pagesPath = graphPath + "/pages"
	}
}

func (l *logseqFormatter) formatContent(item models.FullItem) string {
	var sb strings.Builder

	sb.WriteString("- id:: " + item.GetID() + "\n")
	sb.WriteString("- source:: " + item.GetSourceType() + "\n")
	sb.WriteString("- type:: " + item.GetItemType() + "\n")
	sb.WriteString("- created:: [[" + item.GetCreatedAt().Format("Jan 2nd, 2006") + "]]\n")

	for key, value := range item.GetMetadata() {
		fmt.Fprintf(&sb, "- %s:: %v\n", key, value)
	}

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
	sb.WriteString("# " + item.GetTitle() + "\n\n")

	if item.GetContent() != "" {
		sb.WriteString(item.GetContent())
		sb.WriteString("\n\n")
	}

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

	if len(item.GetLinks()) > 0 {
		sb.WriteString("## Links\n")

		for _, link := range item.GetLinks() {
			sb.WriteString("- [" + link.Title + "](" + link.URL + ")\n")
		}
	}

	return sb.String()
}

func (l *logseqFormatter) formatFilename(title string) string {
	return logseqSanitizeFilename(title) + l.fileExtension()
}

func (l *logseqFormatter) fileExtension() string {
	return ".md"
}

func (l *logseqFormatter) formatMetadata(metadata map[string]any) string {
	var sb strings.Builder

	for key, value := range metadata {
		fmt.Fprintf(&sb, "- %s:: %v\n", key, value)
	}

	return sb.String()
}

// logseqSanitizeFilename removes or replaces characters invalid in filenames
// while preserving spaces (Logseq's preferred page reference format).
func logseqSanitizeFilename(filename string) string {
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

	for old, newVal := range replacements {
		filename = strings.ReplaceAll(filename, old, newVal)
	}

	filename = strings.TrimSpace(filename)

	for strings.Contains(filename, "  ") {
		filename = strings.ReplaceAll(filename, "  ", " ")
	}

	return filename
}

// logseqDetermineFileAction determines whether to create, update, or skip a file.
func logseqDetermineFileAction(filePath, newContent string) (string, string, error) {
	existingData, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "create", "", nil
		}

		return "", "", fmt.Errorf("failed to read existing file: %w", err)
	}

	existingContent := string(existingData)
	if existingContent == newContent {
		return "skip", existingContent, nil
	}

	return "update", existingContent, nil
}

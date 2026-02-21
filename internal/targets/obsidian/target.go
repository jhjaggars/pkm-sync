package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pkm-sync/internal/utils"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

type ObsidianTarget struct {
	vaultPath        string
	templateDir      string
	dailyNotesFormat string
}

func NewObsidianTarget() *ObsidianTarget {
	return &ObsidianTarget{
		dailyNotesFormat: "2006-01-02", // Default: YYYY-MM-DD
	}
}

func (o *ObsidianTarget) Name() string {
	return "obsidian"
}

func (o *ObsidianTarget) Configure(config map[string]interface{}) error {
	if vaultPath, ok := config["vault_path"].(string); ok {
		o.vaultPath = vaultPath
	}

	if templateDir, ok := config["template_dir"].(string); ok {
		o.templateDir = templateDir
	}

	if format, ok := config["daily_notes_format"].(string); ok {
		o.dailyNotesFormat = format
	}

	return nil
}

func (o *ObsidianTarget) Export(items []models.FullItem, outputDir string) error {
	for _, item := range items {
		if err := o.exportItem(item, outputDir); err != nil {
			return fmt.Errorf("failed to export item %s: %w", item.GetID(), err)
		}
	}

	return nil
}

func (o *ObsidianTarget) exportItem(item models.FullItem, outputDir string) error {
	filename := o.FormatFilename(item.GetTitle())
	filePath := filepath.Join(outputDir, filename)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	content := o.formatContent(item)

	return os.WriteFile(filePath, []byte(content), 0644)
}

func (o *ObsidianTarget) formatContent(item models.FullItem) string {
	// Handle different item types
	if models.IsThread(item) {
		return o.formatThreadContent(item)
	}

	// Default: format as basic item
	return o.formatBasicItemContent(item)
}

func (o *ObsidianTarget) formatBasicItemContent(item models.FullItem) string {
	var sb strings.Builder

	// YAML frontmatter
	sb.WriteString("---\n")
	sb.WriteString(o.FormatMetadata(item.GetMetadata()))
	sb.WriteString(fmt.Sprintf("id: %s\n", item.GetID()))
	sb.WriteString(fmt.Sprintf("source: %s\n", item.GetSourceType()))
	sb.WriteString(fmt.Sprintf("type: %s\n", item.GetItemType()))
	sb.WriteString(fmt.Sprintf("created: %s\n", item.GetCreatedAt().Format(time.RFC3339)))

	if len(item.GetTags()) > 0 {
		sb.WriteString("tags:\n")

		for _, tag := range item.GetTags() {
			sb.WriteString(fmt.Sprintf("  - %s\n", tag))
		}
	}

	sb.WriteString("---\n\n")

	// Title
	sb.WriteString(fmt.Sprintf("# %s\n\n", item.GetTitle()))

	// Content
	if item.GetContent() != "" {
		sb.WriteString(item.GetContent())
		sb.WriteString("\n\n")
	}

	// Attachments
	if len(item.GetAttachments()) > 0 {
		sb.WriteString("## Attachments\n\n")

		for _, attachment := range item.GetAttachments() {
			if attachment.URL != "" {
				sb.WriteString(fmt.Sprintf("- [%s](%s)\n", attachment.Name, attachment.URL))
			} else {
				sb.WriteString(fmt.Sprintf("- %s\n", attachment.Name))
			}
		}

		sb.WriteString("\n")
	}

	// Links
	if len(item.GetLinks()) > 0 {
		sb.WriteString("## Links\n\n")

		for _, link := range item.GetLinks() {
			sb.WriteString(fmt.Sprintf("- [%s](%s)\n", link.Title, link.URL))
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

func (o *ObsidianTarget) formatThreadContent(item models.FullItem) string {
	thread, ok := models.AsThread(item)
	if !ok {
		// Fallback to basic item formatting
		return o.formatBasicItemContent(item)
	}

	var sb strings.Builder

	// YAML frontmatter for thread
	sb.WriteString("---\n")
	sb.WriteString(o.FormatMetadata(thread.GetMetadata()))
	sb.WriteString(fmt.Sprintf("id: %s\n", thread.GetID()))
	sb.WriteString(fmt.Sprintf("source: %s\n", thread.GetSourceType()))
	sb.WriteString(fmt.Sprintf("type: %s\n", thread.GetItemType()))
	sb.WriteString(fmt.Sprintf("created: %s\n", thread.GetCreatedAt().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("message_count: %d\n", len(thread.GetMessages())))

	if len(thread.GetTags()) > 0 {
		sb.WriteString("tags:\n")

		for _, tag := range thread.GetTags() {
			sb.WriteString(fmt.Sprintf("  - %s\n", tag))
		}
	}

	sb.WriteString("---\n\n")

	// Thread title
	sb.WriteString(fmt.Sprintf("# %s\n\n", thread.GetTitle()))

	// Thread summary/content
	if thread.GetContent() != "" {
		sb.WriteString("## Thread Summary\n\n")
		sb.WriteString(thread.GetContent())
		sb.WriteString("\n\n")
	}

	// Individual messages
	if len(thread.GetMessages()) > 0 {
		sb.WriteString("## Messages\n\n")

		for i, message := range thread.GetMessages() {
			o.formatThreadMessage(&sb, i+1, message)
		}
	}

	return sb.String()
}

// formatThreadMessage formats a single message within a thread to reduce complexity.
func (o *ObsidianTarget) formatThreadMessage(sb *strings.Builder, messageNum int, message models.FullItem) {
	sb.WriteString(fmt.Sprintf("### Message %d: %s\n\n", messageNum, message.GetTitle()))

	// Message metadata
	sb.WriteString(fmt.Sprintf("**From:** %s  \n", message.GetSourceType()))
	sb.WriteString(fmt.Sprintf("**Created:** %s  \n", message.GetCreatedAt().Format(time.RFC3339)))

	if len(message.GetTags()) > 0 {
		sb.WriteString(fmt.Sprintf("**Tags:** %s  \n", strings.Join(message.GetTags(), ", ")))
	}

	sb.WriteString("\n")

	// Message content
	if message.GetContent() != "" {
		sb.WriteString(message.GetContent())
		sb.WriteString("\n\n")
	}

	// Message attachments
	if len(message.GetAttachments()) > 0 {
		sb.WriteString("**Attachments:**\n")

		for _, attachment := range message.GetAttachments() {
			if attachment.URL != "" {
				sb.WriteString(fmt.Sprintf("- [%s](%s)\n", attachment.Name, attachment.URL))
			} else {
				sb.WriteString(fmt.Sprintf("- %s\n", attachment.Name))
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
}

func (o *ObsidianTarget) FormatFilename(title string) string {
	return utils.SanitizeFilename(title) + o.GetFileExtension()
}

func (o *ObsidianTarget) GetFileExtension() string {
	return ".md"
}

func (o *ObsidianTarget) FormatMetadata(metadata map[string]interface{}) string {
	if len(metadata) == 0 {
		return ""
	}

	var sb strings.Builder

	for key, value := range metadata {
		if key == "attendees" {
			sb.WriteString(o.formatAttendees(value))
		} else {
			sb.WriteString(fmt.Sprintf("%s: %v\n", key, value))
		}
	}

	return sb.String()
}

// formatAttendees formats attendees as wikilink arrays for Obsidian.
func (o *ObsidianTarget) formatAttendees(attendeesValue interface{}) string {
	var sb strings.Builder

	// Handle different types that attendees might be stored as
	switch attendees := attendeesValue.(type) {
	case []models.Attendee:
		if len(attendees) == 0 {
			return ""
		}

		sb.WriteString("attendees:\n")

		for _, attendee := range attendees {
			displayName := attendee.GetDisplayName()
			sb.WriteString(fmt.Sprintf("  - \"[[%s]]\"\n", displayName))
		}
	case []interface{}:
		// Handle case where attendees might be stored as generic interface slice
		if len(attendees) == 0 {
			return ""
		}

		sb.WriteString("attendees:\n")

		for _, attendee := range attendees {
			if attendeeMap, ok := attendee.(map[string]interface{}); ok {
				var displayName string
				if name, exists := attendeeMap["DisplayName"].(string); exists && name != "" {
					displayName = name
				} else if email, exists := attendeeMap["Email"].(string); exists {
					displayName = email
				} else {
					displayName = fmt.Sprintf("%v", attendee)
				}

				sb.WriteString(fmt.Sprintf("  - \"[[%s]]\"\n", displayName))
			} else {
				sb.WriteString(fmt.Sprintf("  - \"[[%v]]\"\n", attendee))
			}
		}
	default:
		// Fallback for other types
		sb.WriteString(fmt.Sprintf("attendees: %v\n", attendeesValue))
	}

	return sb.String()
}

// Preview generates a preview of what files would be created/modified without actually writing them.
func (o *ObsidianTarget) Preview(items []models.FullItem, outputDir string) ([]*interfaces.FilePreview, error) {
	previews := make([]*interfaces.FilePreview, 0, len(items))

	for _, item := range items {
		filename := o.FormatFilename(item.GetTitle())
		filePath := filepath.Join(outputDir, filename)
		content := o.formatContent(item)

		var existingContent string

		var conflict bool

		if data, err := os.ReadFile(filePath); err == nil {
			existingContent = string(data)
			conflict = existingContent != content
		}

		action := "create"

		if existingContent != "" {
			if conflict {
				action = "update"
			} else {
				action = "skip"
			}
		}

		preview := &interfaces.FilePreview{
			FilePath:        filePath,
			Action:          action,
			Content:         content,
			ExistingContent: existingContent,
			Conflict:        conflict,
		}

		previews = append(previews, preview)
	}

	return previews, nil
}

// Ensure ObsidianTarget implements Target interface.
var _ interfaces.Target = (*ObsidianTarget)(nil)

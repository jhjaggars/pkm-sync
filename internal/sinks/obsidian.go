package sinks

import (
	"fmt"
	"strings"
	"time"

	"pkm-sync/internal/utils"
	"pkm-sync/pkg/models"
)

type obsidianFormatter struct {
	vaultPath        string
	templateDir      string
	dailyNotesFormat string
}

func newObsidianFormatter() *obsidianFormatter {
	return &obsidianFormatter{
		dailyNotesFormat: "2006-01-02",
	}
}

func (o *obsidianFormatter) name() string {
	return "obsidian"
}

func (o *obsidianFormatter) configure(config map[string]any) {
	if config == nil {
		return
	}

	if vaultPath, ok := config["vault_path"].(string); ok {
		o.vaultPath = vaultPath
	}

	if templateDir, ok := config["template_dir"].(string); ok {
		o.templateDir = templateDir
	}

	if format, ok := config["daily_notes_format"].(string); ok {
		o.dailyNotesFormat = format
	}
}

func (o *obsidianFormatter) formatContent(item models.FullItem) string {
	if models.IsThread(item) {
		return o.formatThreadContent(item)
	}

	return o.formatBasicItemContent(item)
}

func (o *obsidianFormatter) formatBasicItemContent(item models.FullItem) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(o.formatMetadata(item.GetMetadata()))
	fmt.Fprintf(&sb, "id: %s\n", item.GetID())
	fmt.Fprintf(&sb, "source: %s\n", item.GetSourceType())
	fmt.Fprintf(&sb, "type: %s\n", item.GetItemType())
	fmt.Fprintf(&sb, "created: %s\n", item.GetCreatedAt().Format(time.RFC3339))

	if len(item.GetTags()) > 0 {
		sb.WriteString("tags:\n")

		for _, tag := range item.GetTags() {
			fmt.Fprintf(&sb, "  - %s\n", tag)
		}
	}

	sb.WriteString("---\n\n")
	fmt.Fprintf(&sb, "# %s\n\n", item.GetTitle())

	if item.GetContent() != "" {
		sb.WriteString(item.GetContent())
		sb.WriteString("\n\n")
	}

	if len(item.GetAttachments()) > 0 {
		sb.WriteString("## Attachments\n\n")

		for _, attachment := range item.GetAttachments() {
			if attachment.URL != "" {
				fmt.Fprintf(&sb, "- [%s](%s)\n", attachment.Name, attachment.URL)
			} else {
				fmt.Fprintf(&sb, "- %s\n", attachment.Name)
			}
		}

		sb.WriteString("\n")
	}

	if len(item.GetLinks()) > 0 {
		sb.WriteString("## Links\n\n")

		for _, link := range item.GetLinks() {
			fmt.Fprintf(&sb, "- [%s](%s)\n", link.Title, link.URL)
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

func (o *obsidianFormatter) formatThreadContent(item models.FullItem) string {
	thread, ok := models.AsThread(item)
	if !ok {
		return o.formatBasicItemContent(item)
	}

	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(o.formatMetadata(thread.GetMetadata()))
	fmt.Fprintf(&sb, "id: %s\n", thread.GetID())
	fmt.Fprintf(&sb, "source: %s\n", thread.GetSourceType())
	fmt.Fprintf(&sb, "type: %s\n", thread.GetItemType())
	fmt.Fprintf(&sb, "created: %s\n", thread.GetCreatedAt().Format(time.RFC3339))
	fmt.Fprintf(&sb, "message_count: %d\n", len(thread.GetMessages()))

	if len(thread.GetTags()) > 0 {
		sb.WriteString("tags:\n")

		for _, tag := range thread.GetTags() {
			fmt.Fprintf(&sb, "  - %s\n", tag)
		}
	}

	sb.WriteString("---\n\n")
	fmt.Fprintf(&sb, "# %s\n\n", thread.GetTitle())

	if thread.GetContent() != "" {
		sb.WriteString("## Thread Summary\n\n")
		sb.WriteString(thread.GetContent())
		sb.WriteString("\n\n")
	}

	if len(thread.GetMessages()) > 0 {
		sb.WriteString("## Messages\n\n")

		for i, message := range thread.GetMessages() {
			o.formatThreadMessage(&sb, i+1, message)
		}
	}

	return sb.String()
}

func (o *obsidianFormatter) formatThreadMessage(sb *strings.Builder, messageNum int, message models.FullItem) {
	fmt.Fprintf(sb, "### Message %d: %s\n\n", messageNum, message.GetTitle())
	fmt.Fprintf(sb, "**From:** %s  \n", message.GetSourceType())
	fmt.Fprintf(sb, "**Created:** %s  \n", message.GetCreatedAt().Format(time.RFC3339))

	if len(message.GetTags()) > 0 {
		fmt.Fprintf(sb, "**Tags:** %s  \n", strings.Join(message.GetTags(), ", "))
	}

	sb.WriteString("\n")

	if message.GetContent() != "" {
		sb.WriteString(message.GetContent())
		sb.WriteString("\n\n")
	}

	if len(message.GetAttachments()) > 0 {
		sb.WriteString("**Attachments:**\n")

		for _, attachment := range message.GetAttachments() {
			if attachment.URL != "" {
				fmt.Fprintf(sb, "- [%s](%s)\n", attachment.Name, attachment.URL)
			} else {
				fmt.Fprintf(sb, "- %s\n", attachment.Name)
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
}

func (o *obsidianFormatter) formatFilename(title string) string {
	return utils.SanitizeFilename(title) + o.fileExtension()
}

func (o *obsidianFormatter) fileExtension() string {
	return ".md"
}

func (o *obsidianFormatter) formatMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}

	var sb strings.Builder

	for key, value := range metadata {
		if key == "attendees" {
			sb.WriteString(o.formatAttendees(value))
		} else {
			fmt.Fprintf(&sb, "%s: %v\n", key, value)
		}
	}

	return sb.String()
}

func (o *obsidianFormatter) formatAttendees(attendeesValue any) string {
	var sb strings.Builder

	switch attendees := attendeesValue.(type) {
	case []models.Attendee:
		if len(attendees) == 0 {
			return ""
		}

		sb.WriteString("attendees:\n")

		for _, attendee := range attendees {
			fmt.Fprintf(&sb, "  - \"[[%s]]\"\n", attendee.GetDisplayName())
		}
	case []any:
		if len(attendees) == 0 {
			return ""
		}

		sb.WriteString("attendees:\n")

		for _, attendee := range attendees {
			if attendeeMap, ok := attendee.(map[string]any); ok {
				var displayName string

				if name, exists := attendeeMap["DisplayName"].(string); exists && name != "" {
					displayName = name
				} else if email, exists := attendeeMap["Email"].(string); exists {
					displayName = email
				} else {
					displayName = fmt.Sprintf("%v", attendee)
				}

				fmt.Fprintf(&sb, "  - \"[[%s]]\"\n", displayName)
			} else {
				fmt.Fprintf(&sb, "  - \"[[%v]]\"\n", attendee)
			}
		}
	default:
		fmt.Fprintf(&sb, "attendees: %v\n", attendeesValue)
	}

	return sb.String()
}

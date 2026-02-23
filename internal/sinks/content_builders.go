package sinks

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	mdconverter "github.com/JohannesKaufmann/html-to-markdown/v2"
)

var multipleNewlines = regexp.MustCompile(`\n\s*\n\s*\n`)

// itemGroup groups items that belong to the same logical document (thread, event, or document).
type itemGroup struct {
	threadID   string
	subject    string
	messages   []models.FullItem
	startTime  time.Time
	endTime    time.Time
	sourceName string
}

// Source type constants used for builder dispatch and document metadata.
const (
	sourceTypeGmail    = "gmail"
	sourceTypeCalendar = "google_calendar"
	sourceTypeDrive    = "google_drive"
	sourceTypeUnknown  = "unknown"
)

// contentBuilder provides source-type-specific content and metadata construction for VectorSink.
type contentBuilder interface {
	buildContent(group *itemGroup) string
	buildMetadata(group *itemGroup) map[string]any
	cleanTitle(item models.FullItem) string
	sourceType() string
}

// getContentBuilder returns the appropriate builder for the given source type.
func getContentBuilder(srcType string) contentBuilder {
	switch srcType {
	case sourceTypeGmail:
		return &gmailBuilder{}
	case sourceTypeCalendar:
		return &calendarBuilder{}
	case sourceTypeDrive:
		return &driveBuilder{}
	default:
		return &genericBuilder{}
	}
}

// collapseWhitespace reduces multiple consecutive newlines to two.
func collapseWhitespace(content string) string {
	return multipleNewlines.ReplaceAllString(content, "\n\n")
}

// --- gmailBuilder ---

type gmailBuilder struct{}

func (b *gmailBuilder) sourceType() string { return sourceTypeGmail }

func (b *gmailBuilder) cleanTitle(item models.FullItem) string {
	subject := strings.TrimSpace(item.GetTitle())
	prefixes := []string{"Re: ", "RE: ", "Fwd: ", "FWD: "}

	for changed := true; changed; {
		changed = false

		for _, prefix := range prefixes {
			if rest, ok := strings.CutPrefix(subject, prefix); ok {
				subject = rest
				changed = true
			}
		}
	}

	return subject
}

func (b *gmailBuilder) buildContent(group *itemGroup) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Thread: %s\n\n", group.subject))

	for i, item := range group.messages {
		sb.WriteString(fmt.Sprintf("--- Message %d (%s) ---\n", i+1, item.GetCreatedAt().Format("2006-01-02 15:04")))

		metadata := item.GetMetadata()

		if from, ok := metadata["from"].(string); ok && from != "" {
			sb.WriteString(fmt.Sprintf("From: %s\n", from))
		}

		if to, ok := metadata["to"].(string); ok && to != "" {
			sb.WriteString(fmt.Sprintf("To: %s\n", to))
		}

		if cc, ok := metadata["cc"].(string); ok && cc != "" {
			sb.WriteString(fmt.Sprintf("Cc: %s\n", cc))
		}

		if bcc, ok := metadata["bcc"].(string); ok && bcc != "" {
			sb.WriteString(fmt.Sprintf("Bcc: %s\n", bcc))
		}

		sb.WriteString("\n")

		content := b.prepareContent(item.GetContent())
		if content != "" {
			sb.WriteString(content)
		} else {
			sb.WriteString("(no content)")
		}

		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (b *gmailBuilder) buildMetadata(group *itemGroup) map[string]any {
	// Collect participants from all messages
	participantsMap := make(map[string]bool)

	for _, item := range group.messages {
		metadata := item.GetMetadata()

		for _, field := range []string{"from", "to", "cc", "bcc"} {
			if val, ok := metadata[field].(string); ok && val != "" {
				participantsMap[val] = true
			}
		}
	}

	participants := make([]string, 0, len(participantsMap))
	for p := range participantsMap {
		participants = append(participants, p)
	}

	messageIDs := make([]string, len(group.messages))
	for i, msg := range group.messages {
		messageIDs[i] = msg.GetID()
	}

	messages := make([]map[string]any, len(group.messages))
	for i, msg := range group.messages {
		metadata := msg.GetMetadata()

		msgData := map[string]any{
			"date":    msg.GetCreatedAt().Format(time.RFC3339),
			"subject": msg.GetTitle(),
		}

		if from, ok := metadata["from"].(string); ok {
			msgData["from"] = from
		}

		if to, ok := metadata["to"].(string); ok {
			msgData["to"] = to
		}

		if cc, ok := metadata["cc"].(string); ok {
			msgData["cc"] = cc
		}

		if bcc, ok := metadata["bcc"].(string); ok {
			msgData["bcc"] = bcc
		}

		messages[i] = msgData
	}

	return map[string]any{
		"participants":  participants,
		"message_ids":   messageIDs,
		"message_count": len(group.messages),
		"date_range": map[string]string{
			"start": group.startTime.Format(time.RFC3339),
			"end":   group.endTime.Format(time.RFC3339),
		},
		"messages": messages,
	}
}

// prepareContent converts HTML to markdown and cleans content for embeddings.
func (b *gmailBuilder) prepareContent(content string) string {
	if !strings.Contains(content, "<") || !strings.Contains(content, ">") {
		return content
	}

	markdown, err := mdconverter.ConvertString(content)
	if err != nil {
		return content
	}

	markdown = b.stripQuotedText(markdown)
	markdown = collapseWhitespace(markdown)

	return strings.TrimSpace(markdown)
}

// stripQuotedText removes email quoted text from content.
func (b *gmailBuilder) stripQuotedText(content string) string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			break
		}

		if strings.HasPrefix(trimmed, "On ") && strings.Contains(trimmed, " wrote:") {
			break
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// --- calendarBuilder ---

type calendarBuilder struct{}

func (b *calendarBuilder) sourceType() string { return sourceTypeCalendar }

func (b *calendarBuilder) cleanTitle(item models.FullItem) string {
	return strings.TrimSpace(item.GetTitle())
}

func (b *calendarBuilder) buildContent(group *itemGroup) string {
	if len(group.messages) == 0 {
		return ""
	}

	item := group.messages[0]

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Event: %s\n\n", group.subject))

	metadata := item.GetMetadata()

	if start, ok := metadata["start_time"].(time.Time); ok {
		sb.WriteString(fmt.Sprintf("Start: %s\n", start.Format("2006-01-02 15:04")))
	}

	if end, ok := metadata["end_time"].(time.Time); ok {
		sb.WriteString(fmt.Sprintf("End: %s\n", end.Format("2006-01-02 15:04")))
	}

	if location, ok := metadata["location"].(string); ok && location != "" {
		sb.WriteString(fmt.Sprintf("Location: %s\n", location))
	}

	if attendees, ok := metadata["attendees"].([]models.Attendee); ok && len(attendees) > 0 {
		names := make([]string, len(attendees))
		for i, a := range attendees {
			names[i] = a.GetDisplayName()
		}

		sb.WriteString(fmt.Sprintf("Attendees: %s\n", strings.Join(names, ", ")))
	}

	for _, link := range item.GetLinks() {
		if link.Type == "meeting_url" {
			sb.WriteString(fmt.Sprintf("Meeting URL: %s\n", link.URL))

			break
		}
	}

	if content := strings.TrimSpace(item.GetContent()); content != "" {
		sb.WriteString(fmt.Sprintf("\n%s\n", content))
	}

	return sb.String()
}

func (b *calendarBuilder) buildMetadata(group *itemGroup) map[string]any {
	result := map[string]any{
		"date_range": map[string]string{
			"start": group.startTime.Format(time.RFC3339),
			"end":   group.endTime.Format(time.RFC3339),
		},
	}

	if len(group.messages) == 0 {
		return result
	}

	metadata := group.messages[0].GetMetadata()

	if start, ok := metadata["start_time"]; ok {
		result["start_time"] = start
	}

	if end, ok := metadata["end_time"]; ok {
		result["end_time"] = end
	}

	if location, ok := metadata["location"].(string); ok && location != "" {
		result["location"] = location
	}

	if attendees, ok := metadata["attendees"].([]models.Attendee); ok && len(attendees) > 0 {
		names := make([]string, len(attendees))
		for i, a := range attendees {
			names[i] = a.GetDisplayName()
		}

		result["attendees"] = names
	}

	return result
}

// --- driveBuilder ---

type driveBuilder struct{}

func (b *driveBuilder) sourceType() string { return sourceTypeDrive }

func (b *driveBuilder) cleanTitle(item models.FullItem) string {
	return strings.TrimSpace(item.GetTitle())
}

func (b *driveBuilder) buildContent(group *itemGroup) string {
	if len(group.messages) == 0 {
		return ""
	}

	item := group.messages[0]

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Document: %s\n\n", group.subject))

	metadata := item.GetMetadata()

	if mimeType, ok := metadata["mime_type"].(string); ok && mimeType != "" {
		sb.WriteString(fmt.Sprintf("Type: %s\n", mimeType))
	}

	if owners, ok := metadata["owners"].([]string); ok && len(owners) > 0 {
		sb.WriteString(fmt.Sprintf("Owners: %s\n", strings.Join(owners, ", ")))
	}

	if webLink, ok := metadata["web_view_link"].(string); ok && webLink != "" {
		sb.WriteString(fmt.Sprintf("Link: %s\n", webLink))
	}

	if content := strings.TrimSpace(item.GetContent()); content != "" {
		sb.WriteString(fmt.Sprintf("\n%s\n", content))
	}

	return sb.String()
}

func (b *driveBuilder) buildMetadata(group *itemGroup) map[string]any {
	result := map[string]any{
		"date_range": map[string]string{
			"start": group.startTime.Format(time.RFC3339),
			"end":   group.endTime.Format(time.RFC3339),
		},
	}

	if len(group.messages) == 0 {
		return result
	}

	metadata := group.messages[0].GetMetadata()

	for _, key := range []string{"mime_type", "web_view_link", "owners"} {
		if val, ok := metadata[key]; ok {
			result[key] = val
		}
	}

	return result
}

// --- genericBuilder ---

type genericBuilder struct{}

func (b *genericBuilder) sourceType() string { return sourceTypeUnknown }

func (b *genericBuilder) cleanTitle(item models.FullItem) string {
	return strings.TrimSpace(item.GetTitle())
}

func (b *genericBuilder) buildContent(group *itemGroup) string {
	if len(group.messages) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Item: %s\n\n", group.subject))

	for _, item := range group.messages {
		if content := strings.TrimSpace(item.GetContent()); content != "" {
			sb.WriteString(content)
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}

func (b *genericBuilder) buildMetadata(group *itemGroup) map[string]any {
	result := map[string]any{
		"date_range": map[string]string{
			"start": group.startTime.Format(time.RFC3339),
			"end":   group.endTime.Format(time.RFC3339),
		},
	}

	if len(group.messages) == 0 {
		return result
	}

	for k, v := range group.messages[0].GetMetadata() {
		result[k] = v
	}

	return result
}

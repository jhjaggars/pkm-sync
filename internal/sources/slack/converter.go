package slack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pkm-sync/pkg/models"
)

// messageEntry holds a top-level Slack message with its resolved author and any thread replies.
type messageEntry struct {
	msg     RawMessage
	author  string
	replies []replyEntry
}

// replyEntry holds a single thread reply with its resolved author.
type replyEntry struct {
	msg    RawMessage
	author string
}

// ExtractMessageText walks rich_text blocks or falls back to the text field.
func ExtractMessageText(msg *RawMessage) string {
	if len(msg.Blocks) > 0 {
		var texts []string

		for _, blockRaw := range msg.Blocks {
			var block map[string]any
			if err := json.Unmarshal(blockRaw, &block); err != nil {
				continue
			}

			if block["type"] != "rich_text" {
				continue
			}

			elements, _ := block["elements"].([]any)

			for _, elemRaw := range elements {
				elem, _ := elemRaw.(map[string]any)
				subElements, _ := elem["elements"].([]any)

				for _, subRaw := range subElements {
					sub, _ := subRaw.(map[string]any)

					if text, ok := sub["text"].(string); ok && text != "" {
						texts = append(texts, text)
					}
				}
			}
		}

		if len(texts) > 0 {
			return strings.Join(texts, "")
		}
	}

	return msg.Text
}

// tsToTime converts a Slack timestamp string (Unix seconds with decimals) to time.Time.
func tsToTime(ts string) time.Time {
	if ts == "" {
		return time.Now()
	}

	f, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return time.Now()
	}

	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)

	return time.Unix(sec, nsec)
}

// messageURL builds the Slack deep link for a message.
func messageURL(workspaceURL, channelID, ts string) string {
	tsNoDecimal := strings.ReplaceAll(ts, ".", "")

	return fmt.Sprintf("%s/archives/%s/p%s", strings.TrimRight(workspaceURL, "/"), channelID, tsNoDecimal)
}

// formatTime formats a Slack timestamp as "HH:MM" in UTC.
func formatTime(ts string) string {
	return tsToTime(ts).UTC().Format("15:04")
}

// BuildDailyNote aggregates all messages for a channel on a given day into a single FullItem.
// Threads are rendered inline — replies appear indented under the parent message.
func BuildDailyNote(
	date time.Time, entries []messageEntry, channelID, channelName, workspaceURL string,
) models.FullItem {
	dateStr := date.Format("2006-01-02")
	title := fmt.Sprintf("%s \u2014 %s", channelName, dateStr)

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# #%s \u2014 %s\n\n", channelName, dateStr))

	for i, entry := range entries {
		content := ExtractMessageText(&entry.msg)
		url := messageURL(workspaceURL, channelID, entry.msg.Ts)
		t := formatTime(entry.msg.Ts)

		sb.WriteString(fmt.Sprintf("**%s** \u00b7 [%s](%s)\n", entry.author, t, url))

		if content != "" {
			sb.WriteString(content)
			sb.WriteString("\n")
		}

		if len(entry.replies) > 0 {
			sb.WriteString("\n")

			for _, reply := range entry.replies {
				replyContent := ExtractMessageText(&reply.msg)
				replyTime := formatTime(reply.msg.Ts)
				sb.WriteString(fmt.Sprintf("> **%s** \u00b7 %s \u2014 %s\n", reply.author, replyTime, replyContent))
			}
		}

		if i < len(entries)-1 {
			sb.WriteString("\n---\n\n")
		}
	}

	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	return &models.BasicItem{
		ID:         fmt.Sprintf("slack_daily_%s_%s", channelID, dateStr),
		Title:      title,
		Content:    sb.String(),
		SourceType: "slack",
		ItemType:   "slack_daily",
		CreatedAt:  dayStart,
		UpdatedAt:  dayStart,
		Tags:       []string{"slack", fmt.Sprintf("channel:%s", channelName)},
		Metadata: map[string]any{
			"channel":       channelName,
			"channel_id":    channelID,
			"workspace":     workspaceURL,
			"date":          dateStr,
			"message_count": len(entries),
		},
		Links:       []models.Link{},
		Attachments: []models.Attachment{},
	}
}

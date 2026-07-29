package slack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pkm-sync/pkg/models"
)

// UserResolver is a function that resolves Slack user IDs to display names.
type UserResolver func(userID string) string

// processRichTextElement converts a single rich_text subelement to text.
func processRichTextElement(sub map[string]any, userResolver UserResolver) string {
	subType, _ := sub["type"].(string)

	switch subType {
	case "text":
		if text, ok := sub["text"].(string); ok && text != "" {
			return text
		}

	case "user":
		if userID, ok := sub["user_id"].(string); ok && userID != "" {
			displayName := userID
			if userResolver != nil {
				displayName = userResolver(userID)
			}

			return "@" + displayName
		}

	case "link":
		url, hasURL := sub["url"].(string)
		linkText, hasText := sub["text"].(string)

		if hasText && linkText != "" && hasURL && url != "" {
			return fmt.Sprintf("%s (%s)", linkText, url)
		} else if hasText && linkText != "" {
			return linkText
		} else if hasURL && url != "" {
			return url
		}

	case "channel":
		if channelID, ok := sub["channel_id"].(string); ok && channelID != "" {
			return "#" + channelID
		}

	case "emoji":
		if name, ok := sub["name"].(string); ok && name != "" {
			return ":" + name + ":"
		}
	}

	return ""
}

// ExtractMessageText walks rich_text blocks or falls back to the text field.
// The userResolver parameter is used to resolve user mentions; if nil, raw user IDs are used.
func ExtractMessageText(msg *RawMessage, userResolver UserResolver) string {
	if len(msg.Blocks) == 0 {
		return msg.Text
	}

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
				if text := processRichTextElement(sub, userResolver); text != "" {
					texts = append(texts, text)
				}
			}
		}
	}

	if len(texts) > 0 {
		return strings.Join(texts, "")
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

// FromSlackMessage converts a raw Slack message into an individual *models.BasicItem.
// isReply indicates whether the message is a thread reply (as opposed to a top-level message).
// userResolver is used to resolve user mentions in rich_text; pass nil to use raw user IDs.
func FromSlackMessage(
	msg *RawMessage, channelID, channelName, workspaceURL, author string, isReply bool, userResolver UserResolver,
) *models.BasicItem {
	content := ExtractMessageText(msg, userResolver)

	// Build title: first 80 chars of content, or fallback to channel name.
	title := content
	if len(title) > 80 {
		title = title[:80]
	}

	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("[slack] #%s", channelName)
	}

	itemType := "slack_message"
	if isReply {
		itemType = "slack_reply"
	}

	ts := tsToTime(msg.Ts)

	tags := []string{sourceTypeSlack, fmt.Sprintf("channel:%s", channelName)}

	url := messageURL(workspaceURL, channelID, msg.Ts)
	links := []models.Link{
		{
			URL:   url,
			Title: fmt.Sprintf("Slack message in #%s", channelName),
			Type:  "external",
		},
	}

	threadTs := ""
	isThreadRoot := false

	if isReply {
		threadTs = msg.ThreadTs
		isThreadRoot = false
	} else {
		isThreadRoot = msg.ThreadTs == msg.Ts && msg.ReplyCount > 0
		if isThreadRoot {
			threadTs = msg.ThreadTs
		}
	}

	return &models.BasicItem{
		ID:          fmt.Sprintf("slack_%s_%s", channelID, msg.Ts),
		Title:       title,
		Content:     content,
		SourceType:  sourceTypeSlack,
		ItemType:    itemType,
		CreatedAt:   ts,
		UpdatedAt:   ts,
		Tags:        tags,
		Links:       links,
		Attachments: []models.Attachment{},
		Metadata: map[string]any{
			channelParamKey:  channelName,
			"channel_id":     channelID,
			"workspace":      workspaceURL,
			"author":         author,
			"ts":             msg.Ts,
			"thread_ts":      threadTs,
			"is_thread_root": isThreadRoot,
			"reply_count":    msg.ReplyCount,
		},
	}
}

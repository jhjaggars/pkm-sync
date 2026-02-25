package slack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pkm-sync/pkg/models"
)

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

// FromSlackMessage converts a RawMessage to a models.FullItem.
func FromSlackMessage(msg *RawMessage, channelID, channelName, workspaceURL, authorName string) models.FullItem {
	id := fmt.Sprintf("slack_%s_%s", channelID, msg.Ts)
	t := tsToTime(msg.Ts)
	content := ExtractMessageText(msg)

	title := content
	if len(title) > 80 {
		title = title[:80]
	}

	if title == "" {
		title = fmt.Sprintf("Slack message in #%s", channelName)
	}

	item := &models.BasicItem{
		ID:         id,
		Title:      title,
		Content:    content,
		SourceType: "slack",
		ItemType:   "message",
		CreatedAt:  t,
		UpdatedAt:  t,
		Tags:       []string{"slack", fmt.Sprintf("channel:%s", channelName)},
		Metadata: map[string]any{
			"channel":    channelName,
			"channel_id": channelID,
			"workspace":  workspaceURL,
			"ts":         msg.Ts,
			"author":     authorName,
		},
		Links: []models.Link{
			{
				URL:   messageURL(workspaceURL, channelID, msg.Ts),
				Title: "Slack Message",
				Type:  "source",
			},
		},
		Attachments: []models.Attachment{},
	}

	return item
}

// FromSlackThread converts a parent message and its replies into a models.Thread.
func FromSlackThread(
	parent *RawMessage, replies []RawMessage,
	channelID, channelName, workspaceURL, authorName string,
	userCache *UserCache, client *Client,
) *models.Thread {
	parentText := ExtractMessageText(parent)

	threadTitle := parentText
	if len(threadTitle) > 60 {
		threadTitle = threadTitle[:60]
	}

	thread := models.NewThread(
		fmt.Sprintf("slack_thread_%s_%s", channelID, parent.Ts),
		fmt.Sprintf("Thread in #%s: %s", channelName, threadTitle),
	)

	thread.SourceType = "slack"
	thread.ItemType = "slack_thread"
	thread.CreatedAt = tsToTime(parent.Ts)
	thread.UpdatedAt = tsToTime(parent.Ts)
	thread.Tags = []string{"slack", fmt.Sprintf("channel:%s", channelName)}
	thread.Metadata = map[string]any{
		"channel":      channelName,
		"channel_id":   channelID,
		"workspace":    workspaceURL,
		"ts":           parent.Ts,
		"author":       authorName,
		"reply_count":  parent.ReplyCount,
		"participants": collectParticipants(parent, replies),
	}
	thread.Links = []models.Link{
		{
			URL:   messageURL(workspaceURL, channelID, parent.Ts),
			Title: "Slack Thread",
			Type:  "source",
		},
	}

	// Add parent as first message
	thread.AddMessage(FromSlackMessage(parent, channelID, channelName, workspaceURL, authorName))

	// Add replies (skip parent message if included in replies list)
	for i := range replies {
		if replies[i].Ts == parent.Ts {
			continue
		}

		replyAuthor := replies[i].User

		if replyAuthor == "" {
			replyAuthor = replies[i].Username
		}

		if replyAuthor == "" {
			replyAuthor = replies[i].BotID
		}

		if replies[i].User != "" && client != nil && userCache != nil {
			replyAuthor = userCache.ResolveUser(replies[i].User, client)
		}

		thread.AddMessage(FromSlackMessage(&replies[i], channelID, channelName, workspaceURL, replyAuthor))
	}

	// Build consolidated content
	var sb strings.Builder

	for _, msg := range thread.Messages {
		author, _ := msg.GetMetadata()["author"].(string)
		sb.WriteString(fmt.Sprintf("**%s**: %s\n\n", author, msg.GetContent()))
	}

	thread.Content = sb.String()

	return thread
}

func collectParticipants(parent *RawMessage, replies []RawMessage) []string {
	seen := make(map[string]bool)
	participants := make([]string, 0)

	if parent.User != "" {
		seen[parent.User] = true
		participants = append(participants, parent.User)
	}

	for _, r := range replies {
		if r.User != "" && !seen[r.User] {
			seen[r.User] = true
			participants = append(participants, r.User)
		}
	}

	return participants
}

package gmail

import (
	"strings"
	"testing"
	"time"

	"pkm-sync/internal/transform"
	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
)

func TestFromGmailMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *gmail.Message
		config  models.GmailSourceConfig
		want    func(*models.Item) bool
		wantErr bool
	}{
		{
			name:    "nil message",
			message: nil,
			config:  models.GmailSourceConfig{},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "simple text message",
			message: createSimpleTextMessage(),
			config:  models.GmailSourceConfig{ExtractRecipients: true},
			want: func(item *models.Item) bool {
				return item.ID == "test-message-1" &&
					item.Title == "Test Subject" &&
					item.SourceType == "gmail" &&
					item.ItemType == "email" &&
					len(item.Tags) > 0
			},
			wantErr: false,
		},
		{
			name:    "HTML message with processing",
			message: createHTMLMessage(),
			config: models.GmailSourceConfig{
				ProcessHTMLContent: true,
				ExtractLinks:       true,
				ExtractRecipients:  true,
			},
			want: func(item *models.Item) bool {
				// Test basic converter functionality
				if !(item.ID == "test-message-html" &&
					item.Title == "HTML Email Test" &&
					item.Content != "" &&
					!strings.Contains(item.Content, "<html>")) {
					return false
				}

				// Test link extraction via transformer
				linkTransformer := transform.NewLinkExtractionTransformer()
				linkTransformer.Configure(map[string]interface{}{"enabled": true})

				transformedItems, err := linkTransformer.Transform([]models.FullItem{models.AsFullItem(item)})
				if err != nil || len(transformedItems) != 1 {
					return false
				}

				return len(transformedItems[0].GetLinks()) > 0
			},
			wantErr: false,
		},
		{
			name:    "message with attachments",
			message: createMessageWithAttachments(),
			config:  models.GmailSourceConfig{ExtractRecipients: true, DownloadAttachments: true},
			want: func(item *models.Item) bool {
				return item.ID == "test-message-attach" &&
					len(item.Attachments) == 2 &&
					item.Attachments[0].Name == "document.pdf" &&
					item.Attachments[1].Name == "image.jpg"
			},
			wantErr: false,
		},
		{
			name:    "message with custom tagging rules",
			message: createMessageFromCEO(),
			config: models.GmailSourceConfig{
				ExtractRecipients: true,
				TaggingRules: []models.TaggingRule{
					{
						Condition: "from:ceo@company.com",
						Tags:      []string{"urgent", "leadership"},
					},
					{
						Condition: "has:attachment",
						Tags:      []string{"has-attachment"},
					},
				},
			},
			want: func(item *models.Item) bool {
				return containsAll(item.Tags, []string{"urgent", "leadership"})
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromGmailMessage(tt.message, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromGmailMessage() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if tt.want != nil && !tt.want(got) {
				t.Errorf("FromGmailMessage() validation failed for %s", tt.name)
			}
		})
	}
}

func TestParseEmailAddress(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  EmailRecipient
	}{
		{
			name:  "simple email",
			input: "test@example.com",
			want: EmailRecipient{
				Name:  "",
				Email: "test@example.com",
			},
		},
		{
			name:  "email with name",
			input: "John Doe <john@example.com>",
			want: EmailRecipient{
				Name:  "John Doe",
				Email: "john@example.com",
			},
		},
		{
			name:  "email with quoted name",
			input: `"John, Jr. Doe" <john@example.com>`,
			want: EmailRecipient{
				Name:  "John, Jr. Doe",
				Email: "john@example.com",
			},
		},
		{
			name:  "empty input",
			input: "",
			want: EmailRecipient{
				Name:  "",
				Email: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEmailAddress(tt.input)
			if got.Name != tt.want.Name || got.Email != tt.want.Email {
				t.Errorf("parseEmailAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseEmailAddressList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []EmailRecipient
	}{
		{
			name:  "single email",
			input: "test@example.com",
			want: []EmailRecipient{
				{Name: "", Email: "test@example.com"},
			},
		},
		{
			name:  "multiple emails",
			input: "test1@example.com, test2@example.com",
			want: []EmailRecipient{
				{Name: "", Email: "test1@example.com"},
				{Name: "", Email: "test2@example.com"},
			},
		},
		{
			name:  "emails with names and quoted commas",
			input: `"Doe, John" <john@example.com>, Jane Smith <jane@example.com>`,
			want: []EmailRecipient{
				{Name: "Doe, John", Email: "john@example.com"},
				{Name: "Jane Smith", Email: "jane@example.com"},
			},
		},
		{
			name:  "empty input",
			input: "",
			want:  []EmailRecipient{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEmailAddressList(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseEmailAddressList() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i, recipient := range got {
				if recipient.Name != tt.want[i].Name || recipient.Email != tt.want[i].Email {
					t.Errorf("parseEmailAddressList()[%d] = %v, want %v", i, recipient, tt.want[i])
				}
			}
		})
	}
}

func TestBuildTags(t *testing.T) {
	msg := &gmail.Message{
		Id:       "test",
		LabelIds: []string{"IMPORTANT", "STARRED", "INBOX"},
	}

	config := models.GmailSourceConfig{
		Name: "Work Emails",
		TaggingRules: []models.TaggingRule{
			{
				Condition: "label:IMPORTANT",
				Tags:      []string{"high-priority"},
			},
		},
	}

	tags := buildTags(msg, config)

	expectedTags := []string{"gmail", "important", "starred", "inbox", "high-priority", "source:work-emails"}
	if !containsAll(tags, expectedTags) {
		t.Errorf("buildTags() = %v, want to contain all of %v", tags, expectedTags)
	}
}

func TestMatchesCondition(t *testing.T) {
	msg := createMessageFromCEO()

	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		{
			name:      "from condition match",
			condition: "from:ceo@company.com",
			want:      true,
		},
		{
			name:      "from condition no match",
			condition: "from:other@company.com",
			want:      false,
		},
		{
			name:      "subject condition match",
			condition: "subject:urgent",
			want:      true,
		},
		{
			name:      "subject condition no match",
			condition: "subject:casual",
			want:      false,
		},
		{
			name:      "has attachment condition",
			condition: "has:attachment",
			want:      false, // CEO message doesn't have attachments
		},
		{
			name:      "label condition match",
			condition: "label:important",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesCondition(msg, tt.condition)
			if got != tt.want {
				t.Errorf("matchesCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper functions for creating test data

func createSimpleTextMessage() *gmail.Message {
	return &gmail.Message{
		Id:           "test-message-1",
		ThreadId:     "thread-1",
		LabelIds:     []string{"INBOX", "UNREAD"},
		Snippet:      "This is a test message...",
		InternalDate: time.Now().Unix() * 1000,
		Payload: &gmail.MessagePart{
			MimeType: "text/plain",
			Headers: []*gmail.MessagePartHeader{
				{Name: "Subject", Value: "Test Subject"},
				{Name: "From", Value: "sender@example.com"},
				{Name: "To", Value: "recipient@example.com"},
				{Name: "Date", Value: time.Now().Format(time.RFC1123Z)},
				{Name: "Message-ID", Value: "<test1@example.com>"},
			},
			Body: &gmail.MessagePartBody{
				Data: "VGhpcyBpcyBhIHRlc3QgbWVzc2FnZSBjb250ZW50Lg==", // "This is a test message content." in base64
			},
		},
	}
}

func createHTMLMessage() *gmail.Message {
	return &gmail.Message{
		Id:           "test-message-html",
		ThreadId:     "thread-html",
		LabelIds:     []string{"INBOX"},
		Snippet:      "HTML email test...",
		InternalDate: time.Now().Unix() * 1000,
		Payload: &gmail.MessagePart{
			MimeType: "multipart/alternative",
			Headers: []*gmail.MessagePartHeader{
				{Name: "Subject", Value: "HTML Email Test"},
				{Name: "From", Value: "html@example.com"},
				{Name: "To", Value: "test@example.com"},
				{Name: "Date", Value: time.Now().Format(time.RFC1123Z)},
			},
			Parts: []*gmail.MessagePart{
				{
					MimeType: "text/plain",
					Body: &gmail.MessagePartBody{
						Data: "UGxhaW4gdGV4dCB2ZXJzaW9u", // "Plain text version" in base64
					},
				},
				{
					MimeType: "text/html",
					Body: &gmail.MessagePartBody{
						Data: "PHA+VGhpcyBpcyBhbiA8c3Ryb25nPkhUTUw8L3N0cm9uZz4gZW1haWwgd2l0aCBhIDxhIGhyZWY9Imh0dHBzOi8vZXhhbXBsZS5jb20iPmxpbms8L2E+LjwvcD4=", // HTML content in base64
					},
				},
			},
		},
	}
}

func createMessageWithAttachments() *gmail.Message {
	return &gmail.Message{
		Id:           "test-message-attach",
		ThreadId:     "thread-attach",
		LabelIds:     []string{"INBOX"},
		Snippet:      "Message with attachments...",
		InternalDate: time.Now().Unix() * 1000,
		Payload: &gmail.MessagePart{
			MimeType: "multipart/mixed",
			Headers: []*gmail.MessagePartHeader{
				{Name: "Subject", Value: "Message with Attachments"},
				{Name: "From", Value: "attach@example.com"},
				{Name: "To", Value: "test@example.com"},
				{Name: "Date", Value: time.Now().Format(time.RFC1123Z)},
			},
			Parts: []*gmail.MessagePart{
				{
					MimeType: "text/plain",
					Body: &gmail.MessagePartBody{
						Data: "TWVzc2FnZSB3aXRoIGF0dGFjaG1lbnRz", // "Message with attachments" in base64
					},
				},
				{
					MimeType: "application/pdf",
					Filename: "document.pdf",
					Body: &gmail.MessagePartBody{
						AttachmentId: "attachment-1",
						Size:         1024,
					},
				},
				{
					MimeType: "image/jpeg",
					Filename: "image.jpg",
					Body: &gmail.MessagePartBody{
						AttachmentId: "attachment-2",
						Size:         2048,
					},
				},
			},
		},
	}
}

func createMessageFromCEO() *gmail.Message {
	return &gmail.Message{
		Id:           "test-message-ceo",
		ThreadId:     "thread-ceo",
		LabelIds:     []string{"INBOX", "IMPORTANT"},
		Snippet:      "Urgent company update...",
		InternalDate: time.Now().Unix() * 1000,
		Payload: &gmail.MessagePart{
			MimeType: "text/plain",
			Headers: []*gmail.MessagePartHeader{
				{Name: "Subject", Value: "Urgent Company Update"},
				{Name: "From", Value: "CEO <ceo@company.com>"},
				{Name: "To", Value: "all@company.com"},
				{Name: "Date", Value: time.Now().Format(time.RFC1123Z)},
			},
			Body: &gmail.MessagePartBody{
				Data: "VXJnZW50IGNvbXBhbnkgdXBkYXRlIGZyb20gQ0VP", // "Urgent company update from CEO" in base64
			},
		},
	}
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}

	return false
}

func containsAll(slice []string, items []string) bool {
	for _, item := range items {
		if !contains(slice, item) {
			return false
		}
	}

	return true
}

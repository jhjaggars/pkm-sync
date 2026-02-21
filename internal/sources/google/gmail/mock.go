package gmail

import (
	"fmt"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
)

// MockService provides a mock implementation of the Gmail service for testing.
type MockService struct {
	messages []*gmail.Message
	labels   []*gmail.Label
	profile  *gmail.Profile
	config   models.GmailSourceConfig
	sourceID string
}

// NewMockService creates a new mock Gmail service with test data.
func NewMockService(config models.GmailSourceConfig, sourceID string) *MockService {
	return &MockService{
		config:   config,
		sourceID: sourceID,
		messages: createTestMessages(),
		labels:   createTestLabels(),
		profile:  createTestProfile(),
	}
}

// GetMessages returns mock messages filtered by the query.
func (m *MockService) GetMessages(since time.Time, limit int) ([]*gmail.Message, error) {
	if limit <= 0 {
		limit = 100
	}

	// Apply basic filtering based on configuration.
	var filtered []*gmail.Message

	for _, msg := range m.messages {
		if m.messageMatchesFilters(msg) {
			filtered = append(filtered, msg)
		}
	}

	// Apply limit.
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

// GetMessage returns a specific mock message.
func (m *MockService) GetMessage(messageID string) (*gmail.Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	for _, msg := range m.messages {
		if msg.Id == messageID {
			return msg, nil
		}
	}

	return nil, fmt.Errorf("message not found: %s", messageID)
}

// GetThreads returns mock threads built from the mock messages.
func (m *MockService) GetThreads(since time.Time, limit int) ([]*gmail.Thread, error) {
	if limit <= 0 {
		limit = 100
	}

	// Group messages by thread ID to build mock threads.
	threadMap := make(map[string][]*gmail.Message)

	for _, msg := range m.messages {
		if m.messageMatchesFilters(msg) {
			threadMap[msg.ThreadId] = append(threadMap[msg.ThreadId], msg)
		}
	}

	threads := make([]*gmail.Thread, 0, len(threadMap))

	for threadID, msgs := range threadMap {
		threads = append(threads, &gmail.Thread{
			Id:       threadID,
			Messages: msgs,
			Snippet:  msgs[0].Snippet,
		})

		if len(threads) >= limit {
			break
		}
	}

	return threads, nil
}

// GetThread returns a specific mock thread by ID.
func (m *MockService) GetThread(threadID string) (*gmail.Thread, error) {
	if threadID == "" {
		return nil, fmt.Errorf("thread ID is required")
	}

	var threadMessages []*gmail.Message

	for _, msg := range m.messages {
		if msg.ThreadId == threadID {
			threadMessages = append(threadMessages, msg)
		}
	}

	if len(threadMessages) == 0 {
		return nil, fmt.Errorf("thread not found: %s", threadID)
	}

	return &gmail.Thread{
		Id:       threadID,
		Messages: threadMessages,
		Snippet:  threadMessages[0].Snippet,
	}, nil
}

// GetMessagesInRange returns mock messages within a time range.
func (m *MockService) GetMessagesInRange(start, end time.Time, limit int) ([]*gmail.Message, error) {
	if end.Before(start) {
		return nil, fmt.Errorf("end time must be after start time")
	}

	return m.GetMessages(start, limit)
}

// GetLabels returns mock labels.
func (m *MockService) GetLabels() ([]*gmail.Label, error) {
	return m.labels, nil
}

// GetProfile returns mock profile.
func (m *MockService) GetProfile() (*gmail.Profile, error) {
	return m.profile, nil
}

// ValidateConfiguration validates the mock configuration.
func (m *MockService) ValidateConfiguration() error {
	// Validate that configured labels exist in mock labels.
	if len(m.config.Labels) > 0 {
		labelMap := make(map[string]bool)
		for _, label := range m.labels {
			labelMap[label.Name] = true
			labelMap[label.Id] = true
		}

		for _, configLabel := range m.config.Labels {
			if !labelMap[configLabel] {
				return fmt.Errorf("configured label '%s' not found in mock Gmail", configLabel)
			}
		}
	}

	return nil
}

// messageMatchesFilters checks if a message matches the configured filters.
func (m *MockService) messageMatchesFilters(msg *gmail.Message) bool {
	// Check labels.
	if len(m.config.Labels) > 0 {
		hasLabel := false

		for _, configLabel := range m.config.Labels {
			for _, msgLabel := range msg.LabelIds {
				if msgLabel == configLabel {
					hasLabel = true

					break
				}
			}

			if hasLabel {
				break
			}
		}

		if !hasLabel {
			return false
		}
	}

	// Check from domains.
	if len(m.config.FromDomains) > 0 {
		from := getHeaderValue(msg, "From")
		hasValidDomain := false

		for _, domain := range m.config.FromDomains {
			if strings.Contains(from, domain) {
				hasValidDomain = true

				break
			}
		}

		if !hasValidDomain {
			return false
		}
	}

	// Check excluded domains.
	if len(m.config.ExcludeFromDomains) > 0 {
		from := getHeaderValue(msg, "From")
		for _, domain := range m.config.ExcludeFromDomains {
			if strings.Contains(from, domain) {
				return false
			}
		}
	}

	return true
}

// Helper function to get header value from message.
func getHeaderValue(msg *gmail.Message, headerName string) string {
	if msg.Payload == nil || msg.Payload.Headers == nil {
		return ""
	}

	for _, header := range msg.Payload.Headers {
		if header.Name == headerName {
			return header.Value
		}
	}

	return ""
}

// createTestMessages creates sample test messages.
func createTestMessages() []*gmail.Message {
	return []*gmail.Message{
		{
			Id:           "msg1",
			ThreadId:     "thread1",
			LabelIds:     []string{"INBOX", "IMPORTANT"},
			Snippet:      "Important work email from company",
			SizeEstimate: 1024,
			Payload: &gmail.MessagePart{
				MimeType: "multipart/alternative",
				Headers: []*gmail.MessagePartHeader{
					{Name: "Subject", Value: "Important work update"},
					{Name: "From", Value: "boss@company.com"},
					{Name: "To", Value: "employee@company.com"},
					{Name: "Date", Value: time.Now().Add(-2 * time.Hour).Format(time.RFC1123)},
					{Name: "Message-ID", Value: "<msg1@company.com>"},
				},
				Body: &gmail.MessagePartBody{
					Data: "VGhpcyBpcyBhbiBpbXBvcnRhbnQgd29yayBlbWFpbA==", // Base64 encoded.
				},
			},
		},
		{
			Id:           "msg2",
			ThreadId:     "thread2",
			LabelIds:     []string{"INBOX", "STARRED"},
			Snippet:      "Personal starred email",
			SizeEstimate: 512,
			Payload: &gmail.MessagePart{
				MimeType: "text/plain",
				Headers: []*gmail.MessagePartHeader{
					{Name: "Subject", Value: "Personal important message"},
					{Name: "From", Value: "friend@personal.com"},
					{Name: "To", Value: "user@personal.com"},
					{Name: "Date", Value: time.Now().Add(-4 * time.Hour).Format(time.RFC1123)},
					{Name: "Message-ID", Value: "<msg2@personal.com>"},
				},
				Body: &gmail.MessagePartBody{
					Data: "VGhpcyBpcyBhIHBlcnNvbmFsIGVtYWls", // Base64 encoded.
				},
			},
		},
		{
			Id:           "msg3",
			ThreadId:     "thread3",
			LabelIds:     []string{"INBOX"},
			Snippet:      "Newsletter email",
			SizeEstimate: 2048,
			Payload: &gmail.MessagePart{
				MimeType: "text/html",
				Headers: []*gmail.MessagePartHeader{
					{Name: "Subject", Value: "Weekly Newsletter"},
					{Name: "From", Value: "newsletter@noreply.com"},
					{Name: "To", Value: "user@personal.com"},
					{Name: "Date", Value: time.Now().Add(-6 * time.Hour).Format(time.RFC1123)},
					{Name: "Message-ID", Value: "<msg3@noreply.com>"},
				},
				Body: &gmail.MessagePartBody{
					Data: "VGhpcyBpcyBhIG5ld3NsZXR0ZXIgZW1haWw=", // Base64 encoded.
				},
			},
		},
		{
			Id:           "msg4",
			ThreadId:     "thread4",
			LabelIds:     []string{"INBOX", "UNREAD"},
			Snippet:      "Unread work email",
			SizeEstimate: 768,
			Payload: &gmail.MessagePart{
				MimeType: "multipart/mixed",
				Headers: []*gmail.MessagePartHeader{
					{Name: "Subject", Value: "Project update with attachment"},
					{Name: "From", Value: "colleague@company.com"},
					{Name: "To", Value: "employee@company.com"},
					{Name: "Date", Value: time.Now().Add(-1 * time.Hour).Format(time.RFC1123)},
					{Name: "Message-ID", Value: "<msg4@company.com>"},
				},
				Body: &gmail.MessagePartBody{
					Data: "UHJvamVjdCB1cGRhdGUgd2l0aCBhdHRhY2htZW50", // Base64 encoded.
				},
				Parts: []*gmail.MessagePart{
					{
						Filename: "report.pdf",
						MimeType: "application/pdf",
						Body: &gmail.MessagePartBody{
							AttachmentId: "attachment1",
							Size:         1024,
						},
					},
				},
			},
		},
	}
}

// createTestLabels creates sample test labels.
func createTestLabels() []*gmail.Label {
	return []*gmail.Label{
		{Id: "INBOX", Name: "INBOX", Type: "system"},
		{Id: "IMPORTANT", Name: "IMPORTANT", Type: "system"},
		{Id: "STARRED", Name: "STARRED", Type: "system"},
		{Id: "UNREAD", Name: "UNREAD", Type: "system"},
		{Id: "SENT", Name: "SENT", Type: "system"},
		{Id: "DRAFT", Name: "DRAFT", Type: "system"},
		{Id: "Label_1", Name: "Work", Type: "user"},
		{Id: "Label_2", Name: "Personal", Type: "user"},
		{Id: "Label_3", Name: "Projects", Type: "user"},
	}
}

// createTestProfile creates a sample test profile.
func createTestProfile() *gmail.Profile {
	return &gmail.Profile{
		EmailAddress:  "testuser@example.com",
		MessagesTotal: 250,
		ThreadsTotal:  125,
		HistoryId:     12345,
	}
}

// AddTestMessage adds a custom test message to the mock service.
func (m *MockService) AddTestMessage(msg *gmail.Message) {
	m.messages = append(m.messages, msg)
}

// ClearMessages removes all test messages.
func (m *MockService) ClearMessages() {
	m.messages = []*gmail.Message{}
}

// SetConfig updates the mock service configuration.
func (m *MockService) SetConfig(config models.GmailSourceConfig) {
	m.config = config
}

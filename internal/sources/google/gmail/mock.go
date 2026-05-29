package gmail

import (
	"fmt"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
)

// Mock data constants used when constructing test messages and labels.
const (
	mockLabelTypeSystem = "system"
	mockLabelTypeUser   = "user"
	mimeTypePlain       = "text/plain"
	mimeTypeHTML        = "text/html"
	mimeTypeMultiAlt    = "multipart/alternative"
	mimeTypeMultiMixed  = "multipart/mixed"
	testMsgID1          = "msg1"
	testMsgID2          = "msg2"
	testMsgID3          = "msg3"
	testMsgID4          = "msg4"
	testThreadID1       = "thread1"
	testThreadID2       = "thread2"
	testThreadID3       = "thread3"
	testThreadID4       = "thread4"
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
		from := getHeaderValue(msg, headerFrom)
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
		from := getHeaderValue(msg, headerFrom)
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
			Id:           testMsgID1,
			ThreadId:     testThreadID1,
			LabelIds:     []string{gmailLabelInbox, gmailLabelImportant},
			Snippet:      "Important work email from company",
			SizeEstimate: 1024,
			Payload: &gmail.MessagePart{
				MimeType: mimeTypeMultiAlt,
				Headers: []*gmail.MessagePartHeader{
					{Name: headerSubject, Value: "Important work update"},
					{Name: headerFrom, Value: "boss@company.com"},
					{Name: headerTo, Value: "employee@company.com"},
					{Name: headerDate, Value: time.Now().Add(-2 * time.Hour).Format(time.RFC1123)},
					{Name: headerMessageID, Value: "<msg1@company.com>"},
				},
				Body: &gmail.MessagePartBody{
					Data: "VGhpcyBpcyBhbiBpbXBvcnRhbnQgd29yayBlbWFpbA==", // Base64 encoded.
				},
			},
		},
		{
			Id:           testMsgID2,
			ThreadId:     testThreadID2,
			LabelIds:     []string{gmailLabelInbox, gmailLabelStarred},
			Snippet:      "Personal starred email",
			SizeEstimate: 512,
			Payload: &gmail.MessagePart{
				MimeType: mimeTypePlain,
				Headers: []*gmail.MessagePartHeader{
					{Name: headerSubject, Value: "Personal important message"},
					{Name: headerFrom, Value: "friend@personal.com"},
					{Name: headerTo, Value: "user@personal.com"},
					{Name: headerDate, Value: time.Now().Add(-4 * time.Hour).Format(time.RFC1123)},
					{Name: headerMessageID, Value: "<msg2@personal.com>"},
				},
				Body: &gmail.MessagePartBody{
					Data: "VGhpcyBpcyBhIHBlcnNvbmFsIGVtYWls", // Base64 encoded.
				},
			},
		},
		{
			Id:           testMsgID3,
			ThreadId:     testThreadID3,
			LabelIds:     []string{gmailLabelInbox},
			Snippet:      "Newsletter email",
			SizeEstimate: 2048,
			Payload: &gmail.MessagePart{
				MimeType: mimeTypeHTML,
				Headers: []*gmail.MessagePartHeader{
					{Name: headerSubject, Value: "Weekly Newsletter"},
					{Name: headerFrom, Value: "newsletter@noreply.com"},
					{Name: headerTo, Value: "user@personal.com"},
					{Name: headerDate, Value: time.Now().Add(-6 * time.Hour).Format(time.RFC1123)},
					{Name: headerMessageID, Value: "<msg3@noreply.com>"},
				},
				Body: &gmail.MessagePartBody{
					Data: "VGhpcyBpcyBhIG5ld3NsZXR0ZXIgZW1haWw=", // Base64 encoded.
				},
			},
		},
		{
			Id:           testMsgID4,
			ThreadId:     testThreadID4,
			LabelIds:     []string{gmailLabelInbox, gmailLabelUnread},
			Snippet:      "Unread work email",
			SizeEstimate: 768,
			Payload: &gmail.MessagePart{
				MimeType: mimeTypeMultiMixed,
				Headers: []*gmail.MessagePartHeader{
					{Name: headerSubject, Value: "Project update with attachment"},
					{Name: headerFrom, Value: "colleague@company.com"},
					{Name: headerTo, Value: "employee@company.com"},
					{Name: headerDate, Value: time.Now().Add(-1 * time.Hour).Format(time.RFC1123)},
					{Name: headerMessageID, Value: "<msg4@company.com>"},
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
		{Id: gmailLabelInbox, Name: gmailLabelInbox, Type: mockLabelTypeSystem},
		{Id: gmailLabelImportant, Name: gmailLabelImportant, Type: mockLabelTypeSystem},
		{Id: gmailLabelStarred, Name: gmailLabelStarred, Type: mockLabelTypeSystem},
		{Id: gmailLabelUnread, Name: gmailLabelUnread, Type: mockLabelTypeSystem},
		{Id: gmailLabelSent, Name: gmailLabelSent, Type: mockLabelTypeSystem},
		{Id: gmailLabelDraft, Name: gmailLabelDraft, Type: mockLabelTypeSystem},
		{Id: "Label_1", Name: "Work", Type: mockLabelTypeUser},
		{Id: "Label_2", Name: "Personal", Type: mockLabelTypeUser},
		{Id: "Label_3", Name: "Projects", Type: mockLabelTypeUser},
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

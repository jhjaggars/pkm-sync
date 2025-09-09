// Gmail Test Helpers
// Provides utilities for testing Gmail API integration with thread processing

package helpers

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/gmail/v1"
)

// CreateTestThread creates a test Gmail thread for testing purposes.
func CreateTestThread(t *testing.T, messageCount int) *gmail.Thread {
	t.Helper()

	thread := &gmail.Thread{
		Id:       "test-thread-123",
		Snippet:  "Test thread snippet",
		Messages: make([]*gmail.Message, messageCount),
	}

	for i := 0; i < messageCount; i++ {
		message := &gmail.Message{
			Id:           fmt.Sprintf("test-message-%d", i+1),
			ThreadId:     thread.Id,
			InternalDate: time.Now().Add(time.Duration(i)*time.Hour).Unix() * 1000,
			Payload: &gmail.MessagePart{
				Headers: []*gmail.MessagePartHeader{
					{Name: "From", Value: fmt.Sprintf("sender%d@example.com", i+1)},
					{Name: "To", Value: "recipient@example.com"},
					{Name: "Subject", Value: "Test Subject"},
					{Name: "Date", Value: time.Now().Add(time.Duration(i) * time.Hour).Format(time.RFC1123)},
				},
				Body: &gmail.MessagePartBody{
					Data: base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("Message %d content", i+1))),
				},
			},
		}
		thread.Messages[i] = message
	}

	return thread
}

// CreateTestThreadWithDates creates a test thread with specific message timestamps.
func CreateTestThreadWithDates(t *testing.T, dates []time.Time) *gmail.Thread {
	t.Helper()

	thread := &gmail.Thread{
		Id:       "test-thread-dates",
		Snippet:  "Test thread with specific dates",
		Messages: make([]*gmail.Message, len(dates)),
	}

	for i, date := range dates {
		message := &gmail.Message{
			Id:           fmt.Sprintf("test-message-date-%d", i+1),
			ThreadId:     thread.Id,
			InternalDate: date.Unix() * 1000,
			Payload: &gmail.MessagePart{
				Headers: []*gmail.MessagePartHeader{
					{Name: "From", Value: fmt.Sprintf("sender%d@example.com", i+1)},
					{Name: "To", Value: "recipient@example.com"},
					{Name: "Subject", Value: "Test Subject"},
					{Name: "Date", Value: date.Format(time.RFC1123)},
				},
				Body: &gmail.MessagePartBody{
					Data: base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("Message %d content", i+1))),
				},
			},
		}
		thread.Messages[i] = message
	}

	return thread
}

// CreateTestThreadWithMetadata creates a test thread with rich metadata for testing.
func CreateTestThreadWithMetadata(t *testing.T) *gmail.Thread {
	t.Helper()

	thread := &gmail.Thread{
		Id:      "test-thread-metadata",
		Snippet: "Test thread with metadata",
		Messages: []*gmail.Message{
			{
				Id:           "test-message-meta-1",
				ThreadId:     "test-thread-metadata",
				InternalDate: time.Now().Add(-2*time.Hour).Unix() * 1000,
				LabelIds:     []string{"INBOX", "IMPORTANT"},
				Payload: &gmail.MessagePart{
					Headers: []*gmail.MessagePartHeader{
						{Name: "From", Value: "sender1@example.com"},
						{Name: "To", Value: "recipient@example.com"},
						{Name: "Subject", Value: "Test Subject"},
						{Name: "Date", Value: time.Now().Add(-2 * time.Hour).Format(time.RFC1123)},
					},
					Body: &gmail.MessagePartBody{
						Data: base64.URLEncoding.EncodeToString([]byte("First message content")),
					},
					Parts: []*gmail.MessagePart{
						{
							Filename: "attachment.pdf",
							Body: &gmail.MessagePartBody{
								AttachmentId: "test-attachment-1",
								Size:         1024,
							},
						},
					},
				},
			},
			{
				Id:           "test-message-meta-2",
				ThreadId:     "test-thread-metadata",
				InternalDate: time.Now().Add(-1*time.Hour).Unix() * 1000,
				LabelIds:     []string{"INBOX"},
				Payload: &gmail.MessagePart{
					Headers: []*gmail.MessagePartHeader{
						{Name: "From", Value: "sender2@example.com"},
						{Name: "To", Value: "recipient@example.com"},
						{Name: "Subject", Value: "Re: Test Subject"},
						{Name: "Date", Value: time.Now().Add(-1 * time.Hour).Format(time.RFC1123)},
					},
					Body: &gmail.MessagePartBody{
						Data: base64.URLEncoding.EncodeToString([]byte("Second message content")),
					},
				},
			},
		},
	}

	return thread
}

// MockGmailService provides a mock Gmail service for testing.
type MockGmailService struct {
	Threads map[string]*gmail.Thread
	Error   error
}

// NewMockGmailService creates a new mock Gmail service.
func NewMockGmailService() *MockGmailService {
	return &MockGmailService{
		Threads: make(map[string]*gmail.Thread),
	}
}

// AddTestThread adds a test thread to the mock service.
func (m *MockGmailService) AddTestThread(thread *gmail.Thread) {
	m.Threads[thread.Id] = thread
}

// SetError configures the mock to return an error.
func (m *MockGmailService) SetError(err error) {
	m.Error = err
}

// RequireTestDataSetup ensures test environment is properly configured.
func RequireTestDataSetup(t *testing.T) {
	t.Helper()

	// Check for test configuration or skip
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Add any other test environment requirements here
}

// AssertThreadConversion validates that a thread was converted correctly to an Item.
func AssertThreadConversion(t *testing.T, thread *gmail.Thread, item interface{}) {
	t.Helper()

	// This will be implemented once we have the Item interface defined
	// For now, just check that item is not nil
	require.NotNil(t, item, "Converted item should not be nil")
}

// AssertAPICallReduction validates that API calls were reduced vs baseline.
func AssertAPICallReduction(t *testing.T, threadCount, messageCount int, actualCalls int) {
	t.Helper()

	// Expected calls with Messages API: 1 list + messageCount gets = messageCount + 1
	expectedMessagesAPICalls := messageCount + 1

	// Expected calls with Threads API: 1 list + threadCount gets = threadCount + 1
	expectedThreadsAPICalls := threadCount + 1

	require.Equal(t, expectedThreadsAPICalls, actualCalls,
		"API calls should match Threads API pattern (1 list + %d gets)", threadCount)

	reductionPercentage := float64(expectedMessagesAPICalls-actualCalls) / float64(expectedMessagesAPICalls) * 100
	require.Greater(t, reductionPercentage, 60.0,
		"Should achieve at least 60%% API call reduction")
}

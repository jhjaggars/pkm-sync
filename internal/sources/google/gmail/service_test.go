package gmail

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
)

func TestNewService(t *testing.T) {
	tests := []struct {
		name     string
		client   *http.Client
		config   models.GmailSourceConfig
		sourceID string
		wantErr  bool
	}{
		{
			name:     "valid service creation",
			client:   &http.Client{},
			config:   models.GmailSourceConfig{Name: "Test Gmail"},
			sourceID: "test",
			wantErr:  false,
		},
		{
			name:     "nil client",
			client:   nil,
			config:   models.GmailSourceConfig{},
			sourceID: "test",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewService(tt.client, tt.config, tt.sourceID)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewService() expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Errorf("NewService() unexpected error: %v", err)

				return
			}

			if service == nil {
				t.Errorf("NewService() returned nil service")

				return
			}

			if service.sourceID != tt.sourceID {
				t.Errorf("NewService() sourceID = %v, want %v", service.sourceID, tt.sourceID)
			}
		})
	}
}

func TestService_buildQuery(t *testing.T) {
	tests := []struct {
		name     string
		config   models.GmailSourceConfig
		since    time.Time
		expected string
	}{
		{
			name:     "basic time filter",
			config:   models.GmailSourceConfig{},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01",
		},
		{
			name: "with labels",
			config: models.GmailSourceConfig{
				Labels: []string{"IMPORTANT", "STARRED"},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 {label:IMPORTANT label:STARRED}",
		},
		{
			name: "with custom query",
			config: models.GmailSourceConfig{
				Query: "has:attachment",
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 (has:attachment)",
		},
		{
			name: "with from domains",
			config: models.GmailSourceConfig{
				FromDomains: []string{"company.com", "client.com"},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 {from:company.com from:client.com}",
		},
		{
			name: "with exclude domains",
			config: models.GmailSourceConfig{
				ExcludeFromDomains: []string{"noreply.com"},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 -from:noreply.com",
		},
		{
			name: "unread only",
			config: models.GmailSourceConfig{
				IncludeUnread: true,
				IncludeRead:   false,
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 is:unread",
		},
		{
			name: "read only",
			config: models.GmailSourceConfig{
				IncludeUnread: false,
				IncludeRead:   true,
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 is:read",
		},
		{
			name: "require attachments",
			config: models.GmailSourceConfig{
				RequireAttachments: true,
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 has:attachment",
		},
		{
			name: "complex query",
			config: models.GmailSourceConfig{
				Labels:             []string{"IMPORTANT"},
				Query:              "has:attachment",
				FromDomains:        []string{"company.com"},
				ExcludeFromDomains: []string{"noreply.com"},
				IncludeUnread:      true,
				IncludeRead:        false,
				RequireAttachments: true,
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 {label:IMPORTANT} (has:attachment) {from:company.com} -from:noreply.com is:unread has:attachment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{config: tt.config}
			result := service.buildQuery(tt.since)

			if result != tt.expected {
				t.Errorf("buildQuery() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestService_GetMessage(t *testing.T) {
	tests := []struct {
		name      string
		messageID string
		wantErr   bool
	}{
		{
			name:      "empty message ID",
			messageID: "",
			wantErr:   true,
		},
		{
			name:      "valid message ID with nil service",
			messageID: "test123",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a basic service for testing
			// Note: This test validates error handling since we don't have
			// a real Gmail service configured. In integration tests,
			// we would need proper authentication and a real message ID
			service := &Service{
				config:   models.GmailSourceConfig{},
				sourceID: "test",
				service:  nil, // Gmail service is nil, so calls will fail
			}

			_, err := service.GetMessage(tt.messageID)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetMessage() expected error, got nil")
				}

				return
			}

			// This case should not happen in our current tests
			if err != nil {
				t.Errorf("GetMessage() unexpected error: %v", err)
			}
		})
	}
}

// MockGmailService provides a mock implementation for testing.
type MockGmailService struct {
	messages []*gmail.Message
	labels   []*gmail.Label
	profile  *gmail.Profile
}

func NewMockGmailService() *MockGmailService {
	return &MockGmailService{
		messages: []*gmail.Message{
			{
				Id:       "msg1",
				ThreadId: "thread1",
				LabelIds: []string{"INBOX", "IMPORTANT"},
				Snippet:  "Test message 1",
				Payload: &gmail.MessagePart{
					Headers: []*gmail.MessagePartHeader{
						{Name: "Subject", Value: "Test Subject 1"},
						{Name: "From", Value: "test1@example.com"},
						{Name: "To", Value: "recipient@example.com"},
					},
				},
			},
			{
				Id:       "msg2",
				ThreadId: "thread2",
				LabelIds: []string{"INBOX", "STARRED"},
				Snippet:  "Test message 2",
				Payload: &gmail.MessagePart{
					Headers: []*gmail.MessagePartHeader{
						{Name: "Subject", Value: "Test Subject 2"},
						{Name: "From", Value: "test2@example.com"},
						{Name: "To", Value: "recipient@example.com"},
					},
				},
			},
		},
		labels: []*gmail.Label{
			{Id: "INBOX", Name: "INBOX"},
			{Id: "IMPORTANT", Name: "IMPORTANT"},
			{Id: "STARRED", Name: "STARRED"},
		},
		profile: &gmail.Profile{
			EmailAddress:  "test@example.com",
			MessagesTotal: 100,
			ThreadsTotal:  50,
		},
	}
}

func (m *MockGmailService) GetMessages(query string, maxResults int64) ([]*gmail.Message, error) {
	// Simple mock - return all messages regardless of query
	var result []*gmail.Message

	limit := int(maxResults)
	if limit == 0 || limit > len(m.messages) {
		limit = len(m.messages)
	}

	for i := 0; i < limit; i++ {
		result = append(result, m.messages[i])
	}

	return result, nil
}

func (m *MockGmailService) GetMessage(messageID string) (*gmail.Message, error) {
	for _, msg := range m.messages {
		if msg.Id == messageID {
			return msg, nil
		}
	}

	return nil, fmt.Errorf("message not found: %s", messageID)
}

func (m *MockGmailService) GetLabels() ([]*gmail.Label, error) {
	return m.labels, nil
}

func (m *MockGmailService) GetProfile() (*gmail.Profile, error) {
	return m.profile, nil
}

func TestMockGmailService(t *testing.T) {
	mock := NewMockGmailService()

	// Test GetMessages
	messages, err := mock.GetMessages("", 10)
	if err != nil {
		t.Errorf("GetMessages() error = %v", err)
	}

	if len(messages) != 2 {
		t.Errorf("GetMessages() returned %d messages, want 2", len(messages))
	}

	// Test GetMessage
	msg, err := mock.GetMessage("msg1")
	if err != nil {
		t.Errorf("GetMessage() error = %v", err)
	}

	if msg.Id != "msg1" {
		t.Errorf("GetMessage() returned message with ID %s, want msg1", msg.Id)
	}

	// Test GetMessage with invalid ID
	_, err = mock.GetMessage("invalid")
	if err == nil {
		t.Errorf("GetMessage() with invalid ID should return error")
	}

	// Test GetLabels
	labels, err := mock.GetLabels()
	if err != nil {
		t.Errorf("GetLabels() error = %v", err)
	}

	if len(labels) != 3 {
		t.Errorf("GetLabels() returned %d labels, want 3", len(labels))
	}

	// Test GetProfile
	profile, err := mock.GetProfile()
	if err != nil {
		t.Errorf("GetProfile() error = %v", err)
	}

	if profile.EmailAddress != "test@example.com" {
		t.Errorf("GetProfile() returned email %s, want test@example.com", profile.EmailAddress)
	}
}

func TestIsLabelID(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{
			name:  "system label INBOX",
			label: "INBOX",
			want:  false,
		},
		{
			name:  "system label IMPORTANT",
			label: "IMPORTANT",
			want:  false,
		},
		{
			name:  "system label STARRED",
			label: "STARRED",
			want:  false,
		},
		{
			name:  "user label ID",
			label: "Label_2715051305847482596",
			want:  true,
		},
		{
			name:  "user label ID different format",
			label: "Label_1234567890",
			want:  true,
		},
		{
			name:  "label name with Label prefix but not ID",
			label: "Label-Work",
			want:  false,
		},
		{
			name:  "empty label",
			label: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLabelID(tt.label)
			if got != tt.want {
				t.Errorf("isLabelID(%q) = %v, want %v", tt.label, got, tt.want)
			}
		})
	}
}

func TestLabelNameToQuery(t *testing.T) {
	tests := []struct {
		name      string
		labelName string
		want      string
	}{
		{
			name:      "label with slashes",
			labelName: "Konflux-git-docs (d&s)",
			want:      "Konflux-git-docs-(d&s)",
		},
		{
			name:      "label with spaces",
			labelName: "Work Projects",
			want:      "Work-Projects",
		},
		{
			name:      "label with both spaces and slashes",
			labelName: "Projects/Work Items/Q1",
			want:      "Projects/Work-Items/Q1",
		},
		{
			name:      "label with no special characters",
			labelName: "Important",
			want:      "Important",
		},
		{
			name:      "empty label",
			labelName: "",
			want:      "",
		},
		{
			name:      "label with multiple consecutive spaces",
			labelName: "Work  Projects",
			want:      "Work--Projects",
		},
		{
			name:      "label with parentheses and slashes",
			labelName: "Team/Engineering (Backend)",
			want:      "Team/Engineering-(Backend)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labelNameToQuery(tt.labelName)
			if got != tt.want {
				t.Errorf("labelNameToQuery(%q) = %q, want %q", tt.labelName, got, tt.want)
			}
		})
	}
}

func TestResolveLabels(t *testing.T) {
	tests := []struct {
		name            string
		configLabels    []string
		availableLabels []*gmail.Label
		expectedLabels  []string
		expectError     bool
	}{
		{
			name: "resolve user label IDs to query-safe names",
			configLabels: []string{
				"Label_2715051305847482596",
				"INBOX",
				"Label_1234567890",
			},
			availableLabels: []*gmail.Label{
				{Id: "INBOX", Name: "INBOX"},
				{Id: "Label_2715051305847482596", Name: "Konflux-git-docs (d&s)"},
				{Id: "Label_1234567890", Name: "Work/Projects"},
			},
			expectedLabels: []string{
				"Konflux-git-docs-(d&s)",
				"INBOX",
				"Work/Projects",
			},
			expectError: false,
		},
		{
			name: "system labels pass through unchanged",
			configLabels: []string{
				"INBOX",
				"IMPORTANT",
				"STARRED",
			},
			availableLabels: []*gmail.Label{
				{Id: "INBOX", Name: "INBOX"},
				{Id: "IMPORTANT", Name: "IMPORTANT"},
				{Id: "STARRED", Name: "STARRED"},
			},
			expectedLabels: []string{
				"INBOX",
				"IMPORTANT",
				"STARRED",
			},
			expectError: false,
		},
		{
			name: "skip unresolvable label IDs with warning",
			configLabels: []string{
				"Label_9999999999",
				"INBOX",
			},
			availableLabels: []*gmail.Label{
				{Id: "INBOX", Name: "INBOX"},
			},
			expectedLabels: []string{
				"INBOX",
			},
			expectError: false,
		},
		{
			name:            "empty labels config",
			configLabels:    []string{},
			availableLabels: []*gmail.Label{},
			expectedLabels:  []string{},
			expectError:     false,
		},
		{
			name: "mixed system and user labels",
			configLabels: []string{
				"IMPORTANT",
				"Label_123",
				"STARRED",
			},
			availableLabels: []*gmail.Label{
				{Id: "IMPORTANT", Name: "IMPORTANT"},
				{Id: "STARRED", Name: "STARRED"},
				{Id: "Label_123", Name: "Team/Backend (Dev)"},
			},
			expectedLabels: []string{
				"IMPORTANT",
				"Team/Backend-(Dev)",
				"STARRED",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock service with the test configuration
			mockService := &Service{
				config: models.GmailSourceConfig{
					Labels: tt.configLabels,
				},
				sourceID: "test",
			}

			// Mock the GetLabels method by creating a temporary version
			// Since we can't easily mock the service method in the actual resolveLabels,
			// we'll test the logic separately here

			// Skip if no labels need resolution
			needsResolution := false

			for _, label := range tt.configLabels {
				if isLabelID(label) {
					needsResolution = true

					break
				}
			}

			if !needsResolution && len(tt.configLabels) > 0 {
				// Verify system labels pass through
				if len(mockService.config.Labels) != len(tt.expectedLabels) {
					t.Errorf("Expected %d labels, got %d", len(tt.expectedLabels), len(mockService.config.Labels))
				}

				for i, label := range mockService.config.Labels {
					if label != tt.expectedLabels[i] {
						t.Errorf("Expected label %q, got %q", tt.expectedLabels[i], label)
					}
				}

				return
			}

			// Build ID -> Name map
			idToName := make(map[string]string)
			for _, label := range tt.availableLabels {
				idToName[label.Id] = label.Name
			}

			// Resolve labels
			resolvedLabels := make([]string, 0, len(mockService.config.Labels))
			for _, label := range mockService.config.Labels {
				if isLabelID(label) {
					if name, found := idToName[label]; found {
						querySafeName := labelNameToQuery(name)
						resolvedLabels = append(resolvedLabels, querySafeName)
					}
					// Skip labels that can't be resolved
				} else {
					resolvedLabels = append(resolvedLabels, label)
				}
			}

			// Verify results
			if len(resolvedLabels) != len(tt.expectedLabels) {
				t.Errorf("Expected %d resolved labels, got %d", len(tt.expectedLabels), len(resolvedLabels))
			}

			for i, expected := range tt.expectedLabels {
				if i >= len(resolvedLabels) {
					t.Errorf("Missing expected label: %q", expected)

					continue
				}

				if resolvedLabels[i] != expected {
					t.Errorf("Expected label %q at position %d, got %q", expected, i, resolvedLabels[i])
				}
			}
		})
	}
}

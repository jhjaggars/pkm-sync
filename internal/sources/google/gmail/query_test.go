package gmail

import (
	"sort"
	"strings"
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

func TestBuildQuery(t *testing.T) {
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
			name: "with labels (OR logic)",
			config: models.GmailSourceConfig{
				Labels: []string{"IMPORTANT", "STARRED"},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 (label:IMPORTANT OR label:STARRED)",
		},
		{
			name: "with single label",
			config: models.GmailSourceConfig{
				Labels: []string{"IMPORTANT"},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 (label:IMPORTANT)",
		},
		{
			name: "with multiple labels (6 labels)",
			config: models.GmailSourceConfig{
				Labels: []string{"1-gtd", "0-leadership", "0-peers", "0-staff", "IMPORTANT", "STARRED"},
			},
			since:    time.Date(2024, 2, 17, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/02/17 (label:1-gtd OR label:0-leadership OR label:0-peers OR label:0-staff OR label:IMPORTANT OR label:STARRED)",
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
			expected: "after:2024/01/01 (from:company.com OR from:client.com)",
		},
		{
			name: "with to domains",
			config: models.GmailSourceConfig{
				ToDomains: []string{"work.com", "business.com"},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 (to:work.com OR to:business.com)",
		},
		{
			name: "with exclude domains",
			config: models.GmailSourceConfig{
				ExcludeFromDomains: []string{"noreply.com", "spam.com"},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 -from:noreply.com -from:spam.com",
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
			name: "both read and unread (no filter)",
			config: models.GmailSourceConfig{
				IncludeUnread: true,
				IncludeRead:   true,
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01",
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
			name: "complex query with all filters",
			config: models.GmailSourceConfig{
				Labels:             []string{"IMPORTANT"},
				Query:              "subject:meeting",
				FromDomains:        []string{"company.com"},
				ToDomains:          []string{"work.com"},
				ExcludeFromDomains: []string{"noreply.com"},
				IncludeUnread:      true,
				IncludeRead:        false,
				RequireAttachments: true,
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 (label:IMPORTANT) (subject:meeting) (from:company.com) (to:work.com) -from:noreply.com is:unread has:attachment",
		},
		{
			name: "with invalid max email age format",
			config: models.GmailSourceConfig{
				MaxEmailAge: "invalid",
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01", // Invalid duration should be ignored
		},
		{
			name: "empty labels and domains should be filtered",
			config: models.GmailSourceConfig{
				Labels:             []string{"", "IMPORTANT", ""},
				FromDomains:        []string{"", "example.com"},
				ExcludeFromDomains: []string{"", "spam.com", ""},
			},
			since:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 (label:IMPORTANT) (from:example.com) -from:spam.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildQuery(tt.config, tt.since)
			if result != tt.expected {
				t.Errorf("buildQuery() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildQueryWithRange(t *testing.T) {
	tests := []struct {
		name     string
		config   models.GmailSourceConfig
		start    time.Time
		end      time.Time
		expected string
	}{
		{
			name:     "basic time range",
			config:   models.GmailSourceConfig{},
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 before:2024/01/31",
		},
		{
			name: "range with labels",
			config: models.GmailSourceConfig{
				Labels: []string{"IMPORTANT"},
			},
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 before:2024/01/31 (label:IMPORTANT)",
		},
		{
			name: "range with multiple labels",
			config: models.GmailSourceConfig{
				Labels: []string{"IMPORTANT", "STARRED", "INBOX"},
			},
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			expected: "after:2024/01/01 before:2024/01/31 (label:IMPORTANT OR label:STARRED OR label:INBOX)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildQueryWithRange(tt.config, tt.start, tt.end)
			if result != tt.expected {
				t.Errorf("buildQueryWithRange() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "minutes",
			input:    "30m",
			expected: 30 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "hours",
			input:    "2h",
			expected: 2 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "days",
			input:    "7d",
			expected: 7 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "weeks",
			input:    "2w",
			expected: 2 * 7 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "months",
			input:    "1mo",
			expected: 30 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "years",
			input:    "1y",
			expected: 365 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "long form minutes",
			input:    "15minutes",
			expected: 15 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "long form hours",
			input:    "3hours",
			expected: 3 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "long form days",
			input:    "5days",
			expected: 5 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid format",
			input:    "abc",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid number",
			input:    "xyd",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid unit",
			input:    "5z",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "zero value",
			input:    "0d",
			expected: 0,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDuration(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDuration() expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Errorf("parseDuration() unexpected error: %v", err)

				return
			}

			if result != tt.expected {
				t.Errorf("parseDuration() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValidateQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "empty query",
			query:   "",
			wantErr: false,
		},
		{
			name:    "simple query",
			query:   "from:example.com",
			wantErr: false,
		},
		{
			name:    "query with balanced parentheses",
			query:   "(from:example.com OR to:example.com)",
			wantErr: false,
		},
		{
			name:    "complex balanced query",
			query:   "(from:example.com AND (subject:urgent OR subject:important))",
			wantErr: false,
		},
		{
			name:    "unmatched opening parenthesis",
			query:   "(from:example.com",
			wantErr: true,
		},
		{
			name:    "unmatched closing parenthesis",
			query:   "from:example.com)",
			wantErr: true,
		},
		{
			name:    "multiple unmatched parentheses",
			query:   "((from:example.com)",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuery(tt.query)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateQuery() expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Errorf("ValidateQuery() unexpected error: %v", err)
			}
		})
	}
}

func TestBuildComplexQuery(t *testing.T) {
	tests := []struct {
		name     string
		config   models.GmailSourceConfig
		criteria map[string]interface{}
		expected string
	}{
		{
			name:   "basic criteria",
			config: models.GmailSourceConfig{},
			criteria: map[string]interface{}{
				"from": "example.com",
			},
			expected: "from:example.com",
		},
		{
			name: "with base query",
			config: models.GmailSourceConfig{
				Query: "has:attachment",
			},
			criteria: map[string]interface{}{
				"subject": "urgent",
			},
			expected: "(has:attachment) subject:urgent",
		},
		{
			name:   "boolean criteria",
			config: models.GmailSourceConfig{},
			criteria: map[string]interface{}{
				"has_attachment": true,
			},
			expected: "has:attachment",
		},
		{
			name:   "time criteria",
			config: models.GmailSourceConfig{},
			criteria: map[string]interface{}{
				"newer_than": "1d",
				"older_than": "7d",
			},
			expected: "newer_than:1d older_than:7d",
		},
		{
			name:     "empty criteria",
			config:   models.GmailSourceConfig{},
			criteria: map[string]interface{}{},
			expected: "",
		},
		{
			name:   "mixed valid and invalid criteria",
			config: models.GmailSourceConfig{},
			criteria: map[string]interface{}{
				"has_attachment": true,
			},
			expected: "has:attachment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildComplexQuery(tt.config, tt.criteria)
			resultParts := strings.Fields(result)
			expectedParts := strings.Fields(tt.expected)

			sort.Strings(resultParts)
			sort.Strings(expectedParts)

			if strings.Join(resultParts, " ") != strings.Join(expectedParts, " ") {
				t.Errorf("BuildComplexQuery() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestQueryEdgeCases tests edge cases and error conditions.
func TestQueryEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		config   models.GmailSourceConfig
		since    time.Time
		validate func(string) bool
	}{
		{
			name: "empty labels should be ignored",
			config: models.GmailSourceConfig{
				Labels: []string{"", "IMPORTANT", ""},
			},
			since: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			validate: func(result string) bool {
				// Should contain only one label filter for "IMPORTANT".
				return strings.Count(result, "label:") == 1 && strings.Contains(result, "label:IMPORTANT")
			},
		},
		{
			name: "empty domains should be ignored",
			config: models.GmailSourceConfig{
				FromDomains: []string{"", "example.com", ""},
			},
			since: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			validate: func(result string) bool {
				// Should contain only one from domain.
				return strings.Contains(result, "(from:example.com)") && !strings.Contains(result, "from: ")
			},
		},
		{
			name: "whitespace in query should be preserved",
			config: models.GmailSourceConfig{
				Query: "  subject:test   ",
			},
			since: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			validate: func(result string) bool {
				return strings.Contains(result, "(  subject:test   )")
			},
		},
		{
			name: "all empty domains should result in no domain filter",
			config: models.GmailSourceConfig{
				FromDomains:        []string{"", "", ""},
				ToDomains:          []string{"", ""},
				ExcludeFromDomains: []string{""},
			},
			since: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			validate: func(result string) bool {
				// Should only contain the since filter.
				return result == "after:2024/01/01"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildQuery(tt.config, tt.since)
			if !tt.validate(result) {
				t.Errorf("buildQuery() = %v, validation failed", result)
			}
		})
	}
}

// TestQueryValidationEnhanced tests more sophisticated query validation.
func TestQueryValidationEnhanced(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nested parentheses",
			query:   "((from:example.com AND to:test.com) OR subject:urgent)",
			wantErr: false,
		},
		{
			name:    "multiple unmatched opening parentheses",
			query:   "((from:example.com",
			wantErr: true,
			errMsg:  "unmatched opening parenthesis",
		},
		{
			name:    "multiple unmatched closing parentheses",
			query:   "from:example.com))",
			wantErr: true,
			errMsg:  "unmatched closing parenthesis",
		},
		{
			name:    "mixed unmatched parentheses",
			query:   ")from:example.com(",
			wantErr: true,
			errMsg:  "unmatched closing parenthesis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuery(tt.query)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateQuery() expected error, got nil")

					return
				}

				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateQuery() error = %v, want error containing %v", err.Error(), tt.errMsg)
				}

				return
			}

			if err != nil {
				t.Errorf("ValidateQuery() unexpected error: %v", err)
			}
		})
	}
}

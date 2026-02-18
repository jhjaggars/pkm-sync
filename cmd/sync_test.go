package main

import (
	"net/http"
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

func TestGetEnabledSources_ExplicitList(t *testing.T) {
	config := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"google_calendar", "slack"},
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
			"slack": {
				Enabled: true,
				Type:    "slack",
			},
			"gmail": {
				Enabled: false, // Disabled, should not be included
				Type:    "gmail",
			},
		},
	}

	enabledSources := getEnabledSources(config)

	if len(enabledSources) != 2 {
		t.Errorf("Expected 2 enabled sources, got %d", len(enabledSources))
	}

	expectedSources := map[string]bool{
		"google_calendar": false,
		"slack":           false,
	}

	for _, source := range enabledSources {
		if _, exists := expectedSources[source]; !exists {
			t.Errorf("Unexpected source in enabled list: %s", source)
		}

		expectedSources[source] = true
	}

	for source, found := range expectedSources {
		if !found {
			t.Errorf("Expected source %s to be enabled", source)
		}
	}
}

func TestGetEnabledSources_ExplicitListButSourceDisabled(t *testing.T) {
	config := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"google_calendar", "slack"},
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
			"slack": {
				Enabled: false, // Listed in enabled_sources but disabled
				Type:    "slack",
			},
		},
	}

	enabledSources := getEnabledSources(config)

	// Should only include google_calendar since slack is disabled
	if len(enabledSources) != 1 {
		t.Errorf("Expected 1 enabled source, got %d", len(enabledSources))
	}

	if enabledSources[0] != "google_calendar" {
		t.Errorf("Expected google_calendar to be the only enabled source, got %s", enabledSources[0])
	}
}

func TestGetEnabledSources_FallbackToEnabled(t *testing.T) {
	config := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{}, // Empty list, should fallback
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
			"slack": {
				Enabled: false,
				Type:    "slack",
			},
			"gmail": {
				Enabled: true,
				Type:    "gmail",
			},
		},
	}

	enabledSources := getEnabledSources(config)

	// Should include google_calendar and gmail based on enabled flags
	if len(enabledSources) != 2 {
		t.Errorf("Expected 2 enabled sources, got %d", len(enabledSources))
	}

	enabledMap := make(map[string]bool)
	for _, source := range enabledSources {
		enabledMap[source] = true
	}

	if !enabledMap["google_calendar"] {
		t.Error("Expected google_calendar to be enabled")
	}

	if !enabledMap["gmail"] {
		t.Error("Expected gmail to be enabled")
	}

	if enabledMap["slack"] {
		t.Error("Expected slack to be disabled")
	}
}

func TestGetEnabledSources_EmptyConfig(t *testing.T) {
	config := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{},
		},
		Sources: map[string]models.SourceConfig{},
	}

	enabledSources := getEnabledSources(config)

	if len(enabledSources) != 0 {
		t.Errorf("Expected 0 enabled sources for empty config, got %d", len(enabledSources))
	}
}

func TestGetEnabledSources_SourceNotInConfig(t *testing.T) {
	config := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"google_calendar", "nonexistent"},
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
			// "nonexistent" source is not defined
		},
	}

	enabledSources := getEnabledSources(config)

	// Should only include google_calendar, skip nonexistent source
	if len(enabledSources) != 1 {
		t.Errorf("Expected 1 enabled source, got %d", len(enabledSources))
	}

	if enabledSources[0] != "google_calendar" {
		t.Errorf("Expected google_calendar to be the only enabled source, got %s", enabledSources[0])
	}
}

func TestParseSinceTime_RelativeDays(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool // whether it should succeed
	}{
		{"7d", true},
		{"1d", true},
		{"30d", true},
		{"0d", true},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseSinceTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully, got error: %v", tc.input, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time, got zero time", tc.input)
			}

			if !tc.expected && err == nil {
				t.Errorf("Expected %s to fail parsing, but it succeeded", tc.input)
			}
		})
	}
}

func TestParseSinceTime_RelativeHours(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"24h", true},
		{"1h", true},
		{"168h", true}, // 7 days in hours
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseSinceTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully, got error: %v", tc.input, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time, got zero time", tc.input)
			}
		})
	}
}

func TestParseSinceTime_SpecialValues(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"today", true},
		{"yesterday", true},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseSinceTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully, got error: %v", tc.input, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time, got zero time", tc.input)
			}
		})
	}
}

func TestParseSinceTime_TodayReturnsCorrectDate(t *testing.T) {
	now := time.Now()
	result, err := parseSinceTime("today")
	if err != nil {
		t.Fatalf("parseSinceTime(\"today\") failed: %v", err)
	}

	// Verify it returns today's date at midnight
	expected := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if !result.Equal(expected) {
		t.Errorf("parseSinceTime(\"today\") = %v, want %v", result, expected)
	}

	// Verify the date is actually today, not yesterday
	if result.Year() != now.Year() || result.Month() != now.Month() || result.Day() != now.Day() {
		t.Errorf("parseSinceTime(\"today\") returned wrong date: got %v, want today's date %v", result.Format("2006-01-02"), now.Format("2006-01-02"))
	}
}

func TestParseSinceTime_YesterdayReturnsCorrectDate(t *testing.T) {
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)

	result, err := parseSinceTime("yesterday")
	if err != nil {
		t.Fatalf("parseSinceTime(\"yesterday\") failed: %v", err)
	}

	// Verify it returns yesterday's date at midnight
	expected := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, yesterday.Location())
	if !result.Equal(expected) {
		t.Errorf("parseSinceTime(\"yesterday\") = %v, want %v", result, expected)
	}

	// Verify the date is actually yesterday
	if result.Year() != yesterday.Year() || result.Month() != yesterday.Month() || result.Day() != yesterday.Day() {
		t.Errorf("parseSinceTime(\"yesterday\") returned wrong date: got %v, want yesterday's date %v", result.Format("2006-01-02"), yesterday.Format("2006-01-02"))
	}
}

func TestParseSinceTime_AbsoluteDates(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"2025-01-01", true},
		{"2024-12-31", true},
		{"2025-02-29", false}, // Invalid date (2025 is not a leap year)
		{"invalid-date", false},
		{"2025/01/01", false}, // Wrong format
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseSinceTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully, got error: %v", tc.input, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time, got zero time", tc.input)
			}

			if !tc.expected && err == nil {
				t.Errorf("Expected %s to fail parsing, but it succeeded", tc.input)
			}
		})
	}
}

func TestParseSinceTime_InvalidInputs(t *testing.T) {
	testCases := []string{
		"",
		"invalid",
		"abc",
		"-1d",  // Negative days should be invalid
		"-5d",  // Negative days should be invalid
		"d",    // Missing number
		"3.5d", // Float days should be invalid
		// Note: "7days" and "1week" are now valid through natural language parsing
	}

	for _, input := range testCases {
		t.Run(input, func(t *testing.T) {
			_, err := parseSinceTime(input)
			if err == nil {
				t.Errorf("Expected %s to fail parsing, but it succeeded", input)
			}
		})
	}
}

func TestParseSinceTime_EdgeCases(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
		desc     string
	}{
		{"0d", true, "zero days should be valid"},
		{"365d", true, "large number of days should be valid"},
		{"1000d", true, "very large number of days should be valid"},
		{"24h", true, "24 hours should equal 1 day"},
		{"168h", true, "168 hours should equal 7 days"},
	}

	for _, tc := range testCases {
		t.Run(tc.input+"_"+tc.desc, func(t *testing.T) {
			result, err := parseSinceTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully (%s), got error: %v", tc.input, tc.desc, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time (%s), got zero time", tc.input, tc.desc)
			}

			if !tc.expected && err == nil {
				t.Errorf("Expected %s to fail parsing (%s), but it succeeded", tc.input, tc.desc)
			}
		})
	}
}

func TestParseSinceTime_NaturalLanguage(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
		desc     string
	}{
		{"last week", true, "natural language - last week"},
		{"3 days ago", true, "natural language - 3 days ago"},
		{"2 weeks ago", true, "natural language - 2 weeks ago"},
		{"last month", true, "natural language - last month"},
		{"1 hour ago", true, "natural language - 1 hour ago"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseSinceTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully (%s), got error: %v", tc.input, tc.desc, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time (%s), got zero time", tc.input, tc.desc)
			}

			if !tc.expected && err == nil {
				t.Errorf("Expected %s to fail parsing (%s), but it succeeded", tc.input, tc.desc)
			}
		})
	}
}

func TestCreateSource_Google(t *testing.T) {
	source, err := createSource("google_calendar", &http.Client{})
	if err != nil {
		t.Fatalf("Failed to create google_calendar source: %v", err)
	}

	if source == nil {
		t.Error("Expected non-nil source")
	}
}

func TestCreateSource_Unknown(t *testing.T) {
	_, err := createSource("unknown", &http.Client{})
	if err == nil {
		t.Error("Expected error for unknown source")
	}

	expectedError := "unknown source 'unknown': supported sources are 'google_calendar' (others like slack, gmail, jira are planned for future releases)"
	if err.Error() != expectedError {
		t.Errorf("Expected error message %q, got %q", expectedError, err.Error())
	}
}

func TestCreateSource_FutureSources(t *testing.T) {
	futureSources := []string{"slack", "gmail", "jira"}

	for _, sourceName := range futureSources {
		t.Run(sourceName, func(t *testing.T) {
			_, err := createSource(sourceName, &http.Client{})
			if err == nil {
				t.Errorf("Expected error for unimplemented source %s", sourceName)
			}
		})
	}
}

func TestCreateTarget_Obsidian(t *testing.T) {
	target, err := createTarget("obsidian")
	if err != nil {
		t.Fatalf("Failed to create obsidian target: %v", err)
	}

	if target == nil {
		t.Error("Expected non-nil target")
	}
}

func TestCreateTarget_Logseq(t *testing.T) {
	target, err := createTarget("logseq")
	if err != nil {
		t.Fatalf("Failed to create logseq target: %v", err)
	}

	if target == nil {
		t.Error("Expected non-nil target")
	}
}

func TestCreateTarget_Unknown(t *testing.T) {
	_, err := createTarget("unknown")
	if err == nil {
		t.Error("Expected error for unknown target")
	}

	expectedError := "unknown target 'unknown': supported targets are 'obsidian' and 'logseq'"
	if err.Error() != expectedError {
		t.Errorf("Expected error message %q, got %q", expectedError, err.Error())
	}
}

func TestCreateSourceWithConfig_GoogleAttendeeAllowListValidation(t *testing.T) {
	tests := []struct {
		name              string
		attendeeAllowList []string
		expectedEmails    []string
		shouldPassConfig  bool
		description       string
	}{
		{
			name:              "valid emails",
			attendeeAllowList: []string{"user1@example.com", "user2@company.org"},
			expectedEmails:    []string{"user1@example.com", "user2@company.org"},
			shouldPassConfig:  true,
			description:       "Valid emails should be passed to source configuration",
		},
		{
			name:              "emails with whitespace",
			attendeeAllowList: []string{" user1@example.com ", "user2@company.org"},
			expectedEmails:    []string{"user1@example.com", "user2@company.org"},
			shouldPassConfig:  true,
			description:       "Emails with whitespace should be trimmed and passed",
		},
		{
			name:              "invalid emails filtered out",
			attendeeAllowList: []string{"user1@example.com", "invalid-email", "user2@company.org"},
			expectedEmails:    []string{"user1@example.com", "user2@company.org"},
			shouldPassConfig:  true,
			description:       "Invalid emails should be filtered out but valid ones passed",
		},
		{
			name:              "empty strings filtered out",
			attendeeAllowList: []string{"user1@example.com", "", "user2@company.org", "   "},
			expectedEmails:    []string{"user1@example.com", "user2@company.org"},
			shouldPassConfig:  true,
			description:       "Empty strings should be filtered out",
		},
		{
			name:              "all invalid emails",
			attendeeAllowList: []string{"invalid1", "invalid2", ""},
			expectedEmails:    []string{},
			shouldPassConfig:  false,
			description:       "When all emails are invalid, no config should be passed",
		},
		{
			name:              "nil allow list",
			attendeeAllowList: nil,
			expectedEmails:    []string{},
			shouldPassConfig:  false,
			description:       "Nil allow list should not pass any config",
		},
		{
			name:              "empty allow list",
			attendeeAllowList: []string{},
			expectedEmails:    []string{},
			shouldPassConfig:  false,
			description:       "Empty allow list should not pass any config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &models.Config{
				Sources: map[string]models.SourceConfig{
					"google_calendar": {
						Enabled: true,
						Type:    "google_calendar",
						Google: models.GoogleSourceConfig{
							AttendeeAllowList:        tt.attendeeAllowList,
							RequireMultipleAttendees: true,
							IncludeSelfOnlyEvents:    false,
						},
					},
				},
			}

			// Note: This test can't fully verify the actual configuration being passed
			// since createSourceWithConfig calls source.Configure() internally.
			// But we can verify that it doesn't error and creates a valid source.
			source, err := createSourceWithConfig("google_calendar", config.Sources["google_calendar"], &http.Client{})
			if err != nil {
				t.Errorf("createSourceWithConfig failed: %v", err)
			}

			if source == nil {
				t.Error("Expected non-nil source")
			}
		})
	}
}

func TestCreateSourceWithConfig_GoogleMaxResultsValidation(t *testing.T) {
	tests := []struct {
		name        string
		maxResults  int
		description string
	}{
		{
			name:        "valid max results",
			maxResults:  500,
			description: "Valid max results should be accepted",
		},
		{
			name:        "zero max results should use default",
			maxResults:  0,
			description: "Zero max results should use default",
		},
		{
			name:        "negative max results should use default",
			maxResults:  -1,
			description: "Negative max results should use default",
		},
		{
			name:        "max results at limit",
			maxResults:  2500,
			description: "Max results at limit should be accepted",
		},
		{
			name:        "max results over limit",
			maxResults:  5000,
			description: "Max results over limit should be capped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &models.Config{
				Sources: map[string]models.SourceConfig{
					"google_calendar": {
						Enabled: true,
						Type:    "google_calendar",
						Google: models.GoogleSourceConfig{
							MaxResults:               tt.maxResults,
							RequireMultipleAttendees: true,
							IncludeSelfOnlyEvents:    false,
						},
					},
				},
			}

			source, err := createSourceWithConfig("google_calendar", config.Sources["google_calendar"], &http.Client{})
			if err != nil {
				t.Errorf("createSourceWithConfig failed: %v", err)
			}

			if source == nil {
				t.Error("Expected non-nil source")
			}
		})
	}
}

func TestCreateSourceWithConfig_GoogleBooleanOptions(t *testing.T) {
	tests := []struct {
		name                     string
		requireMultipleAttendees bool
		includeSelfOnlyEvents    bool
		description              string
	}{
		{
			name:                     "require multiple attendees true, include self only false",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			description:              "Default configuration",
		},
		{
			name:                     "require multiple attendees false, include self only false",
			requireMultipleAttendees: false,
			includeSelfOnlyEvents:    false,
			description:              "Allow single attendee events",
		},
		{
			name:                     "require multiple attendees true, include self only true",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    true,
			description:              "Include self-only events",
		},
		{
			name:                     "require multiple attendees false, include self only true",
			requireMultipleAttendees: false,
			includeSelfOnlyEvents:    true,
			description:              "All events allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &models.Config{
				Sources: map[string]models.SourceConfig{
					"google_calendar": {
						Enabled: true,
						Type:    "google_calendar",
						Google: models.GoogleSourceConfig{
							RequireMultipleAttendees: tt.requireMultipleAttendees,
							IncludeSelfOnlyEvents:    tt.includeSelfOnlyEvents,
						},
					},
				},
			}

			source, err := createSourceWithConfig("google_calendar", config.Sources["google_calendar"], &http.Client{})
			if err != nil {
				t.Errorf("createSourceWithConfig failed: %v", err)
			}

			if source == nil {
				t.Error("Expected non-nil source")
			}
		})
	}
}

func TestCreateSourceWithConfig_MissingGoogleConfig(t *testing.T) {
	config := &models.Config{
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
				// Google config is zero value (default)
			},
		},
	}

	source, err := createSourceWithConfig("google_calendar", config.Sources["google_calendar"], &http.Client{})
	if err != nil {
		t.Errorf("createSourceWithConfig failed with missing google_calendar config: %v", err)
	}

	if source == nil {
		t.Error("Expected non-nil source even with missing google_calendar config")
	}
}

func TestCreateSourceWithConfig_SourceNotInConfig(t *testing.T) {
	// Test with empty source config - should create default google_calendar source
	emptySourceConfig := models.SourceConfig{
		Type:    "google_calendar", // Need to specify type
		Enabled: true,
	}

	source, err := createSourceWithConfig("google_calendar", emptySourceConfig, &http.Client{})
	if err != nil {
		t.Errorf("createSourceWithConfig failed with source not in config: %v", err)
	}

	if source == nil {
		t.Error("Expected non-nil source even when not in config")
	}
}

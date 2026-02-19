package main

import (
	"testing"
	"time"
)

func TestParseDateTime_EmptyString(t *testing.T) {
	_, err := parseDateTime("")
	if err == nil {
		t.Error("Expected error for empty string")
	}
}

func TestParseDateTime_NamedDates(t *testing.T) {
	testCases := []struct {
		input string
		desc  string
	}{
		{"today", "today should return midnight of current day"},
		{"tomorrow", "tomorrow should return midnight of next day"},
		{"yesterday", "yesterday should return midnight of previous day"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseDateTime(tc.input)
			if err != nil {
				t.Errorf("Expected %s to parse successfully (%s), got error: %v", tc.input, tc.desc, err)
			}

			if result.IsZero() {
				t.Errorf("Expected %s to return valid time (%s), got zero time", tc.input, tc.desc)
			}

			// Verify result is at midnight (hour, minute, second all zero)
			if result.Hour() != 0 || result.Minute() != 0 || result.Second() != 0 {
				t.Errorf("Expected %s to return midnight (%s), got %02d:%02d:%02d",
					tc.input, tc.desc, result.Hour(), result.Minute(), result.Second())
			}
		})
	}
}

func TestParseDateTime_ISO8601Formats(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
		desc     string
	}{
		{"2025-01-01", true, "ISO 8601 date only"},
		{"2025-01-01T15:04:05", true, "ISO 8601 datetime without timezone"},
		{"2025-01-01T15:04:05Z", true, "ISO 8601 datetime with Z suffix"},
		{"2025-01-01T15:04:05-07:00", true, "ISO 8601 datetime with timezone offset"},
		{"2025-01-01T15:04:05+05:30", true, "ISO 8601 datetime with positive timezone offset"},
		{"2025-02-29", false, "Invalid date - 2025 is not a leap year"},
		{"2024-02-29", true, "Valid date - 2024 is a leap year"},
		{"2025/01/01", false, "Wrong format - slashes instead of dashes"},
		{"01-01-2025", false, "Wrong format - wrong order"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseDateTime(tc.input)
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

func TestParseDateTime_RelativeDayDurations(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
		desc     string
	}{
		{"7d", true, "7 days ago"},
		{"1d", true, "1 day ago"},
		{"30d", true, "30 days ago"},
		{"365d", true, "365 days ago (1 year)"},
		{"-1d", false, "negative days should be invalid"},
		{"-7d", false, "negative days should be invalid"},
		{"3.5d", false, "fractional days should be invalid"},
		{"d", false, "missing number should be invalid"},
		{"7days", true, "natural language will parse this"},
	}

	now := time.Now()

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseDateTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully (%s), got error: %v", tc.input, tc.desc, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time (%s), got zero time", tc.input, tc.desc)
			}

			if !tc.expected && err == nil {
				t.Errorf("Expected %s to fail parsing (%s), but it succeeded", tc.input, tc.desc)
			}

			// For valid cases, verify the time is in the past (allow small tolerance for 0d case)
			// Use 1 second tolerance to account for test execution time
			if tc.expected && err == nil && result.After(now.Add(time.Second)) {
				t.Errorf("Expected %s to return a time in the past or now (%s), got future time", tc.input, tc.desc)
			}
		})
	}
}

func TestParseDateTime_GoDurations(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
		desc     string
	}{
		{"24h", true, "24 hours ago"},
		{"1h", true, "1 hour ago"},
		{"2h30m", true, "2.5 hours ago"},
		{"168h", true, "168 hours (7 days) ago"},
		{"1m", true, "1 minute ago"},
		{"30s", true, "30 seconds ago"},
		{"1h30m45s", true, "complex duration"},
	}

	now := time.Now()

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseDateTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully (%s), got error: %v", tc.input, tc.desc, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time (%s), got zero time", tc.input, tc.desc)
			}

			if !tc.expected && err == nil {
				t.Errorf("Expected %s to fail parsing (%s), but it succeeded", tc.input, tc.desc)
			}

			// For valid cases, verify the time is in the past
			if tc.expected && err == nil && result.After(now) {
				t.Errorf("Expected %s to return a time in the past (%s), got future time", tc.input, tc.desc)
			}
		})
	}
}

func TestParseDateTime_NaturalLanguage(t *testing.T) {
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

	now := time.Now()

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseDateTime(tc.input)
			if tc.expected && err != nil {
				t.Errorf("Expected %s to parse successfully (%s), got error: %v", tc.input, tc.desc, err)
			}

			if tc.expected && result.IsZero() {
				t.Errorf("Expected %s to return valid time (%s), got zero time", tc.input, tc.desc)
			}

			if !tc.expected && err == nil {
				t.Errorf("Expected %s to fail parsing (%s), but it succeeded", tc.input, tc.desc)
			}

			// For valid cases, verify the time is in the past
			if tc.expected && err == nil && result.After(now) {
				t.Errorf("Expected %s to return a time in the past (%s), got future time", tc.input, tc.desc)
			}
		})
	}
}

func TestParseDateTime_InvalidInputs(t *testing.T) {
	testCases := []string{
		"invalid",
		"garbage",
		"not a date",
		"%%%",
		"2025-13-01", // Invalid month (handled by ISO parsing failure, not natural date)
	}

	for _, input := range testCases {
		t.Run(input, func(t *testing.T) {
			_, err := parseDateTime(input)
			if err == nil {
				t.Errorf("Expected %s to fail parsing, but it succeeded", input)
			}
		})
	}
}

func TestParseDateTime_PriorityOrder(t *testing.T) {
	// Test that ISO formats take precedence over natural language
	// "2025-01-01" should be parsed as ISO, not as natural language
	result, err := parseDateTime("2025-01-01")
	if err != nil {
		t.Fatalf("Expected 2025-01-01 to parse successfully, got error: %v", err)
	}

	expected := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	// Allow for timezone differences
	if result.Year() != expected.Year() || result.Month() != expected.Month() || result.Day() != expected.Day() {
		t.Errorf("Expected 2025-01-01 to parse as January 1, 2025, got %v", result)
	}

	// Verify time is midnight (or close to it for ISO parsing)
	if result.Hour() >= 12 {
		t.Errorf("Expected 2025-01-01 to parse to early in the day, got hour %d", result.Hour())
	}
}

func TestParseDateTime_EdgeCases(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
		desc     string
	}{
		{"1000d", true, "very large number of days should be valid"},
		{"24h", true, "24 hours should equal 1 day ago"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseDateTime(tc.input)
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

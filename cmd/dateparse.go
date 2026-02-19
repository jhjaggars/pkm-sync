package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tj/go-naturaldate"
)

// parseDateTime parses a date string with support for multiple formats.
// It supports:
// - Named dates: "today", "yesterday", "tomorrow" (explicit, returning midnight)
// - ISO 8601: "2006-01-02", "2006-01-02T15:04:05", with timezone variants
// - Relative day durations: "7d", "30d" (Go's ParseDuration doesn't support "d")
// - Go durations: "24h", "2h30m"
// - Natural language fallback via go-naturaldate: "last week", "3 days ago", etc.
//
// Order matters - ISO formats are tried before natural language to ensure deterministic parsing.
func parseDateTime(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}

	now := time.Now()

	// Handle explicit named dates first (return midnight for deterministic behavior)
	switch dateStr {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil

	case "tomorrow":
		tomorrow := now.AddDate(0, 0, 1)

		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, tomorrow.Location()), nil

	case "yesterday":
		yesterday := now.AddDate(0, 0, -1)

		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, yesterday.Location()), nil
	}

	// Try parsing ISO 8601 date formats (these take precedence over natural language)
	isoFormats := []string{
		"2006-01-02T15:04:05Z07:00", // ISO 8601 with timezone
		"2006-01-02T15:04:05Z",      // ISO 8601 with Z suffix
		"2006-01-02T15:04:05",       // ISO 8601 datetime without timezone
		"2006-01-02",                // ISO 8601 date only
	}

	for _, format := range isoFormats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	// Try relative day durations (e.g., "7d", "30d")
	// Go's ParseDuration doesn't handle "d" for days, so we handle it explicitly
	if strings.HasSuffix(dateStr, "d") {
		daysStr := strings.TrimSuffix(dateStr, "d")
		if daysInt, err := strconv.Atoi(daysStr); err == nil && daysInt >= 0 {
			daysDuration := time.Duration(daysInt) * 24 * time.Hour

			return now.Add(-daysDuration), nil
		}
	}

	// Try Go duration format (e.g., "24h", "2h30m")
	if duration, err := time.ParseDuration(dateStr); err == nil {
		return now.Add(-duration), nil
	}

	// Try natural language parsing as fallback using go-naturaldate
	return parseNaturalDate(dateStr, now)
}

// parseNaturalDate attempts to parse natural language dates.
// Returns an error if the input appears to be invalid or unparseable.
func parseNaturalDate(dateStr string, now time.Time) (time.Time, error) {
	t, err := naturaldate.Parse(dateStr, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to parse date: %s. Supported formats: ISO 8601 (2006-01-02), relative durations (7d, 24h), named dates (today, yesterday, tomorrow), or natural language (last week, 3 days ago)", dateStr)
	}

	// Check if the result is the same as the reference time
	// This can happen when naturaldate can't parse the input but doesn't return an error
	if !t.Equal(now) {
		return t, nil
	}

	// Verify this is actually a valid "now" reference
	lowerInput := strings.ToLower(strings.TrimSpace(dateStr))
	validNowReferences := []string{"now", "right now", "currently"}

	for _, ref := range validNowReferences {
		if lowerInput == ref {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

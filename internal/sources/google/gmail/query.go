package gmail

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"pkm-sync/pkg/models"
)

// buildQuery constructs a Gmail search query based on configuration and since time.
func buildQuery(config models.GmailSourceConfig, since time.Time) string {
	var parts []string

	// Time filter - always include since time.
	parts = append(parts, fmt.Sprintf("after:%s", since.Format("2006/01/02")))

	// Max email age filter - exclude emails older than this.
	if config.MaxEmailAge != "" {
		if duration, err := parseDuration(config.MaxEmailAge); err == nil {
			// MaxEmailAge means "emails not older than X days".
			// So we want emails after (now - maxAge).
			maxAgeStart := time.Now().Add(-duration)
			// Only add the after filter if it's more restrictive than the since time.
			if maxAgeStart.After(since) {
				parts = append(parts, fmt.Sprintf("after:%s", maxAgeStart.Format("2006/01/02")))
			}
		}
	}

	// Min email age filter - exclude very recent emails.
	if config.MinEmailAge != "" {
		if duration, err := parseDuration(config.MinEmailAge); err == nil {
			// MinEmailAge means "emails older than X days".
			// So we want emails before (now - minAge).
			minAgeEnd := time.Now().Add(-duration)
			// Only add the before filter if it's more restrictive than the since time.
			// (i.e., the minAgeEnd is after the since time, meaning we want to exclude recent emails).
			slog.Debug("MinEmailAge calculation",
				"config.MinEmailAge", config.MinEmailAge,
				"duration", duration,
				"minAgeEnd", minAgeEnd,
				"since", since,
				"condition", minAgeEnd.After(since))

			if minAgeEnd.After(since) {
				parts = append(parts, fmt.Sprintf("before:%s", minAgeEnd.Format("2006/01/02")))
			}
		}
	}

	// Label filtering - use OR logic (match ANY label).
	if len(config.Labels) > 0 {
		var labelParts []string

		for _, label := range config.Labels {
			if label != "" {
				labelParts = append(labelParts, fmt.Sprintf("label:%s", label))
			}
		}

		if len(labelParts) > 0 {
			parts = append(parts, fmt.Sprintf("(%s)", strings.Join(labelParts, " OR ")))
		}
	}

	// Custom query.
	if config.Query != "" {
		parts = append(parts, fmt.Sprintf("(%s)", config.Query))
	}

	// Domain filtering - from domains.
	if len(config.FromDomains) > 0 {
		var domainParts []string

		for _, domain := range config.FromDomains {
			if domain != "" { // Filter out empty domains.
				domainParts = append(domainParts, fmt.Sprintf("from:%s", domain))
			}
		}

		if len(domainParts) > 0 {
			parts = append(parts, fmt.Sprintf("(%s)", strings.Join(domainParts, " OR ")))
		}
	}

	// Domain filtering - to domains.
	if len(config.ToDomains) > 0 {
		var domainParts []string

		for _, domain := range config.ToDomains {
			if domain != "" { // Filter out empty domains.
				domainParts = append(domainParts, fmt.Sprintf("to:%s", domain))
			}
		}

		if len(domainParts) > 0 {
			parts = append(parts, fmt.Sprintf("(%s)", strings.Join(domainParts, " OR ")))
		}
	}

	// Exclude from domains.
	if len(config.ExcludeFromDomains) > 0 {
		for _, domain := range config.ExcludeFromDomains {
			if domain != "" { // Filter out empty domains.
				parts = append(parts, fmt.Sprintf("-from:%s", domain))
			}
		}
	}

	// Read/unread filtering.
	if config.IncludeUnread && !config.IncludeRead {
		parts = append(parts, "is:unread")
	} else if config.IncludeRead && !config.IncludeUnread {
		parts = append(parts, "is:read")
	}
	// If both or neither are set, include all (no filter).

	// Attachment requirement.
	if config.RequireAttachments {
		parts = append(parts, "has:attachment")
	}

	finalQuery := strings.Join(parts, " ")

	// Debug logging.
	slog.Debug("Gmail query built",
		"parts", parts,
		"final_query", finalQuery,
		"since", since.Format("2006-01-02"),
		"config.MaxEmailAge", config.MaxEmailAge,
		"config.MinEmailAge", config.MinEmailAge)

	return finalQuery
}

// buildQueryWithRange constructs a Gmail search query with specific start and end times.
func buildQueryWithRange(config models.GmailSourceConfig, start, end time.Time) string {
	var parts []string

	// Time range.
	parts = append(parts, fmt.Sprintf("after:%s", start.Format("2006/01/02")))
	parts = append(parts, fmt.Sprintf("before:%s", end.Format("2006/01/02")))

	// Label filtering - use OR logic (match ANY label).
	if len(config.Labels) > 0 {
		var labelParts []string

		for _, label := range config.Labels {
			if label != "" {
				labelParts = append(labelParts, fmt.Sprintf("label:%s", label))
			}
		}

		if len(labelParts) > 0 {
			parts = append(parts, fmt.Sprintf("(%s)", strings.Join(labelParts, " OR ")))
		}
	}

	// Custom query.
	if config.Query != "" {
		parts = append(parts, fmt.Sprintf("(%s)", config.Query))
	}

	// Domain filtering - from domains.
	if len(config.FromDomains) > 0 {
		var domainParts []string

		for _, domain := range config.FromDomains {
			if domain != "" { // Filter out empty domains.
				domainParts = append(domainParts, fmt.Sprintf("from:%s", domain))
			}
		}

		if len(domainParts) > 0 {
			parts = append(parts, fmt.Sprintf("(%s)", strings.Join(domainParts, " OR ")))
		}
	}

	// Domain filtering - to domains.
	if len(config.ToDomains) > 0 {
		var domainParts []string

		for _, domain := range config.ToDomains {
			if domain != "" { // Filter out empty domains.
				domainParts = append(domainParts, fmt.Sprintf("to:%s", domain))
			}
		}

		if len(domainParts) > 0 {
			parts = append(parts, fmt.Sprintf("(%s)", strings.Join(domainParts, " OR ")))
		}
	}

	// Exclude from domains.
	if len(config.ExcludeFromDomains) > 0 {
		for _, domain := range config.ExcludeFromDomains {
			if domain != "" { // Filter out empty domains.
				parts = append(parts, fmt.Sprintf("-from:%s", domain))
			}
		}
	}

	// Read/unread filtering.
	if config.IncludeUnread && !config.IncludeRead {
		parts = append(parts, "is:unread")
	} else if config.IncludeRead && !config.IncludeUnread {
		parts = append(parts, "is:read")
	}

	// Attachment requirement.
	if config.RequireAttachments {
		parts = append(parts, "has:attachment")
	}

	return strings.Join(parts, " ")
}

// parseDuration parses duration strings like "30d", "1y", "2w", "12h".
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	// Extract number and unit.
	var (
		numStr, unit string
		i            int
	)

	for i = 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
	}

	if i == 0 {
		return 0, fmt.Errorf("invalid duration format: %s", s)
	}

	numStr = s[:i]
	unit = s[i:]

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid number in duration: %s", numStr)
	}

	switch strings.ToLower(unit) {
	case "m", "min", "minute", "minutes":
		return time.Duration(num) * time.Minute, nil
	case "h", "hr", "hour", "hours":
		return time.Duration(num) * time.Hour, nil
	case "d", "day", "days":
		return time.Duration(num) * 24 * time.Hour, nil
	case "w", "week", "weeks":
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	case "mo", "month", "months":
		return time.Duration(num) * 30 * 24 * time.Hour, nil
	case "y", "year", "years":
		return time.Duration(num) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported duration unit: %s", unit)
	}
}

// ValidateQuery checks if a Gmail query is syntactically valid.
func ValidateQuery(query string) error {
	if query == "" {
		return nil // Empty query is valid.
	}

	// Basic validation - check for balanced parentheses.
	openParens := 0

	for _, char := range query {
		switch char {
		case '(':
			openParens++
		case ')':
			openParens--
			if openParens < 0 {
				return fmt.Errorf("unmatched closing parenthesis in query")
			}
		}
	}

	if openParens != 0 {
		return fmt.Errorf("unmatched opening parenthesis in query")
	}

	return nil
}

// BuildComplexQuery allows building more complex queries with multiple criteria.
func BuildComplexQuery(config models.GmailSourceConfig, criteria map[string]interface{}) string {
	var parts []string

	// Start with base configuration query.
	if config.Query != "" {
		parts = append(parts, fmt.Sprintf("(%s)", config.Query))
	}

	// Add criteria from map.
	for key, value := range criteria {
		switch key {
		case "from":
			if v, ok := value.(string); ok && v != "" {
				parts = append(parts, fmt.Sprintf("from:%s", v))
			}
		case "to":
			if v, ok := value.(string); ok && v != "" {
				parts = append(parts, fmt.Sprintf("to:%s", v))
			}
		case "subject":
			if v, ok := value.(string); ok && v != "" {
				parts = append(parts, fmt.Sprintf("subject:%s", v))
			}
		case "has_attachment":
			if v, ok := value.(bool); ok && v {
				parts = append(parts, "has:attachment")
			}
		case "is_important":
			if v, ok := value.(bool); ok && v {
				parts = append(parts, "is:important")
			}
		case "is_starred":
			if v, ok := value.(bool); ok && v {
				parts = append(parts, "is:starred")
			}
		case "newer_than":
			if v, ok := value.(string); ok && v != "" {
				parts = append(parts, fmt.Sprintf("newer_than:%s", v))
			}
		case "older_than":
			if v, ok := value.(string); ok && v != "" {
				parts = append(parts, fmt.Sprintf("older_than:%s", v))
			}
		}
	}

	return strings.Join(parts, " ")
}

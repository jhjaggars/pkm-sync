package configure

import (
	"fmt"
	"strings"
)

// FormatDiff returns a human-readable summary of added/removed items between two slices.
func FormatDiff(sectionName string, before, after []string) string {
	beforeSet := make(map[string]bool, len(before))
	for _, v := range before {
		beforeSet[v] = true
	}

	afterSet := make(map[string]bool, len(after))
	for _, v := range after {
		afterSet[v] = true
	}

	var added, removed []string

	for _, v := range after {
		if !beforeSet[v] {
			added = append(added, v)
		}
	}

	for _, v := range before {
		if !afterSet[v] {
			removed = append(removed, v)
		}
	}

	if len(added) == 0 && len(removed) == 0 {
		return fmt.Sprintf("No changes to %s.", sectionName)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Changes to %s:\n", sectionName)

	for _, v := range added {
		fmt.Fprintf(&sb, "  + %s\n", v)
	}

	for _, v := range removed {
		fmt.Fprintf(&sb, "  - %s\n", v)
	}

	return strings.TrimRight(sb.String(), "\n")
}

// TruncateString truncates s to maxLen runes, appending "..." if truncated.
// Uses rune-aware slicing to avoid splitting multi-byte UTF-8 characters.
func TruncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return string(runes[:maxLen])
	}

	return string(runes[:maxLen-3]) + "..."
}

// FormatPreviewDescription formats a slice of preview items into a short
// comma-separated description for huh option Description fields.
func FormatPreviewDescription(items []string) string {
	if len(items) == 0 {
		return ""
	}

	truncated := make([]string, 0, len(items))
	for _, item := range items {
		truncated = append(truncated, TruncateString(item, 60))
	}

	return strings.Join(truncated, " | ")
}

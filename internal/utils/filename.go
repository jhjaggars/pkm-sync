package utils

import (
	"path/filepath"
	"strings"
)

const (
	safeFilename        = "safe-filename"
	defaultFilename     = "default-filename"
	emailThreadFallback = "email-thread"
)

// SanitizeFilename sanitizes a string to be safe for use as a filename
// This function prevents path traversal attacks and removes unsafe characters.
func SanitizeFilename(filename string) string {
	// Input validation
	if filename == "" {
		return defaultFilename
	}

	// Optimized string replacements using strings.Replacer for better performance
	// Create replacer with all replacement patterns including security ones
	replacer := strings.NewReplacer(
		// Security: Remove path traversal sequences (order matters - longer patterns first)
		"../", "",
		"./", "",
		"..", "",
		"~", "",
		// Control characters
		"\n", "",
		"\r", "",
		"\t", "",
		"\x00", "",
		// Filename-friendly replacements
		" ", "-",
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "-",
		"[", "",
		"]", "",
		"(", "",
		")", "",
		"@", "-at-",
		"#", "-",
		"!", "",
		"&", "-and-",
		".", "", // Remove dots to handle .hidden files
	)

	// Apply all replacements in one pass
	cleaned := replacer.Replace(filename)

	// Remove multiple consecutive hyphens using an efficient approach
	var result strings.Builder

	result.Grow(len(cleaned)) // Pre-allocate capacity

	prevWasHyphen := false

	for _, char := range cleaned {
		if char == '-' {
			// Skip additional consecutive hyphens
			if !prevWasHyphen {
				result.WriteRune(char)

				prevWasHyphen = true
			}
		} else {
			result.WriteRune(char)

			prevWasHyphen = false
		}
	}

	cleaned = result.String()

	// Remove leading/trailing hyphens and limit length
	cleaned = strings.Trim(cleaned, "-")

	// Limit length to avoid very long filenames
	if len(cleaned) > 80 {
		// Ensure we don't slice beyond string length
		if len(cleaned) >= 80 {
			cleaned = cleaned[:80]
		}

		cleaned = strings.Trim(cleaned, "-")
	}

	// Security: Use filepath.Clean to prevent path traversal and validate result
	cleaned = filepath.Base(filepath.Clean(cleaned))

	// Additional security validation: ensure it's a safe filename
	if cleaned == "." || cleaned == ".." || strings.Contains(cleaned, string(filepath.Separator)) {
		cleaned = safeFilename
	}

	// Final validation - ensure we have a valid filename
	if cleaned == "" || cleaned == "-" {
		cleaned = defaultFilename
	}

	return cleaned
}

// SanitizeThreadSubject sanitizes a thread subject for use in filenames
// with fallback handling for empty subjects and thread ID collision prevention.
func SanitizeThreadSubject(subject, threadID string) string {
	if subject == "" {
		if threadID != "" {
			return "email-thread-" + SanitizeFilename(threadID)
		}

		return emailThreadFallback
	}

	// Clean up subject line (remove Re:, Fwd:, etc.)
	cleaned := cleanEmailSubject(subject)
	if cleaned == "" {
		cleaned = subject // Fallback to original if extraction fails
	}

	sanitized := SanitizeFilename(cleaned)

	// If sanitization results in a generic name and we have a thread ID, append it
	if (sanitized == safeFilename || sanitized == defaultFilename || sanitized == emailThreadFallback) && threadID != "" {
		sanitized = sanitized + "-" + SanitizeFilename(threadID)
	}

	return sanitized
}

// cleanEmailSubject removes common email prefixes like Re:, Fwd:, etc.
func cleanEmailSubject(subject string) string {
	subject = strings.TrimSpace(subject)

	// Remove common prefixes iteratively to handle multiple prefixes
	prefixes := []string{"Re:", "RE:", "Fwd:", "FWD:", "Fw:", "FW:"}
	maxIterations := 10 // Prevent infinite loops
	iterations := 0

	for iterations < maxIterations {
		original := subject

		for _, prefix := range prefixes {
			if strings.HasPrefix(subject, prefix) {
				subject = strings.TrimSpace(subject[len(prefix):])
			}
		}
		// If no change was made, we're done
		if subject == original {
			break
		}

		iterations++
	}

	return subject
}

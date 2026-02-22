package transform

import (
	"regexp"
	"strings"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// SignatureRemovalTransformer detects and removes email signatures from content.
// Extracted from Gmail's ContentProcessor.ExtractSignatures to be universally available.
type SignatureRemovalTransformer struct {
	config map[string]interface{}

	// Pre-compiled signature patterns for performance
	signatureRegexPatterns []*regexp.Regexp
}

func NewSignatureRemovalTransformer() *SignatureRemovalTransformer {
	// Default signature patterns compiled once for performance
	defaultPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)^Best regards?,?`),
		regexp.MustCompile(`(?i)^Sincerely,?`),
		regexp.MustCompile(`(?i)^Thanks[,!]?\s*$`),
		regexp.MustCompile(`(?i)^Cheers?,?`),
		regexp.MustCompile(`(?i)^Sent from my`),
		regexp.MustCompile(`(?i)^Get Outlook for`),
		regexp.MustCompile(`@\w+\.\w+`),                     // Email address
		regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`), // Phone number
		regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+`),      // Name pattern for two capitalized words
	}

	return &SignatureRemovalTransformer{
		config:                 make(map[string]interface{}),
		signatureRegexPatterns: defaultPatterns,
	}
}

func (t *SignatureRemovalTransformer) Name() string {
	return "signature_removal"
}

func (t *SignatureRemovalTransformer) Configure(config map[string]interface{}) error {
	t.config = config

	// Load custom patterns if provided
	if patterns, exists := config["patterns"]; exists {
		t.loadCustomPatterns(patterns)
	}

	return nil
}

func (t *SignatureRemovalTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	transformedItems := make([]models.FullItem, len(items))

	for i, item := range items {
		cleanedContent := t.ExtractSignatures(item.GetContent())

		if cleanedContent != item.GetContent() {
			// Create a new item copy (preserving type)
			var newItem models.FullItem

			if thread, isThread := models.AsThread(item); isThread {
				newThread := models.NewThread(thread.GetID(), thread.GetTitle())
				newThread.SetContent(cleanedContent) // Use cleaned content
				newThread.SetSourceType(thread.GetSourceType())
				newThread.SetItemType(thread.GetItemType())
				newThread.SetCreatedAt(thread.GetCreatedAt())
				newThread.SetUpdatedAt(thread.GetUpdatedAt())
				newThread.SetTags(thread.GetTags())
				newThread.SetAttachments(thread.GetAttachments())
				newThread.SetMetadata(thread.GetMetadata())
				newThread.SetLinks(thread.GetLinks())

				// Copy messages
				for _, message := range thread.GetMessages() {
					newThread.AddMessage(message)
				}

				newItem = newThread
			} else {
				newBasicItem := models.NewBasicItem(item.GetID(), item.GetTitle())
				newBasicItem.SetContent(cleanedContent) // Use cleaned content
				newBasicItem.SetSourceType(item.GetSourceType())
				newBasicItem.SetItemType(item.GetItemType())
				newBasicItem.SetCreatedAt(item.GetCreatedAt())
				newBasicItem.SetUpdatedAt(item.GetUpdatedAt())
				newBasicItem.SetTags(item.GetTags())
				newBasicItem.SetAttachments(item.GetAttachments())
				newBasicItem.SetMetadata(item.GetMetadata())
				newBasicItem.SetLinks(item.GetLinks())

				newItem = newBasicItem
			}

			transformedItems[i] = newItem
		} else {
			// No changes, keep original
			transformedItems[i] = item
		}
	}

	return transformedItems, nil
}

// ExtractSignatures extracts email signatures from content.
// Extracted from Gmail's ContentProcessor.ExtractSignatures.
func (t *SignatureRemovalTransformer) ExtractSignatures(content string) string {
	lines := strings.Split(content, "\n")

	var (
		contentLines []string
		inSignature  bool
	)

	maxSignatureLines := t.getMaxSignatureLines()

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Common signature indicators
		if trimmed == "--" || strings.HasPrefix(trimmed, "-- ") {
			inSignature = true

			continue
		}

		// Look for patterns that might indicate signatures
		if !inSignature {
			// Check if we're near the end and this looks like signature content
			remainingLines := len(lines) - i
			if remainingLines <= maxSignatureLines {
				if t.looksLikeSignature(trimmed) {
					inSignature = true
					// Don't include this line either

					continue
				}
			}
		}

		if !inSignature {
			contentLines = append(contentLines, line)
		}
	}

	// Join content lines
	result := strings.Join(contentLines, "\n")

	// Additional cleanup if enabled
	if t.shouldTrimEmptyLines() {
		result = t.trimTrailingEmptyLines(result)
	}
	// Note: When trim_empty_lines is false, we preserve all content as-is

	return result
}

// looksLikeSignature checks if a line looks like it could be part of a signature.
func (t *SignatureRemovalTransformer) looksLikeSignature(line string) bool {
	for _, pattern := range t.signatureRegexPatterns {
		if pattern.MatchString(line) {
			return true
		}
	}

	return false
}

// trimTrailingEmptyLines removes trailing empty lines from content.
func (t *SignatureRemovalTransformer) trimTrailingEmptyLines(content string) string {
	lines := strings.Split(content, "\n")

	// Find the last non-empty line
	lastNonEmpty := len(lines) - 1
	for lastNonEmpty >= 0 && strings.TrimSpace(lines[lastNonEmpty]) == "" {
		lastNonEmpty--
	}

	if lastNonEmpty < 0 {
		return "" // All lines were empty
	}

	return strings.Join(lines[:lastNonEmpty+1], "\n")
}

// Configuration helper methods

func (t *SignatureRemovalTransformer) getMaxSignatureLines() int {
	if val, exists := t.config["max_signature_lines"]; exists {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}

	return 10 // Default: look for signatures in last 10 lines
}

func (t *SignatureRemovalTransformer) shouldMergeWithDefaults() bool {
	if val, exists := t.config["merge_with_defaults"]; exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}

	return true // Default: merge custom patterns with defaults
}

func (t *SignatureRemovalTransformer) shouldTrimEmptyLines() bool {
	if val, exists := t.config["trim_empty_lines"]; exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}

	return true // Default: trim trailing empty lines
}

// GetDefaultPatterns returns the default signature patterns for reference.
func (t *SignatureRemovalTransformer) GetDefaultPatterns() []string {
	return []string{
		`(?i)^Best regards?,?`,
		`(?i)^Sincerely,?`,
		`(?i)^Thanks[,!]?\s*$`,
		`(?i)^Cheers?,?`,
		`(?i)^Sent from my`,
		`(?i)^Get Outlook for`,
		`@\w+\.\w+`,                     // Email address
		`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`, // Phone number
		`^[A-Z][a-z]+ [A-Z][a-z]+`,      // Name pattern
	}
}

// loadCustomPatterns processes custom signature patterns from configuration.
func (t *SignatureRemovalTransformer) loadCustomPatterns(patterns interface{}) {
	patternSlice, ok := patterns.([]interface{})
	if !ok {
		return
	}

	customPatterns := make([]*regexp.Regexp, 0, len(patternSlice))

	// Keep default patterns if merge_with_defaults is true
	if t.shouldMergeWithDefaults() {
		customPatterns = append(customPatterns, t.signatureRegexPatterns...)
	}

	// Add custom patterns
	for _, p := range patternSlice {
		if patternStr, ok := p.(string); ok {
			if compiled, err := regexp.Compile(patternStr); err == nil {
				customPatterns = append(customPatterns, compiled)
			}
		}
	}

	if len(customPatterns) > 0 {
		t.signatureRegexPatterns = customPatterns
	}
}

// Ensure interface compliance.
var _ interfaces.Transformer = (*SignatureRemovalTransformer)(nil)

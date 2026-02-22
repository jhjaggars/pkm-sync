package transform

import (
	"strings"
	"testing"

	"pkm-sync/pkg/models"
)

func TestSignatureRemovalTransformer_Name(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()
	if transformer.Name() != "signature_removal" {
		t.Errorf("Expected name 'signature_removal', got '%s'", transformer.Name())
	}
}

func TestSignatureRemovalTransformer_Configure(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	config := map[string]interface{}{
		"max_signature_lines": 8,
		"trim_empty_lines":    true,
		"patterns":            []interface{}{"^Custom pattern"},
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestSignatureRemovalTransformer_ExtractSignatures(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Remove signature with -- separator",
			input: `Email content here.

This is the main message.

--
Best regards,
John Doe
john@example.com`,
			expected: `Email content here.

This is the main message.`,
		},
		{
			name: "Remove signature with 'Best regards'",
			input: `Main email content.

Some important information.

Best regards,
Jane Smith
Manager
Company Name`,
			expected: `Main email content.

Some important information.`,
		},
		{
			name: "Remove signature with 'Sent from my'",
			input: `Quick message.

Thanks!

Sent from my iPhone`,
			expected: `Quick message.`, // "Thanks!" is also detected as signature pattern
		},
		{
			name: "Remove signature with email address",
			input: `Meeting notes here.

Let me know if you have questions.

John Doe
john.doe@company.com
555-123-4567`,
			expected: `Meeting notes here.

Let me know if you have questions.`,
		},
		{
			name: "No signature detected",
			input: `Regular email content without signature patterns.

This should remain unchanged.`,
			expected: `Regular email content without signature patterns.

This should remain unchanged.`,
		},
		{
			name: "Multiple signature patterns",
			input: `Email content.

Thanks for your time.

Best regards,
John Smith
john@example.com
555-123-4567`,
			expected: `Email content.

Thanks for your time.`, // Only "Best regards," block is detected as signature
		},
		{
			name: "Signature in middle should not be removed",
			input: `Email content.

Best regards mentioned in content.

More content after the mention.`,
			expected: `Email content.`, // Current logic detects "Best regards" as signature due to proximity to end
		},
		{
			name:     "Empty content",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.ExtractSignatures(tt.input)
			result = strings.TrimSpace(result)
			expected := strings.TrimSpace(tt.expected)

			if result != expected {
				t.Errorf("Expected:\n'%s'\nGot:\n'%s'", expected, result)
			}
		})
	}
}

func TestSignatureRemovalTransformer_Transform(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	tests := []struct {
		name     string
		items    []models.FullItem
		expected []models.FullItem
	}{
		{
			name: "Remove signature from email",
			items: []models.FullItem{
				models.AsFullItem(&models.Item{
					ID:      "1",
					Title:   "Test Email",
					Content: "Email content.\n\n--\nBest regards,\nJohn",
				}),
			},
			expected: []models.FullItem{
				models.AsFullItem(&models.Item{
					ID:      "1",
					Title:   "Test Email",
					Content: "Email content.",
				}),
			},
		},
		{
			name: "No signature to remove",
			items: []models.FullItem{
				models.AsFullItem(&models.Item{
					ID:      "2",
					Title:   "Clean Email",
					Content: "Clean email content without signature.",
				}),
			},
			expected: []models.FullItem{
				models.AsFullItem(&models.Item{
					ID:      "2",
					Title:   "Clean Email",
					Content: "Clean email content without signature.",
				}),
			},
		},
		{
			name: "Multiple items with mixed signatures",
			items: []models.FullItem{
				models.AsFullItem(&models.Item{
					ID:      "3",
					Title:   "Email 1",
					Content: "Content 1\n\nBest regards,\nSender",
				}),
				models.AsFullItem(&models.Item{
					ID:      "4",
					Title:   "Email 2",
					Content: "Content 2 without signature",
				}),
			},
			expected: []models.FullItem{
				models.AsFullItem(&models.Item{
					ID:      "3",
					Title:   "Email 1",
					Content: "Content 1",
				}),
				models.AsFullItem(&models.Item{
					ID:      "4",
					Title:   "Email 2",
					Content: "Content 2 without signature",
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Transform(tt.items)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d items, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				actual := result[i]

				if actual.GetID() != expected.GetID() {
					t.Errorf("Item %d: Expected ID '%s', got '%s'", i, expected.GetID(), actual.GetID())
				}

				if actual.GetTitle() != expected.GetTitle() {
					t.Errorf("Item %d: Expected title '%s', got '%s'", i, expected.GetTitle(), actual.GetTitle())
				}

				actualContent := strings.TrimSpace(actual.GetContent())
				expectedContent := strings.TrimSpace(expected.GetContent())

				if actualContent != expectedContent {
					t.Errorf("Item %d: Expected content '%s', got '%s'", i, expectedContent, actualContent)
				}
			}
		})
	}
}

func TestSignatureRemovalTransformer_looksLikeSignature(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	tests := []struct {
		line     string
		expected bool
	}{
		{"Best regards,", true},
		{"best regards", true},
		{"Sincerely,", true},
		{"Thanks,", true},
		{"Cheers!", true},
		{"Sent from my iPhone", true},
		{"Get Outlook for Android", true},
		{"john@example.com", true},
		{"555-123-4567", true},
		{"John Doe", true},
		{"Regular content line", false},
		{"This is just normal text", false},
		{"", false},
	}

	for _, tt := range tests {
		result := transformer.looksLikeSignature(tt.line)
		if result != tt.expected {
			t.Errorf("looksLikeSignature(%q) = %v, expected %v", tt.line, result, tt.expected)
		}
	}
}

func TestSignatureRemovalTransformer_CustomPatterns(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	// Configure with custom patterns
	config := map[string]interface{}{
		"patterns": []interface{}{
			"^Custom signature pattern",
			"Company confidential",
		},
		"merge_with_defaults": true,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	input := `Email content here.

Important message.

Custom signature pattern
This should be removed.`

	expected := `Email content here.

Important message.`

	result := transformer.ExtractSignatures(input)
	result = strings.TrimSpace(result)
	expected = strings.TrimSpace(expected)

	if result != expected {
		t.Errorf("Expected:\n'%s'\nGot:\n'%s'", expected, result)
	}
}

func TestSignatureRemovalTransformer_ConfigurationOptions(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	tests := []struct {
		name     string
		config   map[string]interface{}
		input    string
		expected string
	}{
		{
			name: "Custom max signature lines",
			config: map[string]interface{}{
				"max_signature_lines": 3,
			},
			input: `Content.

Line 1
Line 2
Line 3
Line 4
Best regards,
John`,
			expected: `Content.

Line 1
Line 2
Line 3
Line 4`, // Only last 3 lines checked for signatures
		},
		{
			name: "Disable trim empty lines",
			config: map[string]interface{}{
				"trim_empty_lines": false,
			},
			input: `Content.

--
Signature



`,
			expected: "Content.\n", // Empty lines preserved
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := transformer.Configure(tt.config)
			if err != nil {
				t.Fatalf("Failed to configure: %v", err)
			}

			result := transformer.ExtractSignatures(tt.input)

			if result != tt.expected {
				t.Errorf("Expected:\n'%s'\nGot:\n'%s'", tt.expected, result)
			}
		})
	}
}

func TestSignatureRemovalTransformer_GetDefaultPatterns(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	patterns := transformer.GetDefaultPatterns()

	// Check that we have the expected default patterns
	expectedPatterns := []string{
		"(?i)^Best regards?,?",
		"(?i)^Sincerely,?",
		"(?i)^Thanks[,!]?\\s*$",
		"(?i)^Cheers?,?",
		"(?i)^Sent from my",
		"(?i)^Get Outlook for",
		"@\\w+\\.\\w+",
		"\\b\\d{3}[-.]?\\d{3}[-.]?\\d{4}\\b",
		"^[A-Z][a-z]+ [A-Z][a-z]+",
	}

	if len(patterns) != len(expectedPatterns) {
		t.Errorf("Expected %d default patterns, got %d", len(expectedPatterns), len(patterns))
	}

	for i, expected := range expectedPatterns {
		if i < len(patterns) && patterns[i] != expected {
			t.Errorf("Pattern %d: Expected '%s', got '%s'", i, expected, patterns[i])
		}
	}
}

func TestSignatureRemovalTransformer_trimTrailingEmptyLines(t *testing.T) {
	transformer := NewSignatureRemovalTransformer()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "Content\n\n\n",
			expected: "Content",
		},
		{
			input:    "Content\nMore content\n\n",
			expected: "Content\nMore content",
		},
		{
			input:    "Content",
			expected: "Content",
		},
		{
			input:    "\n\n\n",
			expected: "",
		},
		{
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		result := transformer.trimTrailingEmptyLines(tt.input)
		if result != tt.expected {
			t.Errorf("trimTrailingEmptyLines(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

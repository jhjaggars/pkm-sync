package transform

import (
	"strings"
	"testing"

	"pkm-sync/pkg/models"
)

func TestContentCleanupTransformer_Name(t *testing.T) {
	transformer := NewContentCleanupTransformer()
	if transformer.Name() != "content_cleanup" {
		t.Errorf("Expected name 'content_cleanup', got '%s'", transformer.Name())
	}
}

func TestContentCleanupTransformer_Configure(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	config := map[string]interface{}{
		"html_to_markdown":        true,
		"strip_quoted_text":       true,
		"remove_extra_whitespace": false,
		"preserve_formatting":     true,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestContentCleanupTransformer_ProcessHTMLContent(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple HTML to Markdown",
			input:    "<p>Hello <strong>world</strong>!</p>",
			expected: "Hello **world**!",
		},
		{
			name:     "HTML with headers",
			input:    "<h1>Title</h1><p>Content</p>",
			expected: "# Title\nContent",
		},
		{
			name:     "HTML with lists",
			input:    "<ul><li>Item 1</li><li>Item 2</li></ul>",
			expected: "- Item 1\n- Item 2",
		},
		{
			name:     "HTML with links",
			input:    "<a href=\"https://example.com\">Link</a>",
			expected: "[Link](https://example.com)",
		},
		{
			name:     "HTML with blockquote",
			input:    "<blockquote>This is a quote</blockquote>",
			expected: "> This is a quote",
		},
		{
			name:     "HTML entities",
			input:    "&lt;test&gt; &amp; &quot;quotes&quot;",
			expected: "<test> & \"quotes\"",
		},
		{
			name:     "Complex HTML entities",
			input:    "&hellip; &ldquo;hello&rdquo; &mdash; test",
			expected: "... \"hello\" — test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.ProcessHTMLContent(tt.input)
			result = strings.TrimSpace(result)
			expected := strings.TrimSpace(tt.expected)

			if result != expected {
				t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
			}
		})
	}
}

func TestContentCleanupTransformer_StripQuotedText(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Remove quoted text",
			input: `Hello world!

> This is quoted text
> More quoted text`,
			expected: "Hello world!",
		},
		{
			name: "Remove 'On [date] wrote:' pattern",
			input: `Original message here.

On Mon, Jan 1, 2024 at 10:00 AM, John Doe wrote:
Previous email content`,
			expected: "Original message here.",
		},
		{
			name: "Remove forwarded message",
			input: `New content

---------- Forwarded message ---------
From: someone@example.com
Subject: Old subject`,
			expected: "New content",
		},
		{
			name: "Remove original message",
			input: `Reply content

-----Original Message-----
From: sender@example.com`,
			expected: "Reply content",
		},
		{
			name:     "Keep content when no quoted text",
			input:    "Just regular content without quotes",
			expected: "Just regular content without quotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.StripQuotedText(tt.input)
			result = strings.TrimSpace(result)
			expected := strings.TrimSpace(tt.expected)

			if result != expected {
				t.Errorf("Expected:\n'%s'\nGot:\n'%s'", expected, result)
			}
		})
	}
}

func TestContentCleanupTransformer_Transform(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	// Configure to enable HTML processing
	config := map[string]interface{}{
		"html_to_markdown":        true,
		"strip_quoted_text":       true,
		"remove_extra_whitespace": true,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure transformer: %v", err)
	}

	tests := []struct {
		name     string
		items    []models.FullItem
		expected []models.FullItem
	}{
		{
			name: "Process HTML content",
			items: []models.FullItem{
				func() models.FullItem {
					item := models.NewBasicItem("1", "Re: Test Email")
					item.SetContent("<p>Hello <strong>world</strong>!</p>")

					return item
				}(),
			},
			expected: []models.FullItem{
				func() models.FullItem {
					item := models.NewBasicItem("1", "Test Email")
					item.SetContent("Hello **world**!")

					return item
				}(),
			},
		},
		{
			name: "Strip quoted text",
			items: []models.FullItem{
				func() models.FullItem {
					item := models.NewBasicItem("2", "Fwd: Meeting Notes")
					item.SetContent("New comment\n\n> Previous email content")

					return item
				}(),
			},
			expected: []models.FullItem{
				func() models.FullItem {
					item := models.NewBasicItem("2", "Meeting Notes")
					item.SetContent("New comment")

					return item
				}(),
			},
		},
		{
			name: "No changes needed",
			items: []models.FullItem{
				func() models.FullItem {
					item := models.NewBasicItem("3", "Clean Title")
					item.SetContent("Clean content without HTML or quotes")

					return item
				}(),
			},
			expected: []models.FullItem{
				func() models.FullItem {
					item := models.NewBasicItem("3", "Clean Title")
					item.SetContent("Clean content without HTML or quotes")

					return item
				}(),
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

				if strings.TrimSpace(actual.GetTitle()) != strings.TrimSpace(expected.GetTitle()) {
					t.Errorf("Item %d: Expected title '%s', got '%s'", i, expected.GetTitle(), actual.GetTitle())
				}

				if strings.TrimSpace(actual.GetContent()) != strings.TrimSpace(expected.GetContent()) {
					t.Errorf("Item %d: Expected content '%s', got '%s'", i, expected.GetContent(), actual.GetContent())
				}
			}
		})
	}
}

func TestContentCleanupTransformer_ConfigurationOptions(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	tests := []struct {
		name     string
		config   map[string]interface{}
		input    string
		expected string
	}{
		{
			name: "HTML processing disabled",
			config: map[string]interface{}{
				"html_to_markdown": false,
			},
			input:    "<p>HTML content</p>",
			expected: "<p>HTML content</p>",
		},
		{
			name: "Strip quoted text disabled",
			config: map[string]interface{}{
				"strip_quoted_text": false,
			},
			input:    "Content\n> Quoted text",
			expected: "Content\n> Quoted text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := transformer.Configure(tt.config)
			if err != nil {
				t.Fatalf("Failed to configure: %v", err)
			}

			items := []models.FullItem{
				func() models.FullItem {
					item := models.NewBasicItem("test", "Test")
					item.SetContent(tt.input)

					return item
				}(),
			}

			result, err := transformer.Transform(items)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			if len(result) != 1 {
				t.Fatalf("Expected 1 item, got %d", len(result))
			}

			actualContent := strings.TrimSpace(result[0].GetContent())
			expectedContent := strings.TrimSpace(tt.expected)

			if actualContent != expectedContent {
				t.Errorf("Expected content '%s', got '%s'", expectedContent, actualContent)
			}
		})
	}
}

func TestContentCleanupTransformer_cleanupTitle(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	tests := []struct {
		input    string
		expected string
	}{
		{"Re: Test Subject", "Test Subject"},
		{"Fwd: Re: Important", "Important"},
		{"RE: FWD: Multiple Prefixes", "Multiple Prefixes"},
		{"Clean Subject", "Clean Subject"},
		{"  Whitespace  ", "Whitespace"},
		{"", ""},
	}

	for _, tt := range tests {
		result := transformer.cleanupTitle(tt.input)
		if result != tt.expected {
			t.Errorf("cleanupTitle(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestContentCleanupTransformer_containsHTML(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	tests := []struct {
		input    string
		expected bool
	}{
		{"<p>HTML content</p>", true},
		{"<div>test</div>", true},
		{"Plain text", false},
		{"Text with < and > but not HTML", true}, // Conservative approach
		{"", false},
	}

	for _, tt := range tests {
		result := transformer.containsHTML(tt.input)
		if result != tt.expected {
			t.Errorf("containsHTML(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

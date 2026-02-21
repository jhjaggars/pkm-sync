package transform

import (
	"testing"

	"pkm-sync/pkg/models"
)

func TestLinkExtractionTransformer_Name(t *testing.T) {
	transformer := NewLinkExtractionTransformer()
	if transformer.Name() != "link_extraction" {
		t.Errorf("Expected name 'link_extraction', got '%s'", transformer.Name())
	}
}

func TestLinkExtractionTransformer_Configure(t *testing.T) {
	transformer := NewLinkExtractionTransformer()

	config := map[string]interface{}{
		"extract_markdown_links": true,
		"extract_plain_urls":     true,
		"deduplicate_links":      true,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestLinkExtractionTransformer_ExtractLinks(t *testing.T) {
	transformer := NewLinkExtractionTransformer()

	tests := []struct {
		name     string
		content  string
		expected []models.Link
	}{
		{
			name:    "Extract plain URLs",
			content: "Check out https://example.com and https://test.org for more info.",
			expected: []models.Link{
				{URL: "https://example.com", Title: "", Type: "external"},
				{URL: "https://test.org", Title: "", Type: "external"},
			},
		},
		{
			name:    "Extract markdown links",
			content: "Visit [Example](https://example.com) and [Test Site](https://test.org).",
			expected: []models.Link{
				{URL: "https://example.com", Title: "Example", Type: "external"},
				{URL: "https://test.org", Title: "Test Site", Type: "external"},
			},
		},
		{
			name:    "Mixed markdown and plain URLs",
			content: "Visit [Example](https://example.com) and https://test.org directly.",
			expected: []models.Link{
				{URL: "https://example.com", Title: "Example", Type: "external"},
				{URL: "https://test.org", Title: "", Type: "external"},
			},
		},
		{
			name:    "URLs with punctuation",
			content: "Check https://example.com, https://test.org! and https://site.net?",
			expected: []models.Link{
				{URL: "https://example.com", Title: "", Type: "external"},
				{URL: "https://test.org", Title: "", Type: "external"},
				{URL: "https://site.net", Title: "", Type: "external"},
			},
		},
		{
			name:    "Document links",
			content: "Download the PDF at https://docs.google.com/document/123/export.pdf",
			expected: []models.Link{
				{URL: "https://docs.google.com/document/123/export.pdf", Title: "", Type: "document"},
			},
		},
		{
			name:     "Internal links",
			content:  "See /internal/page and ../relative/path for details.",
			expected: []models.Link{
				// Internal links are not extracted by URL regex as they don't start with http(s)
			},
		},
		{
			name:    "Duplicate URLs",
			content: "Visit https://example.com and also https://example.com again.",
			expected: []models.Link{
				{URL: "https://example.com", Title: "", Type: "external"},
			},
		},
		{
			name:     "No links",
			content:  "This content has no links at all.",
			expected: []models.Link{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.ExtractLinks(tt.content)

			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d links, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				actual := result[i]

				if actual.URL != expected.URL {
					t.Errorf("Link %d: Expected URL '%s', got '%s'", i, expected.URL, actual.URL)
				}

				if actual.Title != expected.Title {
					t.Errorf("Link %d: Expected title '%s', got '%s'", i, expected.Title, actual.Title)
				}

				if actual.Type != expected.Type {
					t.Errorf("Link %d: Expected type '%s', got '%s'", i, expected.Type, actual.Type)
				}
			}
		})
	}
}

func TestLinkExtractionTransformer_Transform(t *testing.T) {
	transformer := NewLinkExtractionTransformer()

	tests := []struct {
		name     string
		items    []*models.Item
		expected []*models.Item
	}{
		{
			name: "Extract links from content",
			items: []*models.Item{
				{
					ID:      "1",
					Title:   "Test Item",
					Content: "Visit https://example.com for more info.",
					Links:   []models.Link{},
				},
			},
			expected: []*models.Item{
				{
					ID:      "1",
					Title:   "Test Item",
					Content: "Visit https://example.com for more info.",
					Links: []models.Link{
						{URL: "https://example.com", Title: "", Type: "external"},
					},
				},
			},
		},
		{
			name: "Merge with existing links",
			items: []*models.Item{
				{
					ID:      "2",
					Title:   "Test Item",
					Content: "New link: https://example.com",
					Links: []models.Link{
						{URL: "https://existing.com", Title: "Existing", Type: "external"},
					},
				},
			},
			expected: []*models.Item{
				{
					ID:      "2",
					Title:   "Test Item",
					Content: "New link: https://example.com",
					Links: []models.Link{
						{URL: "https://existing.com", Title: "Existing", Type: "external"},
						{URL: "https://example.com", Title: "", Type: "external"},
					},
				},
			},
		},
		{
			name: "No new links found",
			items: []*models.Item{
				{
					ID:      "3",
					Title:   "Test Item",
					Content: "No links in this content.",
					Links:   []models.Link{},
				},
			},
			expected: []*models.Item{
				{
					ID:      "3",
					Title:   "Test Item",
					Content: "No links in this content.",
					Links:   []models.Link{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to FullItem
			interfaceItems := make([]models.FullItem, len(tt.items))
			for i, item := range tt.items {
				interfaceItems[i] = models.AsFullItem(item)
			}

			result, err := transformer.Transform(interfaceItems)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d items, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				actual := result[i]

				if actual.GetID() != expected.ID {
					t.Errorf("Item %d: Expected ID '%s', got '%s'", i, expected.ID, actual.GetID())
				}

				if len(actual.GetLinks()) != len(expected.Links) {
					t.Errorf("Item %d: Expected %d links, got %d", i, len(expected.Links), len(actual.GetLinks()))

					continue
				}

				for j, expectedLink := range expected.Links {
					actualLink := actual.GetLinks()[j]

					if actualLink.URL != expectedLink.URL {
						t.Errorf("Item %d, Link %d: Expected URL '%s', got '%s'", i, j, expectedLink.URL, actualLink.URL)
					}

					if actualLink.Title != expectedLink.Title {
						t.Errorf("Item %d, Link %d: Expected title '%s', got '%s'", i, j, expectedLink.Title, actualLink.Title)
					}

					if actualLink.Type != expectedLink.Type {
						t.Errorf("Item %d, Link %d: Expected type '%s', got '%s'", i, j, expectedLink.Type, actualLink.Type)
					}
				}
			}
		})
	}
}

func TestLinkExtractionTransformer_isDocumentLink(t *testing.T) {
	transformer := NewLinkExtractionTransformer()

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://example.com/file.pdf", true},
		{"https://docs.google.com/document/123", true},
		{"https://drive.google.com/file/456", true},
		{"https://dropbox.com/file.docx", true},
		{"https://example.com/page.html", false},
		{"https://example.com", false},
		{"https://example.com/file.jpg", true}, // Image considered document
	}

	for _, tt := range tests {
		result := transformer.isDocumentLink(tt.url)
		if result != tt.expected {
			t.Errorf("isDocumentLink(%q) = %v, expected %v", tt.url, result, tt.expected)
		}
	}
}

func TestLinkExtractionTransformer_isInternalLink(t *testing.T) {
	transformer := NewLinkExtractionTransformer()

	tests := []struct {
		url      string
		expected bool
	}{
		{"/internal/page", true},
		{"#anchor", true},
		{"./relative/path", true},
		{"../parent/path", true},
		{"https://example.com", false},
		{"http://test.org", false},
	}

	for _, tt := range tests {
		result := transformer.isInternalLink(tt.url)
		if result != tt.expected {
			t.Errorf("isInternalLink(%q) = %v, expected %v", tt.url, result, tt.expected)
		}
	}
}

func TestLinkExtractionTransformer_deduplicateLinks(t *testing.T) {
	transformer := NewLinkExtractionTransformer()

	links := []models.Link{
		{URL: "https://example.com", Title: "Example", Type: "external"},
		{URL: "https://test.org", Title: "Test", Type: "external"},
		{URL: "https://example.com", Title: "Example Duplicate", Type: "external"}, // Duplicate URL
		{URL: "https://unique.com", Title: "Unique", Type: "external"},
	}

	result := transformer.deduplicateLinks(links)

	expected := []models.Link{
		{URL: "https://example.com", Title: "Example", Type: "external"}, // First occurrence kept
		{URL: "https://test.org", Title: "Test", Type: "external"},
		{URL: "https://unique.com", Title: "Unique", Type: "external"},
	}

	if len(result) != len(expected) {
		t.Fatalf("Expected %d links after deduplication, got %d", len(expected), len(result))
	}

	for i, expectedLink := range expected {
		actualLink := result[i]

		if actualLink.URL != expectedLink.URL {
			t.Errorf("Link %d: Expected URL '%s', got '%s'", i, expectedLink.URL, actualLink.URL)
		}

		if actualLink.Title != expectedLink.Title {
			t.Errorf("Link %d: Expected title '%s', got '%s'", i, expectedLink.Title, actualLink.Title)
		}
	}
}

func TestLinkExtractionTransformer_ConfigurationOptions(t *testing.T) {
	transformer := NewLinkExtractionTransformer()

	tests := []struct {
		name     string
		config   map[string]interface{}
		content  string
		expected int
	}{
		{
			name: "Markdown links disabled",
			config: map[string]interface{}{
				"extract_markdown_links": false,
				"extract_plain_urls":     true,
			},
			content:  "Visit [Example](https://example.com) and https://test.org",
			expected: 1, // Only plain URL extracted
		},
		{
			name: "Plain URLs disabled",
			config: map[string]interface{}{
				"extract_markdown_links": true,
				"extract_plain_urls":     false,
			},
			content:  "Visit [Example](https://example.com) and https://test.org",
			expected: 1, // Only markdown link extracted
		},
		{
			name: "Both enabled",
			config: map[string]interface{}{
				"extract_markdown_links": true,
				"extract_plain_urls":     true,
			},
			content:  "Visit [Example](https://example.com) and https://test.org",
			expected: 2, // Both extracted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := transformer.Configure(tt.config)
			if err != nil {
				t.Fatalf("Failed to configure: %v", err)
			}

			result := transformer.ExtractLinks(tt.content)
			if len(result) != tt.expected {
				t.Errorf("Expected %d links, got %d", tt.expected, len(result))
			}
		})
	}
}

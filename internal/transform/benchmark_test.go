package transform

import (
	"fmt"
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

// BenchmarkContentCleanupTransformer tests the performance of HTML to Markdown conversion.
func BenchmarkContentCleanupTransformer(b *testing.B) {
	transformer := NewContentCleanupTransformer()
	transformer.Configure(map[string]interface{}{
		"html_to_markdown":        true,
		"strip_quoted_text":       true,
		"remove_extra_whitespace": true,
	})

	// Create test items with complex HTML content
	items := createBenchmarkHTMLItems(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := transformer.Transform(items)
		if err != nil {
			b.Fatalf("Transform failed: %v", err)
		}
	}
}

// BenchmarkLinkExtractionTransformer tests the performance of link extraction.
func BenchmarkLinkExtractionTransformer(b *testing.B) {
	transformer := NewLinkExtractionTransformer()
	transformer.Configure(map[string]interface{}{
		"extract_markdown_links": true,
		"extract_plain_urls":     true,
		"deduplicate_links":      true,
	})

	// Create test items with many links
	items := createBenchmarkLinkItems(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := transformer.Transform(items)
		if err != nil {
			b.Fatalf("Transform failed: %v", err)
		}
	}
}

// BenchmarkSignatureRemovalTransformer tests the performance of signature detection.
func BenchmarkSignatureRemovalTransformer(b *testing.B) {
	transformer := NewSignatureRemovalTransformer()
	transformer.Configure(map[string]interface{}{
		"max_signature_lines": 8,
		"trim_empty_lines":    true,
	})

	// Create test items with signatures
	items := createBenchmarkSignatureItems(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := transformer.Transform(items)
		if err != nil {
			b.Fatalf("Transform failed: %v", err)
		}
	}
}

// BenchmarkThreadGroupingTransformer tests the performance of thread consolidation.
func BenchmarkThreadGroupingTransformer(b *testing.B) {
	transformer := NewThreadGroupingTransformer()
	transformer.Configure(map[string]interface{}{
		"enabled": true,
		"mode":    "consolidated",
	})

	// Create test items with thread relationships
	items := createBenchmarkThreadItems(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := transformer.Transform(items)
		if err != nil {
			b.Fatalf("Transform failed: %v", err)
		}
	}
}

// BenchmarkTransformPipeline tests the performance of the complete pipeline.
func BenchmarkTransformPipeline(b *testing.B) {
	pipeline := NewPipeline()

	// Register all transformers
	pipeline.AddTransformer(NewContentCleanupTransformer())
	pipeline.AddTransformer(NewSignatureRemovalTransformer())
	pipeline.AddTransformer(NewLinkExtractionTransformer())
	pipeline.AddTransformer(NewThreadGroupingTransformer())

	// Configure pipeline
	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"content_cleanup", "signature_removal", "link_extraction", "thread_grouping"},
		ErrorStrategy: "log_and_continue",
		Transformers: map[string]map[string]interface{}{
			"content_cleanup": {
				"html_to_markdown":  true,
				"strip_quoted_text": true,
			},
			"signature_removal": {
				"max_signature_lines": 5,
			},
			"link_extraction": {
				"extract_plain_urls": true,
			},
			"thread_grouping": {
				"enabled": true,
				"mode":    "consolidated",
			},
		},
	}

	err := pipeline.Configure(config)
	if err != nil {
		b.Fatalf("Failed to configure pipeline: %v", err)
	}

	// Create realistic mixed content
	items := createBenchmarkRealisticItems(50)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := pipeline.Transform(items)
		if err != nil {
			b.Fatalf("Pipeline transform failed: %v", err)
		}
	}
}

// Helper functions to create benchmark data

func createBenchmarkHTMLItems(count int) []models.FullItem {
	items := make([]models.FullItem, count)

	complexHTML := `<h1>Important Document</h1>
<p>This is a <strong>complex</strong> HTML document with <em>various</em> formatting.</p>
<ul>
	<li>Item 1</li>
	<li>Item 2 with <a href="https://example.com">link</a></li>
	<li>Item 3</li>
</ul>
<blockquote>
	<p>This is a quoted section with important information.</p>
</blockquote>
<table>
	<tr><th>Column 1</th><th>Column 2</th></tr>
	<tr><td>Data 1</td><td>Data 2</td></tr>
</table>
<p>On Mon, Jan 1, 2024, sender@example.com wrote:</p>
<blockquote>Original message content here.</blockquote>`

	for i := 0; i < count; i++ {
		item := models.NewBasicItem(
			fmt.Sprintf("html_%d", i),
			fmt.Sprintf("HTML Document %d", i),
		)
		item.SetContent(complexHTML)
		item.SetSourceType("gmail")
		item.SetItemType("email")
		item.SetCreatedAt(time.Now())
		item.SetUpdatedAt(time.Now())
		item.SetMetadata(make(map[string]interface{}))
		items[i] = item
	}

	return items
}

func createBenchmarkLinkItems(count int) []models.FullItem {
	items := make([]models.FullItem, count)

	contentWithLinks := `Check out these resources:
- Documentation: https://docs.example.com/guide
- API Reference: https://api.example.com/v1/docs
- GitHub repo: https://github.com/company/project
- Meeting link: https://meet.google.com/abc-def-ghi
- Slack channel: https://company.slack.com/channels/general
- Design specs: [Figma Design](https://figma.com/file/123/design)
- Related issue: [Bug Report](https://jira.company.com/PROJ-123)
- Video tutorial: https://youtube.com/watch?v=abcd1234
- More info at https://company.com/wiki/project and https://confluence.com/spaces/team`

	for i := 0; i < count; i++ {
		item := models.NewBasicItem(
			fmt.Sprintf("links_%d", i),
			fmt.Sprintf("Document with Links %d", i),
		)
		item.SetContent(contentWithLinks)
		item.SetSourceType("gmail")
		item.SetItemType("email")
		item.SetCreatedAt(time.Now())
		item.SetUpdatedAt(time.Now())
		item.SetMetadata(make(map[string]interface{}))
		items[i] = item
	}

	return items
}

func createBenchmarkSignatureItems(count int) []models.FullItem {
	items := make([]models.FullItem, count)

	contentWithSignature := `Thank you for the update on the project status. 

I've reviewed the proposal and have a few questions:
1. What's the timeline for implementation?
2. Do we have sufficient resources?
3. How will this impact our current workload?

Let me know when you'd like to discuss this further.

--
Best regards,
John Smith
Senior Product Manager
Company Name
john.smith@company.com
555-123-4567
This email is confidential and intended solely for the addressee.`

	for i := 0; i < count; i++ {
		item := models.NewBasicItem(
			fmt.Sprintf("sig_%d", i),
			fmt.Sprintf("Email with Signature %d", i),
		)
		item.SetContent(contentWithSignature)
		item.SetSourceType("gmail")
		item.SetItemType("email")
		item.SetCreatedAt(time.Now())
		item.SetUpdatedAt(time.Now())
		item.SetMetadata(make(map[string]interface{}))
		items[i] = item
	}

	return items
}

func createBenchmarkThreadItems(count int) []models.FullItem {
	items := make([]models.FullItem, count)

	// Create items that form multiple threads (5 items per thread on average)
	for i := 0; i < count; i++ {
		threadID := fmt.Sprintf("thread_%d", i/5)

		item := models.NewBasicItem(
			fmt.Sprintf("thread_item_%d", i),
			fmt.Sprintf("Re: Thread Discussion %d", i/5),
		)
		item.SetContent(fmt.Sprintf("This is message %d in the thread.", i%5+1))
		item.SetSourceType("gmail")
		item.SetItemType("email")
		item.SetCreatedAt(time.Now().Add(time.Duration(i) * time.Minute))
		item.SetUpdatedAt(time.Now().Add(time.Duration(i) * time.Minute))
		item.SetMetadata(map[string]interface{}{
			"thread_id": threadID,
			"from":      fmt.Sprintf("user%d@company.com", i%3),
		})

		items[i] = item
	}

	return items
}

func createBenchmarkRealisticItems(count int) []models.FullItem {
	items := make([]models.FullItem, count)

	templates := []string{
		// Gmail-style email
		`<h2>Meeting Recap</h2>
<p>Thanks everyone for attending today's meeting. Here are the key points:</p>
<ul>
	<li>Project timeline updated</li>
	<li>Budget approved for Q2</li>
	<li>Next review: <a href="https://calendar.google.com/event/123">April 15th</a></li>
</ul>
<p>Documents: https://drive.google.com/folder/abc123</p>
<p>--</p>
<p>Best regards,<br>Team Lead<br>lead@company.com</p>`,

		// Calendar event description
		`Weekly standup meeting to discuss:
- Sprint progress
- Blockers and dependencies  
- Upcoming deadlines

Join via: https://meet.google.com/xyz-abc-def
Agenda: https://docs.google.com/document/d/456/edit`,

		// Drive document excerpt
		`# Project Requirements

## Overview
This document outlines the requirements for the new feature.

## Key Features
- User authentication
- Data synchronization  
- Mobile responsiveness

## References
- Design mockups: https://figma.com/file/789
- Technical specs: https://confluence.com/pages/technical-spec`,
	}

	for i := 0; i < count; i++ {
		template := templates[i%len(templates)]
		threadID := ""
		sourceType := "gmail"

		// Some items are part of threads
		if i%3 == 0 {
			threadID = fmt.Sprintf("thread_%d", i/3)
		}

		// Mix of source types
		switch i % 3 {
		case 0:
			sourceType = "gmail"
		case 1:
			sourceType = "google_calendar"
		case 2:
			sourceType = "google_drive"
		}

		metadata := map[string]interface{}{
			"source": sourceType,
		}

		if threadID != "" {
			metadata["thread_id"] = threadID
			metadata["from"] = fmt.Sprintf("user%d@company.com", i%5)
		}

		item := models.NewBasicItem(
			fmt.Sprintf("realistic_%d", i),
			fmt.Sprintf("Realistic Item %d", i),
		)
		item.SetContent(template)
		item.SetSourceType(sourceType)
		item.SetItemType("email")
		item.SetCreatedAt(time.Now().Add(time.Duration(i) * time.Hour))
		item.SetUpdatedAt(time.Now().Add(time.Duration(i) * time.Hour))
		item.SetMetadata(metadata)
		items[i] = item
	}

	return items
}

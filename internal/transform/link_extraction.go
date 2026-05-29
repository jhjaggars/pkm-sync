package transform

import (
	"net/url"
	"regexp"
	"strings"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// LinkExtractionTransformer extracts URLs from content and populates the Links field.
// Extracted from Gmail's ContentProcessor.ExtractLinks to be universally available.
type LinkExtractionTransformer struct {
	config map[string]interface{}

	// Pre-compiled regular expressions for performance
	urlRegex          *regexp.Regexp
	markdownLinkRegex *regexp.Regexp
}

func NewLinkExtractionTransformer() *LinkExtractionTransformer {
	return &LinkExtractionTransformer{
		config:            make(map[string]interface{}),
		urlRegex:          regexp.MustCompile(`https?://[^\s<>"\\]+`),
		markdownLinkRegex: regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`),
	}
}

func (t *LinkExtractionTransformer) Name() string {
	return transformerNameLinkExtraction
}

func (t *LinkExtractionTransformer) Configure(config map[string]interface{}) error {
	t.config = config

	return nil
}

func (t *LinkExtractionTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	transformedItems := make([]models.FullItem, len(items))

	for i, item := range items {
		extractedLinks := t.ExtractLinks(item.GetContent())

		// Check if we found any new links
		if len(extractedLinks) > 0 || t.shouldAlwaysProcessLinks() {
			transformedItems[i] = t.createItemWithLinks(item, extractedLinks)
		} else {
			// No new links found, keep original
			transformedItems[i] = item
		}
	}

	return transformedItems, nil
}

// createItemWithLinks creates a copy of the item with extracted links merged.
func (t *LinkExtractionTransformer) createItemWithLinks(
	item models.FullItem, extractedLinks []models.Link,
) models.FullItem {
	if thread, isThread := models.AsThread(item); isThread {
		return t.createThreadWithLinks(thread, extractedLinks)
	}

	return t.createBasicItemWithLinks(item, extractedLinks)
}

// createThreadWithLinks creates a new thread with merged links.
func (t *LinkExtractionTransformer) createThreadWithLinks(
	thread *models.Thread, extractedLinks []models.Link,
) models.FullItem {
	newThread := models.NewThread(thread.GetID(), thread.GetTitle())
	newThread.SetContent(thread.GetContent())
	newThread.SetSourceType(thread.GetSourceType())
	newThread.SetItemType(thread.GetItemType())
	newThread.SetCreatedAt(thread.GetCreatedAt())
	newThread.SetUpdatedAt(thread.GetUpdatedAt())
	newThread.SetTags(thread.GetTags())
	newThread.SetAttachments(thread.GetAttachments())
	newThread.SetMetadata(thread.GetMetadata())

	// Merge with existing links if any
	if len(thread.GetLinks()) > 0 {
		newThread.SetLinks(t.mergeLinks(thread.GetLinks(), extractedLinks))
	} else {
		newThread.SetLinks(extractedLinks)
	}

	// Copy messages
	for _, message := range thread.GetMessages() {
		newThread.AddMessage(message)
	}

	return newThread
}

// createBasicItemWithLinks creates a new basic item with merged links.
func (t *LinkExtractionTransformer) createBasicItemWithLinks(
	item models.FullItem, extractedLinks []models.Link,
) models.FullItem {
	newBasicItem := models.NewBasicItem(item.GetID(), item.GetTitle())
	newBasicItem.SetContent(item.GetContent())
	newBasicItem.SetSourceType(item.GetSourceType())
	newBasicItem.SetItemType(item.GetItemType())
	newBasicItem.SetCreatedAt(item.GetCreatedAt())
	newBasicItem.SetUpdatedAt(item.GetUpdatedAt())
	newBasicItem.SetTags(item.GetTags())
	newBasicItem.SetAttachments(item.GetAttachments())
	newBasicItem.SetMetadata(item.GetMetadata())

	// Merge with existing links if any
	if len(item.GetLinks()) > 0 {
		newBasicItem.SetLinks(t.mergeLinks(item.GetLinks(), extractedLinks))
	} else {
		newBasicItem.SetLinks(extractedLinks)
	}

	return newBasicItem
}

// ExtractLinks extracts URLs from content with enhanced detection.
// Extracted from Gmail's ContentProcessor.ExtractLinks.
func (t *LinkExtractionTransformer) ExtractLinks(content string) []models.Link {
	// Collect all URL matches with their positions to maintain order
	type urlMatch struct {
		url   string
		title string
		pos   int
	}

	allMatches := make([]urlMatch, 0)
	seenURL := make(map[string]bool)

	// Find markdown URLs first to prioritize them if enabled
	if t.shouldExtractMarkdownLinks() {
		markdownMatches := t.markdownLinkRegex.FindAllStringSubmatchIndex(content, -1)
		for _, match := range markdownMatches {
			if len(match) >= 6 {
				title := content[match[2]:match[3]]
				urlStr := content[match[4]:match[5]]
				urlStr = strings.TrimLeft(strings.TrimRight(urlStr, ".,!?;:)"), "(")

				if t.isValidURL(urlStr) && !seenURL[urlStr] {
					allMatches = append(allMatches, urlMatch{
						url:   urlStr,
						title: title,
						pos:   match[0],
					})
					seenURL[urlStr] = true
				}
			}
		}
	}

	// Find standalone URLs and add them if they haven't been seen in markdown links
	if t.shouldExtractPlainURLs() {
		urlMatches := t.urlRegex.FindAllStringIndex(content, -1)
		markdownMatches := t.markdownLinkRegex.FindAllStringSubmatchIndex(content, -1)

		for _, match := range urlMatches {
			urlStr := content[match[0]:match[1]]
			urlStr = strings.TrimLeft(strings.TrimRight(urlStr, ".,!?;:)"), "(")

			// Check if this match is inside a markdown link
			isInsideMarkdown := false

			for _, mdMatch := range markdownMatches {
				if match[0] >= mdMatch[0] && match[1] <= mdMatch[1] {
					isInsideMarkdown = true

					break
				}
			}

			if !isInsideMarkdown && t.isValidURL(urlStr) && !seenURL[urlStr] {
				allMatches = append(allMatches, urlMatch{
					url:   urlStr,
					title: "",
					pos:   match[0],
				})
				seenURL[urlStr] = true
			}
		}
	}

	// Sort by position to maintain order of appearance
	for i := 0; i < len(allMatches); i++ {
		for j := i + 1; j < len(allMatches); j++ {
			if allMatches[i].pos > allMatches[j].pos {
				allMatches[i], allMatches[j] = allMatches[j], allMatches[i]
			}
		}
	}

	// Convert to Link objects
	links := make([]models.Link, 0, len(allMatches))

	for _, match := range allMatches {
		linkType := linkTypeExternal

		// Determine link type based on URL
		if t.isInternalLink(match.url) {
			linkType = "internal"
		} else if t.isDocumentLink(match.url) {
			linkType = linkTypeDocument
		}

		links = append(links, models.Link{
			URL:   match.url,
			Title: match.title,
			Type:  linkType,
		})
	}

	// Deduplicate if enabled
	if t.shouldDeduplicateLinks() {
		links = t.deduplicateLinks(links)
	}

	return links
}

// mergeLinks combines existing links with newly extracted links.
func (t *LinkExtractionTransformer) mergeLinks(existing []models.Link, extracted []models.Link) []models.Link {
	// Create a map of existing URLs for fast lookup
	existingURLs := make(map[string]bool)
	for _, link := range existing {
		existingURLs[link.URL] = true
	}

	// Start with existing links
	merged := make([]models.Link, 0, len(existing)+len(extracted))
	merged = append(merged, existing...)

	// Add extracted links that don't already exist
	for _, link := range extracted {
		if !existingURLs[link.URL] {
			merged = append(merged, link)
		}
	}

	return merged
}

// deduplicateLinks removes duplicate URLs while preserving order.
func (t *LinkExtractionTransformer) deduplicateLinks(links []models.Link) []models.Link {
	seen := make(map[string]bool)
	deduplicated := make([]models.Link, 0, len(links))

	for _, link := range links {
		if !seen[link.URL] {
			deduplicated = append(deduplicated, link)
			seen[link.URL] = true
		}
	}

	return deduplicated
}

// isInternalLink checks if a URL appears to be an internal/relative link.
func (t *LinkExtractionTransformer) isInternalLink(url string) bool {
	// Simple heuristics for internal links
	return strings.HasPrefix(url, "/") ||
		strings.HasPrefix(url, "#") ||
		strings.HasPrefix(url, "./") ||
		strings.HasPrefix(url, "../")
}

// isDocumentLink checks if a URL points to a document.
func (t *LinkExtractionTransformer) isDocumentLink(url string) bool {
	// Common document extensions
	documentExts := []string{
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".txt", ".md", ".jpg", ".png", ".gif",
	}

	lowerURL := strings.ToLower(url)
	for _, ext := range documentExts {
		if strings.Contains(lowerURL, ext) {
			return true
		}
	}

	// Check for common document hosting domains
	documentDomains := []string{"docs.google.com", "drive.google.com", "dropbox.com", "onedrive.com"}
	for _, domain := range documentDomains {
		if strings.Contains(lowerURL, domain) {
			return true
		}
	}

	return false
}

// isValidURL validates a URL string to ensure it's properly formed and safe.
func (t *LinkExtractionTransformer) isValidURL(urlStr string) bool {
	// Parse the URL to validate its structure
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	// Must have a scheme (http/https)
	if parsedURL.Scheme != backendHTTP && parsedURL.Scheme != "https" {
		return false
	}

	// Must have a host
	if parsedURL.Host == "" {
		return false
	}

	// Additional security checks to prevent malicious URLs
	if strings.Contains(urlStr, "javascript:") ||
		strings.Contains(urlStr, "data:") ||
		strings.Contains(urlStr, "vbscript:") {
		return false
	}

	// Check for suspicious characters that might indicate injection attempts
	if strings.ContainsAny(urlStr, "<>\"'") {
		return false
	}

	// URL length sanity check (most browsers limit URLs to ~2000 chars)
	if len(urlStr) > 2048 {
		return false
	}

	return true
}

// Configuration helper methods

func (t *LinkExtractionTransformer) shouldExtractMarkdownLinks() bool {
	if val, exists := t.config["extract_markdown_links"]; exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}

	return true // Default: enabled
}

func (t *LinkExtractionTransformer) shouldExtractPlainURLs() bool {
	if val, exists := t.config["extract_plain_urls"]; exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}

	return true // Default: enabled
}

func (t *LinkExtractionTransformer) shouldDeduplicateLinks() bool {
	if val, exists := t.config["deduplicate_links"]; exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}

	return true // Default: enabled
}

func (t *LinkExtractionTransformer) shouldAlwaysProcessLinks() bool {
	if val, exists := t.config["always_process"]; exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}

	return false // Default: only process when links found
}

// Ensure interface compliance.
var _ interfaces.Transformer = (*LinkExtractionTransformer)(nil)

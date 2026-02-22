package transform

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"pkm-sync/internal/utils"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

const (
	// DefaultThreadSummaryLength is the default number of messages to include in thread summaries.
	DefaultThreadSummaryLength = 5
	threadModeConsolidated     = "consolidated"
	sourceTypeGmail            = "gmail"
)

// ThreadGroupingTransformer consolidates related items based on thread metadata.
// Extracted from Gmail's ThreadProcessor to be universally available.
type ThreadGroupingTransformer struct {
	config map[string]interface{}
}

// ThreadGroup represents a group of items that belong to the same thread.
type ThreadGroup struct {
	ThreadID     string         `json:"thread_id"`
	Subject      string         `json:"subject"`
	Items        []*models.Item `json:"items"`
	Participants []string       `json:"participants"`
	StartTime    time.Time      `json:"start_time"`
	EndTime      time.Time      `json:"end_time"`
	ItemCount    int            `json:"item_count"`
}

func NewThreadGroupingTransformer() *ThreadGroupingTransformer {
	return &ThreadGroupingTransformer{
		config: make(map[string]interface{}),
	}
}

func (t *ThreadGroupingTransformer) Name() string {
	return "thread_grouping"
}

func (t *ThreadGroupingTransformer) Configure(config map[string]interface{}) error {
	t.config = config

	return nil
}

func (t *ThreadGroupingTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	// Ensure we always return a non-nil slice
	if items == nil {
		return []models.FullItem{}, nil
	}

	if !t.isEnabled() {
		// Threading disabled - return individual items as-is
		return items, nil
	}

	// Convert to legacy items for internal processing
	legacyItems := make([]*models.Item, len(items))
	for i, item := range items {
		legacyItems[i] = models.AsItemStruct(item)
	}

	// Group items by thread ID
	threadGroups := t.groupItemsByThread(legacyItems)

	// Apply the configured thread processing mode
	mode := t.getThreadMode()

	var resultLegacyItems []*models.Item

	switch strings.ToLower(mode) {
	case threadModeConsolidated:
		resultLegacyItems = t.consolidateThreads(threadGroups)
	case "summary":
		resultLegacyItems = t.summarizeThreads(threadGroups)
	case "individual", "":
		// Default: return individual items
		resultLegacyItems = legacyItems
	default:
		return nil, fmt.Errorf("unknown thread mode: %s (supported: individual, consolidated, summary)", mode)
	}

	// Convert back to FullItem
	result := make([]models.FullItem, len(resultLegacyItems))
	for i, item := range resultLegacyItems {
		result[i] = models.AsFullItem(item)
	}

	return result, nil
}

// groupItemsByThread groups items by their thread ID.
func (t *ThreadGroupingTransformer) groupItemsByThread(items []*models.Item) map[string]*ThreadGroup {
	threadGroups := make(map[string]*ThreadGroup)

	for _, item := range items {
		if item == nil {
			continue // Skip nil items to prevent panic
		}

		threadID := t.extractThreadID(item)
		if threadID == "" {
			// No thread ID - treat as individual item
			threadID = item.ID
		}

		if group, exists := threadGroups[threadID]; exists {
			group.Items = append(group.Items, item)

			// Update time range
			if item.CreatedAt.Before(group.StartTime) {
				group.StartTime = item.CreatedAt
			}

			if item.CreatedAt.After(group.EndTime) {
				group.EndTime = item.CreatedAt
			}

			// Update participants
			t.updateParticipants(group, item)
		} else {
			// Create new thread group
			threadGroups[threadID] = &ThreadGroup{
				ThreadID:     threadID,
				Subject:      t.extractThreadSubject(item),
				Items:        []*models.Item{item},
				Participants: t.extractParticipants(item),
				StartTime:    item.CreatedAt,
				EndTime:      item.CreatedAt,
				ItemCount:    1, // Will be updated after processing
			}
		}
	}

	// Sort items within each thread by creation time and update item count
	for _, group := range threadGroups {
		sort.Slice(group.Items, func(i, j int) bool {
			return group.Items[i].CreatedAt.Before(group.Items[j].CreatedAt)
		})
		// Update item count to be thread-safe
		group.ItemCount = len(group.Items)
	}

	return threadGroups
}

// consolidateThreads creates one item per thread containing all items.
func (t *ThreadGroupingTransformer) consolidateThreads(threadGroups map[string]*ThreadGroup) []*models.Item {
	consolidatedItems := make([]*models.Item, 0, len(threadGroups))

	// Create a slice to sort by thread ID for consistent ordering
	groupKeys := make([]string, 0, len(threadGroups))
	for key := range threadGroups {
		groupKeys = append(groupKeys, key)
	}

	sort.Strings(groupKeys)

	for _, key := range groupKeys {
		group := threadGroups[key]

		if len(group.Items) == 1 {
			// Single item - keep as individual
			consolidatedItems = append(consolidatedItems, group.Items[0])

			continue
		}

		// Create consolidated thread item
		title := fmt.Sprintf("Thread_%s_%d-items",
			utils.SanitizeThreadSubject(group.Subject, group.ThreadID),
			group.ItemCount)

		consolidated := &models.Item{
			ID:          fmt.Sprintf("thread_%s", group.ThreadID),
			Title:       title,
			Content:     t.buildConsolidatedContent(group),
			SourceType:  t.inferSourceType(group.Items),
			ItemType:    t.inferConsolidatedItemType(group.Items),
			CreatedAt:   group.StartTime,
			UpdatedAt:   group.EndTime,
			Metadata:    t.buildThreadMetadata(group),
			Tags:        t.buildThreadTags(group),
			Links:       t.consolidateLinks(group.Items),
			Attachments: t.consolidateAttachments(group.Items),
		}

		consolidatedItems = append(consolidatedItems, consolidated)
	}

	return consolidatedItems
}

// summarizeThreads creates summary items for threads with key items.
func (t *ThreadGroupingTransformer) summarizeThreads(threadGroups map[string]*ThreadGroup) []*models.Item {
	summarizedItems := make([]*models.Item, 0, len(threadGroups))

	for _, group := range threadGroups {
		if len(group.Items) == 1 {
			// Single item - keep as individual
			summarizedItems = append(summarizedItems, group.Items[0])

			continue
		}

		// Create thread summary
		maxItems := t.getThreadSummaryLength()
		if maxItems <= 0 {
			maxItems = DefaultThreadSummaryLength
		}

		title := fmt.Sprintf("Thread-Summary_%s_%d-items",
			utils.SanitizeThreadSubject(group.Subject, group.ThreadID),
			group.ItemCount)

		summary := &models.Item{
			ID:          fmt.Sprintf("thread_summary_%s", group.ThreadID),
			Title:       title,
			Content:     t.buildThreadSummary(group, maxItems),
			SourceType:  t.inferSourceType(group.Items),
			ItemType:    t.inferSummaryItemType(group.Items),
			CreatedAt:   group.StartTime,
			UpdatedAt:   group.EndTime,
			Metadata:    t.buildThreadMetadata(group),
			Tags:        t.buildThreadTags(group),
			Links:       t.consolidateLinks(group.Items),
			Attachments: t.consolidateAttachments(group.Items),
		}

		summarizedItems = append(summarizedItems, summary)
	}

	return summarizedItems
}

// buildConsolidatedContent builds content for consolidated thread (all items).
func (t *ThreadGroupingTransformer) buildConsolidatedContent(group *ThreadGroup) string {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("# Thread: %s\n\n", group.Subject))
	content.WriteString(fmt.Sprintf("**Thread ID:** %s  \n", group.ThreadID))
	content.WriteString(fmt.Sprintf("**Items:** %d  \n", group.ItemCount))
	content.WriteString(fmt.Sprintf("**Participants:** %s  \n", strings.Join(group.Participants, ", ")))
	content.WriteString(fmt.Sprintf("**Duration:** %s to %s  \n\n",
		group.StartTime.Format("2006-01-02 15:04"),
		group.EndTime.Format("2006-01-02 15:04")))

	content.WriteString("---\n\n")

	for i, item := range group.Items {
		content.WriteString(fmt.Sprintf("## Item %d: %s\n\n", i+1, item.Title))
		content.WriteString(fmt.Sprintf("**Date:** %s  \n", item.CreatedAt.Format("2006-01-02 15:04:05")))

		// Add author/sender information if available
		if author := t.extractAuthor(item); author != "" {
			content.WriteString(fmt.Sprintf("**From:** %s  \n", author))
		}

		content.WriteString("\n")
		content.WriteString(item.Content)
		content.WriteString("\n\n---\n\n")
	}

	return content.String()
}

// buildThreadSummary builds content for thread summary (key items only).
func (t *ThreadGroupingTransformer) buildThreadSummary(group *ThreadGroup, maxItems int) string {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("# Thread Summary: %s\n\n", group.Subject))
	content.WriteString(fmt.Sprintf("**Thread ID:** %s  \n", group.ThreadID))
	content.WriteString(fmt.Sprintf("**Total Items:** %d  \n", group.ItemCount))
	content.WriteString(fmt.Sprintf("**Showing:** %d key items  \n", minInt(maxItems, len(group.Items))))
	content.WriteString(fmt.Sprintf("**Participants:** %s  \n", strings.Join(group.Participants, ", ")))
	content.WriteString(fmt.Sprintf("**Duration:** %s to %s  \n\n",
		group.StartTime.Format("2006-01-02 15:04"),
		group.EndTime.Format("2006-01-02 15:04")))

	content.WriteString("---\n\n")

	// Select key items to include in summary
	keyItems := t.selectKeyItems(group.Items, maxItems)

	for i, item := range keyItems {
		content.WriteString(fmt.Sprintf("## Key Item %d: %s\n\n", i+1, item.Title))
		content.WriteString(fmt.Sprintf("**Date:** %s  \n", item.CreatedAt.Format("2006-01-02 15:04:05")))

		if author := t.extractAuthor(item); author != "" {
			content.WriteString(fmt.Sprintf("**From:** %s  \n", author))
		}

		content.WriteString("\n")
		content.WriteString(item.Content)
		content.WriteString("\n\n---\n\n")
	}

	// Add summary of remaining items if any
	if len(group.Items) > maxItems {
		remaining := len(group.Items) - maxItems
		content.WriteString(fmt.Sprintf("*%d additional items not shown in summary*\n", remaining))
	}

	return content.String()
}

// selectKeyItems selects the most important items from a thread.
func (t *ThreadGroupingTransformer) selectKeyItems(items []*models.Item, maxItems int) []*models.Item {
	if len(items) <= maxItems {
		return items
	}

	var keyItems []*models.Item

	// Always include first item (thread starter)
	keyItems = append(keyItems, items[0])
	maxItems--

	// Always include last item (most recent)
	if maxItems > 0 && len(items) > 1 {
		keyItems = append(keyItems, items[len(items)-1])
		maxItems--
	}

	// Select additional items from the middle
	if maxItems > 0 && len(items) > 2 {
		additionalItems := t.selectAdditionalItems(items, maxItems)
		keyItems = append(keyItems, additionalItems...)

		// Sort key items by creation time
		sort.Slice(keyItems, func(i, j int) bool {
			return keyItems[i].CreatedAt.Before(keyItems[j].CreatedAt)
		})
	}

	return keyItems
}

func (t *ThreadGroupingTransformer) selectAdditionalItems(items []*models.Item, maxItems int) []*models.Item {
	candidates := items[1 : len(items)-1] // Exclude first and last

	// Score items based on importance criteria
	type scoredItem struct {
		item  *models.Item
		score int
	}

	scored := make([]scoredItem, 0, len(candidates))
	seenAuthors := make(map[string]bool)

	// Track authors from first and last item
	if author := t.extractAuthor(items[0]); author != "" {
		seenAuthors[author] = true
	}

	if author := t.extractAuthor(items[len(items)-1]); author != "" {
		seenAuthors[author] = true
	}

	for _, item := range candidates {
		score := 0

		// Different author bonus
		if author := t.extractAuthor(item); author != "" && !seenAuthors[author] {
			score += 3
		}

		// Content length bonus
		if len(item.Content) > 500 {
			score += 2
		}

		// Attachment bonus
		if len(item.Attachments) > 0 {
			score += 1
		}

		scored = append(scored, scoredItem{item, score})
	}

	// Sort by score (descending)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Add top-scored items
	var additionalItems []*models.Item
	for i := 0; i < minInt(maxItems, len(scored)); i++ {
		additionalItems = append(additionalItems, scored[i].item)
	}

	return additionalItems
}

// Helper functions

func (t *ThreadGroupingTransformer) extractThreadID(item *models.Item) string {
	if threadID, exists := item.Metadata["thread_id"].(string); exists {
		return threadID
	}

	return ""
}

func (t *ThreadGroupingTransformer) extractThreadSubject(item *models.Item) string {
	// Clean up subject line (remove Re:, Fwd:, etc.)
	subject := item.Title
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

func (t *ThreadGroupingTransformer) extractParticipants(item *models.Item) []string {
	var participants []string

	// Extract from metadata if available
	if from, exists := item.Metadata["from"]; exists {
		if author := t.extractEmailFromRecipient(from); author != "" {
			participants = append(participants, author)
		}
	}

	return participants
}

func (t *ThreadGroupingTransformer) updateParticipants(group *ThreadGroup, item *models.Item) {
	from, exists := item.Metadata["from"]
	if !exists {
		return
	}

	author := t.extractEmailFromRecipient(from)
	if author == "" {
		return
	}

	for _, p := range group.Participants {
		if p == author {
			return // Author already exists
		}
	}

	group.Participants = append(group.Participants, author)
}

func (t *ThreadGroupingTransformer) extractAuthor(item *models.Item) string {
	if from, exists := item.Metadata["from"]; exists {
		return t.extractEmailFromRecipient(from)
	}

	return ""
}

func (t *ThreadGroupingTransformer) extractEmailFromRecipient(recipient interface{}) string {
	if recipient == nil {
		return ""
	}

	switch r := recipient.(type) {
	case string:
		// Handle "Name <email@example.com>" format
		if strings.Contains(r, "<") && strings.Contains(r, ">") {
			start := strings.LastIndex(r, "<")

			end := strings.LastIndex(r, ">")
			if start != -1 && end != -1 && end > start {
				return r[start+1 : end]
			}
		}

		return r
	case map[string]interface{}:
		if r == nil {
			return ""
		}

		if email, ok := r["email"].(string); ok && email != "" {
			return email
		}

		if name, ok := r["name"].(string); ok && name != "" {
			return name
		}
	}

	return ""
}

func (t *ThreadGroupingTransformer) buildThreadMetadata(group *ThreadGroup) map[string]interface{} {
	metadata := make(map[string]interface{})

	if group == nil {
		return metadata
	}

	metadata["thread_id"] = group.ThreadID
	metadata["item_count"] = group.ItemCount
	metadata["participants"] = group.Participants
	metadata["start_time"] = group.StartTime
	metadata["end_time"] = group.EndTime

	// Safe duration calculation
	if !group.StartTime.IsZero() && !group.EndTime.IsZero() {
		metadata["duration_hours"] = group.EndTime.Sub(group.StartTime).Hours()
	} else {
		metadata["duration_hours"] = 0.0
	}

	return metadata
}

func (t *ThreadGroupingTransformer) buildThreadTags(group *ThreadGroup) []string {
	var tags []string

	// Infer source type from items
	sourceType := t.inferSourceType(group.Items)
	if sourceType != "" {
		tags = append(tags, sourceType)
	}

	tags = append(tags, "thread")

	if group.ItemCount > 5 {
		tags = append(tags, "long-thread")
	}

	if len(group.Participants) > 2 {
		tags = append(tags, "multi-participant")
	}

	return tags
}

func (t *ThreadGroupingTransformer) inferSourceType(items []*models.Item) string {
	if len(items) == 0 {
		return ""
	}
	// Use the source type from the first item
	return items[0].SourceType
}

func (t *ThreadGroupingTransformer) inferConsolidatedItemType(items []*models.Item) string {
	sourceType := t.inferSourceType(items)
	if sourceType == sourceTypeGmail {
		return "email_thread"
	}

	return "thread"
}

func (t *ThreadGroupingTransformer) inferSummaryItemType(items []*models.Item) string {
	sourceType := t.inferSourceType(items)
	if sourceType == sourceTypeGmail {
		return "email_thread_summary"
	}

	return "thread_summary"
}

// Configuration helper methods

func (t *ThreadGroupingTransformer) isEnabled() bool {
	if val, exists := t.config["enabled"]; exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}

	return true // Default: enabled
}

func (t *ThreadGroupingTransformer) getThreadMode() string {
	if val, exists := t.config["mode"]; exists {
		if mode, ok := val.(string); ok {
			return mode
		}
	}

	return threadModeConsolidated // Default: consolidated
}

func (t *ThreadGroupingTransformer) getThreadSummaryLength() int {
	if val, exists := t.config["max_thread_items"]; exists {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}

	return DefaultThreadSummaryLength
}

// consolidateLinks merges links from all items in a thread, removing duplicates.
func (t *ThreadGroupingTransformer) consolidateLinks(items []*models.Item) []models.Link {
	seenURLs := make(map[string]bool)

	var allLinks []models.Link

	for _, item := range items {
		for _, link := range item.Links {
			if !seenURLs[link.URL] {
				allLinks = append(allLinks, link)
				seenURLs[link.URL] = true
			}
		}
	}

	return allLinks
}

// consolidateAttachments merges attachments from all items in a thread, removing duplicates.
func (t *ThreadGroupingTransformer) consolidateAttachments(items []*models.Item) []models.Attachment {
	seenAttachments := make(map[string]bool)

	var allAttachments []models.Attachment

	for _, item := range items {
		for _, attachment := range item.Attachments {
			key := attachment.ID + "_" + attachment.Name
			if !seenAttachments[key] {
				allAttachments = append(allAttachments, attachment)
				seenAttachments[key] = true
			}
		}
	}

	return allAttachments
}

// minInt returns the smaller of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}

// Ensure interface compliance.
var _ interfaces.Transformer = (*ThreadGroupingTransformer)(nil)

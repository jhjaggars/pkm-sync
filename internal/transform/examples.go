package transform

import (
	"fmt"
	"log"
	"strings"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// NOTE: ContentCleanupTransformer is now implemented in content_cleanup.go
// with enhanced HTML processing capabilities extracted from Gmail processor.

// AutoTaggingTransformer automatically adds tags based on content.
type AutoTaggingTransformer struct {
	config map[string]interface{}
	rules  []TaggingRule
}

type TaggingRule struct {
	Pattern string   `json:"pattern" yaml:"pattern"`
	Tags    []string `json:"tags"    yaml:"tags"`
}

func NewAutoTaggingTransformer() *AutoTaggingTransformer {
	return &AutoTaggingTransformer{
		config: make(map[string]interface{}),
		rules:  make([]TaggingRule, 0),
	}
}

func (t *AutoTaggingTransformer) Name() string {
	return "auto_tagging"
}

func (t *AutoTaggingTransformer) Configure(config map[string]interface{}) error {
	t.config = config

	// Parse tagging rules from config
	rulesInterface, exists := config["rules"]
	if !exists {
		return nil
	}

	rulesSlice, ok := rulesInterface.([]interface{})
	if !ok {
		return nil
	}

	for _, ruleInterface := range rulesSlice {
		rule := t.parseTaggingRule(ruleInterface)
		if rule != nil {
			t.rules = append(t.rules, *rule)
		}
	}

	return nil
}

func (t *AutoTaggingTransformer) parseTaggingRule(ruleInterface interface{}) *TaggingRule {
	ruleMap, ok := ruleInterface.(map[string]interface{})
	if !ok {
		log.Printf("Warning: invalid tagging rule format, expected map[string]interface{}, got %T", ruleInterface)

		return nil
	}

	rule := TaggingRule{}

	pattern, hasPattern := ruleMap["pattern"]
	if !hasPattern {
		log.Printf("Warning: tagging rule missing required 'pattern' field")

		return nil
	}

	patternStr, ok := pattern.(string)
	if !ok {
		log.Printf("Warning: tagging rule 'pattern' must be a string, got %T", pattern)

		return nil
	}

	rule.Pattern = patternStr

	if tagsInterface, exists := ruleMap["tags"]; exists {
		tagsSlice, ok := tagsInterface.([]interface{})
		if !ok {
			log.Printf("Warning: tagging rule 'tags' must be an array, got %T", tagsInterface)

			return nil
		}

		rule.Tags = make([]string, 0, len(tagsSlice))

		for i, tagInterface := range tagsSlice {
			if tag, ok := tagInterface.(string); ok {
				rule.Tags = append(rule.Tags, tag)
			} else {
				log.Printf("Warning: tagging rule tag[%d] must be a string, got %T", i, tagInterface)
			}
		}
	}

	return &rule
}

func (t *AutoTaggingTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	transformedItems := make([]models.FullItem, len(items))

	for i, item := range items {
		// Apply tagging rules directly to ItemInterface
		newTags := t.applyTaggingRulesInterface(item)

		if len(newTags) > 0 {
			// Create a new item with the existing data plus new tags
			var transformedItem models.FullItem

			if thread, isThread := models.AsThread(item); isThread {
				// Handle Thread type
				newThread := models.NewThread(thread.GetID(), thread.GetTitle())
				newThread.SetContent(thread.GetContent())
				newThread.SetSourceType(thread.GetSourceType())
				newThread.SetItemType(thread.GetItemType())
				newThread.SetCreatedAt(thread.GetCreatedAt())
				newThread.SetUpdatedAt(thread.GetUpdatedAt())
				newThread.SetAttachments(thread.GetAttachments())
				newThread.SetMetadata(thread.GetMetadata())
				newThread.SetLinks(thread.GetLinks())

				// Copy messages
				for _, msg := range thread.GetMessages() {
					newThread.AddMessage(msg)
				}

				transformedItem = newThread
			} else {
				// Handle BasicItem type
				newBasicItem := models.NewBasicItem(item.GetID(), item.GetTitle())
				newBasicItem.SetContent(item.GetContent())
				newBasicItem.SetSourceType(item.GetSourceType())
				newBasicItem.SetItemType(item.GetItemType())
				newBasicItem.SetCreatedAt(item.GetCreatedAt())
				newBasicItem.SetUpdatedAt(item.GetUpdatedAt())
				newBasicItem.SetAttachments(item.GetAttachments())
				newBasicItem.SetMetadata(item.GetMetadata())
				newBasicItem.SetLinks(item.GetLinks())

				transformedItem = newBasicItem
			}

			// Add existing tags first, then new tags (avoiding duplicates)
			existingTags := make(map[string]bool)
			for _, tag := range item.GetTags() {
				existingTags[tag] = true
			}

			allTags := append([]string{}, item.GetTags()...)

			for _, newTag := range newTags {
				if !existingTags[newTag] {
					allTags = append(allTags, newTag)
				}
			}

			transformedItem.SetTags(allTags)

			transformedItems[i] = transformedItem
		} else {
			// No new tags, return original item
			transformedItems[i] = item
		}
	}

	return transformedItems, nil
}

func (t *AutoTaggingTransformer) applyTaggingRulesInterface(item models.FullItem) []string {
	var newTags []string

	searchText := strings.ToLower(item.GetTitle() + " " + item.GetContent())

	for _, rule := range t.rules {
		if strings.Contains(searchText, strings.ToLower(rule.Pattern)) {
			newTags = append(newTags, rule.Tags...)
		}
	}

	// Add source-based tags
	if item.GetSourceType() != "" {
		newTags = append(newTags, "source:"+item.GetSourceType())
	}

	if item.GetItemType() != "" {
		newTags = append(newTags, "type:"+item.GetItemType())
	}

	return newTags
}

// Keep the legacy method for backward compatibility (in case it's used elsewhere)

// FilterTransformer filters items based on criteria.
type FilterTransformer struct {
	config map[string]interface{}
}

func NewFilterTransformer() *FilterTransformer {
	return &FilterTransformer{
		config: make(map[string]interface{}),
	}
}

func (t *FilterTransformer) Name() string {
	return "filter"
}

func (t *FilterTransformer) Configure(config map[string]interface{}) error {
	t.config = config

	return nil
}

func (t *FilterTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	var filteredItems []models.FullItem

	minContentLength, err := t.getMinContentLength()
	if err != nil {
		return nil, err
	}

	excludeSourceTypes, err := t.getExcludeSourceTypes()
	if err != nil {
		return nil, err
	}

	requiredTags, err := t.getRequiredTags()
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		// Convert to struct for compatibility with existing filter logic
		legacyItem := models.AsItemStruct(item)
		if t.shouldIncludeItem(legacyItem, minContentLength, excludeSourceTypes, requiredTags) {
			filteredItems = append(filteredItems, item)
		}
	}

	return filteredItems, nil
}

func (t *FilterTransformer) getMinContentLength() (int, error) {
	if val, exists := t.config["min_content_length"]; exists {
		switch v := val.(type) {
		case int:
			return v, nil
		case float64:
			return int(v), nil
		default:
			return 0, fmt.Errorf("invalid type for min_content_length: expected int, got %T", v)
		}
	}

	return 0, nil
}

func (t *FilterTransformer) getExcludeSourceTypes() ([]string, error) {
	val, exists := t.config["exclude_source_types"]
	if !exists {
		return nil, nil
	}

	types, ok := val.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid type for exclude_source_types: expected array, got %T", val)
	}

	result := make([]string, 0, len(types))

	for i, typeInterface := range types {
		if sourceType, ok := typeInterface.(string); ok {
			result = append(result, sourceType)
		} else {
			return nil, fmt.Errorf("invalid type for exclude_source_types[%d]: expected string, got %T", i, typeInterface)
		}
	}

	return result, nil
}

func (t *FilterTransformer) getRequiredTags() ([]string, error) {
	val, exists := t.config["required_tags"]
	if !exists {
		return nil, nil
	}

	tags, ok := val.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid type for required_tags: expected array, got %T", val)
	}

	result := make([]string, 0, len(tags))

	for i, tagInterface := range tags {
		if tag, ok := tagInterface.(string); ok {
			result = append(result, tag)
		} else {
			return nil, fmt.Errorf("invalid type for required_tags[%d]: expected string, got %T", i, tagInterface)
		}
	}

	return result, nil
}

func (t *FilterTransformer) shouldIncludeItem(
	item *models.Item,
	minContentLength int,
	excludeSourceTypes []string,
	requiredTags []string,
) bool {
	// Check minimum content length
	if len(item.Content) < minContentLength {
		return false
	}

	// Check excluded source types
	for _, excludeType := range excludeSourceTypes {
		if item.SourceType == excludeType {
			return false
		}
	}

	// Check required tags
	if len(requiredTags) > 0 {
		itemTagMap := make(map[string]bool)
		for _, tag := range item.Tags {
			itemTagMap[tag] = true
		}

		for _, requiredTag := range requiredTags {
			if !itemTagMap[requiredTag] {
				return false
			}
		}
	}

	return true
}

// GetAllExampleTransformers returns all available transformers for registration.
// This includes all content-processing transformers (content_cleanup, link_extraction,
// signature_removal, thread_grouping) as well as auto_tagging and filter.
func GetAllExampleTransformers() []interfaces.Transformer {
	return GetAllContentProcessingTransformers()
}

// GetAllContentProcessingTransformers returns all content processing transformers.
// These include the enhanced transformers extracted from Gmail processing logic.
func GetAllContentProcessingTransformers() []interfaces.Transformer {
	return []interfaces.Transformer{
		NewContentCleanupTransformer(),   // Enhanced version with HTML processing from content_cleanup.go
		NewLinkExtractionTransformer(),   // URL extraction from link_extraction.go
		NewSignatureRemovalTransformer(), // Signature detection from signature_removal.go
		NewThreadGroupingTransformer(),   // Thread consolidation from thread_grouping.go
		NewAutoTaggingTransformer(),      // Existing example transformer
		NewFilterTransformer(),           // Existing example transformer
	}
}

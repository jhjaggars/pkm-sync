package transform

import (
	"fmt"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// NOTE: ContentCleanupTransformer is now implemented in content_cleanup.go
// with enhanced HTML processing capabilities extracted from Gmail processor.
// NOTE: AutoTaggingTransformer is now implemented in auto_tagging.go
// as EnhancedAutoTaggingTransformer with regex and priority support.
// NOTE: ContentFilterTransformer is now implemented in content_filter.go
// with include/exclude rules, keyword and regex matching.

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
		NewContentCleanupTransformer(),      // Enhanced HTML processing from content_cleanup.go
		NewLinkExtractionTransformer(),      // URL extraction from link_extraction.go
		NewSignatureRemovalTransformer(),    // Signature detection from signature_removal.go
		NewThreadGroupingTransformer(),      // Thread consolidation from thread_grouping.go
		NewEnhancedAutoTaggingTransformer(), // Pattern/regex tagging from auto_tagging.go
		NewContentFilterTransformer(),       // Include/exclude filtering from content_filter.go
		NewFilterTransformer(),              // Legacy filter transformer
		NewAIAnalysisTransformer(),          // AI-powered content analysis (disabled until configured)
	}
}

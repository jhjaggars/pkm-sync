package transform

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

const transformerNameAutoTagging = "auto_tagging"

// TagRule defines a single tagging rule with a pattern and the tags to apply.
// A rule matches when its pattern (string or regex) is found in the item's content or title.
// Priority controls evaluation order — lower numbers run first.
type TagRule struct {
	Pattern  string   `json:"pattern"  yaml:"pattern"`
	Regex    string   `json:"regex"    yaml:"regex"`
	Tags     []string `json:"tags"     yaml:"tags"`
	Priority int      `json:"priority" yaml:"priority"`

	// compiled regex (not serialized)
	compiledRegex *regexp.Regexp
}

// EnhancedAutoTaggingTransformer automatically assigns tags based on configurable rules.
// Rules are evaluated in ascending priority order (0 is highest).
// Both plain-string substring matching and regular-expression matching are supported.
// Source-type and item-type tags are optionally appended automatically.
type EnhancedAutoTaggingTransformer struct {
	config          map[string]interface{}
	rules           []TagRule
	addSourceTags   bool
	addItemTypeTags bool
}

// NewEnhancedAutoTaggingTransformer creates a new EnhancedAutoTaggingTransformer.
func NewEnhancedAutoTaggingTransformer() *EnhancedAutoTaggingTransformer {
	return &EnhancedAutoTaggingTransformer{
		config:          make(map[string]interface{}),
		rules:           make([]TagRule, 0),
		addSourceTags:   true,
		addItemTypeTags: true,
	}
}

// Name returns the transformer's registration name.
func (t *EnhancedAutoTaggingTransformer) Name() string {
	return transformerNameAutoTagging
}

// Configure parses the tagging configuration.
//
// Supported config keys:
//
//	rules              []map  list of tagging rules
//	add_source_tags    bool   prepend "source:<type>" tag (default: true)
//	add_item_type_tags bool   prepend "type:<type>" tag (default: true)
//
// Each rule map:
//
//	pattern  string   substring to match (case-insensitive)
//	regex    string   regular expression to match against title + content
//	tags     []string tags to apply when the rule matches
//	priority int      evaluation order; lower = higher priority (default: 0)
func (t *EnhancedAutoTaggingTransformer) Configure(config map[string]interface{}) error {
	t.config = config
	t.rules = make([]TagRule, 0)

	if v, ok := config["add_source_tags"]; ok {
		if b, ok := v.(bool); ok {
			t.addSourceTags = b
		}
	}

	if v, ok := config["add_item_type_tags"]; ok {
		if b, ok := v.(bool); ok {
			t.addItemTypeTags = b
		}
	}

	rulesRaw, ok := config["rules"]
	if !ok {
		return nil
	}

	rulesSlice, ok := rulesRaw.([]interface{})
	if !ok {
		return fmt.Errorf("auto_tagging: 'rules' must be a list, got %T", rulesRaw)
	}

	for i, item := range rulesSlice {
		m, ok := item.(map[string]interface{})
		if !ok {
			log.Printf("Warning: auto_tagging: rules[%d] must be a map, got %T — skipped", i, item)

			continue
		}

		rule, err := parseTagRule(m, i)
		if err != nil {
			return err
		}

		t.rules = append(t.rules, rule)
	}

	// Sort rules by priority (ascending — lower number = higher priority)
	sort.Slice(t.rules, func(i, j int) bool {
		return t.rules[i].Priority < t.rules[j].Priority
	})

	return nil
}

// parseTagRule builds a TagRule from a raw map.
func parseTagRule(m map[string]interface{}, idx int) (TagRule, error) {
	rule := TagRule{}

	if v, ok := m["pattern"]; ok {
		s, ok := v.(string)
		if !ok {
			return rule, fmt.Errorf("auto_tagging: rules[%d].pattern must be a string, got %T", idx, v)
		}

		rule.Pattern = s
	}

	if v, ok := m["regex"]; ok {
		s, ok := v.(string)
		if !ok {
			return rule, fmt.Errorf("auto_tagging: rules[%d].regex must be a string, got %T", idx, v)
		}

		re, err := regexp.Compile("(?i)" + s)
		if err != nil {
			return rule, fmt.Errorf("auto_tagging: rules[%d].regex invalid pattern %q: %w", idx, s, err)
		}

		rule.Regex = s
		rule.compiledRegex = re
	}

	if rule.Pattern == "" && rule.compiledRegex == nil {
		return rule, fmt.Errorf("auto_tagging: rules[%d] must have at least one of 'pattern' or 'regex'", idx)
	}

	if v, ok := m["tags"]; ok {
		strs, err := toStringSlice(v, fmt.Sprintf("rules[%d].tags", idx))
		if err != nil {
			return rule, fmt.Errorf("auto_tagging: %w", err)
		}

		rule.Tags = strs
	}

	if v, ok := m["priority"]; ok {
		switch n := v.(type) {
		case int:
			rule.Priority = n
		case float64:
			rule.Priority = int(n)
		default:
			log.Printf("Warning: auto_tagging: rules[%d].priority must be a number, got %T — using 0", idx, v)
		}
	}

	return rule, nil
}

// Transform applies tagging rules to each item and returns items with updated tags.
func (t *EnhancedAutoTaggingTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	result := make([]models.FullItem, len(items))

	for i, item := range items {
		newTags := t.computeTags(item)
		if len(newTags) == 0 {
			result[i] = item

			continue
		}

		result[i] = t.cloneWithTags(item, newTags)
	}

	return result, nil
}

// computeTags returns all new tags to apply to an item (deduped, excluding existing ones).
func (t *EnhancedAutoTaggingTransformer) computeTags(item models.FullItem) []string {
	existing := make(map[string]bool, len(item.GetTags()))
	for _, tag := range item.GetTags() {
		existing[tag] = true
	}

	var candidates []string

	searchText := strings.ToLower(item.GetTitle() + " " + item.GetContent())

	for _, rule := range t.rules {
		if t.ruleMatchesItem(rule, searchText, item) {
			for _, tag := range rule.Tags {
				if !existing[tag] {
					candidates = append(candidates, tag)
					existing[tag] = true // prevent duplicates from multiple rules
				}
			}
		}
	}

	if t.addSourceTags && item.GetSourceType() != "" {
		tag := "source:" + item.GetSourceType()
		if !existing[tag] {
			candidates = append(candidates, tag)
			existing[tag] = true
		}
	}

	if t.addItemTypeTags && item.GetItemType() != "" {
		tag := "type:" + item.GetItemType()
		if !existing[tag] {
			candidates = append(candidates, tag)
		}
	}

	return candidates
}

// ruleMatchesItem returns true if the rule's pattern or regex matches the item.
func (t *EnhancedAutoTaggingTransformer) ruleMatchesItem(rule TagRule, lowerText string, item models.FullItem) bool {
	if rule.Pattern != "" {
		if strings.Contains(lowerText, strings.ToLower(rule.Pattern)) {
			return true
		}
	}

	if rule.compiledRegex != nil {
		if rule.compiledRegex.MatchString(item.GetTitle() + " " + item.GetContent()) {
			return true
		}
	}

	return false
}

// cloneWithTags creates a copy of item with the additional tags merged in.
func (t *EnhancedAutoTaggingTransformer) cloneWithTags(item models.FullItem, newTags []string) models.FullItem {
	allTags := append(append([]string{}, item.GetTags()...), newTags...)

	if thread, isThread := models.AsThread(item); isThread {
		newThread := models.NewThread(thread.GetID(), thread.GetTitle())
		newThread.SetContent(thread.GetContent())
		newThread.SetSourceType(thread.GetSourceType())
		newThread.SetItemType(thread.GetItemType())
		newThread.SetCreatedAt(thread.GetCreatedAt())
		newThread.SetUpdatedAt(thread.GetUpdatedAt())
		newThread.SetAttachments(thread.GetAttachments())
		newThread.SetMetadata(thread.GetMetadata())
		newThread.SetLinks(thread.GetLinks())
		newThread.SetTags(allTags)

		for _, msg := range thread.GetMessages() {
			newThread.AddMessage(msg)
		}

		return newThread
	}

	clone := models.NewBasicItem(item.GetID(), item.GetTitle())
	clone.SetContent(item.GetContent())
	clone.SetSourceType(item.GetSourceType())
	clone.SetItemType(item.GetItemType())
	clone.SetCreatedAt(item.GetCreatedAt())
	clone.SetUpdatedAt(item.GetUpdatedAt())
	clone.SetAttachments(item.GetAttachments())
	clone.SetMetadata(item.GetMetadata())
	clone.SetLinks(item.GetLinks())
	clone.SetTags(allTags)

	return clone
}

// Ensure interface compliance.
var _ interfaces.Transformer = (*EnhancedAutoTaggingTransformer)(nil)

package transform

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

const transformerNameContentFilter = "content_filter"

// FilterRule represents a single filtering rule.
type FilterRule struct {
	ContentContains []string `json:"content_contains" yaml:"content_contains"`
	TitleContains   []string `json:"title_contains"   yaml:"title_contains"`
	ContentRegex    string   `json:"content_regex"    yaml:"content_regex"`
	TitleRegex      string   `json:"title_regex"      yaml:"title_regex"`
	SourceTypes     []string `json:"source_types"     yaml:"source_types"`

	// compiled regex (not serialized)
	contentRegex *regexp.Regexp
	titleRegex   *regexp.Regexp
}

// ContentFilterConfig holds the full filter transformer configuration.
type ContentFilterConfig struct {
	IncludeRules     []FilterRule `json:"include"            yaml:"include"`
	ExcludeRules     []FilterRule `json:"exclude"            yaml:"exclude"`
	MinContentLength int          `json:"min_content_length" yaml:"min_content_length"`
}

// ContentFilterTransformer filters items based on configurable include/exclude rules.
// Items must satisfy at least one include rule (when include rules are defined) and
// must not satisfy any exclude rule to pass through the filter.
type ContentFilterTransformer struct {
	config ContentFilterConfig
	raw    map[string]interface{}
}

// NewContentFilterTransformer creates a new ContentFilterTransformer.
func NewContentFilterTransformer() *ContentFilterTransformer {
	return &ContentFilterTransformer{
		raw: make(map[string]interface{}),
	}
}

// Name returns the transformer's name for pipeline registration.
func (t *ContentFilterTransformer) Name() string {
	return transformerNameContentFilter
}

// Configure parses and validates the transformer configuration.
func (t *ContentFilterTransformer) Configure(config map[string]interface{}) error {
	t.raw = config

	cfg := ContentFilterConfig{}

	if v, ok := config["min_content_length"]; ok {
		switch n := v.(type) {
		case int:
			cfg.MinContentLength = n
		case float64:
			cfg.MinContentLength = int(n)
		default:
			return fmt.Errorf("content_filter: min_content_length must be a number, got %T", v)
		}
	}

	if v, ok := config["include"]; ok {
		rules, err := parseFilterRules(v, "include")
		if err != nil {
			return err
		}

		cfg.IncludeRules = rules
	}

	if v, ok := config["exclude"]; ok {
		rules, err := parseFilterRules(v, "exclude")
		if err != nil {
			return err
		}

		cfg.ExcludeRules = rules
	}

	t.config = cfg

	return nil
}

// parseFilterRules converts raw config slice into FilterRule slice.
func parseFilterRules(raw interface{}, field string) ([]FilterRule, error) {
	slice, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("content_filter: '%s' must be a list, got %T", field, raw)
	}

	rules := make([]FilterRule, 0, len(slice))

	for i, item := range slice {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("content_filter: '%s[%d]' must be a map, got %T", field, i, item)
		}

		rule, err := parseFilterRule(m, field, i)
		if err != nil {
			return nil, err
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// parseFilterRule converts a single raw rule map into a FilterRule.
func parseFilterRule(m map[string]interface{}, field string, idx int) (FilterRule, error) {
	rule := FilterRule{}

	if v, ok := m["content_contains"]; ok {
		strs, err := toStringSlice(v, fmt.Sprintf("%s[%d].content_contains", field, idx))
		if err != nil {
			return rule, err
		}

		rule.ContentContains = strs
	}

	if v, ok := m["title_contains"]; ok {
		strs, err := toStringSlice(v, fmt.Sprintf("%s[%d].title_contains", field, idx))
		if err != nil {
			return rule, err
		}

		rule.TitleContains = strs
	}

	if v, ok := m["source_types"]; ok {
		strs, err := toStringSlice(v, fmt.Sprintf("%s[%d].source_types", field, idx))
		if err != nil {
			return rule, err
		}

		rule.SourceTypes = strs
	}

	if v, ok := m["content_regex"]; ok {
		s, ok := v.(string)
		if !ok {
			return rule, fmt.Errorf("content_filter: '%s[%d].content_regex' must be a string, got %T", field, idx, v)
		}

		re, err := regexp.Compile("(?i)" + s)
		if err != nil {
			return rule, fmt.Errorf("content_filter: '%s[%d].content_regex' invalid regex %q: %w", field, idx, s, err)
		}

		rule.ContentRegex = s
		rule.contentRegex = re
	}

	if v, ok := m["title_regex"]; ok {
		s, ok := v.(string)
		if !ok {
			return rule, fmt.Errorf("content_filter: '%s[%d].title_regex' must be a string, got %T", field, idx, v)
		}

		re, err := regexp.Compile("(?i)" + s)
		if err != nil {
			return rule, fmt.Errorf("content_filter: '%s[%d].title_regex' invalid regex %q: %w", field, idx, s, err)
		}

		rule.TitleRegex = s
		rule.titleRegex = re
	}

	return rule, nil
}

// toStringSlice converts an interface{} to []string.
func toStringSlice(v interface{}, path string) ([]string, error) {
	slice, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("content_filter: '%s' must be a list, got %T", path, v)
	}

	result := make([]string, 0, len(slice))

	for i, elem := range slice {
		s, ok := elem.(string)
		if !ok {
			return nil, fmt.Errorf("content_filter: '%s[%d]' must be a string, got %T", path, i, elem)
		}

		result = append(result, s)
	}

	return result, nil
}

// Transform filters items through include/exclude rules.
// Semantics:
//   - If include rules are defined, an item must match at least one.
//   - If exclude rules are defined, an item must not match any.
//   - If only exclude rules are defined, items that don't match any exclude rule pass.
//   - Min content length is applied independently before rule matching.
func (t *ContentFilterTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	result := make([]models.FullItem, 0, len(items))

	for _, item := range items {
		if t.shouldInclude(item) {
			result = append(result, item)
		} else {
			log.Printf("content_filter: dropped item %q (%s)", item.GetTitle(), item.GetID())
		}
	}

	return result, nil
}

// shouldInclude returns true if an item should pass through the filter.
func (t *ContentFilterTransformer) shouldInclude(item models.FullItem) bool {
	// Minimum content length check
	if t.config.MinContentLength > 0 && len(item.GetContent()) < t.config.MinContentLength {
		return false
	}

	// Exclude rules: if any exclude rule matches, drop the item
	for _, rule := range t.config.ExcludeRules {
		if t.ruleMatches(rule, item) {
			return false
		}
	}

	// Include rules: item must match at least one (if any are defined)
	if len(t.config.IncludeRules) > 0 {
		for _, rule := range t.config.IncludeRules {
			if t.ruleMatches(rule, item) {
				return true
			}
		}

		return false // no include rule matched
	}

	return true
}

// ruleMatches returns true when the item satisfies the rule.
// Between conditions (content_contains, title_contains, etc.) AND semantics apply:
// all defined conditions must match. Within a condition list (e.g. content_contains)
// OR semantics apply: at least one keyword must match.
func (t *ContentFilterTransformer) ruleMatches(rule FilterRule, item models.FullItem) bool {
	title := strings.ToLower(item.GetTitle())
	content := strings.ToLower(item.GetContent())

	// content_contains: at least one keyword must appear in content (OR semantics)
	if len(rule.ContentContains) > 0 {
		matched := false

		for _, kw := range rule.ContentContains {
			if strings.Contains(content, strings.ToLower(kw)) {
				matched = true

				break
			}
		}

		if !matched {
			return false
		}
	}

	// title_contains: at least one keyword must appear in title (OR semantics)
	if len(rule.TitleContains) > 0 {
		matched := false

		for _, kw := range rule.TitleContains {
			if strings.Contains(title, strings.ToLower(kw)) {
				matched = true

				break
			}
		}

		if !matched {
			return false
		}
	}

	// source_types: item source type must be in the list
	if len(rule.SourceTypes) > 0 {
		matched := false

		for _, st := range rule.SourceTypes {
			if strings.EqualFold(item.GetSourceType(), st) {
				matched = true

				break
			}
		}

		if !matched {
			return false
		}
	}

	// content_regex: must match content
	if rule.contentRegex != nil {
		if !rule.contentRegex.MatchString(item.GetContent()) {
			return false
		}
	}

	// title_regex: must match title
	if rule.titleRegex != nil {
		if !rule.titleRegex.MatchString(item.GetTitle()) {
			return false
		}
	}

	return true
}

// Ensure interface compliance.
var _ interfaces.Transformer = (*ContentFilterTransformer)(nil)

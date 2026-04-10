package transform

import (
	"testing"

	"pkm-sync/pkg/models"
)

func TestEnhancedAutoTaggingTransformer_Name(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()
	if tr.Name() != "auto_tagging" {
		t.Errorf("expected name 'auto_tagging', got %q", tr.Name())
	}
}

func TestEnhancedAutoTaggingTransformer_NoRules(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()
	if err := tr.Configure(map[string]interface{}{}); err != nil {
		t.Fatalf("configure error: %v", err)
	}

	item := models.NewBasicItem("1", "Hello")
	item.SetContent("some content")
	item.SetSourceType("gmail")
	item.SetItemType("email")

	result, err := tr.Transform([]models.FullItem{item})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}

	// Should have source and type tags by default
	tags := result[0].GetTags()
	if !containsTag(tags, "source:gmail") {
		t.Errorf("expected 'source:gmail' tag, got %v", tags)
	}

	if !containsTag(tags, "type:email") {
		t.Errorf("expected 'type:email' tag, got %v", tags)
	}
}

func TestEnhancedAutoTaggingTransformer_PatternRule(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"add_source_tags":    false,
		"add_item_type_tags": false,
		"rules": []interface{}{
			map[string]interface{}{
				"pattern": "meeting",
				"tags":    []interface{}{"meeting", "work"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	item := models.NewBasicItem("1", "Weekly Meeting")
	item.SetContent("agenda for the meeting")

	result, err := tr.Transform([]models.FullItem{item})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	tags := result[0].GetTags()
	if !containsTag(tags, "meeting") {
		t.Errorf("expected 'meeting' tag, got %v", tags)
	}

	if !containsTag(tags, "work") {
		t.Errorf("expected 'work' tag, got %v", tags)
	}
}

func TestEnhancedAutoTaggingTransformer_RegexRule(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"add_source_tags":    false,
		"add_item_type_tags": false,
		"rules": []interface{}{
			map[string]interface{}{
				"regex": `urgent|asap|deadline`,
				"tags":  []interface{}{"urgent", "high-priority"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	tests := []struct {
		name     string
		title    string
		content  string
		wantTags bool
	}{
		{"matches urgent", "Subject", "Please respond URGENT", true},
		{"matches asap", "ASAP needed", "needs attention", true},
		{"no match", "Normal email", "nothing special", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			item := models.NewBasicItem("1", tc.title)
			item.SetContent(tc.content)

			result, err := tr.Transform([]models.FullItem{item})
			if err != nil {
				t.Fatalf("transform error: %v", err)
			}

			tags := result[0].GetTags()
			gotUrgent := containsTag(tags, "urgent")

			if gotUrgent != tc.wantTags {
				t.Errorf("wantTags=%v but got tags %v", tc.wantTags, tags)
			}
		})
	}
}

func TestEnhancedAutoTaggingTransformer_PriorityOrdering(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"add_source_tags":    false,
		"add_item_type_tags": false,
		"rules": []interface{}{
			// Lower priority number runs first
			map[string]interface{}{
				"pattern":  "urgent",
				"tags":     []interface{}{"urgent"},
				"priority": 2,
			},
			map[string]interface{}{
				"pattern":  "meeting",
				"tags":     []interface{}{"meeting"},
				"priority": 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	item := models.NewBasicItem("1", "Urgent Meeting")
	item.SetContent("this is an urgent meeting request")

	result, err := tr.Transform([]models.FullItem{item})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	tags := result[0].GetTags()
	if !containsTag(tags, "meeting") || !containsTag(tags, "urgent") {
		t.Errorf("expected both 'meeting' and 'urgent' tags, got %v", tags)
	}
}

func TestEnhancedAutoTaggingTransformer_NoDuplicateTags(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"add_source_tags":    false,
		"add_item_type_tags": false,
		"rules": []interface{}{
			map[string]interface{}{
				"pattern": "meeting",
				"tags":    []interface{}{"work"},
			},
			map[string]interface{}{
				"pattern": "urgent",
				"tags":    []interface{}{"work", "urgent"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	item := models.NewBasicItem("1", "Urgent Meeting")
	item.SetContent("urgent meeting content")

	result, err := tr.Transform([]models.FullItem{item})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	tags := result[0].GetTags()
	count := 0

	for _, tag := range tags {
		if tag == "work" {
			count++
		}
	}

	if count != 1 {
		t.Errorf("expected 'work' tag exactly once, got %d occurrences in %v", count, tags)
	}
}

func TestEnhancedAutoTaggingTransformer_PreservesExistingTags(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"add_source_tags":    false,
		"add_item_type_tags": false,
		"rules": []interface{}{
			map[string]interface{}{
				"pattern": "meeting",
				"tags":    []interface{}{"meeting"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	item := models.NewBasicItem("1", "Meeting notes")
	item.SetContent("agenda for meeting")
	item.SetTags([]string{"existing-tag"})

	result, err := tr.Transform([]models.FullItem{item})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	tags := result[0].GetTags()
	if !containsTag(tags, "existing-tag") {
		t.Errorf("expected 'existing-tag' to be preserved, got %v", tags)
	}

	if !containsTag(tags, "meeting") {
		t.Errorf("expected 'meeting' tag, got %v", tags)
	}
}

func TestEnhancedAutoTaggingTransformer_DisableSourceTags(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"add_source_tags":    false,
		"add_item_type_tags": false,
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	item := models.NewBasicItem("1", "Email")
	item.SetSourceType("gmail")
	item.SetItemType("email")

	result, err := tr.Transform([]models.FullItem{item})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	tags := result[0].GetTags()
	if containsTag(tags, "source:gmail") || containsTag(tags, "type:email") {
		t.Errorf("source/type tags should be disabled, got %v", tags)
	}
}

func TestEnhancedAutoTaggingTransformer_InvalidRegex(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"regex": `[invalid`,
				"tags":  []interface{}{"tag"},
			},
		},
	})
	if err == nil {
		t.Error("expected configure error for invalid regex, got nil")
	}
}

func TestEnhancedAutoTaggingTransformer_RuleMissingPatternAndRegex(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"tags": []interface{}{"tag"},
			},
		},
	})
	if err == nil {
		t.Error("expected configure error for rule without pattern or regex")
	}
}

func TestEnhancedAutoTaggingTransformer_ThreadItem(t *testing.T) {
	tr := NewEnhancedAutoTaggingTransformer()

	err := tr.Configure(map[string]interface{}{
		"add_source_tags":    false,
		"add_item_type_tags": false,
		"rules": []interface{}{
			map[string]interface{}{
				"pattern": "meeting",
				"tags":    []interface{}{"meeting"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	thread := models.NewThread("t1", "Weekly Meeting")
	thread.SetContent("meeting notes")

	result, err := tr.Transform([]models.FullItem{thread})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	tags := result[0].GetTags()
	if !containsTag(tags, "meeting") {
		t.Errorf("expected 'meeting' tag on thread, got %v", tags)
	}
}

// containsTag checks whether a string is in a slice.
func containsTag(tags []string, target string) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}

	return false
}

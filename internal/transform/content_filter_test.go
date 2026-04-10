package transform

import (
	"testing"

	"pkm-sync/pkg/models"
)

func makeTestItem(id, title, content, sourceType string) models.FullItem {
	item := models.NewBasicItem(id, title)
	item.SetContent(content)
	item.SetSourceType(sourceType)

	return item
}

func TestContentFilterTransformer_Name(t *testing.T) {
	tr := NewContentFilterTransformer()
	if tr.Name() != "content_filter" {
		t.Errorf("expected name 'content_filter', got %q", tr.Name())
	}
}

func TestContentFilterTransformer_NoRules(t *testing.T) {
	tr := NewContentFilterTransformer()
	if err := tr.Configure(map[string]interface{}{}); err != nil {
		t.Fatalf("unexpected configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Hello", "world content", "gmail"),
		makeTestItem("2", "Another", "more stuff", "slack"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("unexpected transform error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestContentFilterTransformer_MinContentLength(t *testing.T) {
	tr := NewContentFilterTransformer()
	if err := tr.Configure(map[string]interface{}{
		"min_content_length": 10,
	}); err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Short", "hi", "gmail"),
		makeTestItem("2", "Long enough", "this content is long enough to pass", "gmail"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}

	if result[0].GetID() != "2" {
		t.Errorf("expected item id '2', got %q", result[0].GetID())
	}
}

func TestContentFilterTransformer_ExcludeRule_ContentContains(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"exclude": []interface{}{
			map[string]interface{}{
				"content_contains": []interface{}{"spam", "unsubscribe"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Normal", "normal content here", "gmail"),
		makeTestItem("2", "Spam", "click here to unsubscribe from our list", "gmail"),
		makeTestItem("3", "Also Spam", "this email has spam content", "gmail"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	// Item 2 has "unsubscribe", item 3 has "spam" — both excluded
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}

	if result[0].GetID() != "1" {
		t.Errorf("expected item '1' to pass, got %q", result[0].GetID())
	}
}

func TestContentFilterTransformer_IncludeRule_ContentContains(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"include": []interface{}{
			map[string]interface{}{
				"content_contains": []interface{}{"meeting"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Meeting notes", "attend the meeting tomorrow", "gmail"),
		makeTestItem("2", "Random", "nothing relevant here", "gmail"),
		makeTestItem("3", "Calendar", "weekly meeting scheduled", "calendar"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestContentFilterTransformer_IncludeAndExclude(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"include": []interface{}{
			map[string]interface{}{
				"content_contains": []interface{}{"meeting"},
			},
		},
		"exclude": []interface{}{
			map[string]interface{}{
				"content_contains": []interface{}{"canceled"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Meeting", "meeting scheduled for tomorrow", "gmail"),
		makeTestItem("2", "No meeting", "nothing here", "gmail"),
		makeTestItem("3", "Canceled", "meeting has been canceled", "gmail"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	// Only item 1 passes: has "meeting" (include) and no "canceled" (exclude)
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}

	if result[0].GetID() != "1" {
		t.Errorf("expected item '1', got %q", result[0].GetID())
	}
}

func TestContentFilterTransformer_SourceTypeFilter(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"include": []interface{}{
			map[string]interface{}{
				"source_types": []interface{}{"gmail"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Email", "from gmail", "gmail"),
		makeTestItem("2", "Slack", "from slack", "slack"),
		makeTestItem("3", "Drive", "from drive", "drive"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	if len(result) != 1 || result[0].GetID() != "1" {
		t.Errorf("expected only gmail item to pass, got %d items", len(result))
	}
}

func TestContentFilterTransformer_ContentRegex(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"exclude": []interface{}{
			map[string]interface{}{
				"content_regex": `unsubscribe|promotional`,
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Normal", "regular content here", "gmail"),
		makeTestItem("2", "Promo", "this is a Promotional offer", "gmail"),
		makeTestItem("3", "Unsub", "click here to Unsubscribe", "gmail"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	if len(result) != 1 || result[0].GetID() != "1" {
		t.Errorf("expected only 'Normal' item, got %d items", len(result))
	}
}

func TestContentFilterTransformer_InvalidRegex(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"exclude": []interface{}{
			map[string]interface{}{
				"content_regex": `[invalid`,
			},
		},
	})
	if err == nil {
		t.Error("expected configure error for invalid regex, got nil")
	}
}

func TestContentFilterTransformer_TitleContains(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"exclude": []interface{}{
			map[string]interface{}{
				"title_contains": []interface{}{"[SPAM]"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "[SPAM] Win a prize", "click here", "gmail"),
		makeTestItem("2", "Normal subject", "regular content", "gmail"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	if len(result) != 1 || result[0].GetID() != "2" {
		t.Errorf("expected only item '2', got %d items", len(result))
	}
}

func TestContentFilterTransformer_MultipleIncludeRules_AnyMatches(t *testing.T) {
	tr := NewContentFilterTransformer()

	err := tr.Configure(map[string]interface{}{
		"include": []interface{}{
			map[string]interface{}{
				"content_contains": []interface{}{"urgent"},
			},
			map[string]interface{}{
				"source_types": []interface{}{"calendar"},
			},
		},
	})
	if err != nil {
		t.Fatalf("configure error: %v", err)
	}

	items := []models.FullItem{
		makeTestItem("1", "Urgent", "this is urgent", "gmail"),
		makeTestItem("2", "Calendar", "team meeting", "calendar"),
		makeTestItem("3", "Normal", "nothing special", "gmail"),
	}

	result, err := tr.Transform(items)
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

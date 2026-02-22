package transform

import (
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

func TestContentCleanupTransformer(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	if transformer.Name() != "content_cleanup" {
		t.Errorf("Expected name 'content_cleanup', got '%s'", transformer.Name())
	}

	items := []models.FullItem{
		func() models.FullItem {
			item := models.NewBasicItem("1", "  Re: Test Subject  ")
			item.SetContent("  Test content\n\n\n\nwith extra newlines\r\n  ")

			return item
		}(),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result))
	}

	item := result[0]
	if item.GetTitle() != "Test Subject" {
		t.Errorf("Expected cleaned title 'Test Subject', got '%s'", item.GetTitle())
	}

	expectedContent := "Test content\n\nwith extra newlines"
	if item.GetContent() != expectedContent {
		t.Errorf("Expected cleaned content '%s', got '%s'", expectedContent, item.GetContent())
	}
}

func TestContentCleanupTransformerConfigure(t *testing.T) {
	transformer := NewContentCleanupTransformer()

	config := map[string]interface{}{
		"test_setting": "test_value",
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if transformer.config["test_setting"] != "test_value" {
		t.Error("Configuration not properly stored")
	}
}

func TestAutoTaggingTransformer(t *testing.T) {
	transformer := NewAutoTaggingTransformer()

	if transformer.Name() != "auto_tagging" {
		t.Errorf("Expected name 'auto_tagging', got '%s'", transformer.Name())
	}

	// Configure with tagging rules
	config := map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"pattern": "meeting",
				"tags":    []interface{}{"work", "meeting"},
			},
			map[string]interface{}{
				"pattern": "urgent",
				"tags":    []interface{}{"priority", "urgent"},
			},
		},
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	items := []models.FullItem{
		func() models.FullItem {
			item := models.NewBasicItem("1", "Urgent meeting tomorrow")
			item.SetContent("Important meeting discussion")
			item.SetSourceType("gmail")
			item.SetItemType("email")
			item.SetTags([]string{"existing"})

			return item
		}(),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result))
	}

	item := result[0]

	tagMap := make(map[string]bool)
	for _, tag := range item.GetTags() {
		tagMap[tag] = true
	}

	expectedTags := []string{"existing", "work", "meeting", "priority", "urgent", "source:gmail", "type:email"}
	for _, expectedTag := range expectedTags {
		if !tagMap[expectedTag] {
			t.Errorf("Missing expected tag: %s", expectedTag)
		}
	}
}

func TestAutoTaggingTransformerNoDuplicates(t *testing.T) {
	transformer := NewAutoTaggingTransformer()

	items := []models.FullItem{
		func() models.FullItem {
			item := models.NewBasicItem("1", "Test")
			item.SetContent("Test content")
			item.SetSourceType("gmail")
			item.SetItemType("email")
			item.SetTags([]string{"source:gmail"}) // Already has this tag

			return item
		}(),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	item := result[0]

	// Count occurrences of "source:gmail"
	count := 0

	for _, tag := range item.GetTags() {
		if tag == "source:gmail" {
			count++
		}
	}

	if count != 1 {
		t.Errorf("Expected 1 occurrence of 'source:gmail', got %d", count)
	}
}

func TestFilterTransformer(t *testing.T) {
	transformer := NewFilterTransformer()

	if transformer.Name() != "filter" {
		t.Errorf("Expected name 'filter', got '%s'", transformer.Name())
	}

	config := map[string]interface{}{
		"min_content_length":   10,
		"exclude_source_types": []interface{}{"spam"},
		"required_tags":        []interface{}{"important"},
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	items := []models.FullItem{
		func() models.FullItem {
			item := models.NewBasicItem("1", "Valid item")
			item.SetContent("This content is long enough")
			item.SetSourceType("gmail")
			item.SetTags([]string{"important"})

			return item
		}(),
		func() models.FullItem {
			item := models.NewBasicItem("2", "Too short")
			item.SetContent("Short")
			item.SetSourceType("gmail")
			item.SetTags([]string{"important"})

			return item
		}(),
		func() models.FullItem {
			item := models.NewBasicItem("3", "Spam item")
			item.SetContent("This content is long enough")
			item.SetSourceType("spam")
			item.SetTags([]string{"important"})

			return item
		}(),
		func() models.FullItem {
			item := models.NewBasicItem("4", "Missing tag")
			item.SetContent("This content is long enough")
			item.SetSourceType("gmail")
			item.SetTags([]string{})

			return item
		}(),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 filtered item, got %d", len(result))
	}

	if result[0].GetID() != "1" {
		t.Errorf("Expected item ID '1', got '%s'", result[0].GetID())
	}
}

func TestFilterTransformerNoFilters(t *testing.T) {
	transformer := NewFilterTransformer()
	transformer.Configure(make(map[string]interface{}))

	items := []models.FullItem{
		models.AsFullItem(createTestItemExample("1", "Test", "Content")),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result))
	}
}

func TestFilterTransformerInvalidConfig(t *testing.T) {
	transformer := NewFilterTransformer()
	config := map[string]interface{}{
		"min_content_length": "not a number",
	}
	transformer.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(createTestItemExample("1", "Test", "Content")),
	}

	_, err := transformer.Transform(items)
	if err == nil {
		t.Error("Expected an error for invalid config, but got nil")
	}
}

func TestGetAllExampleTransformers(t *testing.T) {
	// GetAllExampleTransformers now returns all 6 transformers (same as GetAllContentProcessingTransformers).
	transformers := GetAllExampleTransformers()
	if len(transformers) != 6 {
		t.Errorf("Expected 6 transformers, got %d", len(transformers))
	}
}

func TestGetAllContentProcessingTransformers(t *testing.T) {
	transformers := GetAllContentProcessingTransformers()
	if len(transformers) != 6 {
		t.Errorf("Expected 6 content processing transformers, got %d", len(transformers))
	}
}

func createTestItemExample(id, title, content string) *models.Item {
	return &models.Item{
		ID:        id,
		Title:     title,
		Content:   content,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Tags:      make([]string, 0),
		Metadata:  make(map[string]interface{}),
	}
}

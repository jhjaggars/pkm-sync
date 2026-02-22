package transform

import (
	"strings"
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

func TestThreadGroupingTransformer_Name(t *testing.T) {
	transformer := NewThreadGroupingTransformer()
	if transformer.Name() != "thread_grouping" {
		t.Errorf("Expected name 'thread_grouping', got '%s'", transformer.Name())
	}
}

func TestThreadGroupingTransformer_Configure(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	config := map[string]interface{}{
		"enabled":          true,
		"mode":             "consolidated",
		"max_thread_items": 3,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestThreadGroupingTransformer_Transform_Disabled(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	// Configure with threading disabled
	config := map[string]interface{}{
		"enabled": false,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Item 1", Content: "Content 1"}),
		models.AsFullItem(&models.Item{ID: "2", Title: "Item 2", Content: "Content 2"}),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should return items unchanged when disabled
	if len(result) != len(items) {
		t.Errorf("Expected %d items, got %d", len(items), len(result))
	}

	for i, expected := range items {
		if result[i].GetID() != expected.GetID() {
			t.Errorf("Item %d: Expected ID '%s', got '%s'", i, expected.GetID(), result[i].GetID())
		}
	}
}

func TestThreadGroupingTransformer_Transform_Individual(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	config := map[string]interface{}{
		"enabled": true,
		"mode":    "individual",
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Item 1", Content: "Content 1"}),
		models.AsFullItem(&models.Item{ID: "2", Title: "Item 2", Content: "Content 2"}),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should return items unchanged in individual mode
	if len(result) != len(items) {
		t.Errorf("Expected %d items, got %d", len(items), len(result))
	}
}

func TestThreadGroupingTransformer_Transform_Consolidated(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	config := map[string]interface{}{
		"enabled": true,
		"mode":    "consolidated",
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	now := time.Now()
	threadID := "thread123"

	items := []models.FullItem{
		models.AsFullItem(&models.Item{
			ID:        "1",
			Title:     "Re: Project Discussion",
			Content:   "First message",
			CreatedAt: now,
			Metadata: map[string]interface{}{
				"thread_id": threadID,
				"from":      "alice@example.com",
			},
		}),
		models.AsFullItem(&models.Item{
			ID:        "2",
			Title:     "Re: Project Discussion",
			Content:   "Second message",
			CreatedAt: now.Add(1 * time.Hour),
			Metadata: map[string]interface{}{
				"thread_id": threadID,
				"from":      "bob@example.com",
			},
		}),
		models.AsFullItem(&models.Item{
			ID:        "3",
			Title:     "Separate Item",
			Content:   "Individual message",
			CreatedAt: now,
			Metadata:  map[string]interface{}{},
		}),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should have consolidated thread + individual item = 2 items
	if len(result) != 2 {
		t.Fatalf("Expected 2 items (1 consolidated thread + 1 individual), got %d", len(result))
	}

	// Check individual item (comes first due to sorting by thread ID: "3" < "thread123")
	individual := result[0]
	if individual.GetID() != "3" {
		t.Errorf("Expected individual item ID '3', got '%s'", individual.GetID())
	}

	// Check consolidated thread (comes second due to sorting)
	consolidated := result[1]
	if !strings.Contains(consolidated.GetID(), "thread_") {
		t.Errorf("Expected consolidated ID to contain 'thread_', got '%s'", consolidated.GetID())
	}

	if !strings.Contains(consolidated.GetTitle(), "Thread_") {
		t.Errorf("Expected consolidated title to contain 'Thread_', got '%s'", consolidated.GetTitle())
	}

	if !strings.Contains(consolidated.GetContent(), "First message") {
		t.Errorf("Expected consolidated content to contain 'First message'")
	}

	if !strings.Contains(consolidated.GetContent(), "Second message") {
		t.Errorf("Expected consolidated content to contain 'Second message'")
	}
}

func TestThreadGroupingTransformer_Transform_Summary(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	config := map[string]interface{}{
		"enabled":          true,
		"mode":             "summary",
		"max_thread_items": 2,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	now := time.Now()
	threadID := "thread456"

	items := []models.FullItem{
		models.AsFullItem(&models.Item{
			ID:        "1",
			Title:     "Project Discussion",
			Content:   "First message with lots of content to make it important",
			CreatedAt: now,
			Metadata: map[string]interface{}{
				"thread_id": threadID,
				"from":      "alice@example.com",
			},
		}),
		models.AsFullItem(&models.Item{
			ID:        "2",
			Title:     "Re: Project Discussion",
			Content:   "Short reply",
			CreatedAt: now.Add(1 * time.Hour),
			Metadata: map[string]interface{}{
				"thread_id": threadID,
				"from":      "bob@example.com",
			},
		}),
		models.AsFullItem(&models.Item{
			ID:        "3",
			Title:     "Re: Project Discussion",
			Content:   "Final message",
			CreatedAt: now.Add(2 * time.Hour),
			Metadata: map[string]interface{}{
				"thread_id": threadID,
				"from":      "charlie@example.com",
			},
		}),
	}

	result, err := transformer.Transform(items)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 summary item, got %d", len(result))
	}

	summary := result[0]
	if !strings.Contains(summary.GetID(), "thread_summary_") {
		t.Errorf("Expected summary ID to contain 'thread_summary_', got '%s'", summary.GetID())
	}

	if !strings.Contains(summary.GetTitle(), "Thread-Summary_") {
		t.Errorf("Expected summary title to contain 'Thread-Summary_', got '%s'", summary.GetTitle())
	}

	if !strings.Contains(summary.GetContent(), "Key Item") {
		t.Errorf("Expected summary content to contain 'Key Item'")
	}
}

func TestThreadGroupingTransformer_extractThreadID(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	tests := []struct {
		item     *models.Item
		expected string
	}{
		{
			item: &models.Item{
				Metadata: map[string]interface{}{
					"thread_id": "thread123",
				},
			},
			expected: "thread123",
		},
		{
			item: &models.Item{
				Metadata: map[string]interface{}{
					"other_field": "value",
				},
			},
			expected: "",
		},
		{
			item: &models.Item{
				Metadata: map[string]interface{}{},
			},
			expected: "",
		},
		{
			item: &models.Item{
				Metadata: nil,
			},
			expected: "",
		},
	}

	for i, tt := range tests {
		result := transformer.extractThreadID(tt.item)
		if result != tt.expected {
			t.Errorf("Test %d: Expected thread ID '%s', got '%s'", i, tt.expected, result)
		}
	}
}

func TestThreadGroupingTransformer_extractThreadSubject(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	tests := []struct {
		item     *models.Item
		expected string
	}{
		{
			item: &models.Item{
				Title: "Re: Project Discussion",
			},
			expected: "Project Discussion",
		},
		{
			item: &models.Item{
				Title: "Fwd: Re: Important Meeting",
			},
			expected: "Important Meeting",
		},
		{
			item: &models.Item{
				Title: "Clean Subject",
			},
			expected: "Clean Subject",
		},
		{
			item: &models.Item{
				Title: "",
			},
			expected: "",
		},
	}

	for i, tt := range tests {
		result := transformer.extractThreadSubject(tt.item)
		if result != tt.expected {
			t.Errorf("Test %d: Expected subject '%s', got '%s'", i, tt.expected, result)
		}
	}
}

func TestThreadGroupingTransformer_extractEmailFromRecipient(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	tests := []struct {
		recipient interface{}
		expected  string
	}{
		{
			recipient: "alice@example.com",
			expected:  "alice@example.com",
		},
		{
			recipient: "Alice Smith <alice@example.com>",
			expected:  "alice@example.com",
		},
		{
			recipient: map[string]interface{}{
				"email": "bob@example.com",
				"name":  "Bob Jones",
			},
			expected: "bob@example.com",
		},
		{
			recipient: map[string]interface{}{
				"name": "Charlie Brown",
			},
			expected: "Charlie Brown",
		},
		{
			recipient: nil,
			expected:  "",
		},
		{
			recipient: 123, // Invalid type
			expected:  "",
		},
	}

	for i, tt := range tests {
		result := transformer.extractEmailFromRecipient(tt.recipient)
		if result != tt.expected {
			t.Errorf("Test %d: Expected '%s', got '%s'", i, tt.expected, result)
		}
	}
}

func TestThreadGroupingTransformer_selectKeyItems(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	now := time.Now()
	items := []*models.Item{
		{
			ID:        "1",
			Content:   "First message",
			CreatedAt: now,
			Metadata: map[string]interface{}{
				"from": "alice@example.com",
			},
		},
		{
			ID:        "2",
			Content:   "Middle message with lots of content to make it more important than others",
			CreatedAt: now.Add(1 * time.Hour),
			Metadata: map[string]interface{}{
				"from": "bob@example.com",
			},
		},
		{
			ID:        "3",
			Content:   "Another middle message",
			CreatedAt: now.Add(2 * time.Hour),
			Metadata: map[string]interface{}{
				"from": "charlie@example.com",
			},
		},
		{
			ID:        "4",
			Content:   "Last message",
			CreatedAt: now.Add(3 * time.Hour),
			Metadata: map[string]interface{}{
				"from": "david@example.com",
			},
		},
	}

	// Test selecting key items
	result := transformer.selectKeyItems(items, 3)

	if len(result) != 3 {
		t.Fatalf("Expected 3 key items, got %d", len(result))
	}

	// Should include first and last items
	if result[0].ID != "1" {
		t.Errorf("Expected first item to be ID '1', got '%s'", result[0].ID)
	}

	if result[len(result)-1].ID != "4" {
		t.Errorf("Expected last item to be ID '4', got '%s'", result[len(result)-1].ID)
	}

	// Test with max items greater than available
	resultAll := transformer.selectKeyItems(items, 10)
	if len(resultAll) != len(items) {
		t.Errorf("Expected all %d items, got %d", len(items), len(resultAll))
	}
}

func TestThreadGroupingTransformer_ConfigurationMethods(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	// Test default values
	if !transformer.isEnabled() {
		t.Error("Expected transformer to be enabled by default")
	}

	if transformer.getThreadMode() != "consolidated" {
		t.Errorf("Expected default mode 'consolidated', got '%s'", transformer.getThreadMode())
	}

	if transformer.getThreadSummaryLength() != DefaultThreadSummaryLength {
		t.Errorf("Expected default summary length %d, got %d", DefaultThreadSummaryLength, transformer.getThreadSummaryLength())
	}

	// Test configuration
	config := map[string]interface{}{
		"enabled":          false,
		"mode":             "summary",
		"max_thread_items": 10,
	}

	err := transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	if transformer.isEnabled() {
		t.Error("Expected transformer to be disabled after configuration")
	}

	if transformer.getThreadMode() != "summary" {
		t.Errorf("Expected mode 'summary', got '%s'", transformer.getThreadMode())
	}

	if transformer.getThreadSummaryLength() != 10 {
		t.Errorf("Expected summary length 10, got %d", transformer.getThreadSummaryLength())
	}
}

func TestThreadGroupingTransformer_groupItemsByThread(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	now := time.Now()
	items := []*models.Item{
		{
			ID:        "1",
			Title:     "Thread A Message 1",
			CreatedAt: now,
			Metadata: map[string]interface{}{
				"thread_id": "threadA",
				"from":      "alice@example.com",
			},
		},
		{
			ID:        "2",
			Title:     "Thread A Message 2",
			CreatedAt: now.Add(1 * time.Hour),
			Metadata: map[string]interface{}{
				"thread_id": "threadA",
				"from":      "bob@example.com",
			},
		},
		{
			ID:        "3",
			Title:     "Individual Message",
			CreatedAt: now,
			Metadata:  map[string]interface{}{},
		},
	}

	groups := transformer.groupItemsByThread(items)

	if len(groups) != 2 {
		t.Fatalf("Expected 2 thread groups, got %d", len(groups))
	}

	// Check thread A group
	threadA := groups["threadA"]
	if threadA == nil {
		t.Fatal("Thread A not found")
	}

	if len(threadA.Items) != 2 {
		t.Errorf("Expected 2 items in thread A, got %d", len(threadA.Items))
	}

	if threadA.ItemCount != 2 {
		t.Errorf("Expected item count 2, got %d", threadA.ItemCount)
	}

	if len(threadA.Participants) != 2 {
		t.Errorf("Expected 2 participants, got %d", len(threadA.Participants))
	}

	// Check individual item group
	individual := groups["3"] // Uses item ID as thread ID
	if individual == nil {
		t.Fatal("Individual item group not found")
	}

	if len(individual.Items) != 1 {
		t.Errorf("Expected 1 item in individual group, got %d", len(individual.Items))
	}
}

func TestThreadGroupingTransformer_ErrorHandling(t *testing.T) {
	transformer := NewThreadGroupingTransformer()

	// Test with nil items
	result, err := transformer.Transform(nil)
	if err != nil {
		t.Errorf("Expected no error with nil items, got: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected empty result with nil items, got %d items", len(result))
	}

	// Test with invalid mode
	config := map[string]interface{}{
		"mode": "invalid_mode",
	}

	err = transformer.Configure(config)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	items := []models.FullItem{models.AsFullItem(&models.Item{ID: "1", Title: "Test"})}

	_, err = transformer.Transform(items)
	if err == nil {
		t.Error("Expected error with invalid mode")
	}
}

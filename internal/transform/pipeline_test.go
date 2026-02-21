package transform

import (
	"fmt"
	"strings"
	"testing"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// MockTransformer for testing.
type MockTransformer struct {
	name          string
	shouldFail    bool
	config        map[string]interface{}
	TransformFunc func(items []models.FullItem) ([]models.FullItem, error)
}

// Compile-time check to ensure MockTransformer implements interfaces.Transformer.
var _ interfaces.Transformer = (*MockTransformer)(nil)

func (m *MockTransformer) Name() string {
	return m.name
}

func (m *MockTransformer) Configure(config map[string]interface{}) error {
	m.config = config

	return nil
}

func (m *MockTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	if m.TransformFunc != nil {
		return m.TransformFunc(items)
	}

	if m.shouldFail {
		return nil, fmt.Errorf("mock transformer failed")
	}

	// Add a tag to indicate this transformer ran
	transformedItems := make([]models.FullItem, len(items))

	for i, item := range items {
		// Create a copy with the new tag
		var newItem models.FullItem

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

			for _, msg := range thread.GetMessages() {
				newThread.AddMessage(msg)
			}

			newTags := append(thread.GetTags(), "transformed_by_"+m.name)
			newThread.SetTags(newTags)
			newItem = newThread
		} else {
			newBasicItem := models.NewBasicItem(item.GetID(), item.GetTitle())
			newBasicItem.SetContent(item.GetContent())
			newBasicItem.SetSourceType(item.GetSourceType())
			newBasicItem.SetItemType(item.GetItemType())
			newBasicItem.SetCreatedAt(item.GetCreatedAt())
			newBasicItem.SetUpdatedAt(item.GetUpdatedAt())
			newBasicItem.SetAttachments(item.GetAttachments())
			newBasicItem.SetMetadata(item.GetMetadata())
			newBasicItem.SetLinks(item.GetLinks())
			newTags := append(item.GetTags(), "transformed_by_"+m.name)
			newBasicItem.SetTags(newTags)
			newItem = newBasicItem
		}

		transformedItems[i] = newItem
	}

	return transformedItems, nil
}

func TestNewPipeline(t *testing.T) {
	pipeline := NewPipeline()

	if pipeline == nil {
		t.Fatal("NewPipeline() returned nil")
	}

	if len(pipeline.transformers) != 0 {
		t.Errorf("Expected empty transformers slice, got %d transformers", len(pipeline.transformers))
	}

	if len(pipeline.transformerRegistry) != 0 {
		t.Errorf("Expected empty transformer registry, got %d transformers", len(pipeline.transformerRegistry))
	}
}

func TestAddTransformer(t *testing.T) {
	pipeline := NewPipeline()
	transformer := &MockTransformer{name: "test_transformer"}

	err := pipeline.AddTransformer(transformer)
	if err != nil {
		t.Fatalf("AddTransformer() failed: %v", err)
	}

	if len(pipeline.transformerRegistry) != 1 {
		t.Errorf("Expected 1 transformer in registry, got %d", len(pipeline.transformerRegistry))
	}

	if pipeline.transformerRegistry["test_transformer"] != transformer {
		t.Error("Transformer not properly registered")
	}
}

func TestAddTransformerNil(t *testing.T) {
	pipeline := NewPipeline()

	err := pipeline.AddTransformer(nil)
	if err == nil {
		t.Error("Expected error when adding nil transformer")
	}
}

func TestAddTransformerEmptyName(t *testing.T) {
	pipeline := NewPipeline()
	transformer := &MockTransformer{name: ""}

	err := pipeline.AddTransformer(transformer)
	if err == nil {
		t.Error("Expected error when adding transformer with empty name")
	}
}

func TestConfigure(t *testing.T) {
	pipeline := NewPipeline()
	transformer1 := &MockTransformer{name: "transformer1"}
	transformer2 := &MockTransformer{name: "transformer2"}

	pipeline.AddTransformer(transformer1)
	pipeline.AddTransformer(transformer2)

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"transformer1", "transformer2"},
		ErrorStrategy: "fail_fast",
		Transformers: map[string]map[string]interface{}{
			"transformer1": {"setting1": "value1"},
			"transformer2": {"setting2": "value2"},
		},
	}

	err := pipeline.Configure(config)
	if err != nil {
		t.Fatalf("Configure() failed: %v", err)
	}

	if len(pipeline.transformers) != 2 {
		t.Errorf("Expected 2 transformers in pipeline, got %d", len(pipeline.transformers))
	}

	if pipeline.transformers[0].Name() != "transformer1" {
		t.Errorf("Expected first transformer to be 'transformer1', got '%s'", pipeline.transformers[0].Name())
	}

	if pipeline.transformers[1].Name() != "transformer2" {
		t.Errorf("Expected second transformer to be 'transformer2', got '%s'", pipeline.transformers[1].Name())
	}
}

func TestConfigureDisabled(t *testing.T) {
	pipeline := NewPipeline()
	transformer := &MockTransformer{name: "test_transformer"}
	pipeline.AddTransformer(transformer)

	config := models.TransformConfig{
		Enabled:       false,
		PipelineOrder: []string{"test_transformer"},
		ErrorStrategy: "fail_fast",
	}

	err := pipeline.Configure(config)
	if err != nil {
		t.Fatalf("Configure() failed: %v", err)
	}

	if len(pipeline.transformers) != 0 {
		t.Errorf("Expected 0 transformers when disabled, got %d", len(pipeline.transformers))
	}
}

func TestConfigureUnknownTransformer(t *testing.T) {
	pipeline := NewPipeline()

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"unknown_transformer"},
		ErrorStrategy: "fail_fast",
	}

	err := pipeline.Configure(config)
	if err == nil {
		t.Error("Expected error when configuring unknown transformer")
	}
}

func TestTransformDisabled(t *testing.T) {
	pipeline := NewPipeline()

	config := models.TransformConfig{
		Enabled: false,
	}
	pipeline.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Test Item"}),
	}

	result, err := pipeline.Transform(items)
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result))
	}

	if result[0].GetID() != items[0].GetID() {
		t.Error("Expected same item when disabled")
	}
}

func TestTransformSuccess(t *testing.T) {
	pipeline := NewPipeline()
	transformer1 := &MockTransformer{name: "transformer1"}
	transformer2 := &MockTransformer{name: "transformer2"}

	pipeline.AddTransformer(transformer1)
	pipeline.AddTransformer(transformer2)

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"transformer1", "transformer2"},
		ErrorStrategy: "fail_fast",
	}
	pipeline.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Test Item", Tags: []string{}}),
	}

	result, err := pipeline.Transform(items)
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result))
	}

	item := result[0]
	if len(item.GetTags()) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(item.GetTags()))
	}

	expectedTags := map[string]bool{
		"transformed_by_transformer1": true,
		"transformed_by_transformer2": true,
	}

	for _, tag := range item.GetTags() {
		if !expectedTags[tag] {
			t.Errorf("Unexpected tag: %s", tag)
		}
	}
}

func TestTransformFailFast(t *testing.T) {
	pipeline := NewPipeline()
	transformer1 := &MockTransformer{name: "transformer1"}
	transformer2 := &MockTransformer{name: "transformer2", shouldFail: true}

	pipeline.AddTransformer(transformer1)
	pipeline.AddTransformer(transformer2)

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"transformer1", "transformer2"},
		ErrorStrategy: "fail_fast",
	}
	pipeline.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Test Item", Tags: []string{}}),
	}

	_, err := pipeline.Transform(items)
	if err == nil {
		t.Error("Expected error with fail_fast strategy")
	}
}

func TestTransformLogAndContinue(t *testing.T) {
	pipeline := NewPipeline()
	transformer1 := &MockTransformer{name: "transformer1"}
	transformer2 := &MockTransformer{name: "transformer2", shouldFail: true}
	transformer3 := &MockTransformer{name: "transformer3"}

	pipeline.AddTransformer(transformer1)
	pipeline.AddTransformer(transformer2)
	pipeline.AddTransformer(transformer3)

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"transformer1", "transformer2", "transformer3"},
		ErrorStrategy: "log_and_continue",
	}
	pipeline.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Test Item", Tags: []string{}}),
	}

	result, err := pipeline.Transform(items)
	if err != nil {
		t.Fatalf("Transform() failed with log_and_continue: %v", err)
	}

	// Should have tags from transformer1 only, since transformer2 failed and transformer3 didn't run on the failed result
	item := result[0]

	// Count expected tags
	hasTransformer1Tag := false
	hasTransformer3Tag := false

	for _, tag := range item.GetTags() {
		if tag == "transformed_by_transformer1" {
			hasTransformer1Tag = true
		}

		if tag == "transformed_by_transformer3" {
			hasTransformer3Tag = true
		}

		if tag == "transformed_by_transformer2" {
			t.Errorf("Should not have tag from failed transformer: %s", tag)
		}
	}

	if !hasTransformer1Tag {
		t.Error("Missing tag from transformer1")
	}

	if !hasTransformer3Tag {
		t.Error("Missing tag from transformer3 (should run on result from transformer1)")
	}
}

func TestTransformLogAndContinueWithPartialSuccess(t *testing.T) {
	pipeline := NewPipeline()
	transformer1 := &MockTransformer{name: "transformer1"}
	failingTransformer := &MockTransformer{
		name: "failing_transformer",
		TransformFunc: func(items []models.FullItem) ([]models.FullItem, error) {
			// Partially succeeds, returns one item and an error
			return []models.FullItem{items[0]}, fmt.Errorf("partial failure")
		},
	}
	transformer3 := &MockTransformer{name: "transformer3"}

	pipeline.AddTransformer(transformer1)
	pipeline.AddTransformer(failingTransformer)
	pipeline.AddTransformer(transformer3)

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"transformer1", "failing_transformer", "transformer3"},
		ErrorStrategy: "log_and_continue",
	}
	pipeline.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Test Item", Tags: []string{}}),
		models.AsFullItem(&models.Item{ID: "2", Title: "Another Item", Tags: []string{}}),
	}

	result, err := pipeline.Transform(items)
	if err != nil {
		t.Fatalf("Transform() failed with log_and_continue: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 items after partial success, got %d", len(result))
	}

	// Check that transformer3 ran on the result from transformer1
	if len(result[0].GetTags()) != 2 {
		t.Errorf("Expected 2 tags on the item, got %d", len(result[0].GetTags()))
	}
}

func TestTransformSkipItem(t *testing.T) {
	pipeline := NewPipeline()
	transformer1 := &MockTransformer{name: "transformer1"}
	transformer2 := &MockTransformer{name: "transformer2", shouldFail: true}
	transformer3 := &MockTransformer{name: "transformer3"}

	pipeline.AddTransformer(transformer1)
	pipeline.AddTransformer(transformer2)
	pipeline.AddTransformer(transformer3)

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"transformer1", "transformer2", "transformer3"},
		ErrorStrategy: "skip_item",
	}
	pipeline.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Test Item", Tags: []string{}}),
	}

	result, err := pipeline.Transform(items)
	if err != nil {
		t.Fatalf("Transform() failed with skip_item: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 items after skip_item, got %d", len(result))
	}
}

func TestTransformPanicHandling(t *testing.T) {
	pipeline := NewPipeline()
	panickingTransformer := &MockTransformer{name: "panicker", shouldFail: true}
	// Overwrite the transform function to cause a panic
	panickingTransformer.TransformFunc = func(items []models.FullItem) ([]models.FullItem, error) {
		panic("test panic")
	}

	pipeline.AddTransformer(panickingTransformer)

	config := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"panicker"},
		ErrorStrategy: "fail_fast",
	}
	pipeline.Configure(config)

	items := []models.FullItem{
		models.AsFullItem(&models.Item{ID: "1", Title: "Test Item"}),
	}

	_, err := pipeline.Transform(items)
	if err == nil {
		t.Fatal("Expected an error from a panicking transformer, but got nil")
	}

	if !strings.Contains(err.Error(), "panic in transformer") {
		t.Errorf("Expected error message to contain 'panic in transformer', but got: %v", err)
	}
}

func TestGetRegisteredTransformers(t *testing.T) {
	pipeline := NewPipeline()
	transformer1 := &MockTransformer{name: "transformer1"}
	transformer2 := &MockTransformer{name: "transformer2"}

	pipeline.AddTransformer(transformer1)
	pipeline.AddTransformer(transformer2)

	names := pipeline.GetRegisteredTransformers()

	if len(names) != 2 {
		t.Errorf("Expected 2 transformer names, got %d", len(names))
	}

	nameMap := make(map[string]bool)
	for _, name := range names {
		nameMap[name] = true
	}

	if !nameMap["transformer1"] || !nameMap["transformer2"] {
		t.Error("Missing expected transformer names")
	}
}

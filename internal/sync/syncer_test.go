package sync

import (
	"context"
	"net/http"
	"testing"
	"time"

	"pkm-sync/internal/transform"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// MockSource is a mock implementation of the Source interface for testing.
type MockSource struct {
	itemsToReturn []models.FullItem
}

func (m *MockSource) Name() string {
	return "mock_source"
}

func (m *MockSource) Configure(config map[string]interface{}, client *http.Client) error {
	return nil
}

func (m *MockSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	return m.itemsToReturn, nil
}

func (m *MockSource) SupportsRealtime() bool {
	return false
}

// MockSink is a mock implementation of the Sink interface for testing.
type MockSink struct {
	writtenItems []models.FullItem
}

func (m *MockSink) Name() string {
	return "mock_sink"
}

func (m *MockSink) Write(_ context.Context, items []models.FullItem) error {
	m.writtenItems = items

	return nil
}

// Ensure MockSink implements Sink.
var _ interfaces.Sink = (*MockSink)(nil)

func TestMultiSyncerWithTransformerPipeline(t *testing.T) {
	// Create a mock source that returns two items
	source := &MockSource{
		itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "1", Title: "Item 1", Content: "short"}),
			models.AsFullItem(&models.Item{ID: "2", Title: "Item 2", Content: "this is a long content"}),
		},
	}

	// Create a mock sink
	sink := &MockSink{}

	// Create a transformer pipeline that filters out short items
	pipeline := transform.NewPipeline()
	filterTransformer := transform.NewFilterTransformer()
	filterTransformer.Configure(map[string]interface{}{
		"min_content_length": 10,
	})
	pipeline.AddTransformer(filterTransformer)

	transformCfg := models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"filter"},
		ErrorStrategy: "fail_fast",
		Transformers: map[string]map[string]interface{}{
			"filter": {"min_content_length": 10},
		},
	}

	// Create a multi-syncer with the pipeline
	ms := NewMultiSyncer(pipeline)

	// Perform the sync
	result, err := ms.SyncAll(
		context.Background(),
		[]SourceEntry{{Name: "mock_source", Src: source}},
		[]interfaces.Sink{sink},
		MultiSyncOptions{
			TransformCfg: transformCfg,
		},
	)
	if err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	// Verify the sink only received the long item
	if len(sink.writtenItems) != 1 {
		t.Errorf("Expected 1 item to be written, but got %d", len(sink.writtenItems))
	}

	if sink.writtenItems[0].GetID() != "2" {
		t.Errorf("Expected item with ID 2 to be written, but got %s", sink.writtenItems[0].GetID())
	}

	// Verify result tracking
	if len(result.SourceResults) != 1 {
		t.Errorf("Expected 1 source result, got %d", len(result.SourceResults))
	}

	if result.SourceResults[0].Name != "mock_source" {
		t.Errorf("Expected source name 'mock_source', got '%s'", result.SourceResults[0].Name)
	}
}

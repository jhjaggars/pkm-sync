package sync

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"pkm-sync/internal/transform"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// MockSource is a mock implementation of the Source interface for testing.
type MockSource struct {
	name          string
	itemsToReturn []models.FullItem
}

func (m *MockSource) Name() string {
	if m.name != "" {
		return m.name
	}

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

// FailingMockSource is a mock Source that always returns an error from Fetch.
type FailingMockSource struct {
	name string
	err  error
}

func (f *FailingMockSource) Name() string {
	if f.name != "" {
		return f.name
	}

	return "failing_source"
}

func (f *FailingMockSource) Configure(config map[string]interface{}, client *http.Client) error {
	return nil
}

func (f *FailingMockSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	return nil, f.err
}

func (f *FailingMockSource) SupportsRealtime() bool {
	return false
}

// MockSink is a mock implementation of the Sink interface for testing.
type MockSink struct {
	name         string
	writtenItems []models.FullItem
}

func (m *MockSink) Name() string {
	if m.name != "" {
		return m.name
	}

	return "mock_sink"
}

func (m *MockSink) Write(_ context.Context, items []models.FullItem) error {
	m.writtenItems = items

	return nil
}

// FailingMockSink is a mock Sink that always returns an error from Write.
type FailingMockSink struct {
	name string
	err  error
}

func (f *FailingMockSink) Name() string {
	if f.name != "" {
		return f.name
	}

	return "failing_sink"
}

func (f *FailingMockSink) Write(_ context.Context, items []models.FullItem) error {
	return f.err
}

// Ensure mock types implement their interfaces.
var _ interfaces.Sink = (*MockSink)(nil)
var _ interfaces.Sink = (*FailingMockSink)(nil)

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

func TestSyncAllConcurrentFetch(t *testing.T) {
	sources := []*MockSource{
		{name: "source_a", itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "a1", Title: "A1"}),
		}},
		{name: "source_b", itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "b1", Title: "B1"}),
			models.AsFullItem(&models.Item{ID: "b2", Title: "B2"}),
		}},
		{name: "source_c", itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "c1", Title: "C1"}),
		}},
	}

	sink := &MockSink{}
	ms := NewMultiSyncer(nil)

	entries := []SourceEntry{
		{Name: "source_a", Src: sources[0]},
		{Name: "source_b", Src: sources[1]},
		{Name: "source_c", Src: sources[2]},
	}

	result, err := ms.SyncAll(context.Background(), entries, []interfaces.Sink{sink}, MultiSyncOptions{})
	if err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	// All 4 items from all 3 sources should reach the sink.
	if len(sink.writtenItems) != 4 {
		t.Errorf("Expected 4 items, got %d", len(sink.writtenItems))
	}

	if len(result.SourceResults) != 3 {
		t.Errorf("Expected 3 source results, got %d", len(result.SourceResults))
	}
}

func TestSyncAllConcurrentSinks(t *testing.T) {
	source := &MockSource{
		name: "source_a",
		itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "1", Title: "Item 1"}),
			models.AsFullItem(&models.Item{ID: "2", Title: "Item 2"}),
		},
	}

	sink1 := &MockSink{name: "sink_1"}
	sink2 := &MockSink{name: "sink_2"}

	ms := NewMultiSyncer(nil)

	_, err := ms.SyncAll(
		context.Background(),
		[]SourceEntry{{Name: "source_a", Src: source}},
		[]interfaces.Sink{sink1, sink2},
		MultiSyncOptions{},
	)
	if err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	// Both sinks should have received all items.
	if len(sink1.writtenItems) != 2 {
		t.Errorf("sink_1: expected 2 items, got %d", len(sink1.writtenItems))
	}

	if len(sink2.writtenItems) != 2 {
		t.Errorf("sink_2: expected 2 items, got %d", len(sink2.writtenItems))
	}
}

func TestSyncAllSourceErrorNonFatal(t *testing.T) {
	fetchErr := errors.New("network timeout")
	failingSource := &FailingMockSource{name: "bad_source", err: fetchErr}
	goodSource := &MockSource{
		name: "good_source",
		itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "1", Title: "Good Item"}),
		},
	}

	sink := &MockSink{}
	ms := NewMultiSyncer(nil)

	result, err := ms.SyncAll(
		context.Background(),
		[]SourceEntry{
			{Name: "bad_source", Src: failingSource},
			{Name: "good_source", Src: goodSource},
		},
		[]interfaces.Sink{sink},
		MultiSyncOptions{},
	)
	if err != nil {
		t.Fatalf("SyncAll should succeed despite source error, got: %v", err)
	}

	// Good source's item should still reach the sink.
	if len(sink.writtenItems) != 1 {
		t.Errorf("Expected 1 item from good_source, got %d", len(sink.writtenItems))
	}

	// Both source results should be recorded.
	if len(result.SourceResults) != 2 {
		t.Errorf("Expected 2 source results, got %d", len(result.SourceResults))
	}

	// Find the bad source result and verify its error.
	var badResult *SourceResult

	for i := range result.SourceResults {
		if result.SourceResults[i].Name == "bad_source" {
			badResult = &result.SourceResults[i]

			break
		}
	}

	if badResult == nil {
		t.Fatal("Expected bad_source result to be recorded")
	}

	if !errors.Is(badResult.Err, fetchErr) {
		t.Errorf("Expected fetch error to be wrapped, got: %v", badResult.Err)
	}
}

func TestSyncAllSinkErrorFatal(t *testing.T) {
	source := &MockSource{
		name: "source_a",
		itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "1", Title: "Item 1"}),
		},
	}

	writeErr := errors.New("disk full")
	failingSink := &FailingMockSink{name: "bad_sink", err: writeErr}

	ms := NewMultiSyncer(nil)

	_, err := ms.SyncAll(
		context.Background(),
		[]SourceEntry{{Name: "source_a", Src: source}},
		[]interfaces.Sink{failingSink},
		MultiSyncOptions{},
	)
	if err == nil {
		t.Fatal("Expected error from failing sink, got nil")
	}

	if !strings.Contains(err.Error(), "bad_sink") {
		t.Errorf("Expected error to contain sink name 'bad_sink', got: %v", err)
	}
}

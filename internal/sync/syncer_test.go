package sync

import (
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

// MockTarget is a mock implementation of the Target interface for testing.
type MockTarget struct {
	exportedItems []models.FullItem
}

func (m *MockTarget) Name() string {
	return "mock_target"
}

func (m *MockTarget) Configure(config map[string]interface{}) error {
	return nil
}

func (m *MockTarget) Export(items []models.FullItem, outputDir string) error {
	m.exportedItems = items

	return nil
}

func (m *MockTarget) FormatFilename(title string) string {
	return title
}

func (m *MockTarget) GetFileExtension() string {
	return ".md"
}

func (m *MockTarget) FormatMetadata(metadata map[string]interface{}) string {
	return ""
}

func (m *MockTarget) Preview(items []models.FullItem, outputDir string) ([]*interfaces.FilePreview, error) {
	return nil, nil
}

func TestSyncerWithTransformerPipeline(t *testing.T) {
	// Create a mock source that returns two items
	source := &MockSource{
		itemsToReturn: []models.FullItem{
			models.AsFullItem(&models.Item{ID: "1", Title: "Item 1", Content: "short"}),
			models.AsFullItem(&models.Item{ID: "2", Title: "Item 2", Content: "this is a long content"}),
		},
	}

	// Create a mock target
	target := &MockTarget{}

	// Create a transformer pipeline that filters out short items
	pipeline := transform.NewPipeline()
	filterTransformer := transform.NewFilterTransformer()
	filterTransformer.Configure(map[string]interface{}{
		"min_content_length": 10,
	})
	pipeline.AddTransformer(filterTransformer)
	pipeline.Configure(models.TransformConfig{
		Enabled:       true,
		PipelineOrder: []string{"filter"},
		ErrorStrategy: "fail_fast",
		Transformers: map[string]map[string]interface{}{
			"filter": {"min_content_length": 10},
		},
	})

	// Create a syncer with the pipeline
	syncer := NewSyncerWithPipeline(pipeline)

	// Perform the sync
	err := syncer.Sync(source, target, interfaces.SyncOptions{})
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Check that the target only received the long item
	if len(target.exportedItems) != 1 {
		t.Errorf("Expected 1 item to be exported, but got %d", len(target.exportedItems))
	}

	if target.exportedItems[0].GetID() != "2" {
		t.Errorf("Expected item with ID 2 to be exported, but got %s", target.exportedItems[0].GetID())
	}
}

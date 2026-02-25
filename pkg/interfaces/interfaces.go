package interfaces

import (
	"context"
	"net/http"
	"time"

	"pkm-sync/pkg/models"
)

// Source represents any data source (Google Calendar, Slack, etc.)
// Returns FullItem interface for maximum compatibility across all components.
type Source interface {
	Name() string
	Configure(config map[string]interface{}, client *http.Client) error
	Fetch(since time.Time, limit int) ([]models.FullItem, error)
	SupportsRealtime() bool
}

// FilePreview represents what would happen to a file during sync.
type FilePreview struct {
	FilePath        string // Full path where file would be created
	Action          string // "create", "update", "skip"
	Content         string // Full content that would be written
	ExistingContent string // Current content if file exists
	Conflict        bool   // True if there would be a conflict
}

// Sink represents any destination that can receive items (file system, vector DB, etc.).
// This is a more general abstraction than the removed Target, which was file-specific.
type Sink interface {
	Name() string
	Write(ctx context.Context, items []models.FullItem) error
}

// Transformer represents a processing step that can modify items.
// Uses FullItem interface for maximum compatibility and access to all item capabilities.
type Transformer interface {
	Name() string
	Transform(items []models.FullItem) ([]models.FullItem, error)
	Configure(config map[string]interface{}) error
}

// ContentTransformer represents a transformer that only needs to access and modify core content.
// Useful for transformers that only need basic item properties.
type ContentTransformer interface {
	Name() string
	Transform(items []models.CoreItem) ([]models.CoreItem, error)
	Configure(config map[string]interface{}) error
}

// MetadataTransformer represents a transformer that works with item metadata and enrichment.
// Useful for transformers that add tags, metadata, or process attachments.
type MetadataTransformer interface {
	Name() string
	Transform(items []models.EnrichedItem) ([]models.EnrichedItem, error)
	Configure(config map[string]interface{}) error
}

// TransformPipeline manages a chain of transformers.
// Uses FullItem interface to maintain backward compatibility while supporting all transformer types.
type TransformPipeline interface {
	AddTransformer(transformer Transformer) error
	Transform(items []models.FullItem) ([]models.FullItem, error)
	Configure(config models.TransformConfig) error
}

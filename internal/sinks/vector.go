package sinks

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"pkm-sync/internal/embeddings"
	"pkm-sync/internal/vectorstore"
	"pkm-sync/pkg/models"
)

// VectorSinkConfig holds configuration for the VectorSink.
type VectorSinkConfig struct {
	DBPath        string
	Reindex       bool
	Delay         int // milliseconds between embeddings (or between batches when BatchSize > 1)
	MaxContentLen int // 0 = no limit
	BatchSize     int // documents per EmbedBatch call; 0 or 1 = single-embed mode
	EmbeddingsCfg models.EmbeddingsConfig
}

// VectorSink indexes items into a vector database for semantic search.
// It replaces the ad-hoc pipeline in cmd/index.go with a proper Sink implementation.
type VectorSink struct {
	store    *vectorstore.Store
	provider embeddings.Provider
	cfg      VectorSinkConfig
}

// NewVectorSink creates a VectorSink, opening the store and (optionally) the
// embedding provider. When no provider is configured (cfg.EmbeddingsCfg.Provider
// is empty), the sink operates in metadata-only mode: document rows including
// timestamps are always written, but vec_documents is not populated. This
// allows timestamp-based incremental sync inference even without embeddings.
// The caller is responsible for calling Close() when done.
func NewVectorSink(cfg VectorSinkConfig) (*VectorSink, error) {
	provider, err := embeddings.NewProvider(cfg.EmbeddingsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding provider: %w", err)
	}

	// provider may be nil when no embeddings are configured (metadata-only mode).
	if provider == nil {
		slog.Info("Vector store: running in metadata-only mode (no embedding provider configured)")
	}

	store, err := vectorstore.NewStore(cfg.DBPath, cfg.EmbeddingsCfg.Dimensions)
	if err != nil {
		if provider != nil {
			provider.Close()
		}

		return nil, fmt.Errorf("failed to open vector store at %s: %w", cfg.DBPath, err)
	}

	return &VectorSink{
		store:    store,
		provider: provider,
		cfg:      cfg,
	}, nil
}

// Name returns the sink name.
func (s *VectorSink) Name() string {
	return "vector_db"
}

// Write indexes items into the vector store.
// Items are grouped by (sourceName, threadID) and embedded together for context.
// Source name is extracted from "source:<name>" tags if present.
func (s *VectorSink) Write(ctx context.Context, items []models.FullItem) error {
	if len(items) == 0 {
		return nil
	}

	// Group items by source then by thread within each source
	bySource := groupBySource(items)

	totalIndexed := 0
	totalMetadataOnly := 0
	totalSkipped := 0
	totalFailed := 0

	for sourceName, sourceItems := range bySource {
		indexed, metadataOnly, skipped, failed, err := s.indexSource(ctx, sourceName, sourceItems)
		if err != nil {
			return fmt.Errorf("failed to index source %s: %w", sourceName, err)
		}

		totalIndexed += indexed
		totalMetadataOnly += metadataOnly
		totalSkipped += skipped
		totalFailed += failed
	}

	slog.Info("Vector indexing complete",
		"indexed", totalIndexed,
		"metadata_only", totalMetadataOnly,
		"skipped", totalSkipped,
		"failed", totalFailed)

	return nil
}

// pendingDoc holds a prepared document awaiting embedding and upsert.
type pendingDoc struct {
	threadID    string
	group       *itemGroup
	originalLen int
	content     string
	doc         vectorstore.Document
}

// indexSource indexes all items for a single source.
func (s *VectorSink) indexSource(
	ctx context.Context,
	sourceName string,
	items []models.FullItem,
) (indexed, metadataOnly, skipped, failed int, err error) {
	// Determine source type and pick the appropriate content builder
	var srcType string
	if len(items) > 0 {
		srcType = items[0].GetSourceType()
	}

	builder := getContentBuilder(srcType)

	// Group messages by thread/document
	groups := groupMessagesByThread(items, sourceName, builder)
	slog.Info("Source grouped", "source", sourceName, "items", len(items), "groups", len(groups))

	// Get already-indexed threads unless reindex is requested
	var indexedThreads map[string]bool

	if !s.cfg.Reindex {
		indexedThreads, err = s.store.GetIndexedThreadIDs(sourceName)
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("failed to get indexed threads: %w", err)
		}

		slog.Info("Source already indexed", "source", sourceName, "count", len(indexedThreads))
	} else {
		indexedThreads = make(map[string]bool)
	}

	// Build list of documents to process, skipping already-indexed ones.
	pending := make([]pendingDoc, 0, len(groups))

	for threadID, group := range groups {
		if indexedThreads[threadID] && !s.cfg.Reindex {
			skipped++

			continue
		}

		content := builder.buildContent(group)

		originalLen := len(content)
		if s.cfg.MaxContentLen > 0 && len(content) > s.cfg.MaxContentLen {
			content = content[:s.cfg.MaxContentLen] + "\n\n[Content truncated for indexing]"
		}

		metadata := builder.buildMetadata(group)

		var firstMsgID string
		if len(group.messages) > 0 {
			firstMsgID = group.messages[0].GetID()
		}

		doc := vectorstore.Document{
			SourceID:     firstMsgID,
			ThreadID:     threadID,
			Title:        group.subject,
			Content:      content,
			SourceType:   srcType,
			SourceName:   sourceName,
			MessageCount: len(group.messages),
			Metadata:     metadata,
			CreatedAt:    group.startTime,
			UpdatedAt:    group.endTime,
		}

		pending = append(pending, pendingDoc{
			threadID:    threadID,
			group:       group,
			originalLen: originalLen,
			content:     content,
			doc:         doc,
		})
	}

	batchSize := s.cfg.BatchSize
	if batchSize <= 1 || s.provider == nil {
		batchSize = 1
	}

	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}

		batch := pending[i:end]

		// Apply rate limiting between batches (not before the first batch).
		if s.provider != nil && s.cfg.Delay > 0 && i > 0 {
			time.Sleep(time.Duration(s.cfg.Delay) * time.Millisecond)
		}

		// Log progress every 10 documents processed.
		if i > 0 && i%10 == 0 {
			slog.Info("Indexing progress",
				"indexed", indexed,
				"metadata_only", metadataOnly,
				"skipped", skipped,
				"failed", failed)
		}

		// Generate embeddings for the batch.
		batchEmbeddings := s.embedBatch(ctx, batch, i)

		// Upsert each document in the batch.
		for j, p := range batch {
			var embedding []float32
			if j < len(batchEmbeddings) {
				embedding = batchEmbeddings[j]
			}

			if upsertErr := s.store.UpsertDocument(p.doc, embedding); upsertErr != nil {
				slog.Warn("Failed to index document", "thread_id", p.threadID, "error", upsertErr)

				failed++

				continue
			}

			if len(embedding) > 0 {
				indexed++
			} else {
				metadataOnly++
			}
		}
	}

	return indexed, metadataOnly, skipped, failed, nil
}

// embedBatch generates embeddings for a batch of pending documents.
// Returns a slice of embeddings (nil entries mean metadata-only for that doc).
func (s *VectorSink) embedBatch(ctx context.Context, batch []pendingDoc, batchIdx int) [][]float32 {
	if s.provider == nil {
		return make([][]float32, len(batch)) // metadata-only: no embeddings
	}

	if len(batch) == 1 {
		embedding, embedErr := s.provider.Embed(ctx, batch[0].content)
		if embedErr != nil {
			slog.Warn("Failed to embed document",
				"thread_id", batch[0].threadID,
				"subject", batch[0].group.subject,
				"chars", batch[0].originalLen,
				"error", embedErr)

			return [][]float32{nil}
		}

		return [][]float32{embedding}
	}

	texts := make([]string, len(batch))
	for j, p := range batch {
		texts[j] = p.content
	}

	embeddings, embedErr := s.provider.EmbedBatch(ctx, texts)
	if embedErr != nil {
		slog.Warn("Failed to batch embed",
			"batch_start", batchIdx,
			"batch_size", len(batch),
			"error", embedErr)

		return make([][]float32, len(batch)) // all nil — fall back to metadata-only
	}

	return embeddings
}

// Search performs a semantic search query against the vector store.
// It requires an embedding provider; returns an error in metadata-only mode.
func (s *VectorSink) Search(
	ctx context.Context, query string, limit int, filters vectorstore.SearchFilters,
) ([]vectorstore.SearchResult, error) {
	if s.provider == nil {
		return nil, fmt.Errorf("search requires an embedding provider; none configured (metadata-only mode)")
	}

	queryEmbedding, err := s.provider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	return s.store.Search(queryEmbedding, limit, filters)
}

// Stats returns statistics about the vector store.
func (s *VectorSink) Stats() (*vectorstore.StoreStats, error) {
	return s.store.Stats()
}

// Close releases resources held by the sink.
func (s *VectorSink) Close() error {
	var errs []string

	if s.provider != nil {
		if err := s.provider.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("provider: %v", err))
		}
	}

	if err := s.store.Close(); err != nil {
		errs = append(errs, fmt.Sprintf("store: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

// groupBySource groups items by their source name (extracted from "source:" tags).
func groupBySource(items []models.FullItem) map[string][]models.FullItem {
	result := make(map[string][]models.FullItem)

	for _, item := range items {
		sourceName := extractSourceName(item)
		result[sourceName] = append(result[sourceName], item)
	}

	return result
}

// extractSourceName extracts the source name from item tags or falls back to source type.
func extractSourceName(item models.FullItem) string {
	for _, tag := range item.GetTags() {
		if rest, ok := strings.CutPrefix(tag, "source:"); ok {
			return rest
		}
	}

	if st := item.GetSourceType(); st != "" {
		return st
	}

	return sourceTypeUnknown
}

// groupMessagesByThread groups items by thread ID using the builder for title cleaning.
// For non-threaded items (calendar, drive), the fallback to item.GetID() produces one group per item.
func groupMessagesByThread(items []models.FullItem, sourceName string, builder contentBuilder) map[string]*itemGroup {
	groups := make(map[string]*itemGroup)

	for _, item := range items {
		if item == nil {
			continue
		}

		threadID := extractThreadID(item)
		if threadID == "" {
			threadID = item.GetID()
		}

		if group, exists := groups[threadID]; exists {
			group.messages = append(group.messages, item)

			if item.GetCreatedAt().Before(group.startTime) {
				group.startTime = item.GetCreatedAt()
			}

			if item.GetCreatedAt().After(group.endTime) {
				group.endTime = item.GetCreatedAt()
			}
		} else {
			groups[threadID] = &itemGroup{
				threadID:   threadID,
				subject:    builder.cleanTitle(item),
				messages:   []models.FullItem{item},
				startTime:  item.GetCreatedAt(),
				endTime:    item.GetCreatedAt(),
				sourceName: sourceName,
			}
		}
	}

	for _, group := range groups {
		sort.Slice(group.messages, func(i, j int) bool {
			return group.messages[i].GetCreatedAt().Before(group.messages[j].GetCreatedAt())
		})
	}

	return groups
}

// extractThreadID extracts the thread ID from item metadata.
func extractThreadID(item models.FullItem) string {
	metadata := item.GetMetadata()
	if threadID, ok := metadata["thread_id"].(string); ok {
		return threadID
	}

	return ""
}

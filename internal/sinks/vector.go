package sinks

import (
	"context"
	"fmt"
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
	Delay         int // milliseconds between embeddings
	MaxContentLen int // 0 = no limit
	EmbeddingsCfg models.EmbeddingsConfig
}

// VectorSink indexes items into a vector database for semantic search.
// It replaces the ad-hoc pipeline in cmd/index.go with a proper Sink implementation.
type VectorSink struct {
	store    *vectorstore.Store
	provider embeddings.Provider
	cfg      VectorSinkConfig
}

// NewVectorSink creates a VectorSink, opening the store and provider.
// The caller is responsible for calling Close() when done.
func NewVectorSink(cfg VectorSinkConfig) (*VectorSink, error) {
	provider, err := embeddings.NewProvider(cfg.EmbeddingsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding provider: %w", err)
	}

	store, err := vectorstore.NewStore(cfg.DBPath, cfg.EmbeddingsCfg.Dimensions)
	if err != nil {
		provider.Close()

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
	totalSkipped := 0
	totalFailed := 0

	for sourceName, sourceItems := range bySource {
		indexed, skipped, failed, err := s.indexSource(ctx, sourceName, sourceItems)
		if err != nil {
			return fmt.Errorf("failed to index source %s: %w", sourceName, err)
		}

		totalIndexed += indexed
		totalSkipped += skipped
		totalFailed += failed
	}

	fmt.Printf("Vector indexing complete: %d indexed, %d skipped, %d failed\n",
		totalIndexed, totalSkipped, totalFailed)

	return nil
}

// indexSource indexes all items for a single source.
func (s *VectorSink) indexSource(
	ctx context.Context,
	sourceName string,
	items []models.FullItem,
) (indexed, skipped, failed int, err error) {
	// Determine source type and pick the appropriate content builder
	var srcType string
	if len(items) > 0 {
		srcType = items[0].GetSourceType()
	}

	builder := getContentBuilder(srcType)

	// Group messages by thread/document
	groups := groupMessagesByThread(items, sourceName, builder)
	fmt.Printf("Source %s: grouped %d items into %d groups\n", sourceName, len(items), len(groups))

	// Get already-indexed threads unless reindex is requested
	var indexedThreads map[string]bool

	if !s.cfg.Reindex {
		indexedThreads, err = s.store.GetIndexedThreadIDs(sourceName)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to get indexed threads: %w", err)
		}

		fmt.Printf("Source %s: already indexed: %d groups\n", sourceName, len(indexedThreads))
	} else {
		indexedThreads = make(map[string]bool)
	}

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

		// Rate limiting between embeddings
		if s.cfg.Delay > 0 && indexed > 0 {
			time.Sleep(time.Duration(s.cfg.Delay) * time.Millisecond)
		}

		// Log progress every 10 groups
		if (indexed+skipped+failed)%10 == 0 && (indexed+skipped+failed) > 0 {
			truncated := ""
			if len(content) < originalLen {
				truncated = fmt.Sprintf(" -> %d", len(content))
			}

			fmt.Printf("Progress: %d indexed, %d skipped, %d failed (current: %s, %d%s chars)\n",
				indexed, skipped, failed, group.subject, originalLen, truncated)
		}

		embedding, err := s.provider.Embed(ctx, content)
		if err != nil {
			fmt.Printf("Warning: Failed to embed group %s (%s, %d chars): %v\n",
				threadID, group.subject, originalLen, err)

			failed++

			continue
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

		if err := s.store.UpsertDocument(doc, embedding); err != nil {
			fmt.Printf("Warning: Failed to index group %s: %v\n", threadID, err)

			continue
		}

		indexed++
	}

	return indexed, skipped, failed, nil
}

// Stats returns statistics about the vector store.
func (s *VectorSink) Stats() (*vectorstore.StoreStats, error) {
	return s.store.Stats()
}

// Close releases resources held by the sink.
func (s *VectorSink) Close() error {
	var errs []string

	if err := s.provider.Close(); err != nil {
		errs = append(errs, fmt.Sprintf("provider: %v", err))
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

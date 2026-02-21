package sinks

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"pkm-sync/internal/embeddings"
	"pkm-sync/internal/vectorstore"
	"pkm-sync/pkg/models"

	mdconverter "github.com/JohannesKaufmann/html-to-markdown/v2"
)

var multipleNewlines = regexp.MustCompile(`\n\s*\n\s*\n`)

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

// threadGroup groups messages belonging to the same email thread.
type threadGroup struct {
	threadID     string
	subject      string
	messages     []models.FullItem
	participants []string
	startTime    time.Time
	endTime      time.Time
	sourceName   string
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
	// Group messages by thread
	threadGroups := groupMessagesByThread(items, sourceName)
	fmt.Printf("Source %s: grouped %d items into %d threads\n", sourceName, len(items), len(threadGroups))

	// Get already-indexed threads unless reindex is requested
	var indexedThreads map[string]bool

	if !s.cfg.Reindex {
		indexedThreads, err = s.store.GetIndexedThreadIDs(sourceName)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to get indexed threads: %w", err)
		}

		fmt.Printf("Source %s: already indexed: %d threads\n", sourceName, len(indexedThreads))
	} else {
		indexedThreads = make(map[string]bool)
	}

	for threadID, group := range threadGroups {
		if indexedThreads[threadID] && !s.cfg.Reindex {
			skipped++

			continue
		}

		content := buildThreadContent(group)

		originalLen := len(content)
		if s.cfg.MaxContentLen > 0 && len(content) > s.cfg.MaxContentLen {
			content = content[:s.cfg.MaxContentLen] + "\n\n[Content truncated for indexing]"
		}

		// Rate limiting between embeddings
		if s.cfg.Delay > 0 && indexed > 0 {
			time.Sleep(time.Duration(s.cfg.Delay) * time.Millisecond)
		}

		// Log progress every 10 threads
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
			fmt.Printf("Warning: Failed to embed thread %s (%s, %d chars): %v\n",
				threadID, group.subject, originalLen, err)

			failed++

			continue
		}

		metadata := buildThreadMetadata(group)

		var firstMsgID string
		if len(group.messages) > 0 {
			firstMsgID = group.messages[0].GetID()
		}

		doc := vectorstore.Document{
			SourceID:     firstMsgID,
			ThreadID:     threadID,
			Title:        group.subject,
			Content:      content,
			SourceType:   "gmail",
			SourceName:   sourceName,
			MessageCount: len(group.messages),
			Metadata:     metadata,
			CreatedAt:    group.startTime,
			UpdatedAt:    group.endTime,
		}

		if err := s.store.UpsertDocument(doc, embedding); err != nil {
			fmt.Printf("Warning: Failed to index thread %s: %v\n", threadID, err)

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
		if strings.HasPrefix(tag, "source:") {
			return strings.TrimPrefix(tag, "source:")
		}
	}

	if st := item.GetSourceType(); st != "" {
		return st
	}

	return "unknown"
}

// groupMessagesByThread groups messages by thread ID.
func groupMessagesByThread(items []models.FullItem, sourceName string) map[string]*threadGroup {
	groups := make(map[string]*threadGroup)

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

			updateGroupParticipants(group, item)
		} else {
			groups[threadID] = &threadGroup{
				threadID:     threadID,
				subject:      extractCleanSubject(item),
				messages:     []models.FullItem{item},
				participants: extractParticipants(item),
				startTime:    item.GetCreatedAt(),
				endTime:      item.GetCreatedAt(),
				sourceName:   sourceName,
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

// buildThreadContent constructs structured embedding text for a thread group.
func buildThreadContent(group *threadGroup) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Thread: %s\n\n", group.subject))

	for i, item := range group.messages {
		b.WriteString(fmt.Sprintf("--- Message %d (%s) ---\n", i+1, item.GetCreatedAt().Format("2006-01-02 15:04")))

		metadata := item.GetMetadata()

		if from, ok := metadata["from"].(string); ok && from != "" {
			b.WriteString(fmt.Sprintf("From: %s\n", from))
		}

		if to, ok := metadata["to"].(string); ok && to != "" {
			b.WriteString(fmt.Sprintf("To: %s\n", to))
		}

		if cc, ok := metadata["cc"].(string); ok && cc != "" {
			b.WriteString(fmt.Sprintf("Cc: %s\n", cc))
		}

		if bcc, ok := metadata["bcc"].(string); ok && bcc != "" {
			b.WriteString(fmt.Sprintf("Bcc: %s\n", bcc))
		}

		b.WriteString("\n")

		content := prepareContentForEmbedding(item.GetContent())
		if content != "" {
			b.WriteString(content)
		} else {
			b.WriteString("(no content)")
		}

		b.WriteString("\n\n")
	}

	return b.String()
}

// buildThreadMetadata constructs metadata map for vector store storage.
func buildThreadMetadata(group *threadGroup) map[string]interface{} {
	participantsMap := make(map[string]bool)
	for _, p := range group.participants {
		participantsMap[p] = true
	}

	participants := make([]string, 0, len(participantsMap))
	for p := range participantsMap {
		participants = append(participants, p)
	}

	messageIDs := make([]string, len(group.messages))
	for i, msg := range group.messages {
		messageIDs[i] = msg.GetID()
	}

	messages := make([]map[string]interface{}, len(group.messages))
	for i, msg := range group.messages {
		metadata := msg.GetMetadata()

		msgData := map[string]interface{}{
			"date":    msg.GetCreatedAt().Format(time.RFC3339),
			"subject": msg.GetTitle(),
		}

		if from, ok := metadata["from"].(string); ok {
			msgData["from"] = from
		}

		if to, ok := metadata["to"].(string); ok {
			msgData["to"] = to
		}

		if cc, ok := metadata["cc"].(string); ok {
			msgData["cc"] = cc
		}

		if bcc, ok := metadata["bcc"].(string); ok {
			msgData["bcc"] = bcc
		}

		messages[i] = msgData
	}

	return map[string]interface{}{
		"participants":  participants,
		"message_ids":   messageIDs,
		"message_count": len(group.messages),
		"date_range": map[string]string{
			"start": group.startTime.Format(time.RFC3339),
			"end":   group.endTime.Format(time.RFC3339),
		},
		"messages": messages,
	}
}

// prepareContentForEmbedding converts HTML to markdown and cleans content for embeddings.
func prepareContentForEmbedding(content string) string {
	if !strings.Contains(content, "<") || !strings.Contains(content, ">") {
		return content
	}

	markdown, err := mdconverter.ConvertString(content)
	if err != nil {
		return content
	}

	markdown = stripQuotedText(markdown)
	markdown = collapseWhitespace(markdown)

	return strings.TrimSpace(markdown)
}

// stripQuotedText removes email quoted text from content.
func stripQuotedText(content string) string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			break
		}

		if strings.HasPrefix(trimmed, "On ") && strings.Contains(trimmed, " wrote:") {
			break
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// collapseWhitespace reduces multiple consecutive newlines to two.
func collapseWhitespace(content string) string {
	return multipleNewlines.ReplaceAllString(content, "\n\n")
}

// extractThreadID extracts the thread ID from item metadata.
func extractThreadID(item models.FullItem) string {
	metadata := item.GetMetadata()
	if threadID, ok := metadata["thread_id"].(string); ok {
		return threadID
	}

	return ""
}

// extractCleanSubject removes Re:/Fwd: prefixes from a subject.
func extractCleanSubject(item models.FullItem) string {
	subject := strings.TrimSpace(item.GetTitle())
	prefixes := []string{"Re: ", "RE: ", "Fwd: ", "FWD: "}

	for changed := true; changed; {
		changed = false

		for _, prefix := range prefixes {
			if strings.HasPrefix(subject, prefix) {
				subject = strings.TrimPrefix(subject, prefix)
				changed = true
			}
		}
	}

	return subject
}

// extractParticipants extracts participant addresses from item metadata.
func extractParticipants(item models.FullItem) []string {
	var participants []string

	metadata := item.GetMetadata()

	for _, field := range []string{"from", "to", "cc", "bcc"} {
		if val, ok := metadata[field].(string); ok && val != "" {
			participants = append(participants, val)
		}
	}

	return participants
}

// updateGroupParticipants adds new participants from an item to the group.
func updateGroupParticipants(group *threadGroup, item models.FullItem) {
	metadata := item.GetMetadata()

	addIfNew := func(email string) {
		if email == "" {
			return
		}

		for _, p := range group.participants {
			if p == email {
				return
			}
		}

		group.participants = append(group.participants, email)
	}

	for _, field := range []string{"from", "to", "cc", "bcc"} {
		if val, ok := metadata[field].(string); ok {
			addIfNew(val)
		}
	}
}

package sinks

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"path/filepath"
	"time"

	"pkm-sync/internal/archive"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// RawMessageFetcher fetches raw RFC 5322 bytes for a Gmail message ID.
// The Gmail *Service type satisfies this interface.
type RawMessageFetcher interface {
	GetMessageRaw(messageID string) ([]byte, error)
}

// ArchiveSinkConfig holds configuration for ArchiveSink.
type ArchiveSinkConfig struct {
	EMLDir       string
	DBPath       string
	RequestDelay int // ms between raw fetches
	MaxPerSync   int // 0 = unlimited
}

// ArchiveSink implements interfaces.Sink by archiving Gmail messages as raw .eml files
// with a SQLite metadata index. It makes its own format=raw API calls using the
// Gmail message IDs present in the items.
type ArchiveSink struct {
	fetcher RawMessageFetcher
	store   *archive.Store
	cfg     ArchiveSinkConfig
}

// NewArchiveSink creates an ArchiveSink. The caller is responsible for calling Close().
func NewArchiveSink(cfg ArchiveSinkConfig, fetcher RawMessageFetcher) (*ArchiveSink, error) {
	if err := os.MkdirAll(cfg.EMLDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create EML directory %s: %w", cfg.EMLDir, err)
	}

	store, err := archive.NewStore(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive store at %s: %w", cfg.DBPath, err)
	}

	return &ArchiveSink{
		fetcher: fetcher,
		store:   store,
		cfg:     cfg,
	}, nil
}

// Name returns the sink name.
func (s *ArchiveSink) Name() string {
	return "archive"
}

// Write archives each eligible Gmail item. It fetches raw RFC 5322 bytes for new
// messages and writes them to .eml files, indexing metadata in SQLite.
//
// Phase 1: individual messages only (thread items are skipped).
func (s *ArchiveSink) Write(ctx context.Context, items []models.FullItem) error {
	if len(items) == 0 {
		return nil
	}

	// Group items by source name for efficient batch dedup.
	bySource := make(map[string][]models.FullItem)

	for _, item := range items {
		if !isGmailItem(item) {
			continue
		}

		// Phase 1: skip thread items (they have no single message ID to fetch).
		if isThreadItem(item) {
			continue
		}

		sourceName := extractSourceName(item)
		bySource[sourceName] = append(bySource[sourceName], item)
	}

	if len(bySource) == 0 {
		return nil
	}

	totalArchived := 0
	totalSkipped := 0
	totalFailed := 0

	for sourceName, sourceItems := range bySource {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		archived, skipped, failed, err := s.archiveSource(ctx, sourceName, sourceItems)
		if err != nil {
			return fmt.Errorf("archive failed for source %s: %w", sourceName, err)
		}

		totalArchived += archived
		totalSkipped += skipped
		totalFailed += failed

		if archived > 0 {
			if err := s.store.UpdateSyncState(sourceName, time.Now(), archived); err != nil {
				slog.Warn("Failed to update sync state", "source", sourceName, "error", err)
			}
		}
	}

	fmt.Printf("Archive complete: %d archived, %d skipped, %d failed\n",
		totalArchived, totalSkipped, totalFailed)

	return nil
}

// archiveSource archives all new messages for a single source.
func (s *ArchiveSink) archiveSource(
	ctx context.Context,
	sourceName string,
	items []models.FullItem,
) (archived, skipped, failed int, err error) {
	// Batch dedup: fetch all already-archived IDs for this source.
	archivedIDs, err := s.store.GetArchivedIDs(sourceName)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get archived IDs for %s: %w", sourceName, err)
	}

	slog.Info("Archive source", "source", sourceName,
		"items", len(items), "already_archived", len(archivedIDs))

	// Ensure per-source EML directory exists.
	sourceEMLDir := filepath.Join(s.cfg.EMLDir, sourceName)
	if err := os.MkdirAll(sourceEMLDir, 0755); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create EML directory %s: %w", sourceEMLDir, err)
	}

	for _, item := range items {
		if ctx.Err() != nil {
			return archived, skipped, failed, ctx.Err()
		}

		// Respect MaxPerSync limit.
		if s.cfg.MaxPerSync > 0 && archived >= s.cfg.MaxPerSync {
			slog.Info("Reached max_per_sync limit", "source", sourceName, "limit", s.cfg.MaxPerSync)

			skipped += len(items) - archived - skipped - failed

			break
		}

		gmailID := item.GetID()
		if gmailID == "" {
			slog.Warn("Skipping item with empty ID", "source", sourceName)

			failed++

			continue
		}

		if archivedIDs[gmailID] {
			skipped++

			continue
		}

		// Rate-limit between fetches.
		if s.cfg.RequestDelay > 0 && archived > 0 {
			time.Sleep(time.Duration(s.cfg.RequestDelay) * time.Millisecond)
		}

		emlPath := filepath.Join(sourceEMLDir, gmailID+".eml")

		if err := s.archiveMessage(item, gmailID, emlPath, sourceName); err != nil {
			slog.Warn("Failed to archive message",
				"gmail_id", gmailID,
				"source", sourceName,
				"error", err)

			failed++

			continue
		}

		archived++

		if archived%50 == 0 {
			slog.Info("Archive progress",
				"source", sourceName,
				"archived", archived,
				"skipped", skipped,
				"failed", failed)
		}
	}

	return archived, skipped, failed, nil
}

// archiveMessage fetches and archives a single message.
func (s *ArchiveSink) archiveMessage(
	item models.FullItem,
	gmailID, emlPath, sourceName string,
) error {
	rawBytes, err := s.fetcher.GetMessageRaw(gmailID)
	if err != nil {
		return fmt.Errorf("failed to fetch raw message: %w", err)
	}

	if err := os.WriteFile(emlPath, rawBytes, 0644); err != nil {
		return fmt.Errorf("failed to write .eml file: %w", err)
	}

	meta := buildArchiveMessage(item, gmailID, emlPath, sourceName, int64(len(rawBytes)))

	if err := s.store.IndexMessage(meta, item.GetContent()); err != nil {
		// The .eml was written; log the failure but don't treat it as fatal.
		slog.Warn("Failed to index message in archive DB",
			"gmail_id", gmailID,
			"error", err)
	}

	return nil
}

// buildArchiveMessage constructs an archive.Message from a FullItem.
func buildArchiveMessage(
	item models.FullItem,
	gmailID, emlPath, sourceName string,
	sizeBytes int64,
) archive.Message {
	metadata := item.GetMetadata()

	threadID, _ := metadata["thread_id"].(string)
	rfc822ID, _ := metadata["message_id"].(string)

	fromAddr := extractFromAddr(metadata)
	toAddrs := extractAddrList(metadata, "to")
	ccAddrs := extractAddrList(metadata, "cc")
	labels := extractLabels(metadata)
	hasAttachments := len(item.GetAttachments()) > 0

	return archive.Message{
		GmailID:         gmailID,
		ThreadID:        threadID,
		RFC822MessageID: rfc822ID,
		Subject:         item.GetTitle(),
		FromAddr:        fromAddr,
		ToAddrs:         toAddrs,
		CCAddrs:         ccAddrs,
		DateSent:        item.GetCreatedAt(),
		Labels:          labels,
		EMLPath:         emlPath,
		SizeBytes:       sizeBytes,
		HasAttachments:  hasAttachments,
		SourceName:      sourceName,
	}
}

// extractFromAddr extracts the from address from item metadata.
func extractFromAddr(metadata map[string]interface{}) string {
	switch v := metadata["from"].(type) {
	case string:
		return v
	case map[string]interface{}:
		name, _ := v["name"].(string)
		email, _ := v["email"].(string)

		if name != "" && email != "" {
			addr := mail.Address{Name: name, Address: email}

			return addr.String()
		}

		return email
	}

	return ""
}

// extractAddrList extracts a list of email addresses from item metadata.
func extractAddrList(metadata map[string]interface{}, key string) []string {
	raw, ok := metadata[key]
	if !ok {
		return nil
	}

	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		addrs := make([]string, 0, len(v))

		for _, elem := range v {
			switch e := elem.(type) {
			case string:
				addrs = append(addrs, e)
			case map[string]interface{}:
				email, _ := e["email"].(string)
				if email != "" {
					addrs = append(addrs, email)
				}
			}
		}

		return addrs
	}

	return nil
}

// extractLabels extracts Gmail labels from item metadata.
func extractLabels(metadata map[string]interface{}) []string {
	raw, ok := metadata["labels"]
	if !ok {
		return nil
	}

	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		labels := make([]string, 0, len(v))

		for _, elem := range v {
			if s, ok := elem.(string); ok {
				labels = append(labels, s)
			}
		}

		return labels
	}

	return nil
}

// isGmailItem returns true if the item originated from a Gmail source.
func isGmailItem(item models.FullItem) bool {
	return item.GetSourceType() == "gmail"
}

// isThreadItem returns true if the item is a thread (not an individual message).
// Threads have IDs starting with "thread_" and have message_count > 1 in metadata.
func isThreadItem(item models.FullItem) bool {
	metadata := item.GetMetadata()
	count, _ := metadata["message_count"].(int)

	return count > 1
}

// Close releases resources held by the sink.
func (s *ArchiveSink) Close() error {
	return s.store.Close()
}

// Ensure ArchiveSink implements Sink.
var _ interfaces.Sink = (*ArchiveSink)(nil)

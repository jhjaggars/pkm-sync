package main

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"pkm-sync/internal/config"
	"pkm-sync/internal/embeddings"
	"pkm-sync/internal/sources/google"
	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/vectorstore"
	"pkm-sync/pkg/models"

	mdconverter "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/spf13/cobra"
)

var (
	indexSourceName    string
	indexSince         string
	indexLimit         int
	indexReindex       bool
	indexDelay         int
	indexMaxContentLen int
)

var multipleNewlines = regexp.MustCompile(`\n\s*\n\s*\n`)

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index Gmail messages into vector database for semantic search",
	Long: `Index Gmail messages into a vector database for semantic search.
Messages are grouped by thread and embedded together for better context.

Examples:
  pkm-sync index --source gmail_work --since 30d
  pkm-sync index --source gmail_work --since 7d --limit 500
  pkm-sync index --reindex  # Re-index all messages from all sources`,
	RunE: runIndexCommand,
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().StringVar(&indexSourceName, "source", "", "Gmail source to index (gmail_work, gmail_personal, etc.)")
	indexCmd.Flags().StringVar(&indexSince, "since", "30d", "Index emails since (7d, 2006-01-02, today)")
	indexCmd.Flags().IntVar(&indexLimit, "limit", 1000, "Maximum number of emails to fetch per source")
	indexCmd.Flags().BoolVar(&indexReindex, "reindex", false, "Re-index already indexed threads")
	indexCmd.Flags().IntVar(&indexDelay, "delay", 200, "Delay between embeddings in milliseconds (prevents Ollama overload)")
	indexCmd.Flags().IntVar(&indexMaxContentLen, "max-content-length", 30000, "Truncate email content to this many characters (0 = no limit)")
}

//nolint:maintidx // Command orchestration function
func runIndexCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine which Gmail sources to index
	var sourcesToIndex []string
	if indexSourceName != "" {
		sourcesToIndex = []string{indexSourceName}
	} else {
		sourcesToIndex = getEnabledGmailSources(cfg)
	}

	if len(sourcesToIndex) == 0 {
		return fmt.Errorf("no Gmail sources configured. Please configure Gmail sources in your config file or use --source flag")
	}

	// Parse since time
	sinceTime, err := parseSinceTime(indexSince)
	if err != nil {
		return fmt.Errorf("failed to parse --since: %w", err)
	}

	// Initialize embedding provider
	provider, err := embeddings.NewProvider(cfg.Embeddings)
	if err != nil {
		return fmt.Errorf("failed to create embedding provider: %w", err)
	}
	defer provider.Close()

	fmt.Printf("Using embedding provider: %s (%s, %d dimensions)\n", cfg.Embeddings.Provider, cfg.Embeddings.Model, cfg.Embeddings.Dimensions)

	// Open vector store
	dbPath := cfg.VectorDB.DBPath
	if dbPath == "" {
		// Default to config directory
		configDir, err := config.GetConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		dbPath = filepath.Join(configDir, "vectors.db")
	}

	store, err := vectorstore.NewStore(dbPath, cfg.Embeddings.Dimensions)
	if err != nil {
		return fmt.Errorf("failed to open vector store at %s: %w", dbPath, err)
	}
	defer store.Close()

	fmt.Printf("Using vector database: %s\n", dbPath)

	// Index each source
	totalThreads := 0
	totalSkipped := 0

	for _, sourceName := range sourcesToIndex {
		sourceConfig, exists := cfg.Sources[sourceName]
		if !exists {
			fmt.Printf("Warning: Source '%s' not found in config, skipping\n", sourceName)

			continue
		}

		if sourceConfig.Type != "gmail" {
			fmt.Printf("Warning: Source '%s' is not a Gmail source (type: %s), skipping\n", sourceName, sourceConfig.Type)

			continue
		}

		fmt.Printf("\nIndexing source: %s\n", sourceName)

		// Force ExtractRecipients to true for indexing
		originalExtractRecipients := sourceConfig.Gmail.ExtractRecipients
		sourceConfig.Gmail.ExtractRecipients = true

		// Create authenticated client
		client, err := auth.GetClient()
		if err != nil {
			return fmt.Errorf("failed to create authenticated client: %w", err)
		}

		source := google.NewGoogleSourceWithConfig(sourceName, sourceConfig)
		if err := source.Configure(nil, client); err != nil {
			return fmt.Errorf("failed to configure source '%s': %w", sourceName, err)
		}

		// Fetch messages
		fmt.Printf("Fetching messages since %s (limit: %d)...\n", sinceTime.Format("2006-01-02"), indexLimit)

		items, err := source.Fetch(sinceTime, indexLimit)
		if err != nil {
			return fmt.Errorf("failed to fetch from source '%s': %w", sourceName, err)
		}

		// Restore original config
		sourceConfig.Gmail.ExtractRecipients = originalExtractRecipients

		fmt.Printf("Fetched %d messages\n", len(items))

		if len(items) == 0 {
			fmt.Println("No messages to index")

			continue
		}

		// Group messages by thread
		threadGroups := groupMessagesByThread(items)
		fmt.Printf("Grouped into %d threads\n", len(threadGroups))

		// Get already indexed threads (unless reindex flag is set)
		var indexedThreads map[string]bool
		if !indexReindex {
			indexedThreads, err = store.GetIndexedThreadIDs(sourceName)
			if err != nil {
				return fmt.Errorf("failed to get indexed threads: %w", err)
			}

			fmt.Printf("Already indexed: %d threads\n", len(indexedThreads))
		} else {
			indexedThreads = make(map[string]bool)
		}

		// Index each thread
		threadsIndexed := 0
		threadsSkipped := 0
		threadsFailed := 0

		fmt.Printf("Indexing with %dms delay between requests, max content length: %d chars\n", indexDelay, indexMaxContentLen)

		for threadID, group := range threadGroups {
			// Skip if already indexed (unless reindex flag is set)
			if indexedThreads[threadID] && !indexReindex {
				threadsSkipped++

				continue
			}

			// Build embeddable content with participant info
			content := buildThreadContent(group)

			// Truncate if needed to prevent Ollama overload
			originalLen := len(content)
			if indexMaxContentLen > 0 && len(content) > indexMaxContentLen {
				content = content[:indexMaxContentLen] + "\n\n[Content truncated for indexing]"
			}

			// Rate limiting: delay between embeddings to prevent Ollama subprocess crashes
			if indexDelay > 0 && threadsIndexed > 0 {
				time.Sleep(time.Duration(indexDelay) * time.Millisecond)
			}

			// Progress update every 10 threads
			if (threadsIndexed+threadsSkipped+threadsFailed)%10 == 0 && (threadsIndexed+threadsSkipped+threadsFailed) > 0 {
				truncated := ""
				if len(content) < originalLen {
					truncated = fmt.Sprintf(" -> %d", len(content))
				}

				fmt.Printf("Progress: %d indexed, %d skipped, %d failed (current: %s, %d%s chars)\n",
					threadsIndexed, threadsSkipped, threadsFailed, group.Subject, originalLen, truncated)
			}

			// Embed the thread
			embedding, err := provider.Embed(ctx, content)
			if err != nil {
				truncated := ""
				if len(content) < originalLen {
					truncated = fmt.Sprintf(" truncated to %d", len(content))
				}

				fmt.Printf("Warning: Failed to embed thread %s (%s, %d chars%s): %v\n",
					threadID, group.Subject, originalLen, truncated, err)

				threadsFailed++

				continue
			}

			// Build metadata
			metadata := buildThreadMetadata(group)

			// Create document
			doc := vectorstore.Document{
				SourceID:     group.Messages[0].GetID(), // Use first message ID
				ThreadID:     threadID,
				Title:        group.Subject,
				Content:      content,
				SourceType:   "gmail",
				SourceName:   sourceName,
				MessageCount: len(group.Messages),
				Metadata:     metadata,
				CreatedAt:    group.StartTime,
				UpdatedAt:    group.EndTime,
			}

			// Upsert into vector store
			if err := store.UpsertDocument(doc, embedding); err != nil {
				fmt.Printf("Warning: Failed to index thread %s: %v\n", threadID, err)

				continue
			}

			threadsIndexed++
		}

		successRate := 0.0
		if threadsIndexed+threadsFailed > 0 {
			successRate = float64(threadsIndexed) * 100.0 / float64(threadsIndexed+threadsFailed)
		}

		fmt.Printf("Indexed: %d threads, Skipped: %d threads, Failed: %d threads (%.1f%% success)\n",
			threadsIndexed, threadsSkipped, threadsFailed, successRate)
		totalThreads += threadsIndexed
		totalSkipped += threadsSkipped
	}

	// Print summary
	fmt.Printf("\n=== Indexing Complete ===\n")
	fmt.Printf("Total threads indexed: %d\n", totalThreads)
	fmt.Printf("Total threads skipped: %d\n", totalSkipped)

	// Get database stats
	stats, err := store.Stats()
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	fmt.Printf("\n=== Vector Database Stats ===\n")
	fmt.Printf("Total documents: %d\n", stats.TotalDocuments)
	fmt.Printf("Total threads: %d\n", stats.TotalThreads)
	fmt.Printf("Average messages per thread: %.1f\n", stats.AverageMessageCount)

	if len(stats.DocumentsBySource) > 0 {
		fmt.Printf("\nDocuments by source:\n")

		for source, count := range stats.DocumentsBySource {
			fmt.Printf("  %s: %d\n", source, count)
		}
	}

	if !stats.OldestDocument.IsZero() && !stats.NewestDocument.IsZero() {
		fmt.Printf("\nDate range: %s to %s\n",
			stats.OldestDocument.Format("2006-01-02"),
			stats.NewestDocument.Format("2006-01-02"))
	}

	return nil
}

// ThreadGroup represents a group of messages in a thread.
type ThreadGroup struct {
	ThreadID     string
	Subject      string
	Messages     []models.ItemInterface
	Participants []string
	StartTime    time.Time
	EndTime      time.Time
}

// groupMessagesByThread groups messages by thread ID.
func groupMessagesByThread(items []models.ItemInterface) map[string]*ThreadGroup {
	threadGroups := make(map[string]*ThreadGroup)

	for _, item := range items {
		if item == nil {
			continue
		}

		// Get thread ID from metadata
		threadID := extractThreadID(item)
		if threadID == "" {
			// No thread ID - treat as individual message
			threadID = item.GetID()
		}

		if group, exists := threadGroups[threadID]; exists {
			group.Messages = append(group.Messages, item)

			// Update time range
			if item.GetCreatedAt().Before(group.StartTime) {
				group.StartTime = item.GetCreatedAt()
			}

			if item.GetCreatedAt().After(group.EndTime) {
				group.EndTime = item.GetCreatedAt()
			}

			// Update participants
			updateParticipants(group, item)
		} else {
			// Create new thread group
			threadGroups[threadID] = &ThreadGroup{
				ThreadID:     threadID,
				Subject:      extractThreadSubject(item),
				Messages:     []models.ItemInterface{item},
				Participants: extractParticipants(item),
				StartTime:    item.GetCreatedAt(),
				EndTime:      item.GetCreatedAt(),
			}
		}
	}

	// Sort messages within each thread by creation time
	for _, group := range threadGroups {
		sort.Slice(group.Messages, func(i, j int) bool {
			return group.Messages[i].GetCreatedAt().Before(group.Messages[j].GetCreatedAt())
		})
	}

	return threadGroups
}

// buildThreadContent builds embeddable content from a thread group.
// Includes participant information for searchability.
func buildThreadContent(group *ThreadGroup) string {
	var builder strings.Builder

	// Thread subject
	builder.WriteString(fmt.Sprintf("Thread: %s\n\n", group.Subject))

	// Build content for each message
	for i, item := range group.Messages {
		builder.WriteString(fmt.Sprintf("--- Message %d (%s) ---\n", i+1, item.GetCreatedAt().Format("2006-01-02 15:04")))

		// Add participant info from metadata
		metadata := item.GetMetadata()
		if from, ok := metadata["from"].(string); ok && from != "" {
			builder.WriteString(fmt.Sprintf("From: %s\n", from))
		}

		if to, ok := metadata["to"].(string); ok && to != "" {
			builder.WriteString(fmt.Sprintf("To: %s\n", to))
		}

		if cc, ok := metadata["cc"].(string); ok && cc != "" {
			builder.WriteString(fmt.Sprintf("Cc: %s\n", cc))
		}

		if bcc, ok := metadata["bcc"].(string); ok && bcc != "" {
			builder.WriteString(fmt.Sprintf("Bcc: %s\n", bcc))
		}

		builder.WriteString("\n")

		// Add message content
		content := prepareContentForEmbedding(item.GetContent())
		if content != "" {
			builder.WriteString(content)
		} else {
			builder.WriteString("(no content)")
		}

		builder.WriteString("\n\n")
	}

	return builder.String()
}

// buildThreadMetadata builds metadata for a thread.
func buildThreadMetadata(group *ThreadGroup) map[string]interface{} {
	// Extract all unique participants
	participantsMap := make(map[string]bool)
	for _, p := range group.Participants {
		participantsMap[p] = true
	}

	participants := make([]string, 0, len(participantsMap))
	for p := range participantsMap {
		participants = append(participants, p)
	}

	// Extract message IDs
	messageIDs := make([]string, len(group.Messages))
	for i, msg := range group.Messages {
		messageIDs[i] = msg.GetID()
	}

	// Build per-message detail
	messages := make([]map[string]interface{}, len(group.Messages))
	for i, msg := range group.Messages {
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
		"message_count": len(group.Messages),
		"date_range": map[string]string{
			"start": group.StartTime.Format(time.RFC3339),
			"end":   group.EndTime.Format(time.RFC3339),
		},
		"messages": messages,
	}
}

// prepareContentForEmbedding converts HTML to markdown and cleans up content for better embeddings.
func prepareContentForEmbedding(content string) string {
	// Detect HTML
	if !strings.Contains(content, "<") || !strings.Contains(content, ">") {
		return content
	}

	// Convert HTML to markdown
	markdown, err := mdconverter.ConvertString(content)
	if err != nil {
		// Fall back to raw content if conversion fails
		return content
	}

	// Strip email quoted text
	markdown = stripQuotedText(markdown)

	// Normalize whitespace
	markdown = collapseWhitespace(markdown)

	return strings.TrimSpace(markdown)
}

// stripQuotedText removes quoted text from email content.
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

// collapseWhitespace reduces multiple consecutive newlines to just two.
func collapseWhitespace(content string) string {
	return multipleNewlines.ReplaceAllString(content, "\n\n")
}

// extractThreadID extracts the thread ID from an item's metadata.
func extractThreadID(item models.ItemInterface) string {
	metadata := item.GetMetadata()
	if threadID, ok := metadata["thread_id"].(string); ok {
		return threadID
	}

	return ""
}

// extractThreadSubject extracts the subject for a thread.
func extractThreadSubject(item models.ItemInterface) string {
	subject := item.GetTitle()
	// Clean up subject by removing Re:, Fwd: prefixes
	subject = strings.TrimSpace(subject)
	subject = strings.TrimPrefix(subject, "Re: ")
	subject = strings.TrimPrefix(subject, "RE: ")
	subject = strings.TrimPrefix(subject, "Fwd: ")
	subject = strings.TrimPrefix(subject, "FWD: ")

	return subject
}

// extractParticipants extracts participants from an item's metadata.
func extractParticipants(item models.ItemInterface) []string {
	participants := []string{}
	metadata := item.GetMetadata()

	// Extract from, to, cc, bcc
	if from, ok := metadata["from"].(string); ok && from != "" {
		participants = append(participants, from)
	}

	if to, ok := metadata["to"].(string); ok && to != "" {
		participants = append(participants, to)
	}

	if cc, ok := metadata["cc"].(string); ok && cc != "" {
		participants = append(participants, cc)
	}

	if bcc, ok := metadata["bcc"].(string); ok && bcc != "" {
		participants = append(participants, bcc)
	}

	return participants
}

// updateParticipants updates the participants list for a thread group.
func updateParticipants(group *ThreadGroup, item models.ItemInterface) {
	metadata := item.GetMetadata()

	addIfNew := func(email string) {
		if email == "" {
			return
		}

		for _, p := range group.Participants {
			if p == email {
				return
			}
		}

		group.Participants = append(group.Participants, email)
	}

	if from, ok := metadata["from"].(string); ok {
		addIfNew(from)
	}

	if to, ok := metadata["to"].(string); ok {
		addIfNew(to)
	}

	if cc, ok := metadata["cc"].(string); ok {
		addIfNew(cc)
	}

	if bcc, ok := metadata["bcc"].(string); ok {
		addIfNew(bcc)
	}
}

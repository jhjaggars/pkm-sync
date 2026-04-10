package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const (
	// defaultConcurrentWorkers is the default number of concurrent API workers.
	defaultConcurrentWorkers = 5
	// throttledConcurrentWorkers is used when request delay is high to avoid rate limiting.
	throttledConcurrentWorkers = 2
	// highDelayThreshold is the delay above which worker concurrency is reduced.
	highDelayThreshold = 100 * time.Millisecond
)

// Service wraps the Gmail API with configuration and convenience methods.
type Service struct {
	client   *http.Client
	service  *gmail.Service
	config   models.GmailSourceConfig
	sourceID string
}

// NewService creates a new Gmail service wrapper.
func NewService(client *http.Client, config models.GmailSourceConfig, sourceID string) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}

	gmailService, err := gmail.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Gmail service: %w", err)
	}

	s := &Service{
		client:   client,
		service:  gmailService,
		config:   config,
		sourceID: sourceID,
	}

	// Resolve label IDs to query-safe names
	if err := s.resolveLabels(); err != nil {
		slog.Warn("Failed to resolve label IDs", "source_id", sourceID, "error", err)
	}

	return s, nil
}

// GetMessages retrieves messages based on the configured filters and time range.
func (s *Service) GetMessages(since time.Time, limit int) ([]*gmail.Message, error) {
	// For large mailboxes, use batch processing.
	if limit > 1000 {
		return s.getMessagesWithBatchProcessing(since, limit)
	}

	// Build the query based on configuration.
	query := s.buildQuery(since)

	// Debug logging.
	slog.Info("Gmail query built",
		"source_id", s.sourceID,
		"query", query,
		"since", since.Format("2006-01-02"),
		"limit", limit)

	// Set default limit if not specified.
	if limit <= 0 {
		limit = 100
	}

	// Apply configuration limits.
	if s.config.MaxRequests > 0 && limit > s.config.MaxRequests {
		limit = s.config.MaxRequests
	}

	// List messages using the Gmail API with retry logic.
	req := s.service.Users.Messages.List("me").Q(query).MaxResults(int64(limit))

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list messages: %w", err)
	}

	listResp := resp.(*gmail.ListMessagesResponse)
	slog.Info("Gmail API response", "source_id", s.sourceID, "messages_found", len(listResp.Messages), "query", query)

	if len(listResp.Messages) == 0 {
		return []*gmail.Message{}, nil
	}

	// Fetch full message details for each message with controlled concurrency.
	messages, skippedCount := s.fetchMessagesConcurrently(listResp.Messages)

	if skippedCount > 0 {
		slog.Info("Message retrieval completed", "retrieved", len(messages), "skipped", skippedCount)
	}

	return messages, nil
}

// GetMessage retrieves a single message with full details.
func (s *Service) GetMessage(messageID string) (*gmail.Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	if s.service == nil {
		return nil, fmt.Errorf("gmail service is not initialized")
	}

	// Get the full message including body.
	req := s.service.Users.Messages.Get("me", messageID).Format("full")

	message, err := req.Do()
	if err != nil {
		return nil, fmt.Errorf("unable to get message %s: %w", messageID, err)
	}

	return message, nil
}

// GetMessageWithRetry retrieves a single message with retry logic.
func (s *Service) GetMessageWithRetry(messageID string) (*gmail.Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	if s.service == nil {
		return nil, fmt.Errorf("gmail service is not initialized")
	}

	// Get the full message including body with retry logic.
	req := s.service.Users.Messages.Get("me", messageID).Format("full")

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get message %s: %w", messageID, err)
	}

	return resp.(*gmail.Message), nil
}

// GetMessageRaw fetches a single message in RFC 5322 format (format=raw) and returns
// the decoded bytes. This is used by the archive sink for lossless storage.
func (s *Service) GetMessageRaw(messageID string) ([]byte, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	if s.service == nil {
		return nil, fmt.Errorf("gmail service is not initialized")
	}

	req := s.service.Users.Messages.Get("me", messageID).Format("raw")

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get raw message %s: %w", messageID, err)
	}

	msg := resp.(*gmail.Message)
	if msg.Raw == "" {
		return nil, fmt.Errorf("raw field is empty for message %s", messageID)
	}

	// Gmail returns base64url-encoded RFC 5322 bytes.
	data, err := base64.URLEncoding.DecodeString(msg.Raw)
	if err != nil {
		// Try standard base64 as fallback.
		data, err = base64.StdEncoding.DecodeString(msg.Raw)
		if err != nil {
			return nil, fmt.Errorf("failed to decode raw message %s: %w", messageID, err)
		}
	}

	return data, nil
}

// GetMessagesInRange retrieves messages within a specific time range.
func (s *Service) GetMessagesInRange(start, end time.Time, limit int) ([]*gmail.Message, error) {
	if end.Before(start) {
		return nil, fmt.Errorf("end time must be after start time")
	}

	// Build query with both start and end time filters.
	query := s.buildQueryWithRange(start, end)

	if limit <= 0 {
		limit = 100
	}

	req := s.service.Users.Messages.List("me").Q(query).MaxResults(int64(limit))

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list messages in range: %w", err)
	}

	listResp := resp.(*gmail.ListMessagesResponse)
	if len(listResp.Messages) == 0 {
		return []*gmail.Message{}, nil
	}

	// Fetch full message details with concurrent processing.
	messages, skippedCount := s.fetchMessagesConcurrently(listResp.Messages)

	if skippedCount > 0 {
		slog.Info("Message range retrieval completed", "retrieved", len(messages), "skipped", skippedCount)
	}

	return messages, nil
}

// buildQuery constructs a Gmail search query based on configuration and since time.
func (s *Service) buildQuery(since time.Time) string {
	return buildQuery(s.config, since)
}

// buildQueryWithRange constructs a Gmail search query with start and end times.
func (s *Service) buildQueryWithRange(start, end time.Time) string {
	return buildQueryWithRange(s.config, start, end)
}

// GetLabels retrieves all available labels for the user.
func (s *Service) GetLabels() ([]*gmail.Label, error) {
	req := s.service.Users.Labels.List("me")

	resp, err := req.Do()
	if err != nil {
		return nil, fmt.Errorf("unable to list labels: %w", err)
	}

	return resp.Labels, nil
}

// GetRecentSubjects returns up to limit recent email subjects matching the given Gmail query.
// It fetches only message metadata (Subject header) to minimize quota usage.
func (s *Service) GetRecentSubjects(query string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}

	listResp, err := s.service.Users.Messages.List("me").Q(query).MaxResults(int64(limit)).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	if listResp == nil || len(listResp.Messages) == 0 {
		return nil, nil
	}

	subjects := make([]string, 0, len(listResp.Messages))

	for _, m := range listResp.Messages {
		msg, err := s.service.Users.Messages.Get("me", m.Id).
			Format("metadata").
			MetadataHeaders("Subject").
			Do()
		if err != nil {
			continue
		}

		if msg.Payload == nil {
			continue
		}

		for _, h := range msg.Payload.Headers {
			if h.Name == "Subject" {
				subjects = append(subjects, h.Value)

				break
			}
		}
	}

	return subjects, nil
}

// GetProfile retrieves the user's Gmail profile information.
func (s *Service) GetProfile() (*gmail.Profile, error) {
	req := s.service.Users.GetProfile("me")

	profile, err := req.Do()
	if err != nil {
		return nil, fmt.Errorf("unable to get profile: %w", err)
	}

	return profile, nil
}

// ValidateConfiguration checks if the Gmail configuration is valid.
func (s *Service) ValidateConfiguration() error {
	// Test API access by getting profile.
	_, err := s.GetProfile()
	if err != nil {
		return fmt.Errorf("unable to access Gmail API: %w", err)
	}

	// Validate configured labels exist.
	if len(s.config.Labels) > 0 {
		availableLabels, err := s.GetLabels()
		if err != nil {
			return fmt.Errorf("unable to verify labels: %w", err)
		}

		labelMap := make(map[string]bool)
		for _, label := range availableLabels {
			labelMap[label.Name] = true
			labelMap[label.Id] = true
		}

		for _, configLabel := range s.config.Labels {
			if !labelMap[configLabel] {
				return fmt.Errorf("configured label '%s' not found in user's Gmail", configLabel)
			}
		}
	}

	return nil
}

// executeWithRetry executes a function with exponential backoff retry logic.
func (s *Service) executeWithRetry(fn func() (interface{}, error)) (interface{}, error) {
	const (
		maxRetries = 3
		baseDelay  = time.Second
	)

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter.
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}

			slog.Info("Retrying Gmail API call", "delay", delay, "attempt", attempt+1, "max_retries", maxRetries)
			time.Sleep(delay)
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if error is retryable.
		if googleErr, ok := err.(*googleapi.Error); ok {
			switch googleErr.Code {
			case 403: // Rate limit exceeded.
				if attempt < maxRetries-1 {
					slog.Info("Rate limit exceeded, retrying", "code", googleErr.Code)

					continue
				}
			case 429: // Too many requests.
				if attempt < maxRetries-1 {
					slog.Info("Too many requests, retrying", "code", googleErr.Code)

					continue
				}
			case 500, 502, 503, 504: // Server errors.
				if attempt < maxRetries-1 {
					slog.Info("Server error, retrying", "code", googleErr.Code)

					continue
				}
			default:
				// Non-retryable error.
				return nil, err
			}
		}

		// For other types of errors, check if they're temporary.
		if isTemporaryError(err) && attempt < maxRetries-1 {
			slog.Info("Temporary error, retrying", "error", err)

			continue
		}

		// Non-retryable error.
		return nil, err
	}

	return nil, fmt.Errorf("max retries (%d) exceeded, last error: %w", maxRetries, lastErr)
}

// isTemporaryError checks if an error is likely temporary and retryable.
func isTemporaryError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	temporaryErrors := []string{
		"connection reset",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"connection refused",
		"i/o timeout",
	}

	for _, tempErr := range temporaryErrors {
		if strings.Contains(strings.ToLower(errStr), tempErr) {
			return true
		}
	}

	return false
}

// getMessagesWithBatchProcessing handles large mailbox scenarios with optimized batch processing.
func (s *Service) getMessagesWithBatchProcessing(since time.Time, limit int) ([]*gmail.Message, error) {
	// Configure batch size based on configuration or use defaults.
	batchSize := 100
	if s.config.BatchSize > 0 && s.config.BatchSize <= 500 {
		batchSize = s.config.BatchSize
	}

	// Adjust request delay for large batches to avoid rate limiting.
	requestDelay := s.config.RequestDelay
	if requestDelay == 0 {
		requestDelay = 50 * time.Millisecond // Default delay for large batches.
	}

	slog.Info("Processing large mailbox", "batch_size", batchSize, "request_delay", requestDelay)

	var (
		allMessages  []*gmail.Message
		totalSkipped int
	)

	remaining := limit
	pageToken := ""

	for remaining > 0 {
		// Calculate current batch size.
		currentBatch := batchSize
		if remaining < batchSize {
			currentBatch = remaining
		}

		messages, nextPageToken, skipped, err := s.getMessageBatch(since, currentBatch, pageToken, requestDelay)
		if err != nil {
			return allMessages, fmt.Errorf("batch processing failed: %w", err)
		}

		allMessages = append(allMessages, messages...)
		totalSkipped += skipped
		remaining -= len(messages)

		// Progress reporting for large batches.
		if len(allMessages)%500 == 0 || remaining == 0 {
			slog.Info("Batch processing progress",
				"processed", len(allMessages),
				"remaining", remaining,
				"skipped", totalSkipped)
		}

		// Check if there are more pages.
		if nextPageToken == "" || len(messages) == 0 {
			break
		}

		pageToken = nextPageToken
	}

	if totalSkipped > 0 {
		slog.Info("Batch processing completed", "retrieved", len(allMessages), "skipped", totalSkipped)
	}

	return allMessages, nil
}

// getMessageBatch retrieves a single batch of messages with optimizations.
func (s *Service) getMessageBatch(
	since time.Time,
	batchSize int,
	pageToken string,
	_ time.Duration,
) ([]*gmail.Message, string, int, error) {
	query := s.buildQuery(since)

	// List messages for this batch.
	req := s.service.Users.Messages.List("me").Q(query).MaxResults(int64(batchSize))
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, "", 0, fmt.Errorf("unable to list message batch: %w", err)
	}

	listResp := resp.(*gmail.ListMessagesResponse)
	if len(listResp.Messages) == 0 {
		return []*gmail.Message{}, "", 0, nil
	}

	// Fetch full message details with concurrent processing.
	messages, skippedCount := s.fetchMessagesConcurrently(listResp.Messages)

	return messages, listResp.NextPageToken, skippedCount, nil
}

// GetAttachment retrieves attachment data for a specific message and attachment ID.
func (s *Service) GetAttachment(messageID, attachmentID string) (*gmail.MessagePartBody, error) {
	if messageID == "" || attachmentID == "" {
		return nil, fmt.Errorf("message ID and attachment ID are required")
	}

	if s.service == nil {
		return nil, fmt.Errorf("gmail service is not initialized")
	}

	req := s.service.Users.Messages.Attachments.Get("me", messageID, attachmentID)

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get attachment %s from message %s: %w", attachmentID, messageID, err)
	}

	return resp.(*gmail.MessagePartBody), nil
}

// GetMessagesStream provides a streaming interface for very large mailboxes.
func (s *Service) GetMessagesStream(since time.Time, batchSize int, callback func([]*gmail.Message) error) error {
	if batchSize <= 0 {
		batchSize = 50 // Smaller default for streaming.
	}

	pageToken := ""
	totalProcessed := 0

	for {
		messages, nextPageToken, skipped, err := s.getMessageBatch(since, batchSize, pageToken, s.config.RequestDelay)
		if err != nil {
			return fmt.Errorf("streaming batch failed: %w", err)
		}

		if len(messages) == 0 {
			break
		}

		// Call the callback with this batch.
		if err := callback(messages); err != nil {
			return fmt.Errorf("callback failed: %w", err)
		}

		totalProcessed += len(messages)

		if skipped > 0 {
			slog.Info("Streamed batch processed", "processed", len(messages), "skipped", skipped)
		}

		// Check if there are more pages.
		if nextPageToken == "" {
			break
		}

		pageToken = nextPageToken

		// Optional: implement max processing limit.
		if s.config.MaxRequests > 0 && totalProcessed >= s.config.MaxRequests {
			slog.Info("Reached maximum request limit", "limit", s.config.MaxRequests)

			break
		}
	}

	slog.Info("Streaming completed", "total_processed", totalProcessed)

	return nil
}

// GetThreads retrieves threads based on the configured filters and time range.
func (s *Service) GetThreads(since time.Time, limit int) ([]*gmail.Thread, error) {
	query := s.buildQuery(since)

	slog.Info("Gmail thread query built",
		"source_id", s.sourceID,
		"query", query,
		"since", since.Format("2006-01-02"),
		"limit", limit)

	if limit <= 0 {
		limit = 100
	}

	if s.config.MaxRequests > 0 && limit > s.config.MaxRequests {
		limit = s.config.MaxRequests
	}

	req := s.service.Users.Threads.List("me").Q(query).MaxResults(int64(limit))

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list threads: %w", err)
	}

	listResp, ok := resp.(*gmail.ListThreadsResponse)
	if !ok || listResp == nil {
		return nil, fmt.Errorf("unexpected response type from Gmail Threads API")
	}

	slog.Info("Gmail Threads API response",
		"source_id", s.sourceID,
		"threads_found", len(listResp.Threads),
		"query", query)

	if len(listResp.Threads) == 0 {
		return []*gmail.Thread{}, nil
	}

	// Fetch full thread details concurrently.
	threads, skippedCount := s.fetchThreadsConcurrently(listResp.Threads)

	if skippedCount > 0 {
		slog.Info("Thread retrieval completed", "retrieved", len(threads), "skipped", skippedCount)
	}

	return threads, nil
}

// GetThread retrieves a single thread with full message details.
func (s *Service) GetThread(threadID string) (*gmail.Thread, error) {
	if threadID == "" {
		return nil, fmt.Errorf("thread ID is required")
	}

	if s.service == nil {
		return nil, fmt.Errorf("gmail service is not initialized")
	}

	req := s.service.Users.Threads.Get("me", threadID).Format("full")

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, handleThreadError(threadID, err)
	}

	return resp.(*gmail.Thread), nil
}

// fetchThreadsConcurrently fetches full thread details concurrently with rate limiting.
// Uses context.Background(); callers can provide a real context once Source.Fetch adds one.
func (s *Service) fetchThreadsConcurrently(threadList []*gmail.Thread) ([]*gmail.Thread, int) {
	return fetchConcurrently(
		context.Background(),
		s.config.RequestDelay,
		threadList,
		func(t *gmail.Thread) string { return t.Id },
		s.GetThread,
		"thread",
	)
}

// isThreadError checks if an error is related to thread fetching.
func isThreadError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	return strings.Contains(errStr, "thread") || strings.Contains(errStr, "threads")
}

// handleThreadError provides better error context for thread-related errors.
func handleThreadError(threadID string, err error) error {
	if googleErr, ok := err.(*googleapi.Error); ok {
		switch googleErr.Code {
		case http.StatusNotFound:
			return fmt.Errorf("thread %s not found: %w", threadID, err)
		case http.StatusForbidden:
			return fmt.Errorf("access denied to thread %s: %w", threadID, err)
		default:
			return fmt.Errorf("API error for thread %s (code %d): %w", threadID, googleErr.Code, err)
		}
	}

	return fmt.Errorf("failed to get thread %s: %w", threadID, err)
}

// fetchConcurrently is a generic worker pool that fetches full items from the Gmail API.
// Items is the list of stubs, getID extracts an item's ID, fetch retrieves the full
// item by ID, and itemType is used in log messages (e.g. "message" or "thread").
// ctx is checked between items so callers can cancel in-flight work.
func fetchConcurrently[T any](
	ctx context.Context,
	delay time.Duration,
	items []T,
	getID func(T) string,
	fetch func(string) (T, error),
	itemType string,
) ([]T, int) {
	// Configure concurrency based on rate limiting needs.
	maxWorkers := defaultConcurrentWorkers
	if delay > highDelayThreshold {
		// If delay is high, reduce concurrency.
		maxWorkers = throttledConcurrentWorkers
	}

	// Create channels for work distribution.
	itemChan := make(chan T, len(items))
	resultChan := make(chan T, len(items))
	errorChan := make(chan error, len(items))

	// Use atomic counter to avoid data race.
	var skippedCount int32

	// Start workers.
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-itemChan:
					if !ok {
						return
					}

					// Apply rate limiting per worker.
					if delay > 0 {
						time.Sleep(delay)
					}

					id := getID(item)

					full, err := fetch(id)
					if err != nil {
						slog.Warn("Worker failed to get "+itemType,
							"worker_id", workerID,
							itemType+"_id", id,
							"error", err)
						atomic.AddInt32(&skippedCount, 1)

						errorChan <- err
					} else {
						resultChan <- full
					}
				}
			}
		}(i)
	}

	// Send work to workers; close channel on exit so workers drain cleanly.
	go func() {
		defer close(itemChan)

		for _, item := range items {
			select {
			case <-ctx.Done():
				return
			case itemChan <- item:
			}
		}
	}()

	// Wait for workers to complete.
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()

	// Collect results.
	var results []T

	// Collect all results.
	for {
		select {
		case result, ok := <-resultChan:
			if !ok {
				resultChan = nil
			} else {
				results = append(results, result)
			}
		case _, ok := <-errorChan:
			if !ok {
				errorChan = nil
			}
		}

		// Break when both channels are closed.
		if resultChan == nil && errorChan == nil {
			break
		}
	}

	return results, int(atomic.LoadInt32(&skippedCount))
}

// fetchMessagesConcurrently fetches messages concurrently with rate limiting.
// Uses context.Background(); callers can provide a real context once Source.Fetch adds one.
func (s *Service) fetchMessagesConcurrently(messageList []*gmail.Message) ([]*gmail.Message, int) {
	return fetchConcurrently(
		context.Background(),
		s.config.RequestDelay,
		messageList,
		func(msg *gmail.Message) string { return msg.Id },
		s.GetMessageWithRetry,
		"message",
	)
}

// isLabelID returns true if the label appears to be a user-defined label ID
// (starts with "Label_"). System labels like INBOX, IMPORTANT, STARRED don't
// need resolution because they work as both IDs and query names.
func isLabelID(label string) bool {
	return strings.HasPrefix(label, "Label_")
}

// labelNameToQuery converts a Gmail label name to the format used in query
// strings. Gmail's label: operator only requires spaces to be replaced with
// hyphens; slashes and parentheses must be preserved as-is.
func labelNameToQuery(name string) string {
	return strings.ReplaceAll(name, " ", "-")
}

// resolveLabels resolves label IDs to query-safe names by fetching the full
// label list from Gmail API and replacing IDs in s.config.Labels with their
// corresponding query-safe names.
func (s *Service) resolveLabels() error {
	if len(s.config.Labels) == 0 {
		return nil
	}

	// Check if any labels need resolution
	needsResolution := false

	for _, label := range s.config.Labels {
		if isLabelID(label) {
			needsResolution = true

			break
		}
	}

	if !needsResolution {
		return nil
	}

	// Fetch all labels from Gmail
	labels, err := s.GetLabels()
	if err != nil {
		return fmt.Errorf("failed to fetch labels: %w", err)
	}

	// Build ID -> Name map
	idToName := make(map[string]string)
	for _, label := range labels {
		idToName[label.Id] = label.Name
	}

	// Resolve labels in config
	resolvedLabels := make([]string, 0, len(s.config.Labels))
	for _, label := range s.config.Labels {
		if isLabelID(label) {
			// It's a label ID, try to resolve it
			if name, found := idToName[label]; found {
				querySafeName := labelNameToQuery(name)
				slog.Info("Resolved label ID to query-safe name",
					"source_id", s.sourceID,
					"label_id", label,
					"label_name", name,
					"query_name", querySafeName)
				resolvedLabels = append(resolvedLabels, querySafeName)
			} else {
				slog.Warn("Label ID not found in Gmail account, skipping",
					"source_id", s.sourceID,
					"label_id", label)
			}
		} else {
			// System label or already a name, keep as-is
			resolvedLabels = append(resolvedLabels, label)
		}
	}

	s.config.Labels = resolvedLabels

	return nil
}

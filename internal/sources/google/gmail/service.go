package gmail

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
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

	return &Service{
		client:   client,
		service:  gmailService,
		config:   config,
		sourceID: sourceID,
	}, nil
}

// GetMessages retrieves messages based on the configured filters and time range.
func (s *Service) GetMessages(since time.Time, limit int) ([]*gmail.Message, error) {
	// For large mailboxes, use batch processing.
	if limit > 1000 {
		return s.getMessagesWithBatchProcessing(since, limit)
	}

	// Build the query based on configuration.
	query := s.BuildQuery(since)

	// Debug logging.
	slog.Info("Gmail query built (using Threads API)",
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

	// Get threads using the Threads API instead of Messages API
	threads, err := s.GetThreads(query, limit)
	if err != nil {
		return nil, fmt.Errorf("unable to get threads: %w", err)
	}

	// Extract messages from threads while preserving interface compatibility
	var messages []*gmail.Message

	for _, thread := range threads {
		if thread.Messages != nil {
			messages = append(messages, thread.Messages...)
		}
	}

	slog.Info("Gmail API response (from threads)",
		"source_id", s.sourceID,
		"threads_found", len(threads),
		"messages_extracted", len(messages),
		"query", query)

	return messages, nil
}

// GetThreads fetches Gmail threads using the Threads API.
func (s *Service) GetThreads(query string, limit int) ([]*gmail.Thread, error) {
	// List threads using the Gmail Threads API with retry logic.
	req := s.service.Users.Threads.List("me").Q(query).MaxResults(int64(limit))

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list threads: %w", err)
	}

	listResp := resp.(*gmail.ListThreadsResponse)
	slog.Info("Gmail Threads API response",
		"source_id", s.sourceID,
		"threads_found", len(listResp.Threads),
		"query", query)

	if len(listResp.Threads) == 0 {
		return []*gmail.Thread{}, nil
	}

	// Fetch full thread details for each thread with controlled concurrency.
	threads, skippedCount := s.fetchThreadsConcurrently(listResp.Threads)

	if skippedCount > 0 {
		slog.Info("Thread retrieval completed", "retrieved", len(threads), "skipped", skippedCount)
	}

	return threads, nil
}

// GetThread fetches a single Gmail thread with full message content.
func (s *Service) GetThread(threadID string) (*gmail.Thread, error) {
	req := s.service.Users.Threads.Get("me", threadID).Format("full")

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, s.handleThreadError(err, threadID, "get_thread")
	}

	thread := resp.(*gmail.Thread)
	slog.Debug("Retrieved thread", "thread_id", threadID, "message_count", len(thread.Messages))

	return thread, nil
}

// fetchThreadsConcurrently fetches multiple threads concurrently with rate limiting.
func (s *Service) fetchThreadsConcurrently(threadList []*gmail.Thread) ([]*gmail.Thread, int) {
	const maxWorkers = 10

	workers := maxWorkers
	if len(threadList) < workers {
		workers = len(threadList)
	}

	threadCh := make(chan *gmail.Thread, len(threadList))
	resultCh := make(chan *gmail.Thread, len(threadList))
	errorCh := make(chan error, len(threadList))

	// Send thread IDs to worker channel
	for _, thread := range threadList {
		threadCh <- thread
	}

	close(threadCh)

	// Start workers
	for i := 0; i < workers; i++ {
		go func() {
			for thread := range threadCh {
				// Apply request delay if configured
				if s.config.RequestDelay > 0 {
					time.Sleep(s.config.RequestDelay)
				}

				fullThread, err := s.GetThread(thread.Id)
				if err != nil {
					slog.Warn("Failed to get thread", "thread_id", thread.Id, "error", err)

					errorCh <- err

					continue
				}

				resultCh <- fullThread
			}
		}()
	}

	// Collect results
	var threads []*gmail.Thread

	var errors []error

	for i := 0; i < len(threadList); i++ {
		select {
		case thread := <-resultCh:
			threads = append(threads, thread)
		case err := <-errorCh:
			errors = append(errors, err)
		}
	}

	skippedCount := len(errors)
	if skippedCount > 0 {
		slog.Warn("Some threads could not be retrieved", "errors", skippedCount)
	}

	return threads, skippedCount
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

// BuildQuery constructs a Gmail search query based on configuration and since time.
func (s *Service) BuildQuery(since time.Time) string {
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

// isThreadError checks if an error is thread-specific and provides helpful context.
func isThreadError(err error) (bool, string) {
	if err == nil {
		return false, ""
	}

	errStr := strings.ToLower(err.Error())

	// Thread-specific error patterns
	threadErrors := map[string]string{
		"thread not found":     "Thread may have been deleted or moved",
		"invalid thread id":    "Thread ID format is incorrect",
		"thread access denied": "Insufficient permissions to access thread",
		"thread too large":     "Thread contains too many messages for processing",
		"quota exceeded":       "API quota exceeded - implement rate limiting",
	}

	for pattern, context := range threadErrors {
		if strings.Contains(errStr, pattern) {
			return true, context
		}
	}

	return false, ""
}

// handleThreadError provides enhanced error handling for thread operations.
func (s *Service) handleThreadError(err error, threadID string, operation string) error {
	if err == nil {
		return nil
	}

	isThread, context := isThreadError(err)
	if isThread {
		return fmt.Errorf("thread %s operation '%s' failed: %w (context: %s)",
			threadID, operation, err, context)
	}

	// Check for rate limiting specifically in thread context
	if googleErr, ok := err.(*googleapi.Error); ok {
		switch googleErr.Code {
		case 403:
			return fmt.Errorf("thread %s operation '%s' rate limited: %w (suggestion: increase RequestDelay)",
				threadID, operation, err)
		case 429:
			return fmt.Errorf("thread %s operation '%s' too many requests: %w (suggestion: reduce batch size)",
				threadID, operation, err)
		case 404:
			return fmt.Errorf("thread %s not found during '%s': %w (thread may have been deleted)",
				threadID, operation, err)
		}
	}

	// Generic thread operation error
	return fmt.Errorf("thread %s operation '%s' failed: %w", threadID, operation, err)
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

	slog.Info("Processing large mailbox with threads", "batch_size", batchSize, "request_delay", requestDelay)

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

		threads, nextPageToken, skipped, err := s.getThreadBatch(since, currentBatch, pageToken, requestDelay)
		if err != nil {
			return allMessages, fmt.Errorf("thread batch processing failed: %w", err)
		}

		// Extract messages from threads
		var batchMessages []*gmail.Message

		for _, thread := range threads {
			if thread.Messages != nil {
				batchMessages = append(batchMessages, thread.Messages...)
			}
		}

		allMessages = append(allMessages, batchMessages...)
		totalSkipped += skipped
		remaining -= len(threads) // Count threads, not messages for batch limiting

		// Progress reporting for large batches.
		if len(allMessages)%500 == 0 || remaining == 0 {
			slog.Info("Thread batch processing progress",
				"threads_processed", len(allMessages)/5, // Rough estimate
				"messages_extracted", len(allMessages),
				"remaining", remaining,
				"skipped", totalSkipped)
		}

		// Check if there are more pages.
		if nextPageToken == "" || len(threads) == 0 {
			break
		}

		pageToken = nextPageToken
	}

	if totalSkipped > 0 {
		slog.Info("Thread batch processing completed", "messages_retrieved", len(allMessages), "skipped", totalSkipped)
	}

	return allMessages, nil
}

// getThreadBatch retrieves a single batch of threads with optimizations.
func (s *Service) getThreadBatch(
	since time.Time,
	batchSize int,
	pageToken string,
	_ time.Duration,
) ([]*gmail.Thread, string, int, error) {
	query := s.BuildQuery(since)

	// List threads for this batch.
	req := s.service.Users.Threads.List("me").Q(query).MaxResults(int64(batchSize))
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}

	resp, err := s.executeWithRetry(func() (interface{}, error) {
		return req.Do()
	})
	if err != nil {
		return nil, "", 0, fmt.Errorf("unable to list thread batch: %w", err)
	}

	listResp := resp.(*gmail.ListThreadsResponse)
	if len(listResp.Threads) == 0 {
		return []*gmail.Thread{}, "", 0, nil
	}

	// Fetch full thread details with concurrent processing.
	threads, skippedCount := s.fetchThreadsConcurrently(listResp.Threads)

	return threads, listResp.NextPageToken, skippedCount, nil
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

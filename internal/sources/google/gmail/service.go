package gmail

import (
	"context"
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

	slog.Info("Gmail Threads API response", "source_id", s.sourceID, "threads_found", len(listResp.Threads), "query", query)

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
func (s *Service) fetchThreadsConcurrently(threadList []*gmail.Thread) ([]*gmail.Thread, int) {
	maxWorkers := defaultConcurrentWorkers
	if s.config.RequestDelay > highDelayThreshold {
		maxWorkers = throttledConcurrentWorkers
	}

	threadChan := make(chan *gmail.Thread, len(threadList))
	resultChan := make(chan *gmail.Thread, len(threadList))
	errorChan := make(chan error, len(threadList))

	var skippedCount int32

	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			for thread := range threadChan {
				if s.config.RequestDelay > 0 {
					time.Sleep(s.config.RequestDelay)
				}

				fullThread, err := s.GetThread(thread.Id)
				if err != nil {
					slog.Warn("Worker failed to get thread", "worker_id", workerID, "thread_id", thread.Id, "error", err)
					atomic.AddInt32(&skippedCount, 1)

					errorChan <- err
				} else {
					resultChan <- fullThread
				}
			}
		}(i)
	}

	go func() {
		for _, thread := range threadList {
			threadChan <- thread
		}

		close(threadChan)
	}()

	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()

	var threads []*gmail.Thread

	for {
		select {
		case thread, ok := <-resultChan:
			if !ok {
				resultChan = nil
			} else {
				threads = append(threads, thread)
			}
		case _, ok := <-errorChan:
			if !ok {
				errorChan = nil
			}
		}

		if resultChan == nil && errorChan == nil {
			break
		}
	}

	return threads, int(atomic.LoadInt32(&skippedCount))
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

// fetchMessagesConcurrently fetches messages concurrently with rate limiting.
func (s *Service) fetchMessagesConcurrently(messageList []*gmail.Message) ([]*gmail.Message, int) {
	// Configure concurrency based on configuration and rate limiting needs.
	maxWorkers := defaultConcurrentWorkers
	if s.config.RequestDelay > highDelayThreshold {
		// If delay is high, reduce concurrency.
		maxWorkers = throttledConcurrentWorkers
	}

	// Create channels for work distribution.
	messageChan := make(chan *gmail.Message, len(messageList))
	resultChan := make(chan *gmail.Message, len(messageList))
	errorChan := make(chan error, len(messageList))

	// Use atomic counter to avoid data race.
	var skippedCount int32

	// Start workers.
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			for msg := range messageChan {
				// Apply rate limiting per worker.
				if s.config.RequestDelay > 0 {
					time.Sleep(s.config.RequestDelay)
				}

				fullMessage, err := s.GetMessageWithRetry(msg.Id)
				if err != nil {
					slog.Warn("Worker failed to get message", "worker_id", workerID, "message_id", msg.Id, "error", err)
					atomic.AddInt32(&skippedCount, 1)

					errorChan <- err
				} else {
					resultChan <- fullMessage
				}
			}
		}(i)
	}

	// Send work to workers.
	go func() {
		for _, msg := range messageList {
			messageChan <- msg
		}

		close(messageChan)
	}()

	// Wait for workers to complete.
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()

	// Collect results.
	var messages []*gmail.Message

	// Collect all results.
	for {
		select {
		case msg, ok := <-resultChan:
			if !ok {
				resultChan = nil
			} else {
				messages = append(messages, msg)
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

	return messages, int(atomic.LoadInt32(&skippedCount))
}

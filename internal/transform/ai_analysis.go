package transform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

const (
	transformerNameAIAnalysis = "ai_analysis"

	// Metadata keys for AI analysis results.
	metaKeyAISummary     = "ai_summary"
	metaKeyAIPriority    = "ai_priority_score"
	metaKeyAIActionItems = "ai_action_items"
	metaKeyAITags        = "ai_tags"

	defaultRetryAttempts = 3
	defaultRetryDelay    = time.Second
	defaultTimeout       = 30 * time.Second
	defaultBatchSize     = 10
)

// Default prompt strings broken into variables to keep line length under the lll limit.
var (
	defaultPromptPrioritize = "Rate this content's importance from 0 to 1 where 1 is most urgent." +
		" Respond with only a number: {content}"

	defaultPromptExtractActions = "List action items from this content as a JSON array of strings." +
		` Respond with only valid JSON, example: ["action 1","action 2"]. Content: {content}`
)

// AIBackend defines the interface for AI inference backends.
type AIBackend interface {
	// Complete sends a prompt and returns the text completion.
	Complete(ctx context.Context, prompt string) (string, error)
}

// CLIBackend calls an external command (ollama, ramalama, etc.) for inference.
type CLIBackend struct {
	command string
	timeout time.Duration
}

// NewCLIBackend creates a CLIBackend from a command string (e.g., "ollama run llama3.2:3b").
func NewCLIBackend(command string, timeout time.Duration) *CLIBackend {
	return &CLIBackend{command: command, timeout: timeout}
}

// Complete runs the CLI command with the prompt piped to stdin.
func (b *CLIBackend) Complete(ctx context.Context, prompt string) (string, error) {
	if b.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	parts := strings.Fields(b.command)
	if len(parts) == 0 {
		return "", fmt.Errorf("ai_analysis: empty CLI command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...) //nolint:gosec // user-configured command
	cmd.Stdin = strings.NewReader(prompt)

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("ai_analysis: CLI command timed out: %w", ctx.Err())
		}

		return "", fmt.Errorf("ai_analysis: CLI command failed: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// HTTPBackend calls an OpenAI-compatible chat completions endpoint.
type HTTPBackend struct {
	url     string
	headers map[string]string
	model   string
	timeout time.Duration
	client  *http.Client
}

// NewHTTPBackend creates an HTTPBackend for OpenAI-compatible APIs.
func NewHTTPBackend(url string, headers map[string]string, model string, timeout time.Duration) *HTTPBackend {
	return &HTTPBackend{
		url:     url,
		headers: headers,
		model:   model,
		timeout: timeout,
		client:  &http.Client{},
	}
}

type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete sends a chat completion request to the configured HTTP endpoint.
func (b *HTTPBackend) Complete(ctx context.Context, prompt string) (string, error) {
	if b.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	reqBody := openAIChatRequest{
		Model: b.model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ai_analysis: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("ai_analysis: failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	for k, v := range b.headers {
		req.Header.Set(k, v)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("ai_analysis: HTTP request timed out: %w", ctx.Err())
		}

		return "", fmt.Errorf("ai_analysis: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ai_analysis: failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai_analysis: HTTP error %d: %s", resp.StatusCode, string(body))
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("ai_analysis: failed to decode response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("ai_analysis: API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("ai_analysis: no choices in response")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}

// AIPrompts holds configurable prompt templates. Use {content} as the placeholder.
type AIPrompts struct {
	Summarize      string `json:"summarize"       yaml:"summarize"`
	Prioritize     string `json:"prioritize"      yaml:"prioritize"`
	ExtractActions string `json:"extract_actions" yaml:"extract_actions"`
}

func defaultPrompts() AIPrompts {
	return AIPrompts{
		Summarize:      "Summarize this content in 2-3 sentences: {content}",
		Prioritize:     defaultPromptPrioritize,
		ExtractActions: defaultPromptExtractActions,
	}
}

// AIAnalysisTransformer enriches items with AI-generated summary, priority score,
// and action items. Results are stored in item metadata so the FullItem interface
// is not modified.
type AIAnalysisTransformer struct {
	backend       AIBackend
	prompts       AIPrompts
	retryAttempts int
	retryDelay    time.Duration
	batchSize     int
	onFailure     string // "log_and_continue", "fail_fast", "skip_item"
	enabled       bool
	config        map[string]interface{}
}

// NewAIAnalysisTransformer creates an AIAnalysisTransformer with no backend configured.
// Call Configure with a valid config map before use.
func NewAIAnalysisTransformer() *AIAnalysisTransformer {
	return &AIAnalysisTransformer{
		prompts:       defaultPrompts(),
		retryAttempts: defaultRetryAttempts,
		retryDelay:    defaultRetryDelay,
		batchSize:     defaultBatchSize,
		onFailure:     errorStrategyLogAndContinue,
		enabled:       false,
		config:        make(map[string]interface{}),
	}
}

func (t *AIAnalysisTransformer) Name() string {
	return transformerNameAIAnalysis
}

// Configure parses the ai_analysis transformer config block.
//
// Supported keys:
//
//	backend: "cli" | "http"
//	cli.command: string (e.g. "ollama run llama3.2:3b")
//	cli.timeout: string duration (e.g. "30s")
//	http.url: string
//	http.headers: map[string]string
//	http.model: string
//	http.timeout: string duration
//	prompts.summarize / prompts.prioritize / prompts.extract_actions: string with {content}
//	retry_attempts: int
//	retry_delay: string duration
//	batch_size: int
//	on_failure: "log_and_continue" | "fail_fast" | "skip_item"
func (t *AIAnalysisTransformer) Configure(config map[string]interface{}) error {
	t.config = config

	backendType, _ := config["backend"].(string)
	if backendType == "" {
		// No backend configured — transformer is a no-op (graceful degradation).
		t.enabled = false

		return nil
	}

	switch backendType {
	case "cli":
		backend, err := t.buildCLIBackend(config)
		if err != nil {
			return err
		}

		t.backend = backend
	case "http":
		backend, err := t.buildHTTPBackend(config)
		if err != nil {
			return err
		}

		t.backend = backend
	default:
		return fmt.Errorf("ai_analysis: unknown backend %q (must be 'cli' or 'http')", backendType)
	}

	t.prompts = t.parsePrompts(config)
	t.retryAttempts = t.intConfig(config, "retry_attempts", defaultRetryAttempts)
	t.retryDelay = t.durationConfig(config, "retry_delay", defaultRetryDelay)
	t.batchSize = t.intConfig(config, "batch_size", defaultBatchSize)

	if onFailure, ok := config["on_failure"].(string); ok {
		t.onFailure = onFailure
	}

	t.enabled = true

	return nil
}

// Transform processes items through AI analysis in batches.
// When the AI backend is unavailable, items pass through unmodified (graceful degradation).
func (t *AIAnalysisTransformer) Transform(items []models.FullItem) ([]models.FullItem, error) {
	if !t.enabled || t.backend == nil {
		return items, nil
	}

	result := make([]models.FullItem, 0, len(items))

	for start := 0; start < len(items); start += t.batchSize {
		end := start + t.batchSize
		if end > len(items) {
			end = len(items)
		}

		batch := items[start:end]

		processed, err := t.processBatch(batch)
		if err != nil {
			switch t.onFailure {
			case errorStrategyFailFast:
				return nil, err
			case errorStrategySkipItem:
				log.Printf("Warning: ai_analysis batch failed, skipping %d items: %v", len(batch), err)

				continue
			default: // log_and_continue
				log.Printf("Warning: ai_analysis batch failed, passing items through unchanged: %v", err)

				result = append(result, batch...)

				continue
			}
		}

		result = append(result, processed...)
	}

	return result, nil
}

// processBatch runs AI analysis on a slice of items.
// For skip_item and fail_fast failure modes, an error is returned so the caller
// can skip or abort the entire batch. For log_and_continue, failed items are
// kept as-is and no error is returned.
func (t *AIAnalysisTransformer) processBatch(items []models.FullItem) ([]models.FullItem, error) {
	result := make([]models.FullItem, 0, len(items))

	for _, item := range items {
		enriched, err := t.analyzeItem(item)
		if err != nil {
			switch t.onFailure {
			case errorStrategyFailFast:
				return nil, fmt.Errorf("ai_analysis: item %q failed: %w", item.GetID(), err)
			case errorStrategySkipItem:
				// Return error so the batch-level handler skips the whole batch.
				return nil, fmt.Errorf("ai_analysis: item %q failed: %w", item.GetID(), err)
			default: // log_and_continue
				log.Printf("Warning: ai_analysis item %q failed, keeping original: %v", item.GetID(), err)
				result = append(result, item)

				continue
			}
		}

		result = append(result, enriched)
	}

	return result, nil
}

// analyzeItem runs all configured AI prompts against a single item.
func (t *AIAnalysisTransformer) analyzeItem(item models.FullItem) (models.FullItem, error) {
	content := item.GetContent()
	if strings.TrimSpace(content) == "" {
		// Nothing to analyze — return as-is.
		return item, nil
	}

	ctx := context.Background()

	// Collect metadata updates.
	extra := make(map[string]interface{})

	if t.prompts.Summarize != "" {
		summary, err := t.completeWithRetry(ctx, t.buildPrompt(t.prompts.Summarize, content))
		if err != nil {
			return nil, fmt.Errorf("summarize: %w", err)
		}

		extra[metaKeyAISummary] = summary
	}

	if t.prompts.Prioritize != "" {
		priorityStr, err := t.completeWithRetry(ctx, t.buildPrompt(t.prompts.Prioritize, content))
		if err != nil {
			return nil, fmt.Errorf("prioritize: %w", err)
		}

		extra[metaKeyAIPriority] = parsePriorityScore(priorityStr)
	}

	if t.prompts.ExtractActions != "" {
		actionsStr, err := t.completeWithRetry(ctx, t.buildPrompt(t.prompts.ExtractActions, content))
		if err != nil {
			return nil, fmt.Errorf("extract_actions: %w", err)
		}

		extra[metaKeyAIActionItems] = parseActionItems(actionsStr)
	}

	return withMetadata(item, extra), nil
}

// completeWithRetry calls the backend with exponential backoff.
func (t *AIAnalysisTransformer) completeWithRetry(ctx context.Context, prompt string) (string, error) {
	var lastErr error

	for attempt := 0; attempt < t.retryAttempts; attempt++ {
		if attempt > 0 {
			delay := t.retryDelay * time.Duration(1<<uint(attempt-1))
			time.Sleep(delay)
		}

		result, err := t.backend.Complete(ctx, prompt)
		if err == nil {
			return result, nil
		}

		lastErr = err
	}

	return "", fmt.Errorf("failed after %d attempts: %w", t.retryAttempts, lastErr)
}

// buildPrompt substitutes {content} in the template.
func (t *AIAnalysisTransformer) buildPrompt(template, content string) string {
	return strings.ReplaceAll(template, "{content}", content)
}

// withMetadata returns a copy of item with extra metadata merged in.
func withMetadata(item models.FullItem, extra map[string]interface{}) models.FullItem {
	existing := item.GetMetadata()
	merged := make(map[string]interface{}, len(existing)+len(extra))

	for k, v := range existing {
		merged[k] = v
	}

	for k, v := range extra {
		merged[k] = v
	}

	// Clone the item and set the new metadata.
	cloned := cloneFullItem(item)
	cloned.SetMetadata(merged)

	return cloned
}

// cloneFullItem creates a shallow copy of the item preserving its concrete type.
func cloneFullItem(item models.FullItem) models.FullItem {
	if thread, ok := models.AsThread(item); ok {
		newThread := models.NewThread(thread.GetID(), thread.GetTitle())
		newThread.SetContent(thread.GetContent())
		newThread.SetSourceType(thread.GetSourceType())
		newThread.SetItemType(thread.GetItemType())
		newThread.SetCreatedAt(thread.GetCreatedAt())
		newThread.SetUpdatedAt(thread.GetUpdatedAt())
		newThread.SetTags(thread.GetTags())
		newThread.SetAttachments(thread.GetAttachments())
		newThread.SetMetadata(thread.GetMetadata())
		newThread.SetLinks(thread.GetLinks())
		newThread.SetMessages(thread.GetMessages())

		return newThread
	}

	newItem := models.NewBasicItem(item.GetID(), item.GetTitle())
	newItem.SetContent(item.GetContent())
	newItem.SetSourceType(item.GetSourceType())
	newItem.SetItemType(item.GetItemType())
	newItem.SetCreatedAt(item.GetCreatedAt())
	newItem.SetUpdatedAt(item.GetUpdatedAt())
	newItem.SetTags(item.GetTags())
	newItem.SetAttachments(item.GetAttachments())
	newItem.SetMetadata(item.GetMetadata())
	newItem.SetLinks(item.GetLinks())

	return newItem
}

// parsePriorityScore parses the model's response into a float64 in [0,1].
func parsePriorityScore(s string) float64 {
	s = strings.TrimSpace(s)

	var score float64

	if _, err := fmt.Sscanf(s, "%f", &score); err != nil {
		return 0
	}

	if score < 0 {
		return 0
	}

	if score > 1 {
		return 1
	}

	return score
}

// parseActionItems parses a JSON array of strings from the model's response.
func parseActionItems(s string) []string {
	s = strings.TrimSpace(s)

	// Find the first '[' to be tolerant of leading prose.
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")

	if start == -1 || end == -1 || end <= start {
		if s != "" {
			return []string{s}
		}

		return []string{}
	}

	jsonArray := s[start : end+1]

	var items []string
	if err := json.Unmarshal([]byte(jsonArray), &items); err != nil {
		return []string{s}
	}

	return items
}

// --- Config helpers ---

func (t *AIAnalysisTransformer) buildCLIBackend(config map[string]interface{}) (*CLIBackend, error) {
	cliCfg, _ := config["cli"].(map[string]interface{})
	if cliCfg == nil {
		return nil, fmt.Errorf("ai_analysis: 'cli' config block required for CLI backend")
	}

	command, _ := cliCfg["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("ai_analysis: cli.command is required")
	}

	timeout := t.durationConfig(cliCfg, "timeout", defaultTimeout)

	return NewCLIBackend(command, timeout), nil
}

func (t *AIAnalysisTransformer) buildHTTPBackend(config map[string]interface{}) (*HTTPBackend, error) {
	httpCfg, _ := config["http"].(map[string]interface{})
	if httpCfg == nil {
		return nil, fmt.Errorf("ai_analysis: 'http' config block required for HTTP backend")
	}

	url, _ := httpCfg["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("ai_analysis: http.url is required")
	}

	model, _ := httpCfg["model"].(string)
	timeout := t.durationConfig(httpCfg, "timeout", defaultTimeout)

	headers := make(map[string]string)

	if rawHeaders, ok := httpCfg["headers"].(map[string]interface{}); ok {
		for k, v := range rawHeaders {
			if sv, ok := v.(string); ok {
				headers[k] = sv
			}
		}
	}

	return NewHTTPBackend(url, headers, model, timeout), nil
}

func (t *AIAnalysisTransformer) parsePrompts(config map[string]interface{}) AIPrompts {
	p := defaultPrompts()

	promptCfg, ok := config["prompts"].(map[string]interface{})
	if !ok {
		return p
	}

	if v, ok := promptCfg["summarize"].(string); ok && v != "" {
		p.Summarize = v
	}

	if v, ok := promptCfg["prioritize"].(string); ok && v != "" {
		p.Prioritize = v
	}

	if v, ok := promptCfg["extract_actions"].(string); ok && v != "" {
		p.ExtractActions = v
	}

	return p
}

func (t *AIAnalysisTransformer) intConfig(config map[string]interface{}, key string, defaultVal int) int {
	v, ok := config[key]
	if !ok {
		return defaultVal
	}

	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}

	return defaultVal
}

func (t *AIAnalysisTransformer) durationConfig(
	config map[string]interface{},
	key string,
	defaultVal time.Duration,
) time.Duration {
	s, ok := config[key].(string)
	if !ok || s == "" {
		return defaultVal
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}

	return d
}

// GetAISummary returns the AI-generated summary stored in item metadata.
func GetAISummary(item models.FullItem) string {
	v, _ := item.GetMetadata()[metaKeyAISummary].(string)

	return v
}

// GetAIPriorityScore returns the AI-generated priority score stored in item metadata.
func GetAIPriorityScore(item models.FullItem) float64 {
	v, _ := item.GetMetadata()[metaKeyAIPriority].(float64)

	return v
}

// GetAIActionItems returns the AI-generated action items stored in item metadata.
func GetAIActionItems(item models.FullItem) []string {
	v, _ := item.GetMetadata()[metaKeyAIActionItems].([]string)

	return v
}

// Ensure interface compliance.
var _ interfaces.Transformer = (*AIAnalysisTransformer)(nil)

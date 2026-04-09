package transform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pkm-sync/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBackend is a test AI backend that returns canned responses.
type mockBackend struct {
	response string
	err      error
	calls    int
}

func (m *mockBackend) Complete(_ context.Context, _ string) (string, error) {
	m.calls++

	return m.response, m.err
}

// errorAfterN fails for the first N calls then succeeds.
type errorAfterN struct {
	failCount int
	calls     int
	response  string
}

func (e *errorAfterN) Complete(_ context.Context, _ string) (string, error) {
	e.calls++
	if e.calls <= e.failCount {
		return "", fmt.Errorf("transient error %d", e.calls)
	}

	return e.response, nil
}

func makeItem(id, content string) models.FullItem {
	item := models.NewBasicItem(id, "Test Item "+id)
	item.SetContent(content)
	item.SetMetadata(map[string]interface{}{})

	return item
}

// --- AIAnalysisTransformer ---

func TestAIAnalysisTransformer_Name(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	assert.Equal(t, transformerNameAIAnalysis, tr.Name())
}

func TestAIAnalysisTransformer_DisabledByDefault(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	items := []models.FullItem{makeItem("1", "some content")}

	out, err := tr.Transform(items)
	require.NoError(t, err)
	require.Len(t, out, 1)
	// No metadata should be added when disabled.
	assert.Empty(t, GetAISummary(out[0]))
}

func TestAIAnalysisTransformer_Configure_NilBackend(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	err := tr.Configure(map[string]interface{}{
		// no backend key
	})
	require.NoError(t, err)
	assert.False(t, tr.enabled)
}

func TestAIAnalysisTransformer_Configure_UnknownBackend(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	err := tr.Configure(map[string]interface{}{
		"backend": "grpc",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown backend")
}

func TestAIAnalysisTransformer_Configure_CLIMissingCommand(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	err := tr.Configure(map[string]interface{}{
		"backend": "cli",
		"cli":     map[string]interface{}{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli.command")
}

func TestAIAnalysisTransformer_Configure_HTTPMissingURL(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	err := tr.Configure(map[string]interface{}{
		"backend": "http",
		"http":    map[string]interface{}{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http.url")
}

func TestAIAnalysisTransformer_TransformEnrichesItems(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 1
	tr.onFailure = "log_and_continue"

	// Use a custom backend that rotates responses.
	responses := []string{
		"This is a summary.",        // summarize
		"0.8",                       // prioritize
		`["buy milk","call Alice"]`, // extract_actions
	}
	tr.backend = &rotatingBackend{responses: responses}

	items := []models.FullItem{makeItem("item1", "Full content here.")}
	out, err := tr.Transform(items)
	require.NoError(t, err)
	require.Len(t, out, 1)

	assert.Equal(t, "This is a summary.", GetAISummary(out[0]))
	assert.InDelta(t, 0.8, GetAIPriorityScore(out[0]), 0.001)
	assert.Equal(t, []string{"buy milk", "call Alice"}, GetAIActionItems(out[0]))
}

func TestAIAnalysisTransformer_EmptyContentSkipped(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 1
	tr.backend = &mockBackend{response: "summary"}
	tr.prompts = defaultPrompts()

	item := makeItem("empty", "")
	out, err := tr.Transform([]models.FullItem{item})
	require.NoError(t, err)
	require.Len(t, out, 1)
	// No AI metadata should be added for empty content.
	assert.Empty(t, GetAISummary(out[0]))
}

func TestAIAnalysisTransformer_GracefulDegradationOnError(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 1
	tr.retryDelay = 0
	tr.onFailure = "log_and_continue"
	tr.backend = &mockBackend{err: fmt.Errorf("backend unavailable")}
	tr.prompts = defaultPrompts()

	item := makeItem("1", "content")
	out, err := tr.Transform([]models.FullItem{item})
	require.NoError(t, err)
	// Item passes through unchanged.
	require.Len(t, out, 1)
	assert.Equal(t, "content", out[0].GetContent())
}

func TestAIAnalysisTransformer_FailFast(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 1
	tr.retryDelay = 0
	tr.onFailure = "fail_fast"
	tr.backend = &mockBackend{err: fmt.Errorf("backend down")}
	tr.prompts = defaultPrompts()

	_, err := tr.Transform([]models.FullItem{makeItem("1", "content")})
	require.Error(t, err)
}

func TestAIAnalysisTransformer_SkipItem(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 1
	tr.retryDelay = 0
	tr.onFailure = "skip_item"
	tr.backend = &mockBackend{err: fmt.Errorf("backend down")}
	tr.prompts = defaultPrompts()

	// skip_item: batch is skipped entirely so result is empty.
	out, err := tr.Transform([]models.FullItem{makeItem("1", "content")})
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestAIAnalysisTransformer_RetryOnTransientError(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 3
	tr.retryDelay = time.Millisecond
	tr.onFailure = "fail_fast"
	tr.prompts = AIPrompts{Summarize: "Summarize: {content}"}

	backend := &errorAfterN{failCount: 2, response: "ok summary"}
	tr.backend = backend

	out, err := tr.Transform([]models.FullItem{makeItem("1", "content")})
	require.NoError(t, err)
	assert.Equal(t, "ok summary", GetAISummary(out[0]))
	assert.Equal(t, 3, backend.calls) // 2 failures + 1 success
}

func TestAIAnalysisTransformer_BatchProcessing(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 2
	tr.retryAttempts = 1
	tr.retryDelay = 0
	tr.onFailure = "log_and_continue"
	tr.prompts = AIPrompts{Summarize: "Summarize: {content}"}
	tr.backend = &mockBackend{response: "batch summary"}

	items := []models.FullItem{
		makeItem("1", "content1"),
		makeItem("2", "content2"),
		makeItem("3", "content3"),
	}

	out, err := tr.Transform(items)
	require.NoError(t, err)
	assert.Len(t, out, 3)

	for _, item := range out {
		assert.Equal(t, "batch summary", GetAISummary(item))
	}
}

func TestAIAnalysisTransformer_PreservesExistingMetadata(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 1
	tr.retryDelay = 0
	tr.onFailure = "fail_fast"
	tr.prompts = AIPrompts{Summarize: "Summarize: {content}"}
	tr.backend = &mockBackend{response: "summary"}

	item := makeItem("1", "content")
	item.SetMetadata(map[string]interface{}{"existing_key": "existing_value"})

	out, err := tr.Transform([]models.FullItem{item})
	require.NoError(t, err)

	meta := out[0].GetMetadata()
	assert.Equal(t, "existing_value", meta["existing_key"])
	assert.Equal(t, "summary", meta[metaKeyAISummary])
}

func TestAIAnalysisTransformer_ThreadItemPreserved(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	tr.enabled = true
	tr.batchSize = 10
	tr.retryAttempts = 1
	tr.retryDelay = 0
	tr.onFailure = "fail_fast"
	tr.prompts = AIPrompts{Summarize: "Summarize: {content}"}
	tr.backend = &mockBackend{response: "thread summary"}

	thread := models.NewThread("t1", "Thread Subject")
	thread.SetContent("thread content")
	thread.SetMetadata(map[string]interface{}{})

	out, err := tr.Transform([]models.FullItem{thread})
	require.NoError(t, err)
	require.Len(t, out, 1)

	_, isThread := models.AsThread(out[0])
	assert.True(t, isThread, "output should still be a Thread")
	assert.Equal(t, "thread summary", GetAISummary(out[0]))
}

// --- parsePriorityScore ---

func TestParsePriorityScore(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"0.8", 0.8},
		{"1", 1.0},
		{"0", 0.0},
		{"  0.5  ", 0.5},
		{"not a number", 0},
		{"1.5", 1.0}, // clamped
		{"-0.1", 0},  // clamped
		{"0.0", 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parsePriorityScore(tc.input)
			assert.InDelta(t, tc.expected, got, 0.001)
		})
	}
}

// --- parseActionItems ---

func TestParseActionItems(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "valid JSON array",
			input:    `["buy milk", "call Alice"]`,
			expected: []string{"buy milk", "call Alice"},
		},
		{
			name:     "JSON array with leading text",
			input:    "Here are the actions:\n[\"action 1\",\"action 2\"]",
			expected: []string{"action 1", "action 2"},
		},
		{
			name:     "empty array",
			input:    "[]",
			expected: []string{},
		},
		{
			name:     "plain text fallback",
			input:    "just a single action",
			expected: []string{"just a single action"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseActionItems(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- HTTPBackend ---

func TestHTTPBackend_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		resp := openAIChatResponse{
			Choices: []struct {
				Message openAIChatMessage `json:"message"`
			}{
				{Message: openAIChatMessage{Role: "assistant", Content: "test response"}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	backend := NewHTTPBackend(
		server.URL,
		map[string]string{"Authorization": "Bearer test-key"},
		"gpt-4o",
		5*time.Second,
	)

	result, err := backend.Complete(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "test response", result)
}

func TestHTTPBackend_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	backend := NewHTTPBackend(server.URL, nil, "model", 5*time.Second)
	_, err := backend.Complete(context.Background(), "prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestHTTPBackend_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	backend := NewHTTPBackend(server.URL, nil, "model", 50*time.Millisecond)
	_, err := backend.Complete(context.Background(), "prompt")
	require.Error(t, err)
}

// --- CLIBackend ---

func TestCLIBackend_Complete_Echo(t *testing.T) {
	backend := NewCLIBackend("echo hello", 5*time.Second)
	result, err := backend.Complete(context.Background(), "anything")
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestCLIBackend_InvalidCommand(t *testing.T) {
	backend := NewCLIBackend("nonexistent-command-xyz", 5*time.Second)
	_, err := backend.Complete(context.Background(), "prompt")
	require.Error(t, err)
}

func TestCLIBackend_EmptyCommand(t *testing.T) {
	backend := NewCLIBackend("", 5*time.Second)
	_, err := backend.Complete(context.Background(), "prompt")
	require.Error(t, err)
}

// --- Configure integration ---

func TestConfigure_CLIBackend(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	err := tr.Configure(map[string]interface{}{
		"backend": "cli",
		"cli": map[string]interface{}{
			"command": "echo hi",
			"timeout": "10s",
		},
		"retry_attempts": float64(2),
		"retry_delay":    "500ms",
		"batch_size":     float64(5),
		"on_failure":     "skip_item",
	})
	require.NoError(t, err)
	assert.True(t, tr.enabled)
	assert.Equal(t, 2, tr.retryAttempts)
	assert.Equal(t, 5, tr.batchSize)
	assert.Equal(t, "skip_item", tr.onFailure)
	assert.Equal(t, 500*time.Millisecond, tr.retryDelay)
}

func TestConfigure_HTTPBackend(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	err := tr.Configure(map[string]interface{}{
		"backend": "http",
		"http": map[string]interface{}{
			"url":   "https://api.example.com/v1/chat/completions",
			"model": "gpt-4o",
			"headers": map[string]interface{}{
				"Authorization": "Bearer sk-test",
			},
			"timeout": "30s",
		},
		"prompts": map[string]interface{}{
			"summarize":       "Custom summarize: {content}",
			"prioritize":      "Custom prioritize: {content}",
			"extract_actions": "Custom actions: {content}",
		},
	})
	require.NoError(t, err)
	assert.True(t, tr.enabled)
	assert.Equal(t, "Custom summarize: {content}", tr.prompts.Summarize)
	assert.Equal(t, "Custom prioritize: {content}", tr.prompts.Prioritize)
	assert.Equal(t, "Custom actions: {content}", tr.prompts.ExtractActions)
}

// --- Helper accessors ---

func TestGetAIHelpers_Empty(t *testing.T) {
	item := makeItem("1", "content")

	assert.Equal(t, "", GetAISummary(item))
	assert.Equal(t, 0.0, GetAIPriorityScore(item))
	assert.Nil(t, GetAIActionItems(item))
}

// --- rotatingBackend helper ---

type rotatingBackend struct {
	responses []string
	idx       int
}

func (r *rotatingBackend) Complete(_ context.Context, _ string) (string, error) {
	if r.idx >= len(r.responses) {
		return "", fmt.Errorf("no more responses")
	}

	resp := r.responses[r.idx]
	r.idx++

	return resp, nil
}

// --- buildPrompt ---

func TestBuildPrompt(t *testing.T) {
	tr := NewAIAnalysisTransformer()
	result := tr.buildPrompt("Summarize: {content}", "hello world")
	assert.Equal(t, "Summarize: hello world", result)
	assert.False(t, strings.Contains(result, "{content}"))
}

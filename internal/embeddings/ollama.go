package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaProvider implements the Provider interface for Ollama.
type OllamaProvider struct {
	apiURL     string
	model      string
	dimensions int
	client     *http.Client
}

// NewOllamaProvider creates a new Ollama embedding provider.
func NewOllamaProvider(apiURL, model string, dimensions int) *OllamaProvider {
	return &OllamaProvider{
		apiURL:     apiURL,
		model:      model,
		dimensions: dimensions,
		client:     &http.Client{},
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Input  string `json:"input"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Embedding  []float64   `json:"embedding"`
}

// Embed generates an embedding for a single text input with retry logic.
func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	const maxRetries = 3

	const baseDelay = 500 * time.Millisecond

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			time.Sleep(delay)
		}

		embedding, err := p.embedWithoutRetry(ctx, text)
		if err == nil {
			return embedding, nil
		}

		lastErr = err
		// Only retry on transient errors (EOF, empty response)
		// Don't retry on 4xx errors or decode failures
		if !isRetriableError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// embedWithoutRetry performs a single embedding request without retry logic.
func (p *OllamaProvider) embedWithoutRetry(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:  p.model,
		Input:  text,
		Prompt: text,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL+"/api/embed", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Ollama returns embeddings in "embeddings" array or "embedding" field
	var sourceEmbedding []float64
	if len(embedResp.Embeddings) > 0 && len(embedResp.Embeddings[0]) > 0 {
		sourceEmbedding = embedResp.Embeddings[0]
	} else if len(embedResp.Embedding) > 0 {
		sourceEmbedding = embedResp.Embedding
	} else {
		return nil, fmt.Errorf("empty embedding returned from Ollama")
	}

	// Convert float64 to float32
	embedding := make([]float32, len(sourceEmbedding))
	for i, v := range sourceEmbedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// isRetriableError determines if an error should be retried.
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Retry on EOF, connection errors, empty responses, and 500 errors
	return containsStr(errStr, "EOF") ||
		containsStr(errStr, "connection") ||
		containsStr(errStr, "empty embedding") ||
		containsStr(errStr, "status 500")
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

// EmbedBatch generates embeddings for multiple text inputs.
func (p *OllamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := p.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text at index %d: %w", i, err)
		}

		embeddings[i] = embedding
	}

	return embeddings, nil
}

// Dimensions returns the dimensionality of the embeddings.
func (p *OllamaProvider) Dimensions() int {
	return p.dimensions
}

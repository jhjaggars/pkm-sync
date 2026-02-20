package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIProvider implements the Provider interface for OpenAI.
type OpenAIProvider struct {
	apiURL     string
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

// NewOpenAIProvider creates a new OpenAI embedding provider.
func NewOpenAIProvider(apiURL, apiKey, model string, dimensions int) *OpenAIProvider {
	return &OpenAIProvider{
		apiURL:     apiURL,
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		client:     &http.Client{},
	}
}

type openAIEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// Embed generates an embedding for a single text input.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple text inputs.
func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := openAIEmbedRequest{
		Model:      p.model,
		Input:      texts,
		Dimensions: p.dimensions,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL+"/v1/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp openAIEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(embedResp.Data) == 0 {
		return nil, fmt.Errorf("empty embeddings returned from OpenAI")
	}

	// Convert to [][]float32 and sort by index
	embeddings := make([][]float32, len(embedResp.Data))
	for _, item := range embedResp.Data {
		embedding := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			embedding[i] = float32(v)
		}

		embeddings[item.Index] = embedding
	}

	return embeddings, nil
}

// Dimensions returns the dimensionality of the embeddings.
func (p *OpenAIProvider) Dimensions() int {
	return p.dimensions
}

// Close closes any idle HTTP connections.
func (p *OpenAIProvider) Close() error {
	if transport, ok := p.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	return nil
}

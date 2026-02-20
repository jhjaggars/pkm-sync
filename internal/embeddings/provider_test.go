package embeddings

import (
	"testing"

	"pkm-sync/pkg/models"
)

func TestNewProvider_Ollama(t *testing.T) {
	cfg := models.EmbeddingsConfig{
		Provider:   "ollama",
		Model:      "nomic-embed-text",
		APIURL:     "http://localhost:11434",
		Dimensions: 768,
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}

	if provider.Dimensions() != 768 {
		t.Errorf("expected dimensions 768, got %d", provider.Dimensions())
	}
}

func TestNewProvider_OpenAI(t *testing.T) {
	cfg := models.EmbeddingsConfig{
		Provider:   "openai",
		Model:      "text-embedding-3-small",
		APIURL:     "https://api.openai.com",
		APIKey:     "test-key",
		Dimensions: 1536,
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}

	if provider.Dimensions() != 1536 {
		t.Errorf("expected dimensions 1536, got %d", provider.Dimensions())
	}
}

func TestNewProvider_UnsupportedProvider(t *testing.T) {
	cfg := models.EmbeddingsConfig{
		Provider:   "unsupported",
		Model:      "test",
		Dimensions: 768,
	}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestNewProvider_MissingModel(t *testing.T) {
	cfg := models.EmbeddingsConfig{
		Provider:   "ollama",
		Dimensions: 768,
	}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestNewProvider_MissingDimensions(t *testing.T) {
	cfg := models.EmbeddingsConfig{
		Provider: "ollama",
		Model:    "nomic-embed-text",
	}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing dimensions")
	}
}

func TestNewProvider_OpenAIMissingAPIKey(t *testing.T) {
	cfg := models.EmbeddingsConfig{
		Provider:   "openai",
		Model:      "text-embedding-3-small",
		Dimensions: 1536,
	}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

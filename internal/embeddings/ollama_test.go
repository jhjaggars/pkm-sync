package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Embed(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("expected path /api/embed, got %s", r.URL.Path)
		}

		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Model != "test-model" {
			t.Errorf("expected model test-model, got %s", req.Model)
		}

		resp := ollamaEmbedResponse{
			Embedding: []float64{0.1, 0.2, 0.3},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "test-model", 3)

	embedding, err := provider.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(embedding) != 3 {
		t.Errorf("expected embedding length 3, got %d", len(embedding))
	}

	expected := []float32{0.1, 0.2, 0.3}
	for i, v := range expected {
		if embedding[i] != v {
			t.Errorf("expected embedding[%d] = %f, got %f", i, v, embedding[i])
		}
	}
}

func TestOllamaProvider_EmbedBatch(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := ollamaEmbedResponse{
			Embedding: []float64{float64(callCount), float64(callCount + 1), float64(callCount + 2)},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "test-model", 3)

	embeddings, err := provider.EmbedBatch(context.Background(), []string{"text1", "text2"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(embeddings))
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestOllamaProvider_Embed_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "test-model", 3)

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestOllamaProvider_Dimensions(t *testing.T) {
	provider := NewOllamaProvider("http://localhost:11434", "test-model", 768)
	if provider.Dimensions() != 768 {
		t.Errorf("expected dimensions 768, got %d", provider.Dimensions())
	}
}

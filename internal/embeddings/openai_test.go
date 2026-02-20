package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProvider_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("expected path /v1/embeddings, got %s", r.URL.Path)
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-key" {
			t.Errorf("expected Authorization header 'Bearer test-key', got '%s'", authHeader)
		}

		var req openAIEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Model != "test-model" {
			t.Errorf("expected model test-model, got %s", req.Model)
		}

		resp := openAIEmbedResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{
					Embedding: []float64{0.1, 0.2, 0.3},
					Index:     0,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "test-model", 3)

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

func TestOpenAIProvider_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if len(req.Input) != 2 {
			t.Errorf("expected 2 input texts, got %d", len(req.Input))
		}

		resp := openAIEmbedResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{
					Embedding: []float64{0.1, 0.2, 0.3},
					Index:     0,
				},
				{
					Embedding: []float64{0.4, 0.5, 0.6},
					Index:     1,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "test-model", 3)

	embeddings, err := provider.EmbedBatch(context.Background(), []string{"text1", "text2"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(embeddings))
	}

	expected := [][]float32{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}
	for i, exp := range expected {
		for j, v := range exp {
			if embeddings[i][j] != v {
				t.Errorf("expected embeddings[%d][%d] = %f, got %f", i, j, v, embeddings[i][j])
			}
		}
	}
}

func TestOpenAIProvider_Embed_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "test-model", 3)

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for unauthorized response")
	}
}

func TestOpenAIProvider_Dimensions(t *testing.T) {
	provider := NewOpenAIProvider("https://api.openai.com", "test-key", "test-model", 1536)
	if provider.Dimensions() != 1536 {
		t.Errorf("expected dimensions 1536, got %d", provider.Dimensions())
	}
}

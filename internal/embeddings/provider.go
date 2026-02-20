package embeddings

import "context"

// Provider defines the interface for embedding providers.
type Provider interface {
	// Embed generates an embedding for a single text input.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple text inputs.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of the embeddings.
	Dimensions() int

	// Close closes any resources held by the provider.
	Close() error
}

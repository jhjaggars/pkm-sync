package embeddings

import (
	"fmt"

	"pkm-sync/pkg/models"
)

// NewProvider creates a new embedding provider based on the configuration.
func NewProvider(cfg models.EmbeddingsConfig) (Provider, error) {
	switch cfg.Provider {
	case "ollama":
		if cfg.APIURL == "" {
			cfg.APIURL = "http://localhost:11434"
		}

		if cfg.Model == "" {
			return nil, fmt.Errorf("model is required for ollama provider")
		}

		if cfg.Dimensions == 0 {
			return nil, fmt.Errorf("dimensions is required for ollama provider")
		}

		return NewOllamaProvider(cfg.APIURL, cfg.Model, cfg.Dimensions), nil

	case "openai":
		if cfg.APIURL == "" {
			cfg.APIURL = "https://api.openai.com"
		}

		if cfg.APIKey == "" {
			return nil, fmt.Errorf("api_key is required for openai provider")
		}

		if cfg.Model == "" {
			return nil, fmt.Errorf("model is required for openai provider")
		}

		if cfg.Dimensions == 0 {
			return nil, fmt.Errorf("dimensions is required for openai provider")
		}

		return NewOpenAIProvider(cfg.APIURL, cfg.APIKey, cfg.Model, cfg.Dimensions), nil

	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}
}

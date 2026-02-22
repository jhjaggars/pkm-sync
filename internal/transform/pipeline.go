package transform

import (
	"fmt"
	"log"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// DefaultTransformPipeline implements the TransformPipeline interface using FullItem.
type DefaultTransformPipeline struct {
	transformers        []interfaces.Transformer
	config              models.TransformConfig
	transformerRegistry map[string]interfaces.Transformer
}

// NewPipeline creates a new transform pipeline using FullItem.
func NewPipeline() *DefaultTransformPipeline {
	return &DefaultTransformPipeline{
		transformers:        make([]interfaces.Transformer, 0),
		transformerRegistry: make(map[string]interfaces.Transformer),
	}
}

// Configure sets up the pipeline based on configuration.
func (p *DefaultTransformPipeline) Configure(config models.TransformConfig) error {
	p.config = config

	if !config.Enabled {
		return nil
	}

	// Clear existing transformers
	p.transformers = make([]interfaces.Transformer, 0)

	// Validate no duplicate transformers in pipeline order
	seenTransformers := make(map[string]bool)
	for _, name := range config.PipelineOrder {
		if seenTransformers[name] {
			return fmt.Errorf("duplicate transformer '%s' found in pipeline_order", name)
		}

		seenTransformers[name] = true
	}

	// Add transformers in the specified order
	for _, name := range config.PipelineOrder {
		transformer, exists := p.transformerRegistry[name]
		if !exists {
			return fmt.Errorf("transformer '%s' not found in registry", name)
		}

		// Configure the transformer if config exists
		if transformerConfig, hasConfig := config.Transformers[name]; hasConfig {
			if err := transformer.Configure(transformerConfig); err != nil {
				return fmt.Errorf("failed to configure transformer '%s': %w", name, err)
			}
		}

		p.transformers = append(p.transformers, transformer)
	}

	return nil
}

// AddTransformer adds a transformer to the registry.
func (p *DefaultTransformPipeline) AddTransformer(transformer interfaces.Transformer) error {
	if transformer == nil {
		return fmt.Errorf("transformer cannot be nil")
	}

	name := transformer.Name()
	if name == "" {
		return fmt.Errorf("transformer name cannot be empty")
	}

	p.transformerRegistry[name] = transformer

	return nil
}

// Transform processes items through the configured pipeline.
func (p *DefaultTransformPipeline) Transform(items []models.FullItem) ([]models.FullItem, error) {
	if !p.config.Enabled || len(p.transformers) == 0 {
		return items, nil
	}

	currentItems := items

	for _, transformer := range p.transformers {
		transformedItems, err := p.processWithErrorHandling(transformer, currentItems)
		if err != nil {
			if err := p.handleTransformerError(transformer, currentItems, err); err != nil {
				return nil, err
			}
			// currentItems remains unchanged for log_and_continue, or becomes empty for skip_item
			if p.config.ErrorStrategy == "skip_item" {
				currentItems = []models.FullItem{}
			}
		} else {
			currentItems = transformedItems
		}
	}

	return currentItems, nil
}

// processWithErrorHandling wraps transformer execution with error handling.
func (p *DefaultTransformPipeline) processWithErrorHandling(
	transformer interfaces.Transformer,
	items []models.FullItem,
) (processedItems []models.FullItem, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Transformer '%s' panicked on batch of %d items: %v", transformer.Name(), len(items), r)
			err = fmt.Errorf("panic in transformer '%s': %v", transformer.Name(), r)
		}
	}()

	processedItems, err = transformer.Transform(items)

	return
}

// handleTransformerError handles transformer errors based on the configured strategy.
func (p *DefaultTransformPipeline) handleTransformerError(
	transformer interfaces.Transformer,
	items []models.FullItem,
	err error,
) error {
	switch p.config.ErrorStrategy {
	case "fail_fast":
		return fmt.Errorf("transformer '%s' failed: %w", transformer.Name(), err)
	case "log_and_continue":
		p.logTransformerError(transformer, items, err, "Continuing with previous items")
	case "skip_item":
		p.logTransformerError(transformer, items, err, "skipping this batch")
	default:
		return fmt.Errorf("unknown error strategy '%s'", p.config.ErrorStrategy)
	}

	return nil
}

// logTransformerError logs transformer errors with context.
func (p *DefaultTransformPipeline) logTransformerError(
	transformer interfaces.Transformer,
	items []models.FullItem,
	err error,
	action string,
) {
	itemIDs := p.getItemIDs(items)
	if len(itemIDs) > 0 && len(itemIDs) <= 5 { // Log item IDs for small batches
		log.Printf("Warning: transformer '%s' failed on items [%v]: %v. %s.",
			transformer.Name(), itemIDs, err, action)
	} else {
		log.Printf("Warning: transformer '%s' failed on batch of %d items: %v. %s.",
			transformer.Name(), len(items), err, action)
	}
}

// RegisterTransformer is a helper function to register transformers.
func (p *DefaultTransformPipeline) RegisterTransformer(transformer interfaces.Transformer) error {
	return p.AddTransformer(transformer)
}

// GetRegisteredTransformers returns a list of registered transformer names.
func (p *DefaultTransformPipeline) GetRegisteredTransformers() []string {
	names := make([]string, 0, len(p.transformerRegistry))
	for name := range p.transformerRegistry {
		names = append(names, name)
	}

	return names
}

// getItemIDs extracts item IDs for logging context.
func (p *DefaultTransformPipeline) getItemIDs(items []models.FullItem) []string {
	ids := make([]string, 0, len(items))

	for _, item := range items {
		if item.GetID() != "" {
			ids = append(ids, item.GetID())
		}
	}

	return ids
}

// Ensure DefaultTransformPipeline implements TransformPipeline.
var _ interfaces.TransformPipeline = (*DefaultTransformPipeline)(nil)

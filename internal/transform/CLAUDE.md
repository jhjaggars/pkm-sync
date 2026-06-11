# internal/transform/ — Transformer Pipeline

Configurable, chainable processing between source fetch and sink write.

## Core Types

- `Transformer` interface: `Transform([]models.FullItem) ([]models.FullItem, error)`
- `ContentTransformer` — modifies `[]models.CoreItem`
- `MetadataTransformer` — enriches `[]models.EnrichedItem`
- `TransformPipeline` — chains transformers; configurable error handling
- `GetAllContentProcessingTransformers()` — returns all 6 registered transformers (same as `GetAllExampleTransformers()`)

## Built-in Transformers

| Name | What it does |
|------|-------------|
| `content_cleanup` | HTML→Markdown, strip quoted text, normalize whitespace, remove "Re:"/"Fwd:" |
| `auto_tagging` | Add tags based on content patterns and source metadata |
| `filter` | Filter by content length, source type, required tags |
| `link_extraction` | Extract and index URLs from content |
| `signature_removal` | Remove email signatures |
| `thread_grouping` | Group related emails into conversation threads |

## Error Handling Strategies

- `fail_fast` — stop on first error
- `log_and_continue` — log errors, continue with original items
- `skip_item` — log errors, drop problematic items

## Configuration

```yaml
transformers:
  enabled: true
  pipeline_order: ["content_cleanup", "auto_tagging", "filter"]
  error_strategy: "log_and_continue"
  transformers:
    content_cleanup:
      strip_prefixes: true
    auto_tagging:
      rules:
        - pattern: "meeting"
          tags: ["work", "meeting"]
    filter:
      min_content_length: 50
      exclude_source_types: ["spam"]
      required_tags: ["important"]
```

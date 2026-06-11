# internal/sinks/ — Sink Implementations

## FileSink

`FileSink` owns all file-writing and formatting logic. It delegates to an unexported `formatter` interface.

```go
// Constructor
NewFileSink(formatterName, outputDir string, config map[string]any) (*FileSink, error)

// Methods
Write(ctx context.Context, items []models.FullItem) error
Preview(items []models.FullItem) ([]*interfaces.FilePreview, error)  // dry-run, no writes
```

Config YAML key: `targets:` (kept for backward compat).

### Formatters

| Name | File | Notes |
|------|------|-------|
| `"obsidian"` | `obsidian.go` | YAML frontmatter, wikilinks, thread-aware |
| `"logseq"` | `logseq.go` | Property blocks, space-preserving filename |

Factory: `newFormatter(name string) (formatter, error)` in `formatter.go`.

## VectorSink (`vector.go`)

Indexes items into SQLite-vec for semantic search. Groups by `"source:<name>"` tags + `thread_id` from metadata. Handles deduplication, rate limiting, content truncation internally. **Must call `Close()`** to release store + provider resources.

Source tagging (`MultiSyncOptions.SourceTags: true`) must be enabled for correct dedup.

## SlackArchiveSink

SQLite-backed sink for Slack message archiving with full-text search (FTS4).

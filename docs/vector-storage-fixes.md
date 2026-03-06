# Vector Storage Layer Fixes

## Overview

This document describes seven fixes applied to the vector storage layer in pkm-sync: two bug fixes, one architectural deduplication, one performance improvement, one code deduplication, one code quality fix, and one logging consistency fix.

---

## 1. Fix nil provider panic in `VectorSink.Close()`

**File:** `internal/sinks/vector.go`

**Problem:** `s.provider.Close()` was called unconditionally in `Close()`, but `s.provider` is `nil` when the sink runs in metadata-only mode (no embedding provider configured). This caused a nil pointer dereference panic.

**Fix:** Guard the `provider.Close()` call with `if s.provider != nil`, matching the pattern already used in `Write()` → `indexSource()`.

```go
// Before
if err := s.provider.Close(); err != nil {
    errs = append(errs, fmt.Sprintf("provider: %v", err))
}

// After
if s.provider != nil {
    if err := s.provider.Close(); err != nil {
        errs = append(errs, fmt.Sprintf("provider: %v", err))
    }
}
```

**Test:** `internal/sinks/vector_test.go` — `TestVectorSinkCloseNilProvider` constructs a `VectorSink` with `provider: nil` and asserts `Close()` returns without error or panic.

---

## 2. Fix `indexed` counter in metadata-only mode

**File:** `internal/sinks/vector.go`

**Problem:** The `indexed` counter only incremented when `len(embedding) > 0`. In metadata-only mode (no provider), every successful upsert produced a `0 indexed` summary, making the output misleading.

**Fix:** Track two counters. `indexSource` now returns five values instead of four:

```go
// Before
func (s *VectorSink) indexSource(...) (indexed, skipped, failed int, err error)

// After
func (s *VectorSink) indexSource(...) (indexed, metadataOnly, skipped, failed int, err error)
```

Successful upserts with an embedding increment `indexed`; successful upserts without an embedding increment `metadataOnly`. The summary log reports all four:

```
indexed=3 metadata_only=5 skipped=12 failed=0
```

---

## 3. Add `Search()` to `VectorSink` and deduplicate `cmd/search.go`

**Files:** `internal/sinks/vector.go`, `cmd/search.go`

**Problem:** `cmd/search.go` constructed its own `embeddings.Provider` and `vectorstore.Store` directly, duplicating the path resolution and initialization logic already present in `cmd/helpers.go:createVectorSink()`. `VectorSink` had no `Search()` method, so the command could not use the sink abstraction.

**Fix:** Add a concrete `Search()` method to `VectorSink` (not on the `Sink` interface — same pattern as the existing `Stats()` method):

```go
func (s *VectorSink) Search(
    ctx context.Context,
    query string,
    limit int,
    filters vectorstore.SearchFilters,
) ([]vectorstore.SearchResult, error) {
    if s.provider == nil {
        return nil, fmt.Errorf("search requires an embedding provider; none configured (metadata-only mode)")
    }
    queryEmbedding, err := s.provider.Embed(ctx, query)
    if err != nil {
        return nil, fmt.Errorf("failed to embed query: %w", err)
    }
    return s.store.Search(queryEmbedding, limit, filters)
}
```

`cmd/search.go` is refactored to call `createVectorSink(cfg)` and then `vectorSink.Search(...)`, removing the direct `embeddings` and `path/filepath` imports.

---

## 4. Use `EmbedBatch` for throughput

**Files:** `internal/sinks/vector.go`, `cmd/index.go`

**Problem:** Documents were embedded one at a time using `Embed()` with a `time.Sleep(delay)` between each call. `Provider.EmbedBatch()` existed and was implemented by the OpenAI provider as a single HTTP call for N texts, but was never used.

**Fix:** Add `BatchSize int` to `VectorSinkConfig`. The `indexSource` loop is restructured in two phases:

1. **Collect phase:** Iterate all groups, skip already-indexed ones, build content and `vectorstore.Document` values, accumulate into a `[]pendingDoc` slice.
2. **Process phase:** Loop through `pendingDoc` in slices of `BatchSize`:
   - `BatchSize == 1` (default): call `provider.Embed()` per document — identical to the original behavior.
   - `BatchSize > 1`: call `provider.EmbedBatch()` once per slice, apply the delay once per batch instead of per document.

```go
type VectorSinkConfig struct {
    // ...existing fields...
    BatchSize int // 0 or 1 = single-embed; >1 = batch via EmbedBatch
}
```

`cmd/index.go` adds a `--batch-size` flag (default `1`):

```
pkm-sync index --batch-size 20 --source gmail_work
```

The `sync` command's `createVectorSink()` in `helpers.go` is unchanged — it leaves `BatchSize` at zero (single-embed mode) since the sync command runs in metadata-only mode anyway.

---

## 5. Deduplicate path resolution in `cmd/index.go`

**File:** `cmd/index.go`

**Problem:** `runIndexCommand` contained an inline 8-line block that resolved the vector DB path by checking `cfg.VectorDB.DBPath` and falling back to `config.GetConfigDir()`. This duplicated `resolveVectorDBPath()` already defined in `cmd/helpers.go`.

**Fix:** Replace the inline block with a call to `resolveVectorDBPath(cfg)` and remove the now-unused `"path/filepath"` import.

```go
// Before (8 lines)
dbPath := cfg.VectorDB.DBPath
if dbPath == "" {
    configDir, err := config.GetConfigDir()
    if err != nil {
        return fmt.Errorf("failed to get config directory: %w", err)
    }
    dbPath = filepath.Join(configDir, "vectors.db")
}

// After (3 lines)
dbPath, err := resolveVectorDBPath(cfg)
if err != nil {
    return fmt.Errorf("failed to resolve vector DB path: %w", err)
}
```

The `index` command still calls `sinks.NewVectorSink()` directly (not `createVectorSink()`) because it passes custom `Reindex`, `Delay`, `MaxContentLen`, and `BatchSize` options that the sync-path helper does not expose.

---

## 6. Replace `containsStr` with `strings.Contains`

**File:** `internal/embeddings/ollama.go`

**Problem:** A hand-rolled `containsStr` function performed byte-by-byte substring search in `isRetriableError()`. The standard library's `strings.Contains` does the same thing with no behavioral difference.

**Fix:** Delete `containsStr`, add `"strings"` to the import block, replace all four call sites:

```go
// Before
return containsStr(errStr, "EOF") ||
    containsStr(errStr, "connection") ||
    containsStr(errStr, "empty embedding") ||
    containsStr(errStr, "status 500")

// After
return strings.Contains(errStr, "EOF") ||
    strings.Contains(errStr, "connection") ||
    strings.Contains(errStr, "empty embedding") ||
    strings.Contains(errStr, "status 500")
```

---

## 7. Switch `VectorSink` logging from `fmt.Printf` to `slog`

**File:** `internal/sinks/vector.go`

**Problem:** `VectorSink` used `fmt.Printf` and `fmt.Println` for all progress and warning output. Every other sink in the package (`ArchiveSink`, `SlackArchiveSink`) used `slog`. The inconsistency made log filtering and structured output impossible for the vector sink.

**Fix:** Replace all progress/warning prints with `slog.Info` or `slog.Warn` using structured key-value pairs. `fmt` is retained for `fmt.Errorf` and `fmt.Sprintf` in error paths.

| Original | Replacement |
|----------|-------------|
| `fmt.Println("Vector store: running in metadata-only mode...")` | `slog.Info("Vector store: running in metadata-only mode...")` |
| `fmt.Printf("Source %s: grouped %d items into %d groups\n", ...)` | `slog.Info("Source grouped", "source", ..., "items", ..., "groups", ...)` |
| `fmt.Printf("Source %s: already indexed: %d groups\n", ...)` | `slog.Info("Source already indexed", "source", ..., "count", ...)` |
| `fmt.Printf("Progress: %d indexed, ...\n", ...)` | `slog.Info("Indexing progress", "indexed", ..., ...)` |
| `fmt.Printf("Warning: Failed to embed group %s...\n", ...)` | `slog.Warn("Failed to embed document", "thread_id", ..., ...)` |
| `fmt.Printf("Warning: Failed to index group %s...\n", ...)` | `slog.Warn("Failed to index document", "thread_id", ..., ...)` |
| `fmt.Printf("Vector indexing complete: %d indexed...\n", ...)` | `slog.Info("Vector indexing complete", "indexed", ..., ...)` |

---

## Files Modified

| File | Fixes |
|------|-------|
| `internal/sinks/vector.go` | 1, 2, 3, 4, 7 |
| `internal/sinks/vector_test.go` | 1 (test) |
| `internal/embeddings/ollama.go` | 6 |
| `cmd/search.go` | 3 |
| `cmd/index.go` | 4, 5 |

---

## Verification

```bash
# Unit tests
go test ./internal/vectorstore/... ./internal/embeddings/... ./internal/sinks/...

# Build check
go build ./...

# Confirm --batch-size flag appears
./pkm-sync index --help | grep batch-size

# Full CI (lint + test + build)
make ci
```

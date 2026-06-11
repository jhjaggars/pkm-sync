# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Build & CI

```bash
go build -v ./...              # Build all packages (mirrors CI)
go build -o pkm-sync ./cmd     # Build named binary
make ci                        # Lint + test + build (must pass before completing tasks)
make test                      # Tests only
./scripts/install-hooks.sh     # Install pre-commit hooks (required after clone)
make check-golangci-version    # Verify golangci-lint v2.0+
```

CI checks: `golangci-lint run` (v2.0+ required), `go test ./... -race`, `go build -v ./...`

Common lint pitfalls: `wsl_v5` (blank lines before assignments), `misspell` (US English), `lll` (120 char line limit).

## Architecture

**Pipeline**: Sources → Transform → ResolveRefs → Sinks, orchestrated by `internal/sync.MultiSyncer.SyncAll()`.

| Layer | Package | Key type |
|-------|---------|---------|
| Interfaces | `pkg/interfaces/` | `Source`, `Sink`, `Transformer`, `Resolver` |
| Data model | `pkg/models/item.go` | `FullItem` (composed), `BasicItem`, `Thread` |
| Sources | `internal/sources/` | Gmail, Calendar, Drive, Jira, Slack, ServiceNow |
| Sinks | `internal/sinks/` | `FileSink` (Obsidian/Logseq), `VectorSink`, `SlackArchiveSink` |
| Transforms | `internal/transform/` | 6 built-in transformers, `TransformPipeline` |
| Sync engine | `internal/sync/` | `MultiSyncer` — concurrent source fetch, transform, sink fan-out |
| Resolve | `internal/resolve/` | Cross-source URL resolution (e.g. Jira link in Slack) |
| Config | `internal/config/config.go` | YAML config; docs in `CONFIGURATION.md` |
| Auth | `internal/keystore/` | System keyring or encrypted file fallback |
| State | `internal/state/` | Tracks active sub-items per source across runs |
| Archive | `internal/archive/` | SQLite FTS4 for Gmail full-text search |
| Vector | `internal/vectorstore/` | SQLite-vec for semantic search |
| Configure TUI | `internal/configure/` | Shared TUI logic for `configure` command |
| Utils | `internal/utils/` | Filename sanitization helpers |

**Data model hierarchy**: `CoreItem` (ID, title, content) → `SourcedItem` → `FullItem` (composed with TimestampedItem, EnrichedItem, SerializableItem).

## Core Interfaces (`pkg/interfaces/interfaces.go`)

```go
Source:    Name() string
           Configure(config map[string]interface{}, client *http.Client) error
           Fetch(since time.Time, limit int) ([]models.FullItem, error)
           SupportsRealtime() bool

Sink:      Name() string
           Write(ctx context.Context, items []models.FullItem) error

Transformer: Name() string
             Configure(config map[string]interface{}) error
             Transform(items []models.FullItem) ([]models.FullItem, error)

Resolver:  Name() string
           CanResolve(rawURL string) bool
           Resolve(ctx context.Context, rawURL string) (models.FullItem, error)
```

## Development Rules

- **Always use `gh` CLI** for GitHub interactions (PRs, issues, repo management)
- **Never use `sudo`**
- Run `make ci` before completing any task
- Pre-commit hook runs `go fmt` + `make ci`; commits blocked if checks fail

## Sub-Package Docs

Detailed context is in sub-CLAUDE.md files — Claude Code loads these automatically when working in those directories:

- `cmd/CLAUDE.md` — command structure, factory functions, per-command flags
- `internal/sinks/CLAUDE.md` — FileSink API, formatters, VectorSink behavior
- `internal/transform/CLAUDE.md` — transformer pipeline, config examples, built-in transformers
- `internal/sources/google/gmail/CLAUDE.md` — Gmail thread grouping modes
- `.claude/CLAUDE.md` — agent definitions, pkm-search skill

## Keeping Docs Up to Date

Update the relevant CLAUDE.md (root or sub-package) when:
- Adding/changing commands, flags, or subcommands → `cmd/CLAUDE.md`
- Adding/changing sources or targets → root + relevant `internal/sources/*/CLAUDE.md`
- Architecture or pipeline changes → root
- Transformer changes → `internal/transform/CLAUDE.md`
- Sink changes → `internal/sinks/CLAUDE.md`
- New dependencies → `go.mod` is authoritative; no need to list here

Update `ObsidianVault/.claude/skills/pkm-search/SKILL.md` when:
- `pkm-sync search` or `pkm-sync index` CLI flags change
- `archive.db` or `slack.db` schema changes
- New source types are added

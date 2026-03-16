# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Development Commands

**Requirements**: Go 1.24.4 or later

```bash
# Build
go build -v ./...              # Build all packages (mirrors CI)
go build -o pkm-sync ./cmd     # Build named binary

# Test
make test                      # Preferred — runs all tests
go test ./...                  # All tests
go test -race ./...            # With race detection
go test -bench=. ./...         # Run benchmarks

# Lint (requires golangci-lint v2.0+)
make check-golangci-version    # Verify installation
golangci-lint run              # Run linter

# Full CI check (lint + test + build) — runs in pre-commit hook
make ci

# Development setup
./scripts/install-hooks.sh     # Install Git hooks (REQUIRED for contributors)
```

The pre-commit hook runs `go fmt` and `make ci` before each commit. Commits are blocked if quality checks fail.

## Architecture Overview

This is a Go CLI application for Personal Knowledge Management (PKM) synchronization. It connects multiple data sources to PKM sinks using a **Sources -> Transformers -> Sinks** pipeline.

### CLI Framework
- Uses **Cobra** for command structure with persistent flags
- Root command (`cmd/root.go`) handles global flags: `--credentials`, `--config-dir`, `--debug`, `--start`, `--end`
- Global flags are processed in `PersistentPreRun` to configure paths

### Universal Data Model (`pkg/models/item.go`)
Segregated interface hierarchy:
- **CoreItem**: Base interface with ID, title, source type
- **SourcedItem**: Extends CoreItem with source URL and metadata
- **FullItem**: Composed interface (SourcedItem + TimestampedItem + EnrichedItem + SerializableItem)
- **BasicItem**: Standard implementation for emails, calendar events, documents, Jira issues, Slack messages
- **Thread**: Specialized implementation for email threads with embedded messages

### Sink Routing by Source Type

Different source types route to different sinks. This is a key architectural decision implemented in `cmd/helpers.go:runSourceSync()`:

- **Jira, Calendar, Drive**: `FileSink` (Obsidian/Logseq markdown files) + `VectorSink`
- **Slack**: `SlackArchiveSink` (SQLite `slack.db`) + `VectorSink` — **no file export**
- **Gmail**: `ArchiveSink` (EML files + SQLite `archive.db`) + `VectorSink` — **no file export**

All source types write to `VectorSink` for semantic search indexing and incremental sync time inference (via `MAX(updated_at)` per source in `vectors.db`).

### Source Implementations

#### Google Sources (`internal/sources/google/`)
- Single `GoogleSource` type handles Calendar, Gmail, and Drive via `GoogleSourceWithConfig`
- Uses Google OAuth 2.0 with automatic web server flow (fallback to manual copy/paste)
- Token and credentials stored in platform-specific config directories
- Gmail requires `gmail.readonly` scope (automatically requested)

#### Jira Source (`internal/sources/jira/`)

- **Authentication chain** (`config.go`): Reads `~/.config/.jira/.config.yml` (jira-cli config file) for server URL, login, auth type, and API token. Falls back to `JIRA_API_TOKEN` then `JIRA_TOKEN` env vars. Per-source `instance_url` in pkm-sync config overrides the server from jira-cli. Supports both bearer (default) and basic auth.
- **API client**: Uses `github.com/ankitpokhrel/jira-cli/pkg/jira` for its HTTP client and data types — not as a CLI wrapper. Search uses `GetV2()` to call `/search?fields=*all` directly, retrieving all issue fields in one call.
- **JQL building** (`jql.go`): Two modes. When `jql` config field is set, it's used as-is (wrapped in parens). Otherwise, structured fields (`project_keys`, `issue_types`, `statuses`, `assignee_filter`) are composed into JQL clauses. A `since` time filter is always appended as `updated >= "..."`. Results are ordered by `updated DESC`.
- **Pagination** (`source.go`): Fetches in pages of 50 via `searchWithAllFields()`, stopping when the limit is reached or results are exhausted.
- **Converter** (`converter.go`): Issue key (e.g. `PROJ-123`) becomes the item title and output filename (`PROJ-123.md`). The human-readable summary is stored in `metadata["summary"]`. Tags are generated from labels, issue type, status, and priority (lowercased, spaces to hyphens). Comments are appended as `## Comments` sections when `include_comments` is true.
- **Discovery** (`discovery.go`): `ListProjects()`, `ListIssueTypes(projectKey)`, `ListStatuses(projectKey)` power the `configure` command's interactive TUI for Jira sources.

#### Slack Source (`internal/sources/slack/`)

- **Authentication** (`auth.go`, `token.go`): Uses Slack's **internal web API** (not the official OAuth Slack API). Session tokens (`xoxc-*`) are extracted from a real browser login via a bundled Playwright script (`playwright/auth.js`). **Runtime dependency on Node.js** — Playwright and Chromium are auto-installed on first run. Token + cookies saved via keystore (OS keychain, `internal/keystore/`) with file fallback (`slack-token-<workspace>.json`). Browser profile persisted at `<configDir>/slack-browser-profile/`.
- **API client** (`client.go`): All calls go through `CallAPI()` using multipart form POST to `<apiBaseURL>/api/<method>`. Automatic retry with exponential backoff (doubling up to 30s cap) on `ratelimited` errors. The `client.userBoot` response is cached for the entire sync cycle — it provides channels, groups, IMs, starred items, and sidebar sections in one call.
- **Channel resolution** (`source.go`): Channels are resolved from three additive sources, then deduplicated by ID before fetching:
  1. Static `channels` list — resolved by name via `FindChannel()`
  2. Dynamic `channel_groups` — `"starred"` reads the boot response's `starred` array; other names match sidebar sections via `GetChannelSections()` (tries boot data first, falls back to `users.channelSections.list` API)
  3. DMs (`include_dms`) and MPDMs (`include_group_dms`) from the boot response
- **Message fetching**: `conversations.history` with pagination (200 per page). System subtypes (join/leave/topic/purpose/archive/name) are filtered out. Bot messages excluded when `exclude_bots` is true. Thread replies fetched via `conversations.replies` for thread root messages when `include_threads` is set. Rate limiting sleep between channels and thread fetches.
- **User cache** (`user_cache.go`): User IDs resolved to display names via `users.info` and cached in `slack-user-cache.json`. Cache persists across syncs to avoid redundant API calls.
- **Converter** (`converter.go`): Each message becomes a `BasicItem`. Text is extracted from `rich_text` blocks (falling back to plain `text` field). Metadata includes `channel`, `channel_id`, `workspace`, `author`, `ts`, `thread_ts`, `is_thread_root`, `reply_count`. Deep links: `<workspace>/archives/<channelID>/p<ts>`.

### Sink Implementations (`internal/sinks/`)

- **FileSink**: Owns formatting logic for Obsidian and Logseq via unexported `formatter` interface
- **VectorSink**: Semantic search indexing into SQLite with optional embeddings
- **ArchiveSink**: Gmail EML files + SQLite FTS4 index (`archive.db`)
- **SlackArchiveSink**: SQLite database (`slack.db`) for Slack messages

#### Slack Archive Schema (`slack.db`)
```sql
-- Table: slack_messages
-- id (TEXT UNIQUE): "slack_<channelID>_<ts>"
-- channel_id, channel_name, workspace, author (TEXT)
-- content (TEXT): message body
-- message_url (TEXT): deep link
-- item_type (TEXT): "slack_message" or "slack_reply"
-- thread_ts (TEXT), is_thread_root (INTEGER), reply_count (INTEGER)
-- created_at, synced_at (TEXT, RFC3339)
-- Indexes: channel_id, thread_ts (partial where != ''), created_at, author
-- Upsert on conflict(id): updates content, author, channel_name, synced_at
```

### Configuration System (`internal/config/config.go`)
- **YAML-based** with multi-source support via `enabled_sources` array
- **Configuration search paths**: custom `--config-dir` flag -> `~/.config/pkm-sync/config.yaml` -> `./config.yaml`
- **Complete reference** in `CONFIGURATION.md`
- **Per-source overrides**: `since`, `output_subdir`, `output_target`, `sync_interval`, `priority`
- **Config models** in `pkg/models/config.go` — all source-specific config structs live here

### Transformer Pipeline (`internal/transform/`)
- **Interface**: `Transform([]models.FullItem) ([]models.FullItem, error)`
- **Segregated interfaces**: `ContentTransformer` for content modification, `MetadataTransformer` for metadata enrichment
- **TransformPipeline**: Chains transformers with configurable error handling (`fail_fast`, `log_and_continue`, `skip_item`)
- **Built-in transformers**: `content_cleanup`, `auto_tagging`, `filter`, `link_extraction`, `signature_removal`, `thread_grouping`
- Sync engine automatically applies all content processing transformers between fetch and export

### Sync Engine (`internal/sync/`)
- `MultiSyncer.SyncAll()` runs Sources -> Transform -> Sinks pipeline
- Per-source `since` time override and limit support
- Incremental sync: infers last-synced time from `vectors.db` via `MAX(updated_at)` per source
- Sub-item change detection (`cmd/helpers.go:getSourceSubItems()`): new project keys, channels, or labels trigger a full lookback window instead of incremental sync
- State tracking in `internal/state/` persists sub-item membership across syncs

### Interactive Configuration (`internal/configure/`)
- `configure` command uses source-type-specific `DiscoveryProvider` implementations
- Each provider implements: `Authenticate()`, `DiscoverySections()`, `ApplySelections()`, `Preview()`, `RequiredFields()`
- **Slack provider**: Discovers channels (with message previews), channel groups (starred + sidebar sections), messaging toggles (DMs, group DMs). Required field: `workspace_url`.
- **Jira provider**: Discovers accessible projects, issue types (from a reference project), statuses (deduplicated across issue types). Required field: `instance_url`.
- **Gmail/Drive/Calendar providers**: Discover labels, folders/shared drives, and calendars respectively.
- Multi-select TUI powered by `github.com/charmbracelet/huh`. Requires interactive TTY.

### Key Dependencies
- `github.com/spf13/cobra` - CLI framework
- `google.golang.org/api/calendar/v3`, `drive/v3`, `gmail/v1` - Google APIs
- `golang.org/x/oauth2/google` - OAuth 2.0 client
- `gopkg.in/yaml.v3` - YAML configuration parsing
- `github.com/JohannesKaufmann/html-to-markdown/v2` - HTML to Markdown conversion
- `github.com/tj/go-naturaldate` - Natural language date parsing
- `github.com/ankitpokhrel/jira-cli/pkg/jira` - Jira V2 REST client (HTTP client and data types; search endpoint called directly via `GetV2()`)
- `github.com/mattn/go-sqlite3` - SQLite driver (Slack archive, email archive, vector store)
- `github.com/charmbracelet/huh` - Terminal form/TUI library (used by `configure` command)
- **Runtime**: Node.js required for Slack auth (Playwright + Chromium auto-installed on first use)

### Development Tools
- **golangci-lint v2.0+** - Required for v2 configuration format
  - The `.golangci.yml` uses v2-specific features like `formatters` section
  - Install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
  - Verify: `make check-golangci-version`

## Development Workflow

### GitHub Workflow
**Always use `gh` CLI for GitHub interactions** — issues, PRs, repository management. Do not use direct API calls or other GitHub tools.

### Agent Development System

The repository includes a Claude Code agent coordination system in `.claude/agents/` with specialized agents for: feature-architect, code-implementer, security-analyst, performance-optimizer, test-strategist, bug-hunter, code-reviewer, technical-writer, documentation-writer, and coordinator.

All code changes must pass `make ci` before completion (lint + test + build). Agents must use `gh` CLI for GitHub interactions and update relevant documentation when changing functionality.

## Claude Code Skills

Five Claude Code skills in `~/.claude/skills/` expose pkm-sync capabilities to Claude directly:

| Skill | Purpose |
|-------|---------|
| `email-search` | Query `~/.config/pkm-sync/archive.db` for Gmail messages via FTS4 or metadata |
| `slack-search` | Query `~/.config/pkm-sync/slack.db` for Slack messages via LIKE |
| `pkm-calendar` | View Google Calendar events via `pkm-sync calendar` |
| `pkm-sync-data` | Run syncs, build vector index, semantic search via `pkm-sync` |
| `pkm-config` | Manage configuration and OAuth setup via `pkm-sync config` / `pkm-sync setup` |

### Keeping Documentation Up to Date

**Before committing any changes**, review whether the changes affect this file (CLAUDE.md), README.md, or the skills and update them accordingly.

**This file (CLAUDE.md)** should be updated when:
- Architecture or data flow changes
- New source or sink implementations are added
- Internal interfaces or abstractions change
- New dependencies or runtime requirements are added
- Build, test, or development workflow changes

**README.md** should be updated when:
- New commands, subcommands, or flags are added
- New user-facing configuration options are introduced
- Authentication setup steps change
- Troubleshooting guidance is needed for new features

**Skills** should be updated when:
- **New CLI flags or commands** -> update `pkm-sync-data`, `pkm-calendar`, or `pkm-config` skills
- **Database schema changes** (archive.db, slack.db) -> update `email-search` or `slack-search` skills
- **New source types** -> add config templates to `pkm-config`, add sync examples to `pkm-sync-data`
- **Changed command names or flags** -> update the relevant skill's examples and flags reference
- **New config options** -> update YAML templates in `pkm-config`

Skills are self-contained SKILL.md files — edit them directly:
```
~/.claude/skills/email-search/SKILL.md
~/.claude/skills/slack-search/SKILL.md
~/.claude/skills/pkm-calendar/SKILL.md
~/.claude/skills/pkm-sync-data/SKILL.md
~/.claude/skills/pkm-config/SKILL.md
```

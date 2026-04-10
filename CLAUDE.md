# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Development Commands

**Requirements**: Go 1.24.4 or later

```bash
# Build the application
go build -v ./...              # Build all packages (mirrors CI)
go build -o pkm-sync ./cmd     # Build named binary

# Run the application (requires OAuth setup first)
./pkm-sync setup             # Verify authentication configuration
./pkm-sync gmail             # Sync Gmail emails to PKM systems
./pkm-sync calendar          # List and display Google Calendar events
./pkm-sync drive             # Sync Google Drive documents to PKM systems

# Configuration management
./pkm-sync config init       # Create default configuration
./pkm-sync config show       # Display current configuration
./pkm-sync config validate   # Validate configuration

# Gmail-specific examples
./pkm-sync gmail --source gmail_work --output ./work-emails
./pkm-sync gmail --since 7d   # Sync last 7 days from all enabled Gmail sources
./pkm-sync gmail --dry-run    # Preview what would be synced

# Custom paths
./pkm-sync --credentials /path/to/credentials.json setup
./pkm-sync --config-dir /custom/config/dir setup

# Development setup
./scripts/install-hooks.sh   # Install Git hooks (pre-commit formatting)
make check-golangci-version  # Verify golangci-lint v2.0+ installation
```

## Architecture Overview

This is a Go CLI application that provides universal Personal Knowledge Management (PKM) synchronization. It connects multiple data sources (Google Calendar, Gmail, Drive) to PKM sinks (Obsidian, Logseq) using OAuth 2.0 authentication.

### CLI Framework
- Uses **Cobra** for command structure with persistent flags
- Root command (`cmd/root.go`) handles global flags: `--credentials`, `--config-dir`, `--debug`, `--start`, `--end`
- Main commands: `sync`, `gmail`, `calendar`, `drive`, `jira`, `slack`, `servicenow`, `index`, `search`, `configure`, `config`, `setup`
- Global flags are processed in `PersistentPreRun` to configure paths

### Multi-Source Architecture (Sources → Transform → ResolveRefs → Sinks)
- **Universal interfaces** (`pkg/interfaces/`) for Source, Sink, Transformer, and Resolver abstractions
- **Universal data model** (`pkg/models/item.go`) with segregated interface hierarchy:
  - **CoreItem**: Base interface with ID, title, content
  - **SourcedItem**: Extends CoreItem with source type (`source_type`) and item type (`item_type`)
  - **FullItem**: Composed interface (SourcedItem + TimestampedItem + EnrichedItem + SerializableItem)
  - **BasicItem**: Standard implementation for emails, calendar events, documents
  - **Thread**: Specialized implementation for email threads with embedded messages
- **Source implementations** in `internal/sources/` (Google Calendar, Gmail, Drive, Jira, Slack, ServiceNow)
- **Sink implementations** in `internal/sinks/` — `FileSink` owns formatting logic for Obsidian and Logseq via unexported `formatter` interface; `VectorSink` for semantic search indexing; `SlackArchiveSink` for SQLite-backed Slack archive
- **Transformer pipeline** (`internal/transform/`) for configurable item processing
- **Sync engine** (`internal/sync/`) — `MultiSyncer.SyncAll()` runs Sources → Transform → ResolveRefs → Sinks pipeline
- **Reference resolution** (`internal/resolve/`) — cross-source URL resolution: fetches content linked from items (e.g. a Jira link in Slack) and appends it to the item set before sinks run
- **State management** (`internal/state/`) — tracks active sub-items (channels, folders, project keys) per source across runs to detect newly added sub-items and give them a full lookback window
- **Archive store** (`internal/archive/`) — SQLite-backed store for Gmail full-text search (FTS4)
- **Vector store** (`internal/vectorstore/`) — SQLite-vec store for semantic search embeddings
- **Embeddings** (`internal/embeddings/`) — pluggable embedding providers (Ollama, OpenAI)
- **Keystore** (`internal/keystore/`) — secure credential storage using system keyring or encrypted file fallback
- **Configure** (`internal/configure/`) — shared TUI configuration logic and per-source providers for the interactive `configure` command
- **Utils** (`internal/utils/`) — shared filename sanitization helpers (path traversal prevention, slug generation, safe character replacement)

### Configuration System (`internal/config/config.go`)
- **Multi-source configuration** supporting enabled sources array
- **YAML-based configuration** with comprehensive options
- **Configuration search paths**:
  1. Custom directory (via `--config-dir` flag)
  2. Global config: `~/.config/pkm-sync/config.yaml`
  3. Local repository: `./config.yaml` (current directory)
- **Complete documentation** in `CONFIGURATION.md`

### Authentication Flow
- **OAuth 2.0 only** (no ADC support) with Google Calendar, Drive, and Gmail APIs
- **Primary method**: Automatic web server flow (opens browser, captures callback on localhost)
- **Fallback method**: Manual copy/paste flow (supports pasting full callback URL or auth code)
- Automatic extraction of auth code from URLs with `extractAuthCode()` function
- Token and credentials stored in platform-specific config directories
- **Gmail requires additional scope**: `gmail.readonly` for email access

### Data Flow
1. **Multi-source collection**: Sync engine fetches from all enabled sources concurrently; source failures are non-fatal
2. **Source tagging**: Optional `source:<name>` tags added to each item
3. **Transform pipeline**: Optional configurable processing chain (cleanup, tagging, filtering)
4. **Reference resolution**: Optional cross-source URL resolution — items containing links to other sources trigger `Resolver` implementations that fetch and append referenced content
5. **Sink fan-out**: Items written to all configured sinks concurrently (FileSink for Obsidian/Logseq, VectorSink for semantic search); first sink failure cancels remaining sinks

### Key Dependencies
- `github.com/spf13/cobra` - CLI framework
- `google.golang.org/api/calendar/v3` - Google Calendar API
- `google.golang.org/api/drive/v3` - Google Drive API
- `google.golang.org/api/gmail/v1` - Gmail API
- `golang.org/x/oauth2/google` - OAuth 2.0 client
- `gopkg.in/yaml.v3` - YAML configuration parsing
- `github.com/JohannesKaufmann/html-to-markdown/v2` - HTML to Markdown conversion
- `github.com/tj/go-naturaldate` - Natural language date parsing
- `github.com/ankitpokhrel/jira-cli/pkg/jira` - Jira REST API client (V2)
- `github.com/charmbracelet/huh` - Terminal form/TUI library (used by `configure` command)

### Development Tools
- **golangci-lint v2.0+** - Required for v2 configuration format
  - The `.golangci.yml` uses v2-specific features like `formatters` section
  - Install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
  - Alternative: `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.4.0`
  - Verify: `make check-golangci-version`

## Current Implementation Status

### Sources
- ✅ **Gmail** - Fully implemented with multi-instance support, advanced filtering, thread grouping, and performance optimizations
- ✅ **Google Calendar** - Fully implemented in `internal/sources/google/`
- ✅ **Google Drive** - Fully implemented as a first-class source (`google_drive` type) syncing Docs/Sheets/Slides from folders and shared drives
- ✅ **Jira** - Implemented in `internal/sources/jira/` with bearer token auth, JQL query building, comments, and pagination (targets on-premise Jira like issues.redhat.com via V2 API)
- ✅ **Slack** - Implemented in `internal/sources/slack/` with bearer token auth, SQLite archive sink, static `channels` list, dynamic `channel_groups` (`"starred"` + custom sidebar sections), threads, DMs, and interactive TUI configure
- ✅ **ServiceNow** - Implemented in `internal/sources/servicenow/` with bearer token auth, configurable table sync (RITMs, incidents), encoded query/filter support

### Targets
- ✅ **Obsidian** - Implemented with YAML frontmatter and hierarchical structure
- ✅ **Logseq** - Implemented with property blocks and flat structure

### Configuration Features
- ✅ **Multi-source support** with `enabled_sources` array
- ✅ **Per-source configuration** (intervals, priorities, filtering, output routing)
- ✅ **Multi-instance Gmail** (work, personal, newsletters) with independent configurations
- ✅ **Thread grouping** with configurable modes (individual, consolidated, summary)
- ✅ **Filename sanitization** (no spaces, command-line friendly)
- ✅ **Simplified output directory** structure with per-source subdirectories
- ✅ **Local repository configuration** support
- ✅ **Comprehensive validation** and management commands

## Command Structure

### Core Commands
- **`sync`** - Primary pipeline command; runs all enabled sources through the full pipeline in one shot
  - Flags: `--source`, `--target`, `--output/-o`, `--since`, `--dry-run`, `--limit` (default 1000), `--format` (summary|json)

- **`gmail`** - Sync Gmail emails to PKM systems
  - Supports multiple Gmail instances (work, personal, newsletters)
  - Thread grouping: individual, consolidated, or summary modes
  - Example: `pkm-sync gmail --source gmail_work --target obsidian`

- **`calendar`** - List and display Google Calendar events (not part of the sync pipeline)
  - Example: `pkm-sync calendar --start 2025-01-01 --end 2025-01-31`

- **`drive`** - Sync Google Drive documents (Docs, Sheets, Slides) to PKM systems
  - Reads `google_drive` sources from config file (folder IDs, shared drives, workspace types)
  - `drive fetch <URL>` - Fetch a single document by URL and write to stdout
  - Example: `pkm-sync drive --source my_drive --target obsidian --since 7d`
  - Example: `pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit" --format md`

- **`jira`** - Sync Jira issues to PKM files
  - Example: `pkm-sync jira --source jira_work --since 7d`

- **`slack`** - Sync Slack messages to a SQLite archive with full-text search
  - Subcommands: `auth` (authenticate), `channels` (list channels)
  - Example: `pkm-sync slack --source slack_work --since 7d`

- **`servicenow`** - Sync ServiceNow tickets (RITMs, incidents) to PKM files
  - Subcommands: `auth` (authenticate)
  - Example: `pkm-sync servicenow --source snow_work --since 7d`

- **`index`** - Index Gmail threads into a local SQLite vector database (requires embedding provider)
  - Example: `pkm-sync index --source gmail_work --since 30d`

- **`search <query>`** - Query the vector database built by `index`
  - Example: `pkm-sync search "kubernetes deployment issues" --limit 5`

### Utility Commands
- **`configure [source-name]`** - Interactively configure what to sync from each source
  - Connects to the source API, shows available options with recent-item previews
  - Multi-select TUI powered by `github.com/charmbracelet/huh`
  - Supports Slack (channels/DMs), Gmail (labels), Google Drive (folders/shared drives), Jira (projects/issue types/statuses), Google Calendar (calendars)
  - Shows a diff of added/removed items before saving
  - Example: `pkm-sync configure` — pick from configured sources
  - Example: `pkm-sync configure slack_redhat` — configure a specific source
  - Example: `pkm-sync configure --type slack` — create a new Slack source interactively
  - Requires an interactive TTY; gracefully errors if run piped/in scripts

- **`setup`** - Verify authentication configuration
  - Tests all Google services (Calendar, Drive, Gmail)
  - Provides clear error messages and instructions

- **`config`** - Manage configuration files
  - Subcommands: `init`, `show`, `path`, `edit`, `validate`, `migrate-secrets` (move credentials to keyring), `clear-token` (revoke OAuth token)
  - Configuration management and validation

## OAuth Setup Requirements

Users must:
1. Create Google Cloud project with Calendar/Drive/Gmail APIs enabled
2. Configure OAuth consent screen for "Desktop application"
3. Add `http://127.0.0.1:*` to authorized redirect URIs (enables automatic authorization flow)
4. Download credentials.json to config directory or use `--credentials` flag
5. Run `./pkm-sync setup` to verify configuration and complete OAuth flow

**Gmail-specific setup**:
- Enable Gmail API in Google Cloud Console
- Required scopes: `gmail.readonly` (automatically requested)
- Same OAuth credentials work for all Google services

### Interfaces (`pkg/interfaces/`)

Key interfaces an agent working on this codebase needs to know:

- **`Source`** — `Name() string`, `Configure(config map[string]interface{}, client *http.Client) error`, `Fetch(since time.Time, limit int) ([]models.FullItem, error)`, `SupportsRealtime() bool`
- **`Sink`** — `Name() string`, `Write(ctx context.Context, items []models.FullItem) error`
- **`Transformer`** — `Name() string`, `Configure(config map[string]interface{}) error`, `Transform(items []models.FullItem) ([]models.FullItem, error)`
- **`TransformPipeline`** — `AddTransformer(t Transformer) error`, `Transform(items []models.FullItem) ([]models.FullItem, error)`, `Configure(config models.TransformConfig) error`
- **`Resolver`** — `Name() string`, `CanResolve(rawURL string) bool`, `Resolve(ctx context.Context, rawURL string) (models.FullItem, error)` — used by `internal/resolve.Engine` for cross-source reference fetching
- **`ContentTransformer`**, **`MetadataTransformer`** — narrower transformer variants operating on `[]models.CoreItem` and `[]models.EnrichedItem` respectively

## Transformer Pipeline System

The transformer pipeline provides a configurable, chainable processing system for items between source fetch and target export. This enables content processing features like filtering, tagging, content cleanup, and future AI analysis.

### Core Architecture
- **Transformer Interface**: `Transform([]models.FullItem) ([]models.FullItem, error)` pattern
- **Segregated interfaces**: `ContentTransformer` for content modification, `MetadataTransformer` for metadata enrichment
- **TransformPipeline**: Chains multiple transformers with configurable error handling
- **Configuration-driven**: Enable/disable transformers and configure processing order
- **Interface-based**: Works seamlessly with FullItem interface supporting Thread and BasicItem types

### Configuration Example
```yaml
transformers:
  enabled: true
  pipeline_order: ["content_cleanup", "auto_tagging", "filter"]
  error_strategy: "log_and_continue"  # or "fail_fast", "skip_item"
  transformers:
    content_cleanup:
      strip_prefixes: true
    auto_tagging:
      rules:
        - pattern: "meeting"
          tags: ["work", "meeting"]
        - pattern: "urgent"
          tags: ["priority", "urgent"]
    filter:
      min_content_length: 50
      exclude_source_types: ["spam"]
      required_tags: ["important"]
```

### Built-in Transformers
- **`content_cleanup`**: Converts HTML to Markdown, strips quoted text, normalizes whitespace, removes email prefixes ("Re:", "Fwd:")
- **`auto_tagging`**: Adds tags based on content patterns and source metadata
- **`filter`**: Filters items by content length, source type, required tags
- **`link_extraction`**: Extracts and indexes URLs from content
- **`signature_removal`**: Removes email signatures from content
- **`thread_grouping`**: Groups related emails into conversation threads

### Error Handling Strategies
- **`fail_fast`**: Stop processing on first transformer error
- **`log_and_continue`**: Log errors but continue with original items
- **`skip_item`**: Log errors and skip problematic items

### Integration Points
- **Sync Engine**: Automatically applies transformations between fetch and export
- **Configuration**: Transformers configured in main config.yaml
- **CLI**: Fully backward compatible - no CLI changes required

## Gmail Thread Grouping

The Gmail source supports intelligent thread grouping to reduce email clutter and improve organization.

### Thread Modes
- **`individual`** (default) - Each email is treated as a separate item
- **`consolidated`** - All messages in a thread are combined into a single file
- **`summary`** - Creates summary files with key messages from each thread

### Configuration Example
```yaml
sources:
  gmail_work:
    type: gmail
    gmail:
      include_threads: true           # Enable thread processing
      thread_mode: "summary"          # Use summary mode
      thread_summary_length: 3        # Show 3 key messages per thread
      query: "in:inbox to:me"
```

### Thread Processing Features
- **Smart message selection** - Prioritizes different senders, longer content, attachments
- **Filename sanitization** - No spaces, command-line friendly filenames
- **Thread metadata** - Participants, duration, message count
- **Subject cleaning** - Removes "Re:", "Fwd:" prefixes

### Output Examples
- Consolidated: `Thread_PR-discussion-fix-security-issue_8-messages.md`
- Summary: `Thread-Summary_meeting-notes-weekly-sync_5-messages.md`
- Individual: `Re-Project-status-update.md`

## Development Workflow

### Development Setup
```bash
# Clone the repository and install Git hooks (REQUIRED)
git clone <repository-url>
cd pkm-sync
./scripts/install-hooks.sh
```

**⚠️ Important:** The pre-commit hook runs `go fmt` and `make ci` (lint, test, build) before each commit. Commits will be blocked if quality checks fail. This is **required** for all contributors to prevent CI failures.

### GitHub Workflow
**Always use `gh` CLI for GitHub interactions** - issues, PRs, repository management. Do not use direct API calls or other GitHub tools.

### Testing
```bash
# Run all tests (preferred)
make test

# Run tests directly
go test ./...                                    # All tests
go test -race ./...                              # With race detection
go test -bench=. ./...                           # Run benchmarks
go test ./cmd                                    # Specific package
go test -v ./internal/sources/google/gmail       # Verbose output
```

## Agent Development System

### Agent Coordination Setup
The repository includes a Claude Code agent coordination system located in `.claude/agents/` for specialized development workflows.

#### Available Agents
- **feature-architect**: System design and architecture planning
- **code-implementer**: Implementation of features and fixes
- **security-analyst**: Security analysis and threat modeling  
- **performance-optimizer**: Performance analysis and optimization
- **test-strategist**: Test strategy and quality assurance
- **bug-hunter**: Debugging and issue resolution
- **code-reviewer**: Code quality and maintainability review
- **technical-writer**: Technical documentation creation
- **documentation-writer**: User-focused documentation and guides
- **coordinator**: Multi-agent workflow orchestration

#### Agent Configuration Files
```
.claude/
├── agents/              # Agent definitions (committed)
│   ├── coordinator.md   # Main coordination agent with patterns
│   ├── feature-architect.md
│   ├── code-implementer.md
│   └── ...             # Other specialized agents
└── settings.local.json  # Local permissions (NOT committed)
```

All code changes must pass `make ci` before completion:
- **Lint**: `golangci-lint run` with comprehensive rules
- **Test**: `go test ./...` with race detection
- **Build**: `go build -v ./...`

### Agent Integration
Agents must follow project standards:
- Run `make ci` before completing tasks
- Use `gh` CLI for GitHub interactions
- Update relevant documentation when changing functionality

## Claude Code Skills

The `pkm-search` skill in the ObsidianVault exposes search across all pkm-sync sinks:

| Skill | Location | Purpose |
|-------|----------|---------|
| `pkm-search` | `ObsidianVault/.claude/skills/pkm-search/SKILL.md` | Unified search: semantic via `pkm-sync search`, Gmail FTS4 via `archive.db`, Slack via `slack.db` |

### Keeping Documentation Up to Date

**Before committing any changes**, review whether the changes affect this file (CLAUDE.md) or the skill below and update them accordingly.

**This file (CLAUDE.md)** should be updated when:
- New commands or subcommands are added
- New sources or targets are implemented
- Architecture or data flow changes
- New dependencies are added
- Build, test, or development workflow changes
- Implementation status changes (mark items ✅ when complete)

**`pkm-search` skill** should be updated when:
- **New CLI flags or commands** added to `pkm-sync search` or `pkm-sync index`
- **Database schema changes** to `archive.db` or `slack.db`
- **New source types** are added (add them to the decision guide and DB paths table)
- **DB paths change** in `config.yaml`

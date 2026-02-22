# pkm-sync

A universal synchronization tool for Personal Knowledge Management (PKM) systems. Connect Google Calendar, Gmail, and Drive to Obsidian or Logseq.

## Quick Start

```bash
# 1. Build
go build -o pkm-sync ./cmd

# 2. Place your Google OAuth credentials
cp credentials.json ~/.config/pkm-sync/credentials.json

# 3. Create a config file and verify authentication
pkm-sync config init
pkm-sync setup

# 4. Sync everything
pkm-sync sync
```

See [Authentication Setup](#authentication-setup) for OAuth credential creation steps.

## How It Works

pkm-sync uses a **Sources → Transformers → Sinks** pipeline:

1. **Sources** fetch items from Gmail, Google Calendar, or Google Drive
2. **Transformers** clean content (HTML→Markdown, strip signatures, auto-tag, filter)
3. **Sinks** write items to a PKM target (Obsidian/Logseq files) or a vector database

```
Gmail ──┐
Drive ──┼──► Transformers ──► FileSink (Obsidian / Logseq)
Drive ──┘                 └──► VectorSink (semantic search)
```

The primary entry point is `pkm-sync sync`, which runs all enabled sources through the full pipeline in one shot.

## Configuration

```bash
pkm-sync config init      # Write default config to ~/.config/pkm-sync/config.yaml
pkm-sync config show      # Print current effective config
pkm-sync config path      # Show config file location
pkm-sync config edit      # Open config in $EDITOR
pkm-sync config validate  # Validate config file
```

Config is loaded from the first location that exists:
1. `--config-dir` flag
2. `~/.config/pkm-sync/config.yaml`
3. `./config.yaml` (current directory)

### Minimal config example

```yaml
sync:
  enabled_sources: ["gmail_work", "my_drive"]
  default_target: obsidian
  default_output_dir: ~/vault
  default_since: 7d

sources:
  gmail_work:
    enabled: true
    type: gmail
    gmail:
      query: "in:inbox to:me"
      include_threads: true
      thread_mode: "summary"   # individual | consolidated | summary

  my_drive:
    enabled: true
    type: google_drive
    drive:
      folder_ids: ["<folder-id>"]
      workspace_types: ["document", "spreadsheet"]

targets:
  obsidian:
    type: obsidian
    obsidian:
      default_folder: Inbox
      include_frontmatter: true
```

See **[CONFIGURATION.md](./CONFIGURATION.md)** for all available options.

## Commands

### `sync` — primary pipeline command

Sync all enabled sources in one operation.

```bash
pkm-sync sync                              # Sync all enabled sources
pkm-sync sync --source gmail_work          # Limit to one source
pkm-sync sync --target logseq --output ~/graph
pkm-sync sync --since 7d --dry-run         # Preview without writing
pkm-sync sync --since 7d --dry-run --format json
```

Flags: `--source`, `--target`, `--output/-o`, `--since`, `--dry-run`, `--limit` (default 1000), `--format` (summary|json)

---

### `gmail` — Gmail-only sync

Same pipeline as `sync`, filtered to Gmail sources.

```bash
pkm-sync gmail                                    # All enabled Gmail sources
pkm-sync gmail --source gmail_work --since today
pkm-sync gmail --dry-run
```

Flags: same as `sync`

---

### `drive` — Google Drive sync

Same pipeline as `sync`, filtered to `google_drive` sources.

```bash
pkm-sync drive                                    # All enabled Drive sources
pkm-sync drive --source my_drive --since 7d
pkm-sync drive --dry-run --format json
```

Flags: same as `sync` (default `--limit` is 100)

#### `drive fetch <URL>` — fetch a single doc to stdout

```bash
pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit"
pkm-sync drive fetch "https://docs.google.com/document/d/abc123/edit" --format md
pkm-sync drive fetch "https://docs.google.com/spreadsheets/d/xyz789/edit" --format csv
```

Formats: `txt` (default), `md`, `html`, `csv` (spreadsheets only)

---

### `calendar` — event viewer

Standalone command; **not** part of the sync pipeline. Displays calendar events as a table or JSON.

```bash
pkm-sync calendar                                     # Current week to today
pkm-sync calendar --start today
pkm-sync calendar --start 2025-01-01 --end 2025-01-31
pkm-sync calendar --format json
pkm-sync calendar --include-details                   # Attendees, meeting URLs
pkm-sync calendar --export-docs --export-dir ./docs   # Export attached Docs
```

Flags: `--start/-s`, `--end/-e`, `--format/-f` (table|json), `--include-details`, `--export-docs`, `--export-dir`, `--max-results/-n`

---

### `index` — index into vector DB for semantic search

Index Gmail threads into a local SQLite vector database (requires Ollama or compatible embedding provider).

```bash
pkm-sync index --source gmail_work --since 30d
pkm-sync index --since 7d --limit 500
pkm-sync index --reindex            # Re-index all threads
```

Flags: `--source`, `--since` (default 30d), `--limit` (default 1000), `--reindex`, `--delay` (ms between embeddings), `--max-content-length`

---

### `search <query>` — semantic search

Query the vector database built by `index`.

```bash
pkm-sync search "kubernetes deployment issues"
pkm-sync search "meetings with alice" --limit 5
pkm-sync search "project status" --source-name gmail_work --format json
```

Flags: `--limit` (default 10), `--source-name`, `--source-type`, `--format` (text|json), `--min-score`

---

### `setup` — verify authentication

```bash
pkm-sync setup
```

Tests connectivity to Google Calendar, Drive, and Gmail. Provides clear error messages if anything is misconfigured.

---

### Global flags

```
--credentials/-c   Path to credentials.json
--config-dir       Custom config directory
--debug/-d         Enable debug logging
--start/-s         Global start date (used by calendar)
--end/-e           Global end date (used by calendar)
```

## Supported Integrations

| Source | Status |
|--------|--------|
| Gmail | Fully implemented — multi-instance, thread grouping |
| Google Calendar | Fully implemented |
| Google Drive | Fully implemented — Docs, Sheets, Slides |
| Slack | Planned |
| Jira | Planned |

| Target | Format |
|--------|--------|
| Obsidian | YAML frontmatter, hierarchical folders, standard Markdown |
| Logseq | Property blocks, flat structure, `[[date]]` links, `#tags` |

## Authentication Setup

### Prerequisites

- Go 1.24.4 or later
- A Google Cloud project with API access

### OAuth 2.0 Setup

1. Go to the [Google Cloud Console](https://console.cloud.google.com/)
2. Create or select a project
3. Enable **Google Calendar API**, **Google Drive API**, and **Gmail API**
4. Configure the OAuth consent screen (Internal or External)
5. Create **OAuth 2.0 Client ID** credentials → Desktop application
6. Add `http://127.0.0.1:*` to authorized redirect URIs
7. Download `credentials.json`

### Place credentials

Default locations (checked in order):
- `~/.config/pkm-sync/credentials.json`
- `~/Library/Application Support/pkm-sync/credentials.json` (macOS)
- `%APPDATA%\pkm-sync\credentials.json` (Windows)
- `./credentials.json` (current directory)

Or use a flag: `pkm-sync --credentials /path/to/credentials.json setup`

### First run

```bash
pkm-sync setup
```

The app opens your browser automatically for OAuth consent. The token is saved in the same directory as `credentials.json`. If the automatic flow fails, it falls back to manual copy/paste mode.

## Troubleshooting

### "Error 403: Access denied" or "insufficient authentication scopes"
- Verify Calendar, Drive, and Gmail APIs are enabled in Google Cloud Console
- Check that the OAuth consent screen includes the required scopes
- Delete `token.json` and re-authenticate

### "credentials.json not found"
- Ensure the file is named exactly `credentials.json` (not `client_secret_*.json`)
- Run `pkm-sync setup` to see which paths are being checked

### "token refresh failed"
- Your token may have expired or been revoked
- Delete `token.json` from your config directory and run `pkm-sync setup` again

### Getting help
Run `pkm-sync setup` to diagnose authentication issues.

## Architecture

### Project structure

```
pkm-sync/
├── cmd/                 # CLI entry points (sync, gmail, drive, calendar, index, search, …)
├── internal/
│   ├── config/          # Config loading and management
│   ├── sinks/           # FileSink (file export) + VectorSink (semantic search)
│   ├── sources/
│   │   └── google/      # Google Calendar, Gmail, Drive source implementations
│   ├── sync/            # MultiSyncer orchestrator
│   ├── targets/
│   │   ├── obsidian/    # Obsidian-specific formatting
│   │   └── logseq/      # Logseq-specific formatting
│   └── transform/       # Transformer pipeline (cleanup, tagging, filtering, …)
└── pkg/
    ├── interfaces/      # Source, Target, Sink, Transformer, Syncer interfaces
    └── models/          # Universal data models (BasicItem, Thread, …)
```

### Extensibility

- **New Sources**: implement the `Source` interface
- **New Targets**: implement the `Target` interface
- **New Sinks**: implement the `Sink` interface (file, vector DB, webhooks, …)
- **New Transformers**: implement the `Transformer` interface

## Documentation

- **[README.md](./README.md)** — This file
- **[CONFIGURATION.md](./CONFIGURATION.md)** — Complete configuration reference
- **[CLAUDE.md](./CLAUDE.md)** — Development guide for Claude Code
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — Development workflow and AI agent coordination

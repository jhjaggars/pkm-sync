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
Gmail    ──┐
Calendar ──┼──► Transformers ──► Files (Obsidian / Logseq)
Drive    ──┘                 └──► Vector DB (semantic search)
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

Sync all enabled sources in one operation. An optional positional argument filters to a source type or specific source name.

```bash
pkm-sync sync                              # All enabled sources
pkm-sync sync gmail                        # All enabled Gmail sources
pkm-sync sync gmail_work                   # Specific source by name
pkm-sync sync drive --since 7d
pkm-sync sync --target logseq --output ~/graph
pkm-sync sync --since 7d --dry-run
pkm-sync sync gmail --dry-run --format json
```

Source type aliases accepted: `gmail`, `drive`, `calendar`, `jira`, `slack`, `snow`/`servicenow`.

Flags: `--source`, `--target`, `--output/-o`, `--since`, `--dry-run`, `--limit` (default 1000), `--format` (summary|json)

---

### `fetch` — fetch a single item

Fetch a single item by URL or source-qualified identifier and write to stdout or a file with YAML frontmatter.

```bash
# URLs are auto-routed to the right source
pkm-sync fetch "https://docs.google.com/document/d/abc123/edit"
pkm-sync fetch "https://company.atlassian.net/browse/PROJ-123"

# Source-type prefix for non-URL keys
pkm-sync fetch jira/PROJ-123
pkm-sync fetch drive/FILE_ID

# Write markdown with frontmatter to a file or directory
pkm-sync fetch "https://docs.google.com/document/d/abc123/edit" --output ./docs/
pkm-sync fetch jira/PROJ-123 --output ./jira/ --format md

# Include Google Doc comments as footnotes
pkm-sync fetch "https://docs.google.com/document/d/abc123/edit" --comments

# Disambiguate when multiple sources of the same type exist
pkm-sync fetch jira/PROJ-123 --source jira_work
```

Flags: `--source`, `--format` (txt|md|json), `--output/-o`, `--comments`

---

### `search` — search indexed items

Query the vector database built by `index`. An optional first argument scopes the search to a source type or specific instance.

```bash
# Semantic search across all sources
pkm-sync search "kubernetes deployment issues"

# Gmail full-text search (uses archive.db when available, falls back to vector)
pkm-sync search gmail "meeting with alice"

# Scope to a source type or specific instance
pkm-sync search slack "deploy failed"
pkm-sync search gmail/work_gmail "rosa boundary"
pkm-sync search jira/jira_work "auth error"

pkm-sync search "project status" --format json --limit 5
```

Flags: `--limit` (default 10), `--source-name`, `--source-type`, `--format` (text|json), `--min-score`

---

### `index` — build vector DB for semantic search

Index items into a local SQLite vector database (requires Ollama or compatible embedding provider).

```bash
pkm-sync index --source gmail_work --since 30d
pkm-sync index --since 7d --limit 500
pkm-sync index --reindex            # Re-index all items
```

Flags: `--source`, `--since` (default 30d), `--limit` (default 1000), `--reindex`, `--delay` (ms between embeddings), `--max-content-length`

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

### `configure` — interactive source configuration

Connects to a source's API and presents a multi-select TUI to pick what to sync.

```bash
pkm-sync configure                       # Pick from configured sources
pkm-sync configure slack_redhat          # Configure a specific source
pkm-sync configure --type slack          # Create a new Slack source interactively
```

Supports Slack (channels, channel groups, DMs), Gmail (labels), Google Drive (folders), Jira (projects), and Google Calendar (calendars). Shows recent-item previews alongside each option and displays a diff of added/removed items before saving.

---

### Legacy per-source commands

`gmail`, `drive`, `jira`, `slack`, and `servicenow` are still available but deprecated. Use `pkm-sync sync <type>` instead.

The `slack` and `servicenow` commands retain their `auth` subcommands for first-time authentication:

```bash
pkm-sync slack auth --workspace https://myorg.slack.com
pkm-sync slack channels                                    # List channels + starred
pkm-sync servicenow auth --instance https://mycompany.service-now.com
```

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
| Slack | Fully implemented — bearer token auth, channel groups, threads, DMs |
| Jira | Fully implemented — JQL queries, comments, bearer token auth |
| ServiceNow | Fully implemented — RITMs, incidents, bearer token auth |

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

## Documentation

- **[CONFIGURATION.md](./CONFIGURATION.md)** — Complete configuration reference
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — Development workflow

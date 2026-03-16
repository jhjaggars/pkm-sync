# pkm-sync

A universal synchronization tool for Personal Knowledge Management (PKM) systems. Connect Google Calendar, Gmail, Drive, Slack, and Jira to Obsidian or Logseq.

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

pkm-sync uses a **Sources -> Transformers -> Sinks** pipeline:

1. **Sources** fetch items from Gmail, Google Calendar, Drive, Slack, or Jira
2. **Transformers** clean content (HTML->Markdown, strip signatures, auto-tag, filter)
3. **Sinks** write items to a PKM target or archive database

```
Gmail ────┐                  ┌──> ArchiveSink (EML + SQLite archive.db)
Drive ────┤                  ├──> FileSink (Obsidian / Logseq markdown)
Calendar ─┼──> Transformers ─┼──> VectorSink (semantic search)
Slack ────┤                  └──> SlackArchiveSink (SQLite slack.db)
Jira ─────┘
```

Note: each source type routes to specific sinks — Slack writes to SQLite (`slack.db`), Gmail writes to EML + SQLite (`archive.db`), while Calendar/Drive/Jira export to Obsidian or Logseq markdown files. All sources also write to the VectorSink for semantic search.

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

### Slack config example

```yaml
sources:
  slack_work:
    enabled: true
    type: slack
    output_subdir: Slack
    slack:
      workspace_url: https://myorg.slack.com
      channels:
        - announce-internal       # Always sync this channel
      channel_groups:
        - starred                 # Plus all currently-starred channels
      include_threads: true
      include_dms: false
      include_group_dms: false
      exclude_bots: true
      min_length: 10
      rate_limit_ms: 500
      max_messages_per_channel: 500
```

### Jira config example

```yaml
sources:
  jira_work:
    enabled: true
    type: jira
    output_subdir: Jira
    since: 7d
    jira:
      instance_url: https://issues.example.com
      project_keys:
        - PROJ
        - TEAM
      issue_types:
        - Bug
        - Story
      statuses:
        - "In Progress"
        - "To Do"
      assignee_filter: me
      include_comments: true
      # Alternative: use raw JQL instead of structured fields
      # jql: "project = PROJ AND assignee = currentUser()"
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

### `slack` — Slack sync

Sync Slack messages to a SQLite archive with full-text search. Messages are stored in a SQLite database (`slack.db`), not exported as markdown files.

```bash
pkm-sync slack --source slack_work
pkm-sync slack --source slack_work --since 7d
pkm-sync slack --source slack_work --dry-run
pkm-sync slack --db-path /custom/path/slack.db
```

Flags: `--source`, `--since`, `--limit` (default 1000), `--dry-run`, `--db-path`

#### `slack auth` — authenticate with Slack

Opens a browser window for you to log in to your Slack workspace. The session token is automatically extracted and saved. Requires Node.js to be installed.

```bash
pkm-sync slack auth --workspace https://myorg.slack.com
```

On first run, this installs Playwright and downloads Chromium (may take a minute). Subsequent runs reuse the cached browser. The token is saved to the config directory and will be used automatically by `pkm-sync slack`.

Flags: `--workspace` (required if not configured in config file)

#### `slack channels` — list and debug channel resolution

Lists available channels, starred channels, and raw boot response keys. Useful for diagnosing Enterprise Grid or unusual workspace layouts.

```bash
pkm-sync slack channels                            # First enabled Slack source
pkm-sync slack channels --source slack_redhat      # Specific source
```

---

### `jira` — Jira sync

Sync Jira issues to PKM markdown files. Issues are exported with YAML frontmatter containing metadata (summary, status, priority, assignee, etc.) and optionally include comments.

```bash
pkm-sync jira --source jira_work
pkm-sync jira --source jira_work --since 7d
pkm-sync jira --source jira_work --dry-run
pkm-sync jira --limit 500
```

Flags: `--source`, `--since`, `--limit` (default 1000), `--dry-run`

---

### `configure` — interactive source configuration

Connects to a source's API and presents a multi-select TUI to pick what to sync.

```bash
pkm-sync configure                       # Pick from configured sources
pkm-sync configure slack_redhat          # Configure a specific source
pkm-sync configure --type slack          # Create a new Slack source interactively
```

Supports Slack (channels, channel groups, DMs), Gmail (labels), Google Drive (folders), Jira (projects, issue types, statuses), and Google Calendar (calendars). Shows recent-item previews alongside each option and displays a diff of added/removed items before saving.

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
| Slack | Fully implemented — browser auth, channel groups, threads, DMs |
| Jira | Fully implemented — JQL queries, comments, bearer token auth |

| Target | Format |
|--------|--------|
| Obsidian | YAML frontmatter, hierarchical folders, standard Markdown |
| Logseq | Property blocks, flat structure, `[[date]]` links, `#tags` |

## Authentication Setup

### Google (Calendar, Drive, Gmail)

#### Prerequisites

- Go 1.24.4 or later
- A Google Cloud project with API access

#### OAuth 2.0 Setup

1. Go to the [Google Cloud Console](https://console.cloud.google.com/)
2. Create or select a project
3. Enable **Google Calendar API**, **Google Drive API**, and **Gmail API**
4. Configure the OAuth consent screen (Internal or External)
5. Create **OAuth 2.0 Client ID** credentials -> Desktop application
6. Add `http://127.0.0.1:*` to authorized redirect URIs
7. Download `credentials.json`

#### Place credentials

Default locations (checked in order):
- `~/.config/pkm-sync/credentials.json`
- `~/Library/Application Support/pkm-sync/credentials.json` (macOS)
- `%APPDATA%\pkm-sync\credentials.json` (Windows)
- `./credentials.json` (current directory)

Or use a flag: `pkm-sync --credentials /path/to/credentials.json setup`

#### First run

```bash
pkm-sync setup
```

The app opens your browser automatically for OAuth consent. The token is saved in the same directory as `credentials.json`. If the automatic flow fails, it falls back to manual copy/paste mode.

### Slack

Slack uses session token authentication extracted from a browser login (not an official Slack OAuth app).

#### Prerequisites

- Node.js installed (for the Playwright-based auth script)

#### Setup

1. Add a Slack source to your config with at minimum a `workspace_url`:
   ```yaml
   sources:
     slack_work:
       enabled: true
       type: slack
       slack:
         workspace_url: https://myorg.slack.com
         channels: ["general"]
   ```

2. Authenticate:
   ```bash
   pkm-sync slack auth --workspace https://myorg.slack.com
   ```
   This opens a Chromium browser window. Log in to your Slack workspace normally. Once logged in, the session token is automatically intercepted and saved. On first run, Playwright and Chromium are installed (may take a minute).

3. Optionally use `configure` to interactively pick channels:
   ```bash
   pkm-sync configure slack_work
   ```

4. Sync:
   ```bash
   pkm-sync slack --source slack_work
   ```

#### Token refresh

Slack session tokens expire periodically. When syncs start failing with authentication errors, re-run `pkm-sync slack auth` to get a fresh token.

### Jira

Jira uses bearer token authentication. pkm-sync reads credentials from the [jira-cli](https://github.com/ankitpokhrel/jira-cli) config file or environment variables.

#### Option A: Using jira-cli (recommended)

If you already use jira-cli, pkm-sync will automatically use its config:

```bash
# Install and configure jira-cli
jira init
# pkm-sync will read ~/.config/.jira/.config.yml automatically
```

#### Option B: Environment variables

Set your Jira API token directly:

```bash
export JIRA_API_TOKEN="your-token-here"
# or
export JIRA_TOKEN="your-token-here"
```

#### Option C: Inline in config

Set `instance_url` in your pkm-sync config to override the server URL from jira-cli:

```yaml
sources:
  jira_work:
    enabled: true
    type: jira
    jira:
      instance_url: https://issues.example.com
      project_keys: ["PROJ"]
```

The token is still read from jira-cli config or environment variables.

#### Verify

```bash
pkm-sync jira --source jira_work --dry-run
```

## Troubleshooting

### Google

#### "Error 403: Access denied" or "insufficient authentication scopes"
- Verify Calendar, Drive, and Gmail APIs are enabled in Google Cloud Console
- Check that the OAuth consent screen includes the required scopes
- Delete `token.json` and re-authenticate

#### "credentials.json not found"
- Ensure the file is named exactly `credentials.json` (not `client_secret_*.json`)
- Run `pkm-sync setup` to see which paths are being checked

#### "token refresh failed"
- Your token may have expired or been revoked
- Delete `token.json` from your config directory and run `pkm-sync setup` again

### Slack

#### "no Slack token found for workspace"
- Run `pkm-sync slack auth --workspace <URL>` to authenticate
- Make sure the workspace URL matches what's in your config

#### "npm install failed" or Playwright errors
- Ensure Node.js is installed and `npm` is on your PATH
- Try deleting `~/.config/pkm-sync/slack-auth/node_modules/` and re-running auth

#### Token expired / authentication errors during sync
- Slack session tokens expire periodically
- Re-run `pkm-sync slack auth --workspace <URL>` to get a fresh token

#### Channels not found or empty results
- Run `pkm-sync slack channels` to see what channels are visible to your token
- Check that channel names in config match exactly (no `#` prefix)
- For Enterprise Grid workspaces, you may need to set `api_url` in the Slack config

### Jira

#### "no Jira API token found"
- Set `JIRA_API_TOKEN` environment variable, or
- Run `jira init` to configure jira-cli, or
- Ensure `~/.config/.jira/.config.yml` exists with a valid `api_token`

#### "jira server URL not configured"
- Set `instance_url` in your pkm-sync Jira source config, or
- Ensure jira-cli config has a `server` field

#### Empty results or missing issues
- Run with `--dry-run` to see the generated JQL query
- Try a simpler query: `jira: { jql: "project = PROJ ORDER BY updated DESC" }`
- Check that your token has access to the target projects

### Getting help
Run `pkm-sync setup` to diagnose Google authentication issues. For Slack, use `pkm-sync slack channels` to debug channel visibility.

## Architecture

### Project structure

```
pkm-sync/
├── cmd/                 # CLI entry points (sync, gmail, drive, calendar, slack, jira, …)
├── internal/
│   ├── config/          # Config loading and management
│   ├── configure/       # Interactive TUI configuration providers
│   ├── keystore/        # Secret storage (OS keychain + file fallback)
│   ├── sinks/           # FileSink, VectorSink, ArchiveSink, SlackArchiveSink
│   ├── sources/
│   │   ├── google/      # Google Calendar, Gmail, Drive source implementations
│   │   ├── jira/        # Jira source (V2 REST API, JQL, discovery)
│   │   └── slack/       # Slack source (internal web API, Playwright auth)
│   ├── state/           # Sync state persistence (sub-item tracking)
│   ├── sync/            # MultiSyncer orchestrator
│   └── transform/       # Transformer pipeline (cleanup, tagging, filtering, …)
└── pkg/
    ├── interfaces/      # Source, Target, Sink, Transformer, Syncer interfaces
    └── models/          # Universal data models (BasicItem, Thread, Config, …)
```

### Extensibility

- **New Sources**: implement the `Source` interface
- **New Targets**: implement the `Target` interface
- **New Sinks**: implement the `Sink` interface (file, vector DB, webhooks, …)
- **New Transformers**: implement the `Transformer` interface

## Documentation

- **[README.md](./README.md)** — This file
- **[CONFIGURATION.md](./CONFIGURATION.md)** — Complete configuration reference
- **[CLAUDE.md](./CLAUDE.md)** — Architecture guide for AI agents and contributors
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — Development workflow and AI agent coordination

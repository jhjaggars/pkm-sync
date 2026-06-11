# cmd/ — Command Layer

## Command Factory Functions (`cmd/helpers.go`)

Shared factory functions used by all commands:
- `createFileSink(name, outputDir string) (*sinks.FileSink, error)` — no config
- `createFileSinkWithConfig(name, outputDir string, cfg *models.Config) (*sinks.FileSink, error)` — reads `cfg.Targets[name]`
- `createSource`, `createSourceWithConfig` — source factory
- `parseSinceTime`, `getEnabledSources`, `getEnabledGmailSources`, `getEnabledDriveSources`
- Dry-run: call `fileSink.Preview(syncResult.Items)` after `SyncAll` returns

## Core Commands

- **`sync`** (`cmd/sync.go`) — primary pipeline; runs all enabled sources through full pipeline
  - Flags: `--source`, `--target`, `--output/-o`, `--since`, `--dry-run`, `--limit` (default 1000), `--format` (summary|json)

- **`gmail`** (`cmd/gmail.go`) — sync Gmail to PKM; thin wrapper over MultiSyncer
  - Supports multiple Gmail instances; thread grouping: individual, consolidated, summary

- **`calendar`** (`cmd/calendar.go`) — list/display Google Calendar events (not part of sync pipeline)

- **`drive`** (`cmd/export.go`) — sync Google Drive Docs/Sheets/Slides; reads `google_drive` sources from config
  - `drive fetch <URL>` (`cmd/drive_fetch.go`) — fetch single doc to stdout

- **`jira`** (`cmd/jira.go`) — sync Jira issues; bearer token auth

- **`slack`** (`cmd/slack.go`) — sync Slack to SQLite archive
  - Subcommands: `auth` (`cmd/slack_auth.go`), `channels` (`cmd/slack_channels.go`)

- **`servicenow`** (`cmd/servicenow.go`) — sync ServiceNow tickets
  - Subcommands: `auth` (`cmd/servicenow_auth.go`)

- **`index`** (`cmd/index.go`) — index Gmail threads into SQLite vector DB (uses VectorSink + MultiSyncer, no transformer pipeline)

- **`search <query>`** (`cmd/search.go`) — query the vector DB built by `index`

## Utility Commands

- **`configure [source-name]`** (`cmd/configure.go`) — interactive TUI to configure what to sync
  - Connects to source API, shows available options with recent-item previews
  - Supports Slack, Gmail, Drive, Jira, Calendar
  - `--type <source-type>` — create a new source interactively
  - Requires interactive TTY; errors gracefully if piped

- **`setup`** (`cmd/setup.go`) — verify authentication; tests all Google services

- **`config`** (`cmd/config.go`) — manage config files
  - Subcommands: `init`, `show`, `path`, `edit`, `validate`, `migrate-secrets`, `clear-token`

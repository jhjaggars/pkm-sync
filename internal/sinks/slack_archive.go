package sinks

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

const slackSchema = `
CREATE TABLE IF NOT EXISTS slack_messages (
    rowid        INTEGER PRIMARY KEY AUTOINCREMENT,
    id           TEXT    UNIQUE NOT NULL,
    channel_id   TEXT    NOT NULL,
    channel_name TEXT    NOT NULL,
    workspace    TEXT    NOT NULL,
    author       TEXT    NOT NULL,
    content      TEXT    NOT NULL,
    message_url  TEXT    NOT NULL,
    item_type    TEXT    NOT NULL,
    thread_ts    TEXT    NOT NULL DEFAULT '',
    is_thread_root INTEGER NOT NULL DEFAULT 0,
    reply_count    INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT    NOT NULL,
    synced_at    TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sm_channel  ON slack_messages(channel_id);
CREATE INDEX IF NOT EXISTS idx_sm_thread   ON slack_messages(thread_ts) WHERE thread_ts != '';
CREATE INDEX IF NOT EXISTS idx_sm_created  ON slack_messages(created_at);
CREATE INDEX IF NOT EXISTS idx_sm_author   ON slack_messages(author);

CREATE VIRTUAL TABLE IF NOT EXISTS slack_messages_fts USING fts5(
    content, author, channel_name,
    content='slack_messages',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS slack_messages_ai AFTER INSERT ON slack_messages BEGIN
    INSERT INTO slack_messages_fts(rowid, content, author, channel_name)
    VALUES (new.rowid, new.content, new.author, new.channel_name);
END;

CREATE TRIGGER IF NOT EXISTS slack_messages_ad AFTER DELETE ON slack_messages BEGIN
    INSERT INTO slack_messages_fts(slack_messages_fts, rowid, content, author, channel_name)
    VALUES ('delete', old.rowid, old.content, old.author, old.channel_name);
END;

CREATE TRIGGER IF NOT EXISTS slack_messages_au AFTER UPDATE ON slack_messages BEGIN
    INSERT INTO slack_messages_fts(slack_messages_fts, rowid, content, author, channel_name)
    VALUES ('delete', old.rowid, old.content, old.author, old.channel_name);
    INSERT INTO slack_messages_fts(rowid, content, author, channel_name)
    VALUES (new.rowid, new.content, new.author, new.channel_name);
END;
`

// SlackArchiveSink writes Slack message items to a SQLite database with FTS5 full-text search.
type SlackArchiveSink struct {
	db     *sql.DB
	dbPath string
}

// NewSlackArchiveSink opens (or creates) the SQLite database at dbPath, runs schema
// migrations, and returns a ready-to-use SlackArchiveSink.
// The caller is responsible for calling Close() when done.
func NewSlackArchiveSink(dbPath string) (*SlackArchiveSink, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open slack archive DB at %s: %w", dbPath, err)
	}

	sink := &SlackArchiveSink{db: db, dbPath: dbPath}

	if err := sink.initSchema(); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("failed to initialize slack archive schema: %w", err)
	}

	return sink, nil
}

// initSchema applies the DDL statements that create tables, indexes, and triggers.
func (s *SlackArchiveSink) initSchema() error {
	if _, err := s.db.Exec(slackSchema); err != nil {
		return fmt.Errorf("schema exec failed: %w", err)
	}

	return nil
}

// Name returns the sink identifier.
func (s *SlackArchiveSink) Name() string { return "slack_archive" }

// Write upserts all Slack items into the SQLite database in a single transaction.
// Items whose SourceType is not "slack" are silently skipped.
func (s *SlackArchiveSink) Write(ctx context.Context, items []models.FullItem) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const upsertSQL = `
INSERT INTO slack_messages
    (id, channel_id, channel_name, workspace, author, content, message_url,
     item_type, thread_ts, is_thread_root, reply_count, created_at, synced_at)
VALUES
    (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    content        = excluded.content,
    author         = excluded.author,
    channel_name   = excluded.channel_name,
    synced_at      = excluded.synced_at`

	stmt, err := tx.PrepareContext(ctx, upsertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare upsert statement: %w", err)
	}

	defer stmt.Close()

	syncedAt := time.Now().UTC().Format(time.RFC3339)
	written := 0

	for _, item := range items {
		if item.GetSourceType() != "slack" {
			continue
		}

		meta := item.GetMetadata()

		channelID, _ := meta["channel_id"].(string)
		channelName, _ := meta["channel"].(string)
		workspace, _ := meta["workspace"].(string)
		author, _ := meta["author"].(string)
		threadTs, _ := meta["thread_ts"].(string)

		isThreadRootRaw, _ := meta["is_thread_root"].(bool)

		isThreadRoot := 0
		if isThreadRootRaw {
			isThreadRoot = 1
		}

		replyCount, _ := meta["reply_count"].(int)

		messageURL := ""
		if links := item.GetLinks(); len(links) > 0 {
			messageURL = links[0].URL
		}

		createdAt := item.GetCreatedAt().UTC().Format(time.RFC3339)

		if _, err = stmt.ExecContext(ctx,
			item.GetID(),
			channelID,
			channelName,
			workspace,
			author,
			item.GetContent(),
			messageURL,
			item.GetItemType(),
			threadTs,
			isThreadRoot,
			replyCount,
			createdAt,
			syncedAt,
		); err != nil {
			return fmt.Errorf("failed to upsert slack message %s: %w", item.GetID(), err)
		}

		written++
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit slack archive transaction: %w", err)
	}

	if written > 0 {
		fmt.Printf("Slack archive: wrote %d messages to %s\n", written, s.dbPath)
	}

	return nil
}

// Close releases the database connection.
func (s *SlackArchiveSink) Close() error { return s.db.Close() }

// Ensure SlackArchiveSink implements interfaces.Sink.
var _ interfaces.Sink = (*SlackArchiveSink)(nil)

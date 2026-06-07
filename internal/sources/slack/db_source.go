package slack

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"pkm-sync/pkg/models"
)

// DBSource reads Slack messages from a local slack.db archive instead of the
// Slack API. It is used by the index command so that indexing never triggers
// remote API calls for data that has already been synced locally.
type DBSource struct {
	dbPath string
	db     *sql.DB
}

// NewDBSource creates a DBSource backed by the SQLite archive at dbPath.
func NewDBSource(dbPath string) (*DBSource, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open slack archive at %s: %w", dbPath, err)
	}

	// Verify the table exists.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM slack_messages LIMIT 1").Scan(&n); err != nil {
		db.Close()
		return nil, fmt.Errorf("slack archive at %s has no slack_messages table: %w", dbPath, err)
	}

	return &DBSource{dbPath: dbPath, db: db}, nil
}

// Name returns the source identifier.
func (s *DBSource) Name() string { return "slack_db" }

// Configure is a no-op — DBSource needs no remote credentials.
func (s *DBSource) Configure(_ map[string]interface{}, _ *http.Client) error { return nil }

// SupportsRealtime returns false — DB sources are batch-only.
func (s *DBSource) SupportsRealtime() bool { return false }

// Fetch returns Slack messages from the local archive newer than since, up to limit items.
func (s *DBSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	const query = `
		SELECT id, channel_id, channel_name, workspace, author, content,
		       message_url, item_type, thread_ts, is_thread_root, reply_count, created_at
		FROM slack_messages
		WHERE created_at >= ?
		ORDER BY created_at ASC
		LIMIT ?`

	rows, err := s.db.Query(query, since.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query slack archive: %w", err)
	}
	defer rows.Close()

	var items []models.FullItem

	for rows.Next() {
		var (
			id, channelID, channelName, workspace string
			author, content, messageURL, itemType string
			threadTs, createdAtStr                string
			isThreadRoot, replyCount              int
		)

		if err := rows.Scan(
			&id, &channelID, &channelName, &workspace,
			&author, &content, &messageURL, &itemType,
			&threadTs, &isThreadRoot, &replyCount, &createdAtStr,
		); err != nil {
			return nil, fmt.Errorf("failed to scan slack message: %w", err)
		}

		createdAt, err := time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			createdAt = time.Now()
		}

		threadID := threadTs
		if threadID == "" {
			threadID = id
		}

		item := models.NewBasicItem(id, channelName)
		item.SetContent(content)
		item.SetSourceType("slack")
		item.SetCreatedAt(createdAt)
		item.SetUpdatedAt(createdAt)
		item.SetMetadata(map[string]interface{}{
			"channel_id":    channelID,
			"channel":       channelName,
			"workspace":     workspace,
			"author":        author,
			"thread_ts":     threadTs,
			"thread_id":     threadID,
			"is_thread_root": isThreadRoot == 1,
			"reply_count":   replyCount,
		})

		if messageURL != "" {
			item.SetLinks([]models.Link{{URL: messageURL}})
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading slack archive rows: %w", err)
	}

	return items, nil
}

// Close releases the database connection.
func (s *DBSource) Close() error { return s.db.Close() }

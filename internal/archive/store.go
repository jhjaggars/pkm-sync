package archive

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Message represents a single archived email message.
type Message struct {
	GmailID         string
	ThreadID        string
	RFC822MessageID string
	Subject         string
	FromAddr        string
	ToAddrs         []string
	CCAddrs         []string
	DateSent        time.Time
	DateArchived    time.Time
	Labels          []string
	EMLPath         string
	SizeBytes       int64
	HasAttachments  bool
	SourceName      string
}

// ArchiveStats contains statistics about the archive.
type ArchiveStats struct {
	TotalMessages    int
	MessagesBySource map[string]int
	OldestMessage    time.Time
	NewestMessage    time.Time
}

// SyncState tracks sync progress per source.
type SyncState struct {
	SourceName   string
	LastSyncTime time.Time
	MessageCount int
}

// Store is a SQLite-backed metadata index for archived email messages.
type Store struct {
	db *sql.DB
}

// NewStore opens or creates the archive database at dbPath, running migrations as needed.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive database: %w", err)
	}

	// Enable WAL mode for better concurrency.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()

		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	store := &Store{db: db}

	if err := store.createSchema(); err != nil {
		db.Close()

		return nil, fmt.Errorf("failed to create archive schema: %w", err)
	}

	return store, nil
}

func (s *Store) createSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS messages (
			gmail_id            TEXT PRIMARY KEY,
			thread_id           TEXT NOT NULL DEFAULT '',
			rfc822_message_id   TEXT NOT NULL DEFAULT '',
			subject             TEXT NOT NULL DEFAULT '',
			from_addr           TEXT NOT NULL DEFAULT '',
			to_addrs            TEXT NOT NULL DEFAULT '[]',
			cc_addrs            TEXT NOT NULL DEFAULT '[]',
			date_sent           DATETIME NOT NULL,
			date_archived       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			labels              TEXT NOT NULL DEFAULT '[]',
			eml_path            TEXT NOT NULL DEFAULT '',
			size_bytes          INTEGER NOT NULL DEFAULT 0,
			has_attachments     BOOLEAN NOT NULL DEFAULT 0,
			source_name         TEXT NOT NULL DEFAULT ''
		);

		CREATE INDEX IF NOT EXISTS idx_messages_thread_id   ON messages(thread_id);
		CREATE INDEX IF NOT EXISTS idx_messages_date_sent   ON messages(date_sent);
		CREATE INDEX IF NOT EXISTS idx_messages_source_name ON messages(source_name);

		CREATE TABLE IF NOT EXISTS sync_state (
			source_name     TEXT PRIMARY KEY,
			last_sync_time  DATETIME NOT NULL,
			message_count   INTEGER NOT NULL DEFAULT 0
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts4(
			subject, body, from_addr,
			tokenize=porter
		);
	`

	_, err := s.db.Exec(schema)

	return err
}

// HasMessage returns true if a message with the given Gmail ID has already been archived.
func (s *Store) HasMessage(gmailID string) (bool, error) {
	var count int

	err := s.db.QueryRow("SELECT COUNT(*) FROM messages WHERE gmail_id = ?", gmailID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check message %s: %w", gmailID, err)
	}

	return count > 0, nil
}

// GetArchivedIDs returns a set of all archived Gmail IDs for a given source.
// This mirrors vectorstore.GetIndexedThreadIDs for batch dedup.
func (s *Store) GetArchivedIDs(sourceName string) (map[string]bool, error) {
	rows, err := s.db.Query("SELECT gmail_id FROM messages WHERE source_name = ?", sourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to query archived IDs for source %s: %w", sourceName, err)
	}
	defer rows.Close()

	archived := make(map[string]bool)

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan gmail_id: %w", err)
		}

		archived[id] = true
	}

	return archived, rows.Err()
}

// IndexMessage upserts a message into the metadata index and FTS5 table.
// bodyText is the plain-text body used for full-text search indexing.
func (s *Store) IndexMessage(msg Message, bodyText string) error {
	toJSON, err := json.Marshal(msg.ToAddrs)
	if err != nil {
		return fmt.Errorf("failed to marshal to_addrs: %w", err)
	}

	ccJSON, err := json.Marshal(msg.CCAddrs)
	if err != nil {
		return fmt.Errorf("failed to marshal cc_addrs: %w", err)
	}

	labelsJSON, err := json.Marshal(msg.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	dateSentStr := msg.DateSent.UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`
		INSERT INTO messages (
			gmail_id, thread_id, rfc822_message_id, subject, from_addr,
			to_addrs, cc_addrs, date_sent, date_archived,
			labels, eml_path, size_bytes, has_attachments, source_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?)
		ON CONFLICT(gmail_id) DO UPDATE SET
			eml_path        = excluded.eml_path,
			size_bytes      = excluded.size_bytes,
			date_archived   = CURRENT_TIMESTAMP
	`,
		msg.GmailID, msg.ThreadID, msg.RFC822MessageID, msg.Subject, msg.FromAddr,
		string(toJSON), string(ccJSON), dateSentStr,
		string(labelsJSON), msg.EMLPath, msg.SizeBytes, msg.HasAttachments, msg.SourceName,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert message %s: %w", msg.GmailID, err)
	}

	// Upsert FTS index: delete old entry then insert fresh.
	deleteSQL := "DELETE FROM messages_fts WHERE rowid = (SELECT rowid FROM messages WHERE gmail_id = ?)"
	if _, err := tx.Exec(deleteSQL, msg.GmailID); err != nil {
		return fmt.Errorf("failed to delete fts row for %s: %w", msg.GmailID, err)
	}

	if _, err := tx.Exec(
		"INSERT INTO messages_fts(rowid, subject, body, from_addr) SELECT rowid, ?, ?, ? FROM messages WHERE gmail_id = ?",
		msg.Subject, bodyText, msg.FromAddr, msg.GmailID,
	); err != nil {
		return fmt.Errorf("failed to insert fts row for %s: %w", msg.GmailID, err)
	}

	return tx.Commit()
}

// UpdateSyncState records the latest sync time and message count for a source.
func (s *Store) UpdateSyncState(sourceName string, syncTime time.Time, messageCount int) error {
	_, err := s.db.Exec(`
		INSERT INTO sync_state (source_name, last_sync_time, message_count)
		VALUES (?, ?, ?)
		ON CONFLICT(source_name) DO UPDATE SET
			last_sync_time = excluded.last_sync_time,
			message_count  = message_count + excluded.message_count
	`, sourceName, syncTime.UTC().Format(time.RFC3339), messageCount)
	if err != nil {
		return fmt.Errorf("failed to update sync state for %s: %w", sourceName, err)
	}

	return nil
}

// Stats returns aggregate statistics about the archive.
func (s *Store) Stats() (*ArchiveStats, error) {
	stats := &ArchiveStats{
		MessagesBySource: make(map[string]int),
	}

	if err := s.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&stats.TotalMessages); err != nil {
		return nil, fmt.Errorf("failed to get total messages: %w", err)
	}

	rows, err := s.db.Query("SELECT source_name, COUNT(*) FROM messages GROUP BY source_name")
	if err != nil {
		return nil, fmt.Errorf("failed to query messages by source: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			sourceName string
			count      int
		)

		if err := rows.Scan(&sourceName, &count); err != nil {
			return nil, fmt.Errorf("failed to scan source stats: %w", err)
		}

		stats.MessagesBySource[sourceName] = count
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	var oldestStr, newestStr sql.NullString

	dateRangeQuery := "SELECT MIN(date_sent), MAX(date_sent) FROM messages"
	if err := s.db.QueryRow(dateRangeQuery).Scan(&oldestStr, &newestStr); err != nil {
		return nil, fmt.Errorf("failed to get date range: %w", err)
	}

	if oldestStr.Valid {
		stats.OldestMessage, _ = time.Parse(time.RFC3339, oldestStr.String)
	}

	if newestStr.Valid {
		stats.NewestMessage, _ = time.Parse(time.RFC3339, newestStr.String)
	}

	return stats, nil
}

// FTSResult holds a message matched by full-text search.
type FTSResult struct {
	GmailID    string
	Subject    string
	FromAddr   string
	SourceName string
	DateSent   time.Time
}

// Search performs a full-text search over subject, body, and from_addr fields.
func (s *Store) Search(query string, limit int) ([]FTSResult, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(`
		SELECT m.gmail_id, m.subject, m.from_addr, m.source_name, m.date_sent
		FROM messages_fts f
		JOIN messages m ON f.rowid = m.rowid
		WHERE messages_fts MATCH ?
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to execute FTS search: %w", err)
	}
	defer rows.Close()

	var results []FTSResult

	for rows.Next() {
		var (
			r       FTSResult
			sentStr string
		)

		if err := rows.Scan(&r.GmailID, &r.Subject, &r.FromAddr, &r.SourceName, &sentStr); err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}

		r.DateSent, _ = time.Parse(time.RFC3339, sentStr)
		results = append(results, r)
	}

	return results, rows.Err()
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

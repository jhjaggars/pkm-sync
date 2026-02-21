package vectorstore

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// Document represents a document in the vector store.
type Document struct {
	ID           int64
	SourceID     string
	ThreadID     string
	Title        string
	Content      string
	SourceType   string
	SourceName   string
	MessageCount int
	Metadata     map[string]interface{}
	CreatedAt    time.Time
	UpdatedAt    time.Time
	IndexedAt    time.Time
}

// SearchResult represents a search result with similarity score.
type SearchResult struct {
	Document

	Distance float64
	Score    float64
}

// SearchFilters defines optional filters for search queries.
type SearchFilters struct {
	SourceType string
	SourceName string
	MinScore   float64
}

// StoreStats contains statistics about the vector store.
type StoreStats struct {
	TotalDocuments      int
	TotalThreads        int
	DocumentsBySource   map[string]int
	DocumentsByType     map[string]int
	OldestDocument      time.Time
	NewestDocument      time.Time
	AverageMessageCount float64
}

// Store wraps a SQLite database with vector search capabilities.
type Store struct {
	db         *sql.DB
	dimensions int
}

// NewStore creates or opens a vector store at the given path.
func NewStore(dbPath string, dimensions int) (*Store, error) {
	sqlite_vec.Auto()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()

		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	store := &Store{
		db:         db,
		dimensions: dimensions,
	}

	if err := store.createSchema(); err != nil {
		db.Close()

		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return store, nil
}

// createSchema creates the database schema if it doesn't exist.
func (s *Store) createSchema() error {
	schema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS documents (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id     TEXT NOT NULL,
			thread_id     TEXT NOT NULL DEFAULT '',
			title         TEXT NOT NULL DEFAULT '',
			content       TEXT NOT NULL DEFAULT '',
			source_type   TEXT NOT NULL DEFAULT '',
			source_name   TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 1,
			metadata      TEXT NOT NULL DEFAULT '{}',
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL,
			indexed_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(thread_id, source_name)
		);

		CREATE INDEX IF NOT EXISTS idx_documents_thread_id ON documents(thread_id);
		CREATE INDEX IF NOT EXISTS idx_documents_source_name ON documents(source_name);
		CREATE INDEX IF NOT EXISTS idx_documents_source_type ON documents(source_type);

		CREATE VIRTUAL TABLE IF NOT EXISTS vec_documents USING vec0(
			document_id INTEGER PRIMARY KEY,
			embedding float[%d]
		);
	`, s.dimensions)

	_, err := s.db.Exec(schema)

	return err
}

// UpsertDocument inserts or updates a document with its embedding.
func (s *Store) UpsertDocument(doc Document, embedding []float32) error {
	if len(embedding) != s.dimensions {
		return fmt.Errorf("embedding dimensions mismatch: expected %d, got %d", s.dimensions, len(embedding))
	}

	// Start transaction
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Marshal metadata
	metadataJSON, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Format timestamps as RFC3339 for consistent parsing
	createdAtStr := doc.CreatedAt.Format(time.RFC3339)
	updatedAtStr := doc.UpdatedAt.Format(time.RFC3339)

	// Upsert document
	result, err := tx.Exec(`
		INSERT INTO documents (
			source_id, thread_id, title, content, source_type, source_name,
			message_count, metadata, created_at, updated_at, indexed_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(thread_id, source_name) DO UPDATE SET
			source_id = excluded.source_id,
			title = excluded.title,
			content = excluded.content,
			source_type = excluded.source_type,
			message_count = excluded.message_count,
			metadata = excluded.metadata,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			indexed_at = CURRENT_TIMESTAMP
	`,
		doc.SourceID, doc.ThreadID, doc.Title, doc.Content, doc.SourceType, doc.SourceName,
		doc.MessageCount, metadataJSON, createdAtStr, updatedAtStr,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert document: %w", err)
	}

	// Get document ID
	docID, err := result.LastInsertId()
	if err != nil {
		// If it was an update, fetch the ID
		query := "SELECT id FROM documents WHERE thread_id = ? AND source_name = ?"

		err = tx.QueryRow(query, doc.ThreadID, doc.SourceName).Scan(&docID)
		if err != nil {
			return fmt.Errorf("failed to get document ID: %w", err)
		}
	}

	// Convert embedding to binary format for sqlite-vec
	embeddingBytes, err := float32SliceToBytes(embedding)
	if err != nil {
		return fmt.Errorf("failed to convert embedding to bytes: %w", err)
	}

	// Delete existing embedding if present (vec0 doesn't support UPSERT)
	_, err = tx.Exec("DELETE FROM vec_documents WHERE document_id = ?", docID)
	if err != nil {
		return fmt.Errorf("failed to delete old embedding: %w", err)
	}

	// Insert new embedding
	_, err = tx.Exec("INSERT INTO vec_documents (document_id, embedding) VALUES (?, ?)", docID, embeddingBytes)
	if err != nil {
		return fmt.Errorf("failed to insert embedding: %w", err)
	}

	return tx.Commit()
}

// Search performs a KNN search for similar documents.
func (s *Store) Search(queryEmbedding []float32, limit int, filters SearchFilters) ([]SearchResult, error) {
	if len(queryEmbedding) != s.dimensions {
		return nil, fmt.Errorf("query embedding dimensions mismatch: expected %d, got %d", s.dimensions, len(queryEmbedding))
	}

	// Convert embedding to binary format
	embeddingBytes, err := float32SliceToBytes(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("failed to convert query embedding to bytes: %w", err)
	}

	// Build query with optional filters
	// sqlite-vec requires the k parameter to be set
	query := `
		SELECT
			d.id, d.source_id, d.thread_id, d.title, d.content, d.source_type, d.source_name,
			d.message_count, d.metadata, d.created_at, d.updated_at, d.indexed_at,
			v.distance
		FROM vec_documents v
		JOIN documents d ON v.document_id = d.id
		WHERE v.embedding MATCH ? AND k = ?
	`

	args := []interface{}{embeddingBytes, limit}

	if filters.SourceType != "" {
		query += " AND d.source_type = ?"

		args = append(args, filters.SourceType)
	}

	if filters.SourceName != "" {
		query += " AND d.source_name = ?"

		args = append(args, filters.SourceName)
	}

	query += " ORDER BY v.distance"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult

	for rows.Next() {
		var (
			result                          SearchResult
			metadataJSON                    string
			createdAt, updatedAt, indexedAt string
		)

		err := rows.Scan(
			&result.ID, &result.SourceID, &result.ThreadID, &result.Title, &result.Content,
			&result.SourceType, &result.SourceName, &result.MessageCount, &metadataJSON,
			&createdAt, &updatedAt, &indexedAt, &result.Distance,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}

		// Parse metadata
		if err := json.Unmarshal([]byte(metadataJSON), &result.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		// Parse timestamps
		result.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		result.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		result.IndexedAt, _ = time.Parse(time.RFC3339, indexedAt)

		// Calculate score (1 / (1 + distance))
		result.Score = 1.0 / (1.0 + result.Distance)

		// Apply score filter
		if filters.MinScore > 0 && result.Score < filters.MinScore {
			continue
		}

		results = append(results, result)
	}

	return results, rows.Err()
}

// IsIndexed checks if a thread is already indexed.
func (s *Store) IsIndexed(threadID, sourceName string) (bool, error) {
	var count int

	query := "SELECT COUNT(*) FROM documents WHERE thread_id = ? AND source_name = ?"

	err := s.db.QueryRow(query, threadID, sourceName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if indexed: %w", err)
	}

	return count > 0, nil
}

// GetIndexedThreadIDs returns a map of indexed thread IDs for a source.
func (s *Store) GetIndexedThreadIDs(sourceName string) (map[string]bool, error) {
	rows, err := s.db.Query("SELECT thread_id FROM documents WHERE source_name = ?", sourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to query indexed threads: %w", err)
	}
	defer rows.Close()

	indexed := make(map[string]bool)

	for rows.Next() {
		var threadID string
		if err := rows.Scan(&threadID); err != nil {
			return nil, fmt.Errorf("failed to scan thread ID: %w", err)
		}

		indexed[threadID] = true
	}

	return indexed, rows.Err()
}

// Stats returns statistics about the vector store.
func (s *Store) Stats() (*StoreStats, error) {
	stats := &StoreStats{
		DocumentsBySource: make(map[string]int),
		DocumentsByType:   make(map[string]int),
	}

	// Total documents
	err := s.db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&stats.TotalDocuments)
	if err != nil {
		return nil, fmt.Errorf("failed to get total documents: %w", err)
	}

	// Total threads (distinct thread IDs)
	err = s.db.QueryRow("SELECT COUNT(DISTINCT thread_id) FROM documents").Scan(&stats.TotalThreads)
	if err != nil {
		return nil, fmt.Errorf("failed to get total threads: %w", err)
	}

	// Documents by source
	rows, err := s.db.Query("SELECT source_name, COUNT(*) FROM documents GROUP BY source_name")
	if err != nil {
		return nil, fmt.Errorf("failed to query documents by source: %w", err)
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

		stats.DocumentsBySource[sourceName] = count
	}

	// Documents by type
	rows, err = s.db.Query("SELECT source_type, COUNT(*) FROM documents GROUP BY source_type")
	if err != nil {
		return nil, fmt.Errorf("failed to query documents by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			sourceType string
			count      int
		)

		if err := rows.Scan(&sourceType, &count); err != nil {
			return nil, fmt.Errorf("failed to scan type stats: %w", err)
		}

		stats.DocumentsByType[sourceType] = count
	}

	// Date range
	var oldestStr, newestStr sql.NullString

	err = s.db.QueryRow("SELECT MIN(created_at), MAX(updated_at) FROM documents").Scan(&oldestStr, &newestStr)
	if err != nil {
		return nil, fmt.Errorf("failed to get date range: %w", err)
	}

	if oldestStr.Valid {
		stats.OldestDocument, _ = time.Parse(time.RFC3339, oldestStr.String)
	}

	if newestStr.Valid {
		stats.NewestDocument, _ = time.Parse(time.RFC3339, newestStr.String)
	}

	// Average message count
	var avgCount sql.NullFloat64

	err = s.db.QueryRow("SELECT AVG(message_count) FROM documents").Scan(&avgCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get average message count: %w", err)
	}

	if avgCount.Valid {
		stats.AverageMessageCount = avgCount.Float64
	}

	return stats, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// float32SliceToBytes converts a []float32 to a byte slice in binary format.
func float32SliceToBytes(data []float32) ([]byte, error) {
	buf := new(bytes.Buffer)

	err := binary.Write(buf, binary.LittleEndian, data)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "archive.db"))
	require.NoError(t, err)

	t.Cleanup(func() { store.Close() })

	return store
}

func TestNewStore_CreatesSchema(t *testing.T) {
	store := newTestStore(t)

	// messages table exists
	var count int

	err := store.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// sync_state table exists
	err = store.db.QueryRow("SELECT COUNT(*) FROM sync_state").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestHasMessage(t *testing.T) {
	store := newTestStore(t)

	msg := testMessage("msg1")
	ok, err := store.HasMessage(msg.GmailID)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, store.IndexMessage(msg, "test body"))

	ok, err = store.HasMessage(msg.GmailID)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestGetArchivedIDs(t *testing.T) {
	store := newTestStore(t)

	require.NoError(t, store.IndexMessage(testMessageForSource("a1", "source_work"), "body a1"))
	require.NoError(t, store.IndexMessage(testMessageForSource("a2", "source_work"), "body a2"))
	require.NoError(t, store.IndexMessage(testMessageForSource("b1", "source_personal"), "body b1"))

	ids, err := store.GetArchivedIDs("source_work")
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.True(t, ids["a1"])
	assert.True(t, ids["a2"])
	assert.False(t, ids["b1"])
}

func TestIndexMessage_Upsert(t *testing.T) {
	store := newTestStore(t)

	msg := testMessage("upsmsg")
	require.NoError(t, store.IndexMessage(msg, "original body"))

	// Update eml_path on second index
	msg.EMLPath = "/new/path.eml"
	msg.SizeBytes = 9999
	require.NoError(t, store.IndexMessage(msg, "updated body"))

	// Only one row should exist
	var count int

	err := store.db.QueryRow("SELECT COUNT(*) FROM messages WHERE gmail_id = ?", msg.GmailID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// eml_path should be updated
	var emlPath string

	err = store.db.QueryRow("SELECT eml_path FROM messages WHERE gmail_id = ?", msg.GmailID).Scan(&emlPath)
	require.NoError(t, err)
	assert.Equal(t, "/new/path.eml", emlPath)
}

func TestSearch_FTS(t *testing.T) {
	store := newTestStore(t)

	msg1 := testMessage("fts1")
	msg1.Subject = "Meeting notes for Q1 planning"
	msg1.FromAddr = "alice@example.com"

	msg2 := testMessage("fts2")
	msg2.Subject = "Lunch order confirmation"
	msg2.FromAddr = "bob@example.com"

	require.NoError(t, store.IndexMessage(msg1, "quarterly planning discussion"))
	require.NoError(t, store.IndexMessage(msg2, "sandwich and salad order"))

	results, err := store.Search("planning", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "fts1", results[0].GmailID)

	results, err = store.Search("salad", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "fts2", results[0].GmailID)
}

func TestStats(t *testing.T) {
	store := newTestStore(t)

	require.NoError(t, store.IndexMessage(testMessageForSource("s1", "work"), "body"))
	require.NoError(t, store.IndexMessage(testMessageForSource("s2", "work"), "body"))
	require.NoError(t, store.IndexMessage(testMessageForSource("s3", "personal"), "body"))

	stats, err := store.Stats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats.TotalMessages)
	assert.Equal(t, 2, stats.MessagesBySource["work"])
	assert.Equal(t, 1, stats.MessagesBySource["personal"])
}

func TestUpdateSyncState(t *testing.T) {
	store := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, store.UpdateSyncState("source_work", now, 5))
	require.NoError(t, store.UpdateSyncState("source_work", now, 3)) // cumulative

	var count int

	err := store.db.QueryRow("SELECT message_count FROM sync_state WHERE source_name = ?", "source_work").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 8, count)
}

func TestNewStore_InvalidPath(t *testing.T) {
	_, err := NewStore("/nonexistent/deeply/nested/path/archive.db")
	assert.Error(t, err)
}

// helpers

func testMessage(gmailID string) Message {
	return testMessageForSource(gmailID, "test_source")
}

func testMessageForSource(gmailID, sourceName string) Message {
	return Message{
		GmailID:         gmailID,
		ThreadID:        "thread_" + gmailID,
		RFC822MessageID: "<" + gmailID + "@mail.example.com>",
		Subject:         "Test subject for " + gmailID,
		FromAddr:        "sender@example.com",
		ToAddrs:         []string{"recipient@example.com"},
		CCAddrs:         []string{},
		DateSent:        time.Now().Add(-24 * time.Hour),
		Labels:          []string{"INBOX"},
		EMLPath:         filepath.Join(os.TempDir(), sourceName, gmailID+".eml"),
		SizeBytes:       1024,
		HasAttachments:  false,
		SourceName:      sourceName,
	}
}

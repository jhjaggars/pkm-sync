package sinks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	gmail "pkm-sync/internal/sources/google/gmail"
	"pkm-sync/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFetcher is a RawMessageFetcher for testing.
type mockFetcher struct {
	calls []string
	data  map[string][]byte
	errOn map[string]error
}

func newMockFetcher() *mockFetcher {
	return &mockFetcher{
		data:  make(map[string][]byte),
		errOn: make(map[string]error),
	}
}

func (m *mockFetcher) GetMessageRaw(messageID string) ([]byte, error) {
	m.calls = append(m.calls, messageID)

	if err, ok := m.errOn[messageID]; ok {
		return nil, err
	}

	if data, ok := m.data[messageID]; ok {
		return data, nil
	}
	// Return a minimal valid EML by default.
	return []byte(fmt.Sprintf("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test %s\r\n\r\nBody of %s\r\n", messageID, messageID)), nil
}

func newTestArchiveSink(t *testing.T) (*ArchiveSink, *mockFetcher, string) {
	t.Helper()

	dir := t.TempDir()
	emlDir := filepath.Join(dir, "eml")
	dbPath := filepath.Join(dir, "archive.db")

	fetcher := newMockFetcher()

	sink, err := NewArchiveSink(ArchiveSinkConfig{
		EMLDir:       emlDir,
		DBPath:       dbPath,
		RequestDelay: 0,
		MaxPerSync:   0,
	}, fetcher)
	require.NoError(t, err)

	t.Cleanup(func() { sink.Close() })

	return sink, fetcher, dir
}

func TestArchiveSink_SkipsNonGmailItems(t *testing.T) {
	sink, fetcher, _ := newTestArchiveSink(t)

	calItem := makeGmailItem("cal1", "google_calendar", false)
	err := sink.Write(context.Background(), []models.FullItem{calItem})
	require.NoError(t, err)
	assert.Empty(t, fetcher.calls)
}

func TestArchiveSink_SkipsThreadItems(t *testing.T) {
	sink, fetcher, _ := newTestArchiveSink(t)

	threadItem := makeGmailItem("thread_abc123", "gmail", true)
	err := sink.Write(context.Background(), []models.FullItem{threadItem})
	require.NoError(t, err)
	assert.Empty(t, fetcher.calls)
}

func TestArchiveSink_WritesEMLFile(t *testing.T) {
	sink, _, dir := newTestArchiveSink(t)

	item := makeGmailItem("msg1abc", "gmail", false)
	err := sink.Write(context.Background(), []models.FullItem{item})
	require.NoError(t, err)

	// EML file should exist at <emlDir>/<sourceName>/<gmailID>.eml
	emlPath := filepath.Join(dir, "eml", "gmail", "msg1abc.eml")
	data, err := os.ReadFile(emlPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Subject: Test msg1abc")
}

func TestArchiveSink_DedupSkipsAlreadyArchived(t *testing.T) {
	sink, fetcher, _ := newTestArchiveSink(t)

	item := makeGmailItem("dedup1", "gmail", false)

	// First write — should archive.
	err := sink.Write(context.Background(), []models.FullItem{item})
	require.NoError(t, err)
	assert.Len(t, fetcher.calls, 1)

	// Second write — should skip (already archived).
	err = sink.Write(context.Background(), []models.FullItem{item})
	require.NoError(t, err)
	assert.Len(t, fetcher.calls, 1, "should not have fetched again")
}

func TestArchiveSink_RespectsMaxPerSync(t *testing.T) {
	dir := t.TempDir()
	fetcher := newMockFetcher()

	sink, err := NewArchiveSink(ArchiveSinkConfig{
		EMLDir:     filepath.Join(dir, "eml"),
		DBPath:     filepath.Join(dir, "archive.db"),
		MaxPerSync: 2,
	}, fetcher)
	require.NoError(t, err)

	defer sink.Close()

	items := []models.FullItem{
		makeGmailItem("limit1", "gmail", false),
		makeGmailItem("limit2", "gmail", false),
		makeGmailItem("limit3", "gmail", false),
		makeGmailItem("limit4", "gmail", false),
	}

	err = sink.Write(context.Background(), items)
	require.NoError(t, err)
	assert.Len(t, fetcher.calls, 2)
}

func TestArchiveSink_FetchErrorContinues(t *testing.T) {
	sink, fetcher, _ := newTestArchiveSink(t)
	fetcher.errOn["fail1"] = fmt.Errorf("simulated fetch error")

	items := []models.FullItem{
		makeGmailItem("fail1", "gmail", false),
		makeGmailItem("ok2", "gmail", false),
	}

	err := sink.Write(context.Background(), items)
	require.NoError(t, err)
	// "ok2" should still have been fetched.
	assert.Contains(t, fetcher.calls, "ok2")
}

func TestArchiveSink_ContextCancellation(t *testing.T) {
	sink, _, _ := newTestArchiveSink(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	items := []models.FullItem{
		makeGmailItem("ctx1", "gmail", false),
	}

	err := sink.Write(ctx, items)
	// Should return context error (canceled before per-source loop or inside it).
	assert.Error(t, err)
}

// makeGmailItem creates a test FullItem for archive sink tests.
func makeGmailItem(id, sourceType string, isThread bool) models.FullItem {
	metadata := map[string]interface{}{
		"thread_id":  "thread_" + id,
		"message_id": "<" + id + "@example.com>",
		"from":       "sender@example.com",
		"labels":     []interface{}{"INBOX"},
	}

	if isThread {
		metadata["message_count"] = 3
	}

	item := &models.BasicItem{
		ID:         id,
		Title:      "Subject for " + id,
		Content:    "Body of " + id,
		SourceType: sourceType,
		ItemType:   "email",
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		UpdatedAt:  time.Now().Add(-1 * time.Hour),
		Tags:       []string{"source:gmail"},
		Metadata:   metadata,
	}

	return item
}

// The Gmail source stores typed EmailRecipient values in item metadata; the
// sink must extract addresses from them (a silent type-switch miss left
// from_addr/to_addrs/cc_addrs empty for every archived message).
func TestBuildArchiveMessage_TypedRecipients(t *testing.T) {
	item := models.NewBasicItem("g1", "Subject line")
	item.SetMetadata(map[string]interface{}{
		"thread_id": "th1",
		"from":      gmail.EmailRecipient{Name: "Alice A", Email: "alice@example.com"},
		"to": []gmail.EmailRecipient{
			{Name: "Bob", Email: "bob@example.com"},
			{Email: "carol@example.com"},
		},
		"cc": []gmail.EmailRecipient{{Email: "dave@example.com"}},
	})

	msg := buildArchiveMessage(item, "g1", "/tmp/g1.eml", "gmail_work", 42)

	assert.Equal(t, `"Alice A" <alice@example.com>`, msg.FromAddr)
	assert.Equal(t, []string{"bob@example.com", "carol@example.com"}, msg.ToAddrs)
	assert.Equal(t, []string{"dave@example.com"}, msg.CCAddrs)
}

// Map-shaped metadata (items that round-tripped through JSON) keeps working.
func TestBuildArchiveMessage_MapRecipients(t *testing.T) {
	item := models.NewBasicItem("g2", "Subject line")
	item.SetMetadata(map[string]interface{}{
		"from": map[string]interface{}{"name": "", "email": "eve@example.com"},
		"to":   []interface{}{map[string]interface{}{"email": "frank@example.com"}},
	})

	msg := buildArchiveMessage(item, "g2", "/tmp/g2.eml", "gmail_work", 1)

	assert.Equal(t, "eve@example.com", msg.FromAddr)
	assert.Equal(t, []string{"frank@example.com"}, msg.ToAddrs)
}

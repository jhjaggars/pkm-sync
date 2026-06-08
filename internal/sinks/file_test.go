package sinks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkm-sync/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestFileSink(t *testing.T) (*FileSink, string) {
	t.Helper()

	dir := t.TempDir()
	sink, err := NewFileSink("obsidian", dir, nil)
	require.NoError(t, err)

	return sink, dir
}

func makeTestItem(id, title, content string) models.FullItem {
	return &models.BasicItem{
		ID:         id,
		Title:      title,
		Content:    content,
		SourceType: "jira",
		ItemType:   "issue",
		CreatedAt:  time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Tags:       []string{"test"},
		Metadata:   map[string]interface{}{"status": "Open"},
	}
}

func TestWriteItem_SkipsUnchangedFile(t *testing.T) {
	sink, dir := newTestFileSink(t)
	item := makeTestItem("TEST-1", "Test Issue", "Some content")

	// First write creates the file.
	err := sink.Write(context.Background(), []models.FullItem{item})
	require.NoError(t, err)

	filePath := filepath.Join(dir, sink.fmt.formatFilename("Test Issue"))
	info1, err := os.Stat(filePath)
	require.NoError(t, err)

	mtime1 := info1.ModTime()

	// Ensure filesystem mtime granularity is exceeded.
	time.Sleep(50 * time.Millisecond)

	// Second write with identical content should not update mtime.
	err = sink.Write(context.Background(), []models.FullItem{item})
	require.NoError(t, err)

	info2, err := os.Stat(filePath)
	require.NoError(t, err)

	assert.Equal(t, mtime1, info2.ModTime(), "mtime should not change for unchanged content")
}

func TestWriteItem_UpdatesChangedFile(t *testing.T) {
	sink, dir := newTestFileSink(t)
	item1 := makeTestItem("TEST-1", "Test Issue", "Original content")

	err := sink.Write(context.Background(), []models.FullItem{item1})
	require.NoError(t, err)

	filePath := filepath.Join(dir, sink.fmt.formatFilename("Test Issue"))
	original, err := os.ReadFile(filePath)
	require.NoError(t, err)

	// Write with different content should update.
	item2 := makeTestItem("TEST-1", "Test Issue", "Updated content")
	err = sink.Write(context.Background(), []models.FullItem{item2})
	require.NoError(t, err)

	updated, err := os.ReadFile(filePath)
	require.NoError(t, err)

	assert.NotEqual(t, string(original), string(updated), "file content should change")
	assert.Contains(t, string(updated), "Updated content")
}

func TestWriteItem_CreatesNewFile(t *testing.T) {
	sink, dir := newTestFileSink(t)
	item := makeTestItem("TEST-1", "New Issue", "Brand new")

	err := sink.Write(context.Background(), []models.FullItem{item})
	require.NoError(t, err)

	filePath := filepath.Join(dir, sink.fmt.formatFilename("New Issue"))
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Brand new")
}

package sinks

import (
	"context"
	"os"
	"testing"
	"time"

	"pkm-sync/internal/vectorstore"
	"pkm-sync/pkg/models"
)

// indexResult holds the count returns from indexSource for call sites that
// need to assert on them, avoiding dogsled violations from blank identifiers.
type indexResult struct {
	indexed, metaOnly, skipped, failed int
}

// mustIndexSource calls indexSource, fatals on error, and returns the counts.
func mustIndexSource(t *testing.T, sink *VectorSink, ctx context.Context, sourceName string, items []models.FullItem) indexResult {
	t.Helper()

	indexed, metaOnly, skipped, failed, err := sink.indexSource(ctx, sourceName, items)
	if err != nil {
		t.Fatalf("indexSource(%q): %v", sourceName, err)
	}

	return indexResult{indexed: indexed, metaOnly: metaOnly, skipped: skipped, failed: failed}
}

// newTestVectorSink creates a VectorSink backed by a temp on-disk DB (no embeddings).
func newTestVectorSink(t *testing.T) (*VectorSink, func()) {
	t.Helper()

	tmp, err := os.CreateTemp("", "vector_staleness_*.db")
	if err != nil {
		t.Fatalf("failed to create temp db file: %v", err)
	}

	tmp.Close()

	store, err := vectorstore.NewStore(tmp.Name(), 0)
	if err != nil {
		os.Remove(tmp.Name())
		t.Fatalf("failed to open vector store: %v", err)
	}

	sink := &VectorSink{
		store:    store,
		provider: nil, // metadata-only mode
		cfg:      VectorSinkConfig{},
	}

	cleanup := func() {
		store.Close()
		os.Remove(tmp.Name())
	}

	return sink, cleanup
}

// makeGmailItems returns n minimal gmail FullItems all sharing the given threadID.
func makeGmailItems(threadID string, n int) []models.FullItem {
	items := make([]models.FullItem, n)
	now := time.Now()

	for i := range items {
		item := models.NewBasicItem(threadID+"-msg"+string(rune('0'+i)), "Subject")
		item.SetSourceType("gmail")
		item.SetCreatedAt(now.Add(time.Duration(i) * time.Minute))
		item.SetMetadata(map[string]any{"thread_id": threadID})
		items[i] = item
	}

	return items
}

// TestIndexSource_SkipsUnchangedThreads verifies that a thread already indexed
// with the same message count is skipped on a subsequent sync.
func TestIndexSource_SkipsUnchangedThreads(t *testing.T) {
	sink, cleanup := newTestVectorSink(t)
	defer cleanup()

	ctx := context.Background()
	items := makeGmailItems("thread-abc", 3)

	// First index.
	first := mustIndexSource(t, sink, ctx, "gmail_work", items)

	if first.skipped != 0 {
		t.Errorf("first pass: expected 0 skipped, got %d", first.skipped)
	}

	if first.indexed+first.metaOnly != 1 {
		t.Errorf("first pass: expected 1 document written, got indexed=%d metaOnly=%d", first.indexed, first.metaOnly)
	}

	// Second index with same items — should be skipped entirely.
	second := mustIndexSource(t, sink, ctx, "gmail_work", items)

	if second.skipped != 1 {
		t.Errorf("second pass: expected 1 skipped, got %d", second.skipped)
	}

	if second.failed != 0 {
		t.Errorf("second pass: expected 0 failed, got %d", second.failed)
	}
}

// TestIndexSource_ReindexesGrownThreads verifies that a thread with more messages
// than its stored count is re-indexed automatically without --reindex.
func TestIndexSource_ReindexesGrownThreads(t *testing.T) {
	sink, cleanup := newTestVectorSink(t)
	defer cleanup()

	ctx := context.Background()

	// First index with 2 messages.
	items2 := makeGmailItems("thread-xyz", 2)
	mustIndexSource(t, sink, ctx, "gmail_work", items2)

	// Verify stored count is 2.
	counts, err := sink.store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("GetIndexedThreadCounts failed: %v", err)
	}

	if counts["thread-xyz"] != 2 {
		t.Errorf("expected stored count=2 after first index, got %d", counts["thread-xyz"])
	}

	// Second index with 5 messages — thread has grown.
	items5 := makeGmailItems("thread-xyz", 5)
	res := mustIndexSource(t, sink, ctx, "gmail_work", items5)

	if res.skipped != 0 {
		t.Errorf("grown thread should not be skipped, got skipped=%d", res.skipped)
	}

	if res.metaOnly != 1 {
		t.Errorf("grown thread should produce 1 metadata-only write, got %d", res.metaOnly)
	}

	// Verify stored count updated to 5.
	counts2, err := sink.store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("GetIndexedThreadCounts failed after update: %v", err)
	}

	if counts2["thread-xyz"] != 5 {
		t.Errorf("expected stored count=5 after re-index, got %d", counts2["thread-xyz"])
	}
}

// TestIndexSource_SkipsSameSizeThread verifies that a thread re-delivered with
// the same message count as stored is still skipped (not re-indexed needlessly).
func TestIndexSource_SkipsSameSizeThread(t *testing.T) {
	sink, cleanup := newTestVectorSink(t)
	defer cleanup()

	ctx := context.Background()
	items := makeGmailItems("thread-same", 4)

	mustIndexSource(t, sink, ctx, "gmail_work", items)

	res := mustIndexSource(t, sink, ctx, "gmail_work", items)

	if res.skipped != 1 {
		t.Errorf("expected thread with unchanged count to be skipped, got skipped=%d", res.skipped)
	}
}

// TestIndexSource_ReindexFlagBypassesStalenessCheck verifies that --reindex
// forces re-indexing even when message count hasn't changed.
func TestIndexSource_ReindexFlagBypassesStalenessCheck(t *testing.T) {
	sink, cleanup := newTestVectorSink(t)
	defer cleanup()

	ctx := context.Background()
	items := makeGmailItems("thread-force", 3)

	mustIndexSource(t, sink, ctx, "gmail_work", items)

	// Enable reindex flag.
	sink.cfg.Reindex = true

	res := mustIndexSource(t, sink, ctx, "gmail_work", items)

	if res.skipped != 0 {
		t.Errorf("--reindex should skip nothing, got skipped=%d", res.skipped)
	}

	if res.metaOnly != 1 {
		t.Errorf("--reindex should write 1 doc, got metaOnly=%d", res.metaOnly)
	}
}

// makeThreadModeItem creates a single FullItem representing an entire Gmail thread,
// as produced by FromGmailThread in thread mode (include_threads: true).
// The item carries the true message count in metadata["message_count"].
func makeThreadModeItem(threadID string, messageCount int) models.FullItem {
	item := models.NewBasicItem("thread_"+threadID, "Re: Subject")
	item.SetSourceType("gmail")
	item.SetCreatedAt(time.Now())
	item.SetMetadata(map[string]any{
		"thread_id":     threadID,
		"message_count": messageCount, // real count set by FromGmailThread
	})

	return item
}

// TestIndexSource_ThreadMode_SkipsUnchangedThread verifies that a thread-mode
// thread (one FullItem with message_count in metadata) is skipped when the
// stored count matches — even though len(group.messages) is always 1.
func TestIndexSource_ThreadMode_SkipsUnchangedThread(t *testing.T) {
	sink, cleanup := newTestVectorSink(t)
	defer cleanup()

	ctx := context.Background()

	// First index: thread with 5 real messages, represented as 1 FullItem.
	first := []models.FullItem{makeThreadModeItem("thr-mode-1", 5)}
	mustIndexSource(t, sink, ctx, "gmail_work", first)

	// Verify the DB stored 5, not 1.
	counts, err := sink.store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("GetIndexedThreadCounts: %v", err)
	}

	if counts["thr-mode-1"] != 5 {
		t.Errorf("expected DB message_count=5, got %d (thread mode metadata not used)", counts["thr-mode-1"])
	}

	// Second index: same 5 messages — should be skipped.
	second := []models.FullItem{makeThreadModeItem("thr-mode-1", 5)}
	res := mustIndexSource(t, sink, ctx, "gmail_work", second)

	if res.skipped != 1 {
		t.Errorf("expected thread-mode unchanged thread to be skipped, got skipped=%d", res.skipped)
	}
}

// TestIndexSource_ThreadMode_ReindexesGrownThread verifies that a thread-mode
// thread with more messages than stored is re-indexed automatically.
// This is the primary regression test for the critical bug where thread mode
// always saw len(group.messages)==1 and never detected growth.
func TestIndexSource_ThreadMode_ReindexesGrownThread(t *testing.T) {
	sink, cleanup := newTestVectorSink(t)
	defer cleanup()

	ctx := context.Background()

	// First index: thread has 3 messages.
	first := []models.FullItem{makeThreadModeItem("thr-growing", 3)}
	mustIndexSource(t, sink, ctx, "gmail_work", first)

	// Thread grows to 8 messages.
	second := []models.FullItem{makeThreadModeItem("thr-growing", 8)}
	res := mustIndexSource(t, sink, ctx, "gmail_work", second)

	if res.skipped != 0 {
		t.Errorf("thread-mode grown thread should not be skipped, got skipped=%d", res.skipped)
	}

	if res.metaOnly != 1 {
		t.Errorf("thread-mode grown thread should produce 1 write, got metaOnly=%d", res.metaOnly)
	}

	// Verify the DB now stores 8.
	counts, err := sink.store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("GetIndexedThreadCounts: %v", err)
	}

	if counts["thr-growing"] != 8 {
		t.Errorf("expected DB message_count=8 after growth, got %d", counts["thr-growing"])
	}
}

// TestEffectiveMessageCount verifies the helper correctly extracts message_count
// from thread-mode metadata vs falling back to len(group.messages) for message mode.
func TestEffectiveMessageCount(t *testing.T) {
	tests := []struct {
		name     string
		items    []models.FullItem
		expected int
	}{
		{
			name: "message mode: multiple items, no metadata count",
			items: func() []models.FullItem {
				items := makeGmailItems("t1", 4)

				return items
			}(),
			expected: 4,
		},
		{
			name:     "thread mode: single item with metadata count=11",
			items:    []models.FullItem{makeThreadModeItem("t2", 11)},
			expected: 11,
		},
		{
			name:     "thread mode: single item with metadata count=1 (no override needed)",
			items:    []models.FullItem{makeThreadModeItem("t3", 1)},
			expected: 1,
		},
		{
			name:     "single message item without message_count metadata",
			items:    []models.FullItem{makeGmailItems("t4", 1)[0]},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			group := &itemGroup{
				threadID:   "test",
				messages:   tt.items,
				startTime:  now,
				endTime:    now,
				sourceName: "gmail_work",
			}

			got := effectiveMessageCount(group)
			if got != tt.expected {
				t.Errorf("effectiveMessageCount() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// TestIndexSource_MultipleThreadsSomeGrown verifies correct per-thread staleness
// decisions when a mix of unchanged and grown threads is presented.
func TestIndexSource_MultipleThreadsSomeGrown(t *testing.T) {
	sink, cleanup := newTestVectorSink(t)
	defer cleanup()

	ctx := context.Background()

	// Index two threads at 2 messages each.
	first := append(makeGmailItems("thread-stable", 2), makeGmailItems("thread-growing", 2)...)
	mustIndexSource(t, sink, ctx, "gmail_work", first)

	// Second pass: stable thread unchanged (2 msgs), growing thread now has 5.
	second := append(makeGmailItems("thread-stable", 2), makeGmailItems("thread-growing", 5)...)
	res := mustIndexSource(t, sink, ctx, "gmail_work", second)

	if res.skipped != 1 {
		t.Errorf("expected 1 skipped (stable thread), got %d", res.skipped)
	}

	if res.metaOnly != 1 {
		t.Errorf("expected 1 written (growing thread), got %d", res.metaOnly)
	}

	// Confirm the growing thread's count was updated.
	counts, err := sink.store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("GetIndexedThreadCounts: %v", err)
	}

	if counts["thread-growing"] != 5 {
		t.Errorf("expected thread-growing count=5, got %d", counts["thread-growing"])
	}

	if counts["thread-stable"] != 2 {
		t.Errorf("expected thread-stable count=2 (unchanged), got %d", counts["thread-stable"])
	}
}

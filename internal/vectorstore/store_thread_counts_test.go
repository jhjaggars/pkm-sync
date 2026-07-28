package vectorstore

import (
	"testing"
	"time"
)

func TestStore_GetIndexedThreadCounts_Empty(t *testing.T) {
	store, err := NewStore(":memory:", 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	counts, err := store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(counts) != 0 {
		t.Errorf("expected empty map for empty store, got %d entries", len(counts))
	}
}

func TestStore_GetIndexedThreadCounts_ReturnsCorrectCounts(t *testing.T) {
	store, err := NewStore(":memory:", 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	docs := []Document{
		{
			SourceID:     "msg1",
			ThreadID:     "thread1",
			Title:        "Thread 1",
			Content:      "content",
			SourceType:   "gmail",
			SourceName:   "gmail_work",
			MessageCount: 3,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			SourceID:     "msg2",
			ThreadID:     "thread2",
			Title:        "Thread 2",
			Content:      "content",
			SourceType:   "gmail",
			SourceName:   "gmail_work",
			MessageCount: 7,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	for _, doc := range docs {
		if err := store.UpsertDocument(doc, nil); err != nil {
			t.Fatalf("failed to upsert document: %v", err)
		}
	}

	counts, err := store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(counts) != 2 {
		t.Errorf("expected 2 entries, got %d", len(counts))
	}

	if counts["thread1"] != 3 {
		t.Errorf("expected thread1 count=3, got %d", counts["thread1"])
	}

	if counts["thread2"] != 7 {
		t.Errorf("expected thread2 count=7, got %d", counts["thread2"])
	}
}

func TestStore_GetIndexedThreadCounts_FiltersBySourceName(t *testing.T) {
	store, err := NewStore(":memory:", 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	docs := []Document{
		{
			SourceID:     "msg1",
			ThreadID:     "thread1",
			Title:        "Work thread",
			Content:      "content",
			SourceType:   "gmail",
			SourceName:   "gmail_work",
			MessageCount: 5,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			SourceID:     "msg2",
			ThreadID:     "thread2",
			Title:        "Personal thread",
			Content:      "content",
			SourceType:   "gmail",
			SourceName:   "gmail_personal",
			MessageCount: 2,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	for _, doc := range docs {
		if err := store.UpsertDocument(doc, nil); err != nil {
			t.Fatalf("failed to upsert document: %v", err)
		}
	}

	counts, err := store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(counts) != 1 {
		t.Errorf("expected 1 entry for gmail_work, got %d", len(counts))
	}

	if counts["thread1"] != 5 {
		t.Errorf("expected thread1 count=5, got %d", counts["thread1"])
	}

	if _, ok := counts["thread2"]; ok {
		t.Error("thread2 from gmail_personal should not appear in gmail_work results")
	}
}

func TestStore_GetIndexedThreadCounts_ReflectsUpsertUpdate(t *testing.T) {
	store, err := NewStore(":memory:", 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	doc := Document{
		SourceID:     "msg1",
		ThreadID:     "thread1",
		Title:        "Growing thread",
		Content:      "content",
		SourceType:   "gmail",
		SourceName:   "gmail_work",
		MessageCount: 1,
		Metadata:     map[string]interface{}{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := store.UpsertDocument(doc, nil); err != nil {
		t.Fatalf("failed to upsert: %v", err)
	}

	counts, err := store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if counts["thread1"] != 1 {
		t.Errorf("expected initial count=1, got %d", counts["thread1"])
	}

	// Simulate thread growing — upsert with higher message count.
	doc.MessageCount = 5
	doc.Content = "more content now"

	if err := store.UpsertDocument(doc, nil); err != nil {
		t.Fatalf("failed to upsert updated doc: %v", err)
	}

	counts, err = store.GetIndexedThreadCounts("gmail_work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if counts["thread1"] != 5 {
		t.Errorf("expected updated count=5, got %d", counts["thread1"])
	}
}

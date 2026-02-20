package vectorstore

import (
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if store.dimensions != 3 {
		t.Errorf("expected dimensions 3, got %d", store.dimensions)
	}
}

func TestStore_UpsertDocument(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	doc := Document{
		SourceID:     "msg123",
		ThreadID:     "thread456",
		Title:        "Test Email",
		Content:      "This is a test email",
		SourceType:   "gmail",
		SourceName:   "gmail_work",
		MessageCount: 1,
		Metadata: map[string]interface{}{
			"from": "test@example.com",
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	embedding := []float32{0.1, 0.2, 0.3}

	err = store.UpsertDocument(doc, embedding)
	if err != nil {
		t.Fatalf("failed to upsert document: %v", err)
	}

	// Verify document was inserted
	indexed, err := store.IsIndexed("thread456", "gmail_work")
	if err != nil {
		t.Fatalf("failed to check if indexed: %v", err)
	}

	if !indexed {
		t.Error("document should be indexed")
	}
}

func TestStore_UpsertDocument_Update(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	doc := Document{
		SourceID:     "msg123",
		ThreadID:     "thread456",
		Title:        "Test Email",
		Content:      "Original content",
		SourceType:   "gmail",
		SourceName:   "gmail_work",
		MessageCount: 1,
		Metadata:     map[string]interface{}{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	embedding := []float32{0.1, 0.2, 0.3}

	err = store.UpsertDocument(doc, embedding)
	if err != nil {
		t.Fatalf("failed to insert document: %v", err)
	}

	// Update the document
	doc.Content = "Updated content"
	doc.MessageCount = 2
	updatedEmbedding := []float32{0.4, 0.5, 0.6}

	err = store.UpsertDocument(doc, updatedEmbedding)
	if err != nil {
		t.Fatalf("failed to update document: %v", err)
	}

	// Verify only one document exists
	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.TotalDocuments != 1 {
		t.Errorf("expected 1 document after update, got %d", stats.TotalDocuments)
	}
}

func TestStore_Search(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert test documents
	docs := []struct {
		doc       Document
		embedding []float32
	}{
		{
			doc: Document{
				SourceID:     "msg1",
				ThreadID:     "thread1",
				Title:        "Email about meetings",
				Content:      "Let's schedule a meeting",
				SourceType:   "gmail",
				SourceName:   "gmail_work",
				MessageCount: 1,
				Metadata:     map[string]interface{}{},
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			embedding: []float32{0.1, 0.2, 0.3},
		},
		{
			doc: Document{
				SourceID:     "msg2",
				ThreadID:     "thread2",
				Title:        "Project update",
				Content:      "Here's the latest project status",
				SourceType:   "gmail",
				SourceName:   "gmail_work",
				MessageCount: 1,
				Metadata:     map[string]interface{}{},
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			embedding: []float32{0.9, 0.8, 0.7},
		},
	}

	for _, d := range docs {
		if err := store.UpsertDocument(d.doc, d.embedding); err != nil {
			t.Fatalf("failed to insert document: %v", err)
		}
	}

	// Search with query similar to first document
	queryEmbedding := []float32{0.15, 0.25, 0.35}

	results, err := store.Search(queryEmbedding, 10, SearchFilters{})
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// First result should be closer (thread1)
	if results[0].ThreadID != "thread1" {
		t.Errorf("expected first result to be thread1, got %s", results[0].ThreadID)
	}

	// Verify scores are calculated
	if results[0].Score <= 0 || results[0].Score > 1 {
		t.Errorf("expected score between 0 and 1, got %f", results[0].Score)
	}
}

func TestStore_Search_WithFilters(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert documents from different sources
	docs := []struct {
		doc       Document
		embedding []float32
	}{
		{
			doc: Document{
				SourceID:     "msg1",
				ThreadID:     "thread1",
				Title:        "Work email",
				Content:      "Work stuff",
				SourceType:   "gmail",
				SourceName:   "gmail_work",
				MessageCount: 1,
				Metadata:     map[string]interface{}{},
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			embedding: []float32{0.1, 0.2, 0.3},
		},
		{
			doc: Document{
				SourceID:     "msg2",
				ThreadID:     "thread2",
				Title:        "Personal email",
				Content:      "Personal stuff",
				SourceType:   "gmail",
				SourceName:   "gmail_personal",
				MessageCount: 1,
				Metadata:     map[string]interface{}{},
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			embedding: []float32{0.1, 0.2, 0.3},
		},
	}

	for _, d := range docs {
		if err := store.UpsertDocument(d.doc, d.embedding); err != nil {
			t.Fatalf("failed to insert document: %v", err)
		}
	}

	// Search with source filter
	queryEmbedding := []float32{0.1, 0.2, 0.3}

	results, err := store.Search(queryEmbedding, 10, SearchFilters{
		SourceName: "gmail_work",
	})
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result with source filter, got %d", len(results))
	}

	if results[0].SourceName != "gmail_work" {
		t.Errorf("expected result from gmail_work, got %s", results[0].SourceName)
	}
}

func TestStore_IsIndexed(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Check non-existent thread
	indexed, err := store.IsIndexed("nonexistent", "gmail_work")
	if err != nil {
		t.Fatalf("failed to check if indexed: %v", err)
	}

	if indexed {
		t.Error("non-existent thread should not be indexed")
	}

	// Insert a document
	doc := Document{
		SourceID:     "msg1",
		ThreadID:     "thread1",
		Title:        "Test",
		Content:      "Test content",
		SourceType:   "gmail",
		SourceName:   "gmail_work",
		MessageCount: 1,
		Metadata:     map[string]interface{}{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	embedding := []float32{0.1, 0.2, 0.3}
	if err := store.UpsertDocument(doc, embedding); err != nil {
		t.Fatalf("failed to insert document: %v", err)
	}

	// Check indexed thread
	indexed, err = store.IsIndexed("thread1", "gmail_work")
	if err != nil {
		t.Fatalf("failed to check if indexed: %v", err)
	}

	if !indexed {
		t.Error("inserted thread should be indexed")
	}
}

func TestStore_GetIndexedThreadIDs(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert documents
	docs := []Document{
		{
			SourceID:     "msg1",
			ThreadID:     "thread1",
			Title:        "Test 1",
			Content:      "Content 1",
			SourceType:   "gmail",
			SourceName:   "gmail_work",
			MessageCount: 1,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			SourceID:     "msg2",
			ThreadID:     "thread2",
			Title:        "Test 2",
			Content:      "Content 2",
			SourceType:   "gmail",
			SourceName:   "gmail_work",
			MessageCount: 1,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			SourceID:     "msg3",
			ThreadID:     "thread3",
			Title:        "Test 3",
			Content:      "Content 3",
			SourceType:   "gmail",
			SourceName:   "gmail_personal",
			MessageCount: 1,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	embedding := []float32{0.1, 0.2, 0.3}
	for _, doc := range docs {
		if err := store.UpsertDocument(doc, embedding); err != nil {
			t.Fatalf("failed to insert document: %v", err)
		}
	}

	// Get indexed thread IDs for gmail_work
	indexed, err := store.GetIndexedThreadIDs("gmail_work")
	if err != nil {
		t.Fatalf("failed to get indexed thread IDs: %v", err)
	}

	if len(indexed) != 2 {
		t.Errorf("expected 2 indexed threads for gmail_work, got %d", len(indexed))
	}

	if !indexed["thread1"] {
		t.Error("thread1 should be indexed")
	}

	if !indexed["thread2"] {
		t.Error("thread2 should be indexed")
	}

	if indexed["thread3"] {
		t.Error("thread3 should not be indexed for gmail_work")
	}
}

func TestStore_Stats(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert test documents
	docs := []Document{
		{
			SourceID:     "msg1",
			ThreadID:     "thread1",
			Title:        "Test 1",
			Content:      "Content 1",
			SourceType:   "gmail",
			SourceName:   "gmail_work",
			MessageCount: 2,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now().Add(-24 * time.Hour),
			UpdatedAt:    time.Now(),
		},
		{
			SourceID:     "msg2",
			ThreadID:     "thread2",
			Title:        "Test 2",
			Content:      "Content 2",
			SourceType:   "gmail",
			SourceName:   "gmail_personal",
			MessageCount: 3,
			Metadata:     map[string]interface{}{},
			CreatedAt:    time.Now().Add(-48 * time.Hour),
			UpdatedAt:    time.Now(),
		},
	}

	embedding := []float32{0.1, 0.2, 0.3}
	for _, doc := range docs {
		if err := store.UpsertDocument(doc, embedding); err != nil {
			t.Fatalf("failed to insert document: %v", err)
		}
	}

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.TotalDocuments != 2 {
		t.Errorf("expected 2 total documents, got %d", stats.TotalDocuments)
	}

	if stats.TotalThreads != 2 {
		t.Errorf("expected 2 total threads, got %d", stats.TotalThreads)
	}

	if stats.DocumentsBySource["gmail_work"] != 1 {
		t.Errorf("expected 1 document from gmail_work, got %d", stats.DocumentsBySource["gmail_work"])
	}

	if stats.DocumentsBySource["gmail_personal"] != 1 {
		t.Errorf("expected 1 document from gmail_personal, got %d", stats.DocumentsBySource["gmail_personal"])
	}

	if stats.DocumentsByType["gmail"] != 2 {
		t.Errorf("expected 2 documents of type gmail, got %d", stats.DocumentsByType["gmail"])
	}

	expectedAvg := 2.5
	if stats.AverageMessageCount != expectedAvg {
		t.Errorf("expected average message count %f, got %f", expectedAvg, stats.AverageMessageCount)
	}

	if stats.OldestDocument.IsZero() {
		t.Error("oldest document should not be zero")
	}

	if stats.NewestDocument.IsZero() {
		t.Error("newest document should not be zero")
	}
}

func TestStore_UpsertDocument_WrongDimensions(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	doc := Document{
		SourceID:     "msg1",
		ThreadID:     "thread1",
		Title:        "Test",
		Content:      "Test content",
		SourceType:   "gmail",
		SourceName:   "gmail_work",
		MessageCount: 1,
		Metadata:     map[string]interface{}{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	wrongEmbedding := []float32{0.1, 0.2} // Only 2 dimensions instead of 3

	err = store.UpsertDocument(doc, wrongEmbedding)
	if err == nil {
		t.Fatal("expected error for wrong embedding dimensions")
	}
}

func TestStore_Search_WrongDimensions(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	wrongQuery := []float32{0.1, 0.2} // Only 2 dimensions instead of 3

	_, err = store.Search(wrongQuery, 10, SearchFilters{})
	if err == nil {
		t.Fatal("expected error for wrong query dimensions")
	}
}

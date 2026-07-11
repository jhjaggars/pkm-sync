package vectorstore

import (
	"strings"
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

func TestStore_NewestDocumentTimeBySource(t *testing.T) {
	store, err := NewStore(":memory:", 3)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Empty store returns zero time, no error.
	ts, err := store.NewestDocumentTimeBySource("gmail_work")
	if err != nil {
		t.Fatalf("unexpected error on empty store: %v", err)
	}

	if !ts.IsZero() {
		t.Errorf("expected zero time for empty store, got %v", ts)
	}

	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	docs := []Document{
		{SourceID: "a", ThreadID: "t1", SourceType: "gmail", SourceName: "gmail_work", Metadata: map[string]interface{}{}, CreatedAt: older, UpdatedAt: older},
		{SourceID: "b", ThreadID: "t2", SourceType: "gmail", SourceName: "gmail_work", Metadata: map[string]interface{}{}, CreatedAt: newer, UpdatedAt: newer},
		{SourceID: "c", ThreadID: "t3", SourceType: "slack", SourceName: "slack_redhat", Metadata: map[string]interface{}{}, CreatedAt: newer, UpdatedAt: newer},
	}

	emb := []float32{0.1, 0.2, 0.3}
	for _, d := range docs {
		if err := store.UpsertDocument(d, emb); err != nil {
			t.Fatalf("upsert failed: %v", err)
		}
	}

	ts, err = store.NewestDocumentTimeBySource("gmail_work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ts.Equal(newer) {
		t.Errorf("expected %v, got %v", newer, ts)
	}

	// Different source is unaffected.
	ts, err = store.NewestDocumentTimeBySource("slack_redhat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ts.Equal(newer) {
		t.Errorf("expected %v for slack_redhat, got %v", newer, ts)
	}
}

// TestStore_UpsertDocument_TimestampsAreUTCZFormatted guards against the
// regression where CreatedAt/UpdatedAt were formatted with time.RFC3339
// without calling .UTC() first: a Document built from a Time in a non-UTC
// Location would persist a local-offset suffix (e.g. "-04:00") instead of
// "Z", breaking lexicographic SQL MIN()/MAX() comparisons against rows
// written while the process happened to be in UTC.
func TestStore_UpsertDocument_TimestampsAreUTCZFormatted(t *testing.T) {
	store, err := NewStore(":memory:", 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	loc := time.FixedZone("UTC-4", -4*60*60)
	localTime := time.Date(2026, 7, 11, 15, 30, 13, 0, loc)

	doc := Document{
		SourceID:   "msg1",
		ThreadID:   "thread1",
		SourceType: "gmail",
		SourceName: "gmail_work",
		Metadata:   map[string]interface{}{},
		CreatedAt:  localTime,
		UpdatedAt:  localTime,
	}

	if err := store.UpsertDocument(doc, nil); err != nil {
		t.Fatalf("failed to upsert document: %v", err)
	}

	var createdAtStr, updatedAtStr, indexedAtStr string

	err = store.db.QueryRow(
		"SELECT created_at, updated_at, indexed_at FROM documents WHERE thread_id = ?", "thread1",
	).Scan(&createdAtStr, &updatedAtStr, &indexedAtStr)
	if err != nil {
		t.Fatalf("failed to query raw timestamps: %v", err)
	}

	for name, raw := range map[string]string{
		"created_at": createdAtStr,
		"updated_at": updatedAtStr,
		"indexed_at": indexedAtStr,
	} {
		if !strings.HasSuffix(raw, "Z") {
			t.Errorf("%s = %q, want Z-suffixed UTC RFC3339", name, raw)
		}
	}

	wantCreated := localTime.UTC().Format(time.RFC3339)
	if createdAtStr != wantCreated {
		t.Errorf("created_at = %q, want %q", createdAtStr, wantCreated)
	}
}

func TestStore_NormalizeTimestamps(t *testing.T) {
	store, err := NewStore(":memory:", 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Seed rows directly with mixed formats, bypassing UpsertDocument, to
	// simulate the pre-fix production data: some rows already Z-suffixed,
	// some carrying a local offset in one or both timestamp columns.
	seed := []struct {
		threadID, createdAt, updatedAt string
	}{
		{"t-good", "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z"},
		{"t-bad-created", "2026-07-11T15:30:13-04:00", "2026-07-11T19:30:13Z"},
		{"t-bad-both", "2026-07-11T09:00:00-04:00", "2026-07-11T09:05:00-04:00"},
	}

	for _, s := range seed {
		_, err := store.db.Exec(
			`INSERT INTO documents (source_id, thread_id, source_name, created_at, updated_at)
			 VALUES (?, ?, 'gmail_work', ?, ?)`,
			s.threadID, s.threadID, s.createdAt, s.updatedAt,
		)
		if err != nil {
			t.Fatalf("failed to seed row %s: %v", s.threadID, err)
		}
	}

	// Dry run must report the would-be fixes without writing anything.
	dryResult, err := store.NormalizeTimestamps(true)
	if err != nil {
		t.Fatalf("dry-run NormalizeTimestamps failed: %v", err)
	}

	if dryResult.RowsScanned != 3 {
		t.Errorf("dry-run RowsScanned = %d, want 3", dryResult.RowsScanned)
	}

	if dryResult.CreatedAtFixed != 2 {
		t.Errorf("dry-run CreatedAtFixed = %d, want 2", dryResult.CreatedAtFixed)
	}

	if dryResult.UpdatedAtFixed != 1 {
		t.Errorf("dry-run UpdatedAtFixed = %d, want 1", dryResult.UpdatedAtFixed)
	}

	assertRaw := func(threadID, wantCreated, wantUpdated string) {
		t.Helper()

		var createdAtStr, updatedAtStr string

		err := store.db.QueryRow(
			"SELECT created_at, updated_at FROM documents WHERE thread_id = ?", threadID,
		).Scan(&createdAtStr, &updatedAtStr)
		if err != nil {
			t.Fatalf("failed to query row %s: %v", threadID, err)
		}

		if createdAtStr != wantCreated {
			t.Errorf("%s created_at = %q, want %q (unchanged during dry-run)", threadID, createdAtStr, wantCreated)
		}

		if updatedAtStr != wantUpdated {
			t.Errorf("%s updated_at = %q, want %q (unchanged during dry-run)", threadID, updatedAtStr, wantUpdated)
		}
	}

	// Dry-run must not have written anything.
	assertRaw("t-bad-created", "2026-07-11T15:30:13-04:00", "2026-07-11T19:30:13Z")
	assertRaw("t-bad-both", "2026-07-11T09:00:00-04:00", "2026-07-11T09:05:00-04:00")

	// Real run applies the fixes.
	result, err := store.NormalizeTimestamps(false)
	if err != nil {
		t.Fatalf("NormalizeTimestamps failed: %v", err)
	}

	if result.CreatedAtFixed != 2 || result.UpdatedAtFixed != 1 {
		t.Errorf("got CreatedAtFixed=%d UpdatedAtFixed=%d, want 2 and 1", result.CreatedAtFixed, result.UpdatedAtFixed)
	}

	assertRaw("t-good", "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z")
	assertRaw("t-bad-created", "2026-07-11T19:30:13Z", "2026-07-11T19:30:13Z")
	assertRaw("t-bad-both", "2026-07-11T13:00:00Z", "2026-07-11T13:05:00Z")

	// Idempotency: running again finds nothing left to fix.
	second, err := store.NormalizeTimestamps(false)
	if err != nil {
		t.Fatalf("second NormalizeTimestamps failed: %v", err)
	}

	if second.CreatedAtFixed != 0 || second.UpdatedAtFixed != 0 {
		t.Errorf("second run: got CreatedAtFixed=%d UpdatedAtFixed=%d, want 0 and 0",
			second.CreatedAtFixed, second.UpdatedAtFixed)
	}

	if second.RowsScanned != 3 {
		t.Errorf("second run: RowsScanned = %d, want 3", second.RowsScanned)
	}
}

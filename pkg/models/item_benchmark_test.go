package models

import (
	"encoding/json"
	"testing"
	"time"
)

// BenchmarkItemStructAccess benchmarks direct struct field access.
func BenchmarkItemStructAccess(b *testing.B) {
	item := &Item{
		ID:        "test-id",
		Title:     "Test Title",
		Content:   "Test content",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Tags:      []string{"tag1", "tag2"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = item.ID
		_ = item.Title
		_ = item.Content
		_ = item.CreatedAt
		_ = item.UpdatedAt
		_ = item.Tags
	}
}

// BenchmarkBasicFullItemAccess benchmarks interface method access.
func BenchmarkBasicFullItemAccess(b *testing.B) {
	item := NewBasicItem("test-id", "Test Title")
	item.SetContent("Test content")
	item.SetCreatedAt(time.Now())
	item.SetUpdatedAt(time.Now())
	item.SetTags([]string{"tag1", "tag2"})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = item.GetID()
		_ = item.GetTitle()
		_ = item.GetContent()
		_ = item.GetCreatedAt()
		_ = item.GetUpdatedAt()
		_ = item.GetTags()
	}
}

// BenchmarkItemStructCreation benchmarks creating legacy Item structs.
func BenchmarkItemStructCreation(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		item := &Item{
			ID:        "test-id",
			Title:     "Test Title",
			Content:   "Test content",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Tags:      []string{"tag1", "tag2"},
		}
		_ = item
	}
}

// BenchmarkBasicItemCreation benchmarks creating BasicItem through interface.
func BenchmarkBasicItemCreation(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		item := NewBasicItem("test-id", "Test Title")
		item.SetContent("Test content")
		item.SetCreatedAt(time.Now())
		item.SetUpdatedAt(time.Now())
		item.SetTags([]string{"tag1", "tag2"})
		_ = item
	}
}

// BenchmarkItemStructJSONMarshal benchmarks JSON marshaling of Item struct.
func BenchmarkItemStructJSONMarshal(b *testing.B) {
	item := &Item{
		ID:        "test-id",
		Title:     "Test Title",
		Content:   "Test content",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Tags:      []string{"tag1", "tag2"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(item)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBasicItemJSONMarshal benchmarks JSON marshaling of BasicItem.
func BenchmarkBasicItemJSONMarshal(b *testing.B) {
	item := NewBasicItem("test-id", "Test Title")
	item.SetContent("Test content")
	item.SetCreatedAt(time.Now())
	item.SetUpdatedAt(time.Now())
	item.SetTags([]string{"tag1", "tag2"})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(item)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkItemSliceProcessing benchmarks processing slices of legacy Items.
func BenchmarkItemSliceProcessing(b *testing.B) {
	// Create slice of legacy items
	items := make([]*Item, 100)

	for i := 0; i < 100; i++ {
		items[i] = &Item{
			ID:      "test-id",
			Title:   "Test Title",
			Content: "Test content",
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Simulate common processing: access fields and modify
		for _, item := range items {
			_ = item.ID + item.Title
			item.Content = item.Content + "_processed"
		}
	}
}

// BenchmarkFullItemSliceProcessing benchmarks processing slices of FullItem.
func BenchmarkFullItemSliceProcessing(b *testing.B) {
	// Create slice of FullItem
	items := make([]FullItem, 100)

	for i := 0; i < 100; i++ {
		item := NewBasicItem("test-id", "Test Title")
		item.SetContent("Test content")
		items[i] = item
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Simulate common processing: access fields and modify
		for _, item := range items {
			_ = item.GetID() + item.GetTitle()
			item.SetContent(item.GetContent() + "_processed")
		}
	}
}

// BenchmarkLegacyConversion benchmarks converting between legacy and interface types.
func BenchmarkLegacyConversion(b *testing.B) {
	legacyItem := &Item{
		ID:        "test-id",
		Title:     "Test Title",
		Content:   "Test content",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Tags:      []string{"tag1", "tag2"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Convert legacy to interface and back
		interfaceItem := AsFullItem(legacyItem)
		_ = AsItemStruct(interfaceItem)
	}
}

// BenchmarkThreadCreationAndAccess benchmarks Thread operations.
func BenchmarkThreadCreationAndAccess(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		thread := NewThread("thread-id", "Thread Subject")

		// Add some messages
		msg1 := NewBasicItem("msg1", "Message 1")
		msg2 := NewBasicItem("msg2", "Message 2")

		thread.AddMessage(msg1)
		thread.AddMessage(msg2)

		// Access thread properties
		_ = thread.GetID()
		_ = thread.GetTitle()
		_ = thread.GetMessages()
	}
}

// BenchmarkTypeAssertion benchmarks type assertion helpers.
func BenchmarkTypeAssertion(b *testing.B) {
	basicItem := NewBasicItem("basic-id", "Basic Item")
	thread := NewThread("thread-id", "Thread Subject")

	items := []FullItem{basicItem, thread, basicItem, thread}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, item := range items {
			if IsThread(item) {
				_, _ = AsThread(item)
			} else {
				_, _ = AsBasicItem(item)
			}
		}
	}
}

// BenchmarkComparisonOverhead compares the overhead of interface vs struct access.
func BenchmarkComparisonOverhead(b *testing.B) {
	// This benchmark helps us measure the exact overhead
	legacyItem := &Item{
		ID:      "test-id",
		Title:   "Test Title",
		Content: "Test content",
	}

	interfaceItem := NewBasicItem("test-id", "Test Title")
	interfaceItem.SetContent("Test content")

	b.Run("StructAccess", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = legacyItem.ID + legacyItem.Title + legacyItem.Content
		}
	})

	b.Run("InterfaceAccess", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = interfaceItem.GetID() + interfaceItem.GetTitle() + interfaceItem.GetContent()
		}
	})
}

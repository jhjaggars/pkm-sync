package models

import (
	"encoding/json"
	"testing"
	"time"
)

// TestFullItemImplementation verifies that both BasicItem and Thread implement FullItem.
func TestFullItemImplementation(t *testing.T) {
	// Test BasicItem implements FullItem
	var basicItem FullItem = NewBasicItem("test-id", "Test Title")
	if basicItem == nil {
		t.Fatal("NewBasicItem should not return nil")
	}

	// Test Thread implements FullItem
	thread := NewThread("thread-id", "Thread Subject")
	if thread.GetID() == "" {
		t.Fatal("NewThread should have valid ID")
	}
}

// TestBasicItemGettersSetters tests all getter/setter methods.
func TestBasicItemGettersSetters(t *testing.T) {
	item := NewBasicItem("test-id", "Test Title")

	// Test initial values
	if item.GetID() != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", item.GetID())
	}

	if item.GetTitle() != "Test Title" {
		t.Errorf("Expected Title 'Test Title', got '%s'", item.GetTitle())
	}

	// Test setters
	item.SetContent("Test content")

	if item.GetContent() != "Test content" {
		t.Errorf("Expected Content 'Test content', got '%s'", item.GetContent())
	}

	item.SetSourceType("test_source")

	if item.GetSourceType() != "test_source" {
		t.Errorf("Expected SourceType 'test_source', got '%s'", item.GetSourceType())
	}

	item.SetItemType("test_type")

	if item.GetItemType() != "test_type" {
		t.Errorf("Expected ItemType 'test_type', got '%s'", item.GetItemType())
	}

	// Test timestamp setters
	testTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	item.SetCreatedAt(testTime)

	if !item.GetCreatedAt().Equal(testTime) {
		t.Errorf("Expected CreatedAt %v, got %v", testTime, item.GetCreatedAt())
	}

	item.SetUpdatedAt(testTime)

	if !item.GetUpdatedAt().Equal(testTime) {
		t.Errorf("Expected UpdatedAt %v, got %v", testTime, item.GetUpdatedAt())
	}

	// Test collections
	tags := []string{"tag1", "tag2"}
	item.SetTags(tags)

	if len(item.GetTags()) != 2 || item.GetTags()[0] != "tag1" {
		t.Errorf("Expected tags %v, got %v", tags, item.GetTags())
	}

	metadata := map[string]interface{}{"key": "value"}
	item.SetMetadata(metadata)

	if item.GetMetadata()["key"] != "value" {
		t.Errorf("Expected metadata %v, got %v", metadata, item.GetMetadata())
	}
}

// TestThreadFunctionality tests Thread-specific functionality.
func TestThreadFunctionality(t *testing.T) {
	thread := NewThread("thread-id", "Thread Subject")

	// Test basic properties
	if thread.GetID() != "thread-id" {
		t.Errorf("Expected ID 'thread-id', got '%s'", thread.GetID())
	}

	if thread.GetTitle() != "Thread Subject" {
		t.Errorf("Expected Title 'Thread Subject', got '%s'", thread.GetTitle())
	}

	if thread.GetItemType() != "thread" {
		t.Errorf("Expected ItemType 'thread', got '%s'", thread.GetItemType())
	}

	// Test messages functionality
	msg1 := NewBasicItem("msg1", "Message 1")
	msg2 := NewBasicItem("msg2", "Message 2")

	thread.AddMessage(msg1)
	thread.AddMessage(msg2)

	messages := thread.GetMessages()
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}

	if messages[0].GetID() != "msg1" {
		t.Errorf("Expected first message ID 'msg1', got '%s'", messages[0].GetID())
	}

	if messages[1].GetID() != "msg2" {
		t.Errorf("Expected second message ID 'msg2', got '%s'", messages[1].GetID())
	}
}

// TestJSONSerialization tests JSON marshaling/unmarshaling.
func TestBasicItemJSONSerialization(t *testing.T) {
	original := NewBasicItem("test-id", "Test Title")
	original.SetContent("Test content")
	original.SetSourceType("test_source")
	original.SetItemType("test_type")
	original.SetTags([]string{"tag1", "tag2"})

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal BasicItem: %v", err)
	}

	// Unmarshal from JSON
	var restored BasicItem

	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal BasicItem: %v", err)
	}

	// Compare values
	if restored.GetID() != original.GetID() {
		t.Errorf("ID mismatch: expected '%s', got '%s'", original.GetID(), restored.GetID())
	}

	if restored.GetTitle() != original.GetTitle() {
		t.Errorf("Title mismatch: expected '%s', got '%s'", original.GetTitle(), restored.GetTitle())
	}

	if restored.GetContent() != original.GetContent() {
		t.Errorf("Content mismatch: expected '%s', got '%s'", original.GetContent(), restored.GetContent())
	}
}

// TestThreadJSONSerialization tests JSON marshaling/unmarshaling for threads.
func TestThreadJSONSerialization(t *testing.T) {
	original := NewThread("thread-id", "Thread Subject")
	original.SetContent("Thread content")

	msg1 := NewBasicItem("msg1", "Message 1")
	msg1.SetContent("Message 1 content")

	msg2 := NewBasicItem("msg2", "Message 2")
	msg2.SetContent("Message 2 content")

	original.AddMessage(msg1)
	original.AddMessage(msg2)

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal Thread: %v", err)
	}

	// Unmarshal from JSON
	var restored Thread

	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal Thread: %v", err)
	}

	// Compare values
	if restored.GetID() != original.GetID() {
		t.Errorf("ID mismatch: expected '%s', got '%s'", original.GetID(), restored.GetID())
	}

	if restored.GetTitle() != original.GetTitle() {
		t.Errorf("Title mismatch: expected '%s', got '%s'", original.GetTitle(), restored.GetTitle())
	}

	if len(restored.GetMessages()) != 2 {
		t.Errorf("Messages count mismatch: expected 2, got %d", len(restored.GetMessages()))
	}

	if restored.GetMessages()[0].GetID() != "msg1" {
		t.Errorf("First message ID mismatch: expected 'msg1', got '%s'", restored.GetMessages()[0].GetID())
	}
}

// TestTypeAssertionHelpers tests the type assertion helper functions.
func TestTypeAssertionHelpers(t *testing.T) {
	basicItem := NewBasicItem("basic-id", "Basic Item")
	thread := NewThread("thread-id", "Thread Subject")

	// Test AsBasicItem

	if basic, ok := AsBasicItem(basicItem); !ok || basic == nil {
		t.Error("AsBasicItem should succeed for BasicItem")
	}

	if _, ok := AsBasicItem(thread); ok {
		t.Error("AsBasicItem should fail for Thread")
	}

	// Test AsThread

	if _, ok := AsThread(basicItem); ok {
		t.Error("AsThread should fail for BasicItem")
	}

	if th, ok := AsThread(thread); !ok || th == nil {
		t.Error("AsThread should succeed for Thread")
	}

	// Test IsThread
	if IsThread(basicItem) {
		t.Error("IsThread should return false for BasicItem")
	}

	if !IsThread(thread) {
		t.Error("IsThread should return true for Thread")
	}
}

// TestLegacyCompatibility tests backward compatibility functions.
func TestLegacyCompatibility(t *testing.T) {
	// Create a legacy Item
	legacyItem := &Item{
		ID:         "legacy-id",
		Title:      "Legacy Title",
		Content:    "Legacy content",
		SourceType: "legacy_source",
		ItemType:   "legacy_type",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Tags:       []string{"legacy"},
	}

	// Convert to FullItem
	itemInterface := AsFullItem(legacyItem)
	if itemInterface.GetID() != "legacy-id" {
		t.Errorf("Expected ID 'legacy-id', got '%s'", itemInterface.GetID())
	}

	// Convert back to legacy Item
	convertedBack := AsItemStruct(itemInterface)
	if convertedBack.ID != legacyItem.ID {
		t.Errorf("Expected ID '%s', got '%s'", legacyItem.ID, convertedBack.ID)
	}

	if convertedBack.Title != legacyItem.Title {
		t.Errorf("Expected Title '%s', got '%s'", legacyItem.Title, convertedBack.Title)
	}
}

// TestBackwardCompatibilityWithExistingStructUsage tests that existing code patterns still work.
func TestBackwardCompatibilityWithExistingStructUsage(t *testing.T) {
	// Test that legacy Item struct still works exactly as before
	legacyItem := &Item{
		ID:      "test-id",
		Title:   "Test Title",
		Content: "Test content",
		Tags:    []string{"tag1", "tag2"},
	}

	// Direct field access should still work
	if legacyItem.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", legacyItem.ID)
	}

	if legacyItem.Title != "Test Title" {
		t.Errorf("Expected Title 'Test Title', got '%s'", legacyItem.Title)
	}

	// JSON marshaling should still work
	data, err := json.Marshal(legacyItem)
	if err != nil {
		t.Fatalf("Failed to marshal legacy Item: %v", err)
	}

	var restored Item

	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal legacy Item: %v", err)
	}

	if restored.ID != legacyItem.ID {
		t.Errorf("JSON roundtrip failed: expected ID '%s', got '%s'", legacyItem.ID, restored.ID)
	}
}

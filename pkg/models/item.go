package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// CoreItem provides essential identity and content methods (6 methods).
type CoreItem interface {
	GetID() string
	SetID(id string)
	GetTitle() string
	SetTitle(title string)
	GetContent() string
	SetContent(content string)
}

// SourcedItem extends CoreItem with source identification (4 additional methods = 10 total).
type SourcedItem interface {
	CoreItem
	GetSourceType() string
	SetSourceType(sourceType string)
	GetItemType() string
	SetItemType(itemType string)
}

// TimestampedItem provides temporal metadata methods (4 methods).
type TimestampedItem interface {
	GetCreatedAt() time.Time
	SetCreatedAt(createdAt time.Time)
	GetUpdatedAt() time.Time
	SetUpdatedAt(updatedAt time.Time)
}

// EnrichedItem provides collections and metadata methods (8 methods).
type EnrichedItem interface {
	GetTags() []string
	SetTags(tags []string)
	GetAttachments() []Attachment
	SetAttachments(attachments []Attachment)
	GetMetadata() map[string]interface{}
	SetMetadata(metadata map[string]interface{})
	GetLinks() []Link
	SetLinks(links []Link)
}

// SerializableItem provides JSON serialization methods (2 methods).
type SerializableItem interface {
	MarshalJSON() ([]byte, error)
	UnmarshalJSON(data []byte) error
}

// FullItem composes all interfaces for complete functionality.
type FullItem interface {
	SourcedItem
	TimestampedItem
	EnrichedItem
	SerializableItem
}

// Item represents a universal data item from any source.
// DEPRECATED: Use FullItem instead. This struct is maintained for backward compatibility.
type Item struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	SourceType  string                 `json:"source_type"` // "google_calendar", "slack", etc.
	ItemType    string                 `json:"item_type"`   // "event", "message", "document", etc.
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Tags        []string               `json:"tags"`
	Attachments []Attachment           `json:"attachments"`
	Metadata    map[string]interface{} `json:"metadata"`
	Links       []Link                 `json:"links"` // URLs, references
}

type Attachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MimeType  string `json:"mime_type"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path,omitempty"`
	Data      string `json:"data,omitempty"` // Base64 encoded attachment data
	Size      int64  `json:"size,omitempty"` // Size in bytes
}

type Link struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Type  string `json:"type"` // "meeting_url", "document", "external"
}

// BasicItem implements FullItem with the same behavior as the legacy Item struct.
// This provides a drop-in replacement that maintains backward compatibility.
type BasicItem struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	SourceType  string                 `json:"source_type"`
	ItemType    string                 `json:"item_type"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Tags        []string               `json:"tags"`
	Attachments []Attachment           `json:"attachments"`
	Metadata    map[string]interface{} `json:"metadata"`
	Links       []Link                 `json:"links"`
}

// Thread represents an email or conversation thread containing multiple messages.
// This enables native thread support for Gmail and other communication sources.
type Thread struct {
	*BasicItem // Embed BasicItem for common functionality

	Messages []FullItem `json:"messages"` // Individual messages in the thread
}

// Ensure BasicItem implements FullItem.
var _ FullItem = (*BasicItem)(nil)

// Ensure Thread implements FullItem.
var _ FullItem = (*Thread)(nil)

// Constructor functions

// NewBasicItem creates a new BasicItem with the specified ID and title.
func NewBasicItem(id, title string) FullItem {
	return &BasicItem{
		ID:          id,
		Title:       title,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Tags:        make([]string, 0),
		Attachments: make([]Attachment, 0),
		Metadata:    make(map[string]interface{}),
		Links:       make([]Link, 0),
	}
}

// NewThread creates a new Thread with the specified ID and subject.
func NewThread(id, threadSubject string) *Thread {
	return &Thread{
		BasicItem: &BasicItem{
			ID:          id,
			Title:       threadSubject,
			ItemType:    "thread",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			Tags:        make([]string, 0),
			Attachments: make([]Attachment, 0),
			Metadata:    make(map[string]interface{}),
			Links:       make([]Link, 0),
		},
		Messages: make([]FullItem, 0),
	}
}

// Legacy constructor functions for backward compatibility

// FromGmailMessage creates an Item from a Gmail message (implemented in converter)
// This is a placeholder - actual implementation is in internal/sources/google/gmail/converter.go.
func FromGmailMessage(msg interface{}, config interface{}) (*Item, error) {
	// Implementation is in internal/sources/google/gmail/converter.go to avoid import cycles
	return nil, fmt.Errorf("use gmail.FromGmailMessage instead")
}

// FromCalendarEvent creates a BasicItem from an existing CalendarEvent model.
func FromCalendarEvent(event *CalendarEvent) *Item {
	item := &Item{
		ID:         event.ID,
		Title:      event.Summary,
		Content:    event.Description,
		SourceType: "google_calendar",
		ItemType:   "event",
		CreatedAt:  event.Start, // Using start time as creation time for events
		UpdatedAt:  event.Start, // Using start time since we don't have modified time in CalendarEvent
		Metadata: map[string]interface{}{
			"start_time": event.Start,
			"end_time":   event.End,
			"location":   event.Location,
			"attendees":  event.Attendees,
		},
	}

	// Convert Calendar attachments
	for _, attachment := range event.Attachments {
		item.Attachments = append(item.Attachments, Attachment{
			ID:       attachment.FileID,
			Name:     attachment.Title,
			MimeType: attachment.MimeType,
			URL:      attachment.FileURL,
		})
	}

	// Add meeting URL as a link
	if event.MeetingURL != "" {
		item.Links = append(item.Links, Link{
			URL:   event.MeetingURL,
			Title: "Meeting URL",
			Type:  "meeting_url",
		})
	}

	return item
}

// BasicItem interface implementation

func (b *BasicItem) GetID() string   { return b.ID }
func (b *BasicItem) SetID(id string) { b.ID = id }

func (b *BasicItem) GetTitle() string      { return b.Title }
func (b *BasicItem) SetTitle(title string) { b.Title = title }

func (b *BasicItem) GetContent() string        { return b.Content }
func (b *BasicItem) SetContent(content string) { b.Content = content }

func (b *BasicItem) GetSourceType() string           { return b.SourceType }
func (b *BasicItem) SetSourceType(sourceType string) { b.SourceType = sourceType }

func (b *BasicItem) GetItemType() string         { return b.ItemType }
func (b *BasicItem) SetItemType(itemType string) { b.ItemType = itemType }

func (b *BasicItem) GetCreatedAt() time.Time          { return b.CreatedAt }
func (b *BasicItem) SetCreatedAt(createdAt time.Time) { b.CreatedAt = createdAt }

func (b *BasicItem) GetUpdatedAt() time.Time          { return b.UpdatedAt }
func (b *BasicItem) SetUpdatedAt(updatedAt time.Time) { b.UpdatedAt = updatedAt }

func (b *BasicItem) GetTags() []string     { return b.Tags }
func (b *BasicItem) SetTags(tags []string) { b.Tags = tags }

func (b *BasicItem) GetAttachments() []Attachment            { return b.Attachments }
func (b *BasicItem) SetAttachments(attachments []Attachment) { b.Attachments = attachments }

func (b *BasicItem) GetMetadata() map[string]interface{}         { return b.Metadata }
func (b *BasicItem) SetMetadata(metadata map[string]interface{}) { b.Metadata = metadata }

func (b *BasicItem) GetLinks() []Link      { return b.Links }
func (b *BasicItem) SetLinks(links []Link) { b.Links = links }

// Thread-specific methods

func (t *Thread) GetMessages() []FullItem         { return t.Messages }
func (t *Thread) SetMessages(messages []FullItem) { t.Messages = messages }
func (t *Thread) AddMessage(message FullItem)     { t.Messages = append(t.Messages, message) }

// MarshalJSON implements custom JSON serialization for BasicItem.
func (b *BasicItem) MarshalJSON() ([]byte, error) {
	// Use an alias to avoid infinite recursion
	type Alias BasicItem

	return json.Marshal((*Alias)(b))
}

func (b *BasicItem) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion
	type Alias BasicItem

	return json.Unmarshal(data, (*Alias)(b))
}

// MarshalJSON implements custom JSON serialization for Thread.
func (t *Thread) MarshalJSON() ([]byte, error) {
	// Create a struct that includes embedded BasicItem fields and Messages
	type ThreadJSON struct {
		ID          string                 `json:"id"`
		Title       string                 `json:"title"`
		Content     string                 `json:"content"`
		SourceType  string                 `json:"source_type"`
		ItemType    string                 `json:"item_type"`
		CreatedAt   time.Time              `json:"created_at"`
		UpdatedAt   time.Time              `json:"updated_at"`
		Tags        []string               `json:"tags"`
		Attachments []Attachment           `json:"attachments"`
		Metadata    map[string]interface{} `json:"metadata"`
		Links       []Link                 `json:"links"`
		Messages    []FullItem             `json:"messages"`
	}

	return json.Marshal(ThreadJSON{
		ID:          t.BasicItem.ID,
		Title:       t.BasicItem.Title,
		Content:     t.BasicItem.Content,
		SourceType:  t.BasicItem.SourceType,
		ItemType:    t.BasicItem.ItemType,
		CreatedAt:   t.BasicItem.CreatedAt,
		UpdatedAt:   t.BasicItem.UpdatedAt,
		Tags:        t.BasicItem.Tags,
		Attachments: t.BasicItem.Attachments,
		Metadata:    t.BasicItem.Metadata,
		Links:       t.BasicItem.Links,
		Messages:    t.Messages,
	})
}

func (t *Thread) UnmarshalJSON(data []byte) error {
	// Similar approach for unmarshaling
	type ThreadJSON struct {
		ID          string                 `json:"id"`
		Title       string                 `json:"title"`
		Content     string                 `json:"content"`
		SourceType  string                 `json:"source_type"`
		ItemType    string                 `json:"item_type"`
		CreatedAt   time.Time              `json:"created_at"`
		UpdatedAt   time.Time              `json:"updated_at"`
		Tags        []string               `json:"tags"`
		Attachments []Attachment           `json:"attachments"`
		Metadata    map[string]interface{} `json:"metadata"`
		Links       []Link                 `json:"links"`
		Messages    []json.RawMessage      `json:"messages"` // Use RawMessage for flexible unmarshaling
	}

	var temp ThreadJSON
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Initialize BasicItem if nil
	if t.BasicItem == nil {
		t.BasicItem = &BasicItem{}
	}

	// Copy basic item fields
	t.BasicItem.ID = temp.ID
	t.BasicItem.Title = temp.Title
	t.BasicItem.Content = temp.Content
	t.BasicItem.SourceType = temp.SourceType
	t.BasicItem.ItemType = temp.ItemType
	t.BasicItem.CreatedAt = temp.CreatedAt
	t.BasicItem.UpdatedAt = temp.UpdatedAt
	t.BasicItem.Tags = temp.Tags
	t.BasicItem.Attachments = temp.Attachments
	t.BasicItem.Metadata = temp.Metadata
	t.BasicItem.Links = temp.Links

	// Unmarshal messages - for now, unmarshal as BasicItems
	// In the future, this could be enhanced to detect and unmarshal different types
	t.Messages = make([]FullItem, len(temp.Messages))

	for i, rawMsg := range temp.Messages {
		var msg BasicItem
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			return fmt.Errorf("failed to unmarshal message %d: %w", i, err)
		}

		t.Messages[i] = &msg
	}

	return nil
}

// Type assertion helpers for migration and backward compatibility

// AsBasicItem safely converts a FullItem to *BasicItem.
func AsBasicItem(item FullItem) (*BasicItem, bool) {
	if basicItem, ok := item.(*BasicItem); ok {
		return basicItem, true
	}

	return nil, false
}

// AsThread safely converts a FullItem to *Thread.
func AsThread(item FullItem) (*Thread, bool) {
	if thread, ok := item.(*Thread); ok {
		return thread, true
	}

	return nil, false
}

// IsThread checks if a FullItem is a Thread.
func IsThread(item FullItem) bool {
	_, ok := item.(*Thread)

	return ok
}

// Conversion functions for compatibility between FullItem and Item struct

// AsItemStruct converts any FullItem to the Item struct for compatibility purposes.
func AsItemStruct(item FullItem) *Item {
	return &Item{
		ID:          item.GetID(),
		Title:       item.GetTitle(),
		Content:     item.GetContent(),
		SourceType:  item.GetSourceType(),
		ItemType:    item.GetItemType(),
		CreatedAt:   item.GetCreatedAt(),
		UpdatedAt:   item.GetUpdatedAt(),
		Tags:        item.GetTags(),
		Attachments: item.GetAttachments(),
		Metadata:    item.GetMetadata(),
		Links:       item.GetLinks(),
	}
}

// AsFullItem converts an Item struct to FullItem (as BasicItem).
func AsFullItem(item *Item) FullItem {
	return &BasicItem{
		ID:          item.ID,
		Title:       item.Title,
		Content:     item.Content,
		SourceType:  item.SourceType,
		ItemType:    item.ItemType,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
		Tags:        item.Tags,
		Attachments: item.Attachments,
		Metadata:    item.Metadata,
		Links:       item.Links,
	}
}

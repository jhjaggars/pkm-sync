package slack

import (
	"encoding/json"
	"testing"
)

// mockUserResolver returns a fixed mapping for testing.
func mockUserResolver(userID string) string {
	users := map[string]string{
		"U123ABC":  "Alice Smith",
		"U456DEF":  "Bob Jones",
		"U789GHI":  "Charlie Brown",
		"UUNKNOWN": "UUNKNOWN", // Fallback for unknown user
	}

	if name, ok := users[userID]; ok {
		return name
	}

	return userID
}

// TestExtractMessageText_TextOnly tests the basic text extraction.
func TestExtractMessageText_TextOnly(t *testing.T) {
	msg := &RawMessage{
		Text: "fallback text",
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "Hello "},
						{"type": "text", "text": "world!"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "Hello world!"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_UserMention tests user mention resolution.
func TestExtractMessageText_UserMention(t *testing.T) {
	msg := &RawMessage{
		Text: "<@U123ABC>, can you help?",
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "user", "user_id": "U123ABC"},
						{"type": "text", "text": ", can you help?"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "@Alice Smith, can you help?"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_MixedTextAndUserMentions tests the Ralph Bean scenario.
func TestExtractMessageText_MixedTextAndUserMentions(t *testing.T) {
	msg := &RawMessage{
		Text: "<@U123ABC> and <@U456DEF>, can you file issues?",
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "user", "user_id": "U123ABC"},
						{"type": "text", "text": " and "},
						{"type": "user", "user_id": "U456DEF"},
						{"type": "text", "text": ", can you file issues?"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "@Alice Smith and @Bob Jones, can you file issues?"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_LinkElement tests link extraction.
func TestExtractMessageText_LinkElement(t *testing.T) {
	msg := &RawMessage{
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "Check out "},
						{"type": "link", "url": "https://example.com", "text": "this link"},
						{"type": "text", "text": "!"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "Check out this link (https://example.com)!"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_LinkElementNoText tests link with URL only.
func TestExtractMessageText_LinkElementNoText(t *testing.T) {
	msg := &RawMessage{
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "Visit "},
						{"type": "link", "url": "https://example.com"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "Visit https://example.com"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_ChannelMention tests channel mentions.
func TestExtractMessageText_ChannelMention(t *testing.T) {
	msg := &RawMessage{
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "See "},
						{"type": "channel", "channel_id": "C123XYZ"},
						{"type": "text", "text": " for details"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "See #C123XYZ for details"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_EmojiElement tests emoji extraction.
func TestExtractMessageText_EmojiElement(t *testing.T) {
	msg := &RawMessage{
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "Great work "},
						{"type": "emoji", "name": "thumbsup"},
						{"type": "text", "text": "!"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "Great work :thumbsup:!"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_FallbackToText tests fallback when no rich_text blocks exist.
func TestExtractMessageText_FallbackToText(t *testing.T) {
	msg := &RawMessage{
		Text:   "<@U123ABC>, you were asking about this",
		Blocks: nil,
	}

	result := ExtractMessageText(msg, mockUserResolver)

	// When falling back to msg.Text, raw tags should be preserved
	expected := "<@U123ABC>, you were asking about this"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_FallbackToTextEmptyBlocks tests fallback with empty blocks.
func TestExtractMessageText_FallbackToTextEmptyBlocks(t *testing.T) {
	msg := &RawMessage{
		Text:   "fallback content",
		Blocks: []json.RawMessage{},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "fallback content"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_UnknownUserFallback tests handling of unknown users.
func TestExtractMessageText_UnknownUserFallback(t *testing.T) {
	msg := &RawMessage{
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "user", "user_id": "UUNKNOWN"},
						{"type": "text", "text": " said something"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	// Unknown user should fall back to raw ID with @ prefix
	expected := "@UUNKNOWN said something"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_MultipleBlocks tests multiple rich_text blocks.
func TestExtractMessageText_MultipleBlocks(t *testing.T) {
	msg := &RawMessage{
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "First paragraph."}
					]
				}]
			}`),
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "Second paragraph."}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "First paragraph.Second paragraph."
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_NonRichTextBlocks tests skipping non-rich_text blocks.
func TestExtractMessageText_NonRichTextBlocks(t *testing.T) {
	msg := &RawMessage{
		Text: "fallback text",
		Blocks: []json.RawMessage{
			json.RawMessage(`{"type": "section", "text": {"type": "mrkdwn", "text": "Should be ignored"}}`),
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "text", "text": "Actual content"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, mockUserResolver)

	expected := "Actual content"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestExtractMessageText_NilResolver tests handling when resolver is nil.
func TestExtractMessageText_NilResolver(t *testing.T) {
	msg := &RawMessage{
		Blocks: []json.RawMessage{
			json.RawMessage(`{
				"type": "rich_text",
				"elements": [{
					"type": "rich_text_section",
					"elements": [
						{"type": "user", "user_id": "U123ABC"},
						{"type": "text", "text": " mentioned"}
					]
				}]
			}`),
		},
	}

	result := ExtractMessageText(msg, nil)

	// With nil resolver, should fall back to raw user ID
	expected := "@U123ABC mentioned"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

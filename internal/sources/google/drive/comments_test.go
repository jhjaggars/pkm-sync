package drive

import (
	"strings"
	"testing"
)

func TestFormatCommentsAsFootnotes(t *testing.T) {
	tests := []struct {
		name     string
		comments []CommentData
		want     []string // substrings that must appear
		notWant  []string // substrings that must NOT appear
	}{
		{
			name:     "empty",
			comments: nil,
			want:     nil,
			notWant:  []string{"## Comments"},
		},
		{
			name: "single open comment",
			comments: []CommentData{{
				CommentNumber: 1,
				Author:        "Alice",
				Content:       "Needs revision",
				CreatedTime:   "2025-06-01 10:00",
				Resolved:      false,
			}},
			want: []string{
				"[^comment-1]:",
				"**Alice**",
				"(2025-06-01 10:00)",
				"*Open*",
				"Needs revision",
			},
		},
		{
			name: "resolved comment",
			comments: []CommentData{{
				CommentNumber: 1,
				Author:        "Bob",
				Content:       "Fixed",
				Resolved:      true,
			}},
			want:    []string{"*Resolved*"},
			notWant: []string{"*Open*"},
		},
		{
			name: "comment with quoted text",
			comments: []CommentData{{
				CommentNumber: 1,
				Author:        "Carol",
				Content:       "Typo here",
				QuotedText:    "the quick brown fox",
			}},
			want: []string{"> the quick brown fox"},
		},
		{
			name: "comment with replies",
			comments: []CommentData{{
				CommentNumber: 1,
				Author:        "Dave",
				Content:       "Question",
				Replies: []ReplyData{
					{Author: "Eve", Content: "Answer", CreatedTime: "2025-06-02 14:00"},
				},
			}},
			want: []string{
				"**Eve**",
				"(2025-06-02 14:00)",
				": Answer",
			},
		},
		{
			name: "unknown author fallback",
			comments: []CommentData{{
				CommentNumber: 1,
				Content:       "Anonymous comment",
			}},
			want: []string{"**Unknown**"},
		},
		{
			name: "author name with markdown characters",
			comments: []CommentData{{
				CommentNumber: 1,
				Author:        "Alice_[Bot]*",
				Content:       "Test comment",
			}},
			want: []string{`**Alice\_\[Bot\]\***`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCommentsAsFootnotes(tt.comments)

			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("FormatCommentsAsFootnotes() missing %q in:\n%s", w, got)
				}
			}

			for _, nw := range tt.notWant {
				if strings.Contains(got, nw) {
					t.Errorf("FormatCommentsAsFootnotes() should not contain %q in:\n%s", nw, got)
				}
			}
		})
	}
}

func TestInsertCommentMarkers(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		comments []CommentData
		want     string
	}{
		{
			name:     "no comments",
			content:  "Hello world",
			comments: nil,
			want:     "Hello world",
		},
		{
			name:    "inserts marker after quoted text",
			content: "The quick brown fox jumps over the lazy dog.",
			comments: []CommentData{{
				CommentNumber: 1,
				QuotedText:    "quick brown fox",
			}},
			want: "The quick brown fox[^comment-1] jumps over the lazy dog.",
		},
		{
			name:    "skips comment without quoted text",
			content: "Hello world",
			comments: []CommentData{{
				CommentNumber: 1,
				QuotedText:    "",
			}},
			want: "Hello world",
		},
		{
			name:    "only replaces first occurrence",
			content: "foo bar foo bar",
			comments: []CommentData{{
				CommentNumber: 1,
				QuotedText:    "foo",
			}},
			want: "foo[^comment-1] bar foo bar",
		},
		{
			name:    "multiple comments",
			content: "alpha beta gamma",
			comments: []CommentData{
				{CommentNumber: 1, QuotedText: "alpha"},
				{CommentNumber: 2, QuotedText: "gamma"},
			},
			want: "alpha[^comment-1] beta gamma[^comment-2]",
		},
		{
			name:    "quoted text not found in content",
			content: "Hello world",
			comments: []CommentData{{
				CommentNumber: 1,
				QuotedText:    "missing text",
			}},
			want: "Hello world",
		},
		{
			name:    "overlapping quoted text",
			content: "The quick brown fox jumps over the lazy dog.",
			comments: []CommentData{
				{CommentNumber: 1, QuotedText: "quick brown"},
				{CommentNumber: 2, QuotedText: "quick brown fox"},
			},
			want: "The quick brown[^comment-1] fox[^comment-2] jumps over the lazy dog.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InsertCommentMarkers(tt.content, tt.comments)
			if got != tt.want {
				t.Errorf("InsertCommentMarkers() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractFileID_FragmentStripping(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "docs URL with fragment",
			url:  "https://docs.google.com/document/d/abc123/edit#heading=h.xyz",
			want: "abc123",
		},
		{
			name: "docs URL without fragment",
			url:  "https://docs.google.com/document/d/abc123/edit",
			want: "abc123",
		},
		{
			name: "docs URL with query and fragment",
			url:  "https://docs.google.com/document/d/abc123/edit?usp=sharing#heading=h.xyz",
			want: "abc123",
		},
		{
			name: "drive URL with fragment",
			url:  "https://drive.google.com/file/d/def456/view#something",
			want: "def456",
		},
		{
			name: "drive URL without fragment",
			url:  "https://drive.google.com/file/d/def456/view",
			want: "def456",
		},
		{
			name: "open URL with fragment",
			url:  "https://drive.google.com/open?id=ghi789#section",
			want: "ghi789",
		},
		{
			name: "open URL without fragment",
			url:  "https://drive.google.com/open?id=ghi789",
			want: "ghi789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractFileID(tt.url)
			if err != nil {
				t.Fatalf("ExtractFileID(%q) error: %v", tt.url, err)
			}

			if got != tt.want {
				t.Errorf("ExtractFileID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

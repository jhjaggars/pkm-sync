package drive

import (
	"fmt"
	"sort"
	"strings"
)

// escapeMarkdown escapes markdown special characters in text.
func escapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
	)

	return replacer.Replace(text)
}

// FormatCommentsAsFootnotes formats comments as markdown footnote definitions.
func FormatCommentsAsFootnotes(comments []CommentData) string {
	if len(comments) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("---\n\n## Comments\n\n")

	for _, c := range comments {
		sb.WriteString(fmt.Sprintf("[^comment-%d]: ", c.CommentNumber))

		// Author and timestamp
		author := c.Author
		if author == "" {
			author = "Unknown"
		}

		sb.WriteString(fmt.Sprintf("**%s**", escapeMarkdown(author)))

		if c.CreatedTime != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", c.CreatedTime))
		}

		sb.WriteString(" — ")

		if c.Resolved {
			sb.WriteString("*Resolved*")
		} else {
			sb.WriteString("*Open*")
		}

		sb.WriteString("  \n")

		if c.QuotedText != "" {
			sb.WriteString(fmt.Sprintf("    > %s  \n", c.QuotedText))
		}

		if c.Content != "" {
			sb.WriteString("    " + c.Content + "  \n")
		}

		for _, r := range c.Replies {
			rAuthor := r.Author
			if rAuthor == "" {
				rAuthor = "Unknown"
			}

			sb.WriteString(fmt.Sprintf("    - **%s**", escapeMarkdown(rAuthor)))

			if r.CreatedTime != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", r.CreatedTime))
			}

			sb.WriteString(": " + r.Content + "  \n")
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// InsertCommentMarkers inserts footnote reference markers into content at the
// first occurrence of each comment's quoted text.
func InsertCommentMarkers(content string, comments []CommentData) string {
	// Find all match positions in original content
	type insertion struct {
		pos    int
		text   string
		marker string
	}

	var insertions []insertion

	for _, c := range comments {
		if c.QuotedText == "" {
			continue
		}

		pos := strings.Index(content, c.QuotedText)
		if pos == -1 {
			continue
		}

		marker := fmt.Sprintf("[^comment-%d]", c.CommentNumber)
		insertions = append(insertions, insertion{
			pos:    pos + len(c.QuotedText),
			text:   c.QuotedText,
			marker: marker,
		})
	}

	// Sort by position descending (apply from end to start)
	sort.Slice(insertions, func(i, j int) bool {
		return insertions[i].pos > insertions[j].pos
	})

	// Apply insertions from end to start
	for _, ins := range insertions {
		content = content[:ins.pos] + ins.marker + content[ins.pos:]
	}

	return content
}

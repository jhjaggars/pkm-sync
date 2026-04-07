package drive

import (
	"fmt"
	"strings"
)

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

		sb.WriteString(fmt.Sprintf("**%s**", author))

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

			sb.WriteString(fmt.Sprintf("    - **%s**", rAuthor))

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
	for _, c := range comments {
		if c.QuotedText == "" {
			continue
		}

		marker := fmt.Sprintf("[^comment-%d]", c.CommentNumber)
		content = strings.Replace(content, c.QuotedText, c.QuotedText+marker, 1)
	}

	return content
}

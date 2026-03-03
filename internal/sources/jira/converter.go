package jira

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jiraclient "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"pkm-sync/pkg/models"
)

// issueToItem converts a Jira issue to a BasicItem.
// Title is set to the issue key (e.g. "PROJ-123") so the output filename is "PROJ-123.md",
// matching the standard Obsidian Jira vault convention.
func issueToItem(issue *jiraclient.Issue, serverURL string, includeComments bool) models.FullItem {
	item := &models.BasicItem{
		ID:         "jira_" + issue.Key,
		Title:      issue.Key,
		SourceType: "jira",
		ItemType:   "issue",
		Tags:       make([]string, 0),
		Metadata:   make(map[string]any),
		Links:      make([]models.Link, 0),
	}

	// Parse timestamps.
	item.CreatedAt = parseJiraTime(issue.Fields.Created)
	item.UpdatedAt = parseJiraTime(issue.Fields.Updated)

	// Build content from description.
	desc := descriptionToString(issue.Fields.Description)
	content := desc

	// Append comments section when requested.
	if includeComments && issue.Fields.Comment.Total > 0 {
		var sb strings.Builder

		if content != "" {
			sb.WriteString(content)
			sb.WriteString("\n\n")
		}

		sb.WriteString("## Comments\n\n")

		for _, c := range issue.Fields.Comment.Comments {
			authorName := c.Author.DisplayName
			if authorName == "" {
				authorName = c.Author.Name
			}

			sb.WriteString(fmt.Sprintf("### %s (%s)\n\n", authorName, c.Created))
			sb.WriteString(descriptionToString(c.Body))
			sb.WriteString("\n\n")
		}

		content = sb.String()
	}

	item.Content = content

	// Build tags from labels, issue type, status, priority.
	tags := make([]string, 0, len(issue.Fields.Labels)+3)
	tags = append(tags, issue.Fields.Labels...)

	if issue.Fields.IssueType.Name != "" {
		tags = append(tags, "type:"+strings.ToLower(strings.ReplaceAll(issue.Fields.IssueType.Name, " ", "-")))
	}

	if issue.Fields.Status.Name != "" {
		tags = append(tags, "status:"+strings.ToLower(strings.ReplaceAll(issue.Fields.Status.Name, " ", "-")))
	}

	if issue.Fields.Priority.Name != "" {
		tags = append(tags, "priority:"+strings.ToLower(issue.Fields.Priority.Name))
	}

	item.Tags = tags

	// Build metadata. "summary" holds the human-readable title so frontmatter
	// includes both the issue key (as note title) and its summary text.
	meta := map[string]any{
		"summary":    issue.Fields.Summary,
		"issue_key":  issue.Key,
		"issue_type": issue.Fields.IssueType.Name,
		"status":     issue.Fields.Status.Name,
		"priority":   issue.Fields.Priority.Name,
		"assignee":   issue.Fields.Assignee.Name,
		"reporter":   issue.Fields.Reporter.Name,
	}

	if issue.Fields.Resolution.Name != "" {
		meta["resolution"] = issue.Fields.Resolution.Name
	}

	meta["project"] = extractProject(issue.Key)

	if len(issue.Fields.Components) > 0 {
		comps := make([]string, len(issue.Fields.Components))
		for i, c := range issue.Fields.Components {
			comps[i] = c.Name
		}

		meta["components"] = comps
	}

	if len(issue.Fields.FixVersions) > 0 {
		versions := make([]string, len(issue.Fields.FixVersions))
		for i, v := range issue.Fields.FixVersions {
			versions[i] = v.Name
		}

		meta["fix_versions"] = versions
	}

	item.Metadata = meta

	// Set source URL.
	if serverURL != "" {
		item.Links = append(item.Links, models.Link{
			URL:   serverURL + "/browse/" + issue.Key,
			Title: issue.Key,
			Type:  "external",
		})
	}

	return item
}

// descriptionToString converts a Jira description field to a plain string.
// In V2 API it's a string; in V3 it's an ADF object.
func descriptionToString(desc any) string {
	if desc == nil {
		return ""
	}

	if s, ok := desc.(string); ok {
		return s
	}

	// Fallback: marshal to JSON for ADF or unexpected types.
	data, err := json.Marshal(desc)
	if err != nil {
		return ""
	}

	return string(data)
}

// parseJiraTime parses a Jira timestamp string trying both RFC3339 formats.
func parseJiraTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	if t, err := time.Parse(jiraclient.RFC3339, s); err == nil {
		return t
	}

	if t, err := time.Parse(jiraclient.RFC3339MilliLayout, s); err == nil {
		return t
	}

	return time.Time{}
}

// extractProject extracts the project key from an issue key like "PROJ-123" → "PROJ".
func extractProject(issueKey string) string {
	if idx := strings.LastIndex(issueKey, "-"); idx > 0 {
		return issueKey[:idx]
	}

	return issueKey
}

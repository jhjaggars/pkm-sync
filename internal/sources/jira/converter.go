package jira

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	jiraclient "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/ankitpokhrel/jira-cli/pkg/adf"

	"pkm-sync/pkg/models"
)

// compileExcludePatterns compiles regex patterns, logging warnings for invalid ones.
func compileExcludePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))

	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			slog.Warn("Invalid comment_exclude_pattern, skipping", "pattern", p, "error", err)
			continue
		}

		compiled = append(compiled, re)
	}

	return compiled
}

// issueToItem converts a Jira issue to a BasicItem.
// Title is set to the issue key (e.g. "PROJ-123") so the output filename is "PROJ-123.md",
// matching the standard Obsidian Jira vault convention.
func issueToItem(issue *jiraclient.Issue, serverURL string, cfg models.JiraSourceConfig) models.FullItem {
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
	desc := descriptionToMarkdown(issue.Fields.Description)
	content := desc

	// Append comments section when requested, filtering out automated noise.
	if cfg.IncludeComments && issue.Fields.Comment.Total > 0 {
		excludePatterns := compileExcludePatterns(cfg.CommentExcludePatterns)

		var sb strings.Builder
		hasComments := false

		for _, c := range issue.Fields.Comment.Comments {
			body := descriptionToMarkdown(c.Body)
			if matchesAny(body, excludePatterns) {
				continue
			}

			if !hasComments {
				if content != "" {
					sb.WriteString(content)
					sb.WriteString("\n\n")
				}

				sb.WriteString("## Comments\n\n")

				hasComments = true
			}

			authorName := c.Author.DisplayName
			if authorName == "" {
				authorName = c.Author.Name
			}

			sb.WriteString(fmt.Sprintf("### %s (%s)\n\n", authorName, c.Created))
			sb.WriteString(body)
			sb.WriteString("\n\n")
		}

		if hasComments {
			content = sb.String()
		}
	}

	item.Content = content

	// Build tags using Obsidian nested tag format (tag/subtag).
	tags := make([]string, 0, len(issue.Fields.Labels)+3)
	tags = append(tags, issue.Fields.Labels...)

	if issue.Fields.IssueType.Name != "" {
		tags = append(tags, "type/"+sanitizeTag(issue.Fields.IssueType.Name))
	}

	if issue.Fields.Status.Name != "" {
		tags = append(tags, "status/"+sanitizeTag(issue.Fields.Status.Name))
	}

	if issue.Fields.Priority.Name != "" {
		tags = append(tags, "priority/"+sanitizeTag(issue.Fields.Priority.Name))
	}

	for _, comp := range issue.Fields.Components {
		if comp.Name != "" {
			tags = append(tags, "component/"+sanitizeTag(comp.Name))
		}
	}

	// Add issue link relationships as tags for Obsidian graph linking.
	for _, link := range issue.Fields.IssueLinks {
		if link.OutwardIssue != nil {
			tags = append(tags, "links/"+sanitizeTag(link.LinkType.Outward)+"/"+link.OutwardIssue.Key)
		}

		if link.InwardIssue != nil {
			tags = append(tags, "links/"+sanitizeTag(link.LinkType.Inward)+"/"+link.InwardIssue.Key)
		}
	}

	item.Tags = tags

	// Build metadata.
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

	// Parent issue.
	if issue.Fields.Parent != nil && issue.Fields.Parent.Key != "" {
		meta["parent"] = issue.Fields.Parent.Key
	}

	// Components.
	if len(issue.Fields.Components) > 0 {
		comps := make([]string, len(issue.Fields.Components))
		for i, c := range issue.Fields.Components {
			comps[i] = c.Name
		}

		meta["components"] = comps
	}

	// Fix versions.
	if len(issue.Fields.FixVersions) > 0 {
		versions := make([]string, len(issue.Fields.FixVersions))
		for i, v := range issue.Fields.FixVersions {
			versions[i] = v.Name
		}

		meta["fix_versions"] = versions
	}

	// Issue links — flat list of "relationship: KEY" for clean YAML serialization.
	if len(issue.Fields.IssueLinks) > 0 {
		var linkEntries []string

		for _, link := range issue.Fields.IssueLinks {
			if link.OutwardIssue != nil {
				linkEntries = append(linkEntries, link.LinkType.Outward+": "+link.OutwardIssue.Key)
			}

			if link.InwardIssue != nil {
				linkEntries = append(linkEntries, link.LinkType.Inward+": "+link.InwardIssue.Key)
			}
		}

		if len(linkEntries) > 0 {
			meta["issue_links"] = linkEntries
		}
	}

	// Contributors — unique set of assignee, reporter, and commenters.
	contributors := collectContributors(issue)
	if len(contributors) > 0 {
		meta["contributors"] = contributors
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

// collectContributors extracts unique contributor names from an issue's
// assignee, reporter, and comment authors.
func collectContributors(issue *jiraclient.Issue) []string {
	seen := make(map[string]bool)
	var contributors []string

	addContributor := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			contributors = append(contributors, name)
		}
	}

	addContributor(issue.Fields.Assignee.Name)
	addContributor(issue.Fields.Reporter.Name)

	for _, c := range issue.Fields.Comment.Comments {
		name := c.Author.DisplayName
		if name == "" {
			name = c.Author.Name
		}

		addContributor(name)
	}

	return contributors
}

// matchesAny returns true if the text matches any of the compiled patterns.
func matchesAny(text string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(text) {
			return true
		}
	}

	return false
}

// sanitizeTag converts a string to a valid Obsidian nested tag segment.
// Replaces spaces with hyphens and lowercases.
func sanitizeTag(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// descriptionToMarkdown converts a Jira description field to markdown.
// In V2 API the description is a string; in V3 it's an ADF document.
func descriptionToMarkdown(desc any) string {
	if desc == nil {
		return ""
	}

	if s, ok := desc.(string); ok {
		return s
	}

	// Try to parse as ADF (Atlassian Document Format) and render to markdown.
	data, err := json.Marshal(desc)
	if err != nil {
		return ""
	}

	var adfDoc adf.ADF
	if err := json.Unmarshal(data, &adfDoc); err == nil && adfDoc.Version > 0 {
		translator := adf.NewTranslator(&adfDoc, adf.NewMarkdownTranslator())
		result := translator.Translate()
		return fixInlineCards(result)
	}

	// Fallback for unexpected types.
	return string(data)
}

// inlineCardPattern matches the jira-cli ADF translator's inlineCard output:
//
//	📍 https://redhat.atlassian.net/browse/KEY-123#icft=KEY-123
//
// and converts it to a markdown link: [KEY-123](url).
var inlineCardPattern = regexp.MustCompile(
	`📍\s*(https://[^\s]+/browse/([A-Z]+-\d+))#icft=[A-Z]+-\d+`,
)

// fixInlineCards replaces raw inlineCard artifacts with proper markdown links.
func fixInlineCards(text string) string {
	return inlineCardPattern.ReplaceAllString(text, "[$2]($1)")
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

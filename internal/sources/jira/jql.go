package jira

import (
	"strings"
	"time"

	"pkm-sync/pkg/models"
)

// buildJQL constructs a JQL query string from the source config.
func buildJQL(cfg models.JiraSourceConfig, since time.Time, currentUser string) string {
	var parts []string

	if cfg.JQL != "" {
		parts = append(parts, "("+cfg.JQL+")")
	} else {
		parts = buildStructuredJQL(cfg, currentUser)
	}

	if !since.IsZero() {
		parts = append(parts, `updated >= "`+since.Format("2006-01-02 15:04")+`"`)
	}

	jql := strings.Join(parts, " AND ")
	if jql != "" {
		return jql + " ORDER BY updated DESC"
	}

	return "ORDER BY updated DESC"
}

// buildStructuredJQL builds JQL clauses from structured config fields.
func buildStructuredJQL(cfg models.JiraSourceConfig, currentUser string) []string {
	var parts []string

	if len(cfg.ProjectKeys) > 0 {
		parts = append(parts, "project IN ("+strings.Join(cfg.ProjectKeys, ", ")+")")
	}

	if len(cfg.IssueTypes) > 0 {
		quoted := make([]string, len(cfg.IssueTypes))
		for i, t := range cfg.IssueTypes {
			quoted[i] = `"` + t + `"`
		}

		parts = append(parts, "issuetype IN ("+strings.Join(quoted, ", ")+")")
	}

	if len(cfg.Statuses) > 0 {
		quoted := make([]string, len(cfg.Statuses))
		for i, s := range cfg.Statuses {
			quoted[i] = `"` + s + `"`
		}

		parts = append(parts, "status IN ("+strings.Join(quoted, ", ")+")")
	}

	if cfg.AssigneeFilter == "me" && currentUser != "" {
		parts = append(parts, `assignee = "`+currentUser+`"`)
	}

	return parts
}

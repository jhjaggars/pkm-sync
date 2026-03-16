package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// JiraProject holds basic project info for discovery.
type JiraProject struct {
	Key  string
	Name string
}

// JiraIssueType holds issue type info for discovery.
type JiraIssueType struct {
	ID   string
	Name string
}

// JiraStatus holds status info for discovery.
type JiraStatus struct {
	ID   string
	Name string
}

// ListProjects returns all projects accessible to the authenticated user.
func (s *JiraSource) ListProjects() ([]*JiraProject, error) {
	res, err := s.client.Get(context.Background(), "/project", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list jira projects: %w", err)
	}

	defer res.Body.Close() //nolint:errcheck

	var raw []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}

	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode projects: %w", err)
	}

	projects := make([]*JiraProject, 0, len(raw))
	for _, p := range raw {
		projects = append(projects, &JiraProject{Key: p.Key, Name: p.Name})
	}

	return projects, nil
}

// ListIssueTypes returns all issue types for the given project key.
func (s *JiraSource) ListIssueTypes(projectKey string) ([]*JiraIssueType, error) {
	path := fmt.Sprintf("/project/%s", url.PathEscape(projectKey))

	res, err := s.client.Get(context.Background(), path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get project %s: %w", projectKey, err)
	}

	defer res.Body.Close() //nolint:errcheck

	var raw struct {
		IssueTypes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"issueTypes"`
	}

	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode issue types: %w", err)
	}

	types := make([]*JiraIssueType, 0, len(raw.IssueTypes))
	for _, t := range raw.IssueTypes {
		types = append(types, &JiraIssueType{ID: t.ID, Name: t.Name})
	}

	return types, nil
}

// ListStatuses returns all statuses for the given project key, deduplicated across issue types.
func (s *JiraSource) ListStatuses(projectKey string) ([]*JiraStatus, error) {
	path := fmt.Sprintf("/project/%s/statuses", url.PathEscape(projectKey))

	res, err := s.client.Get(context.Background(), path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get statuses for %s: %w", projectKey, err)
	}

	defer res.Body.Close() //nolint:errcheck

	// Response is []IssueTypeWithStatuses
	var raw []struct {
		Statuses []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"statuses"`
	}

	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode statuses: %w", err)
	}

	// Deduplicate statuses across issue types.
	seen := make(map[string]bool)

	var statuses []*JiraStatus

	for _, issueType := range raw {
		for _, st := range issueType.Statuses {
			if !seen[st.ID] {
				seen[st.ID] = true
				statuses = append(statuses, &JiraStatus{ID: st.ID, Name: st.Name})
			}
		}
	}

	return statuses, nil
}

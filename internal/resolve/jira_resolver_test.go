package resolve

import (
	"context"
	"errors"
	"testing"

	"pkm-sync/pkg/models"
)

// mockJiraFetcher implements jiraFetcher for tests.
type mockJiraFetcher struct {
	item     models.FullItem
	err      error
	lastKey  string
}

func (m *mockJiraFetcher) FetchIssue(_ context.Context, key string) (models.FullItem, error) {
	m.lastKey = key
	return m.item, m.err
}

func TestJiraResolver_CanResolve(t *testing.T) {
	r, err := NewJiraResolver(&mockJiraFetcher{}, "https://company.atlassian.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://company.atlassian.net/browse/PROJ-123", true},
		{"https://company.atlassian.net/browse/MY-42", true},
		{"https://COMPANY.ATLASSIAN.NET/browse/PROJ-1", true}, // case-insensitive host
		{"https://other.atlassian.net/browse/PROJ-123", false}, // wrong host
		{"https://company.atlassian.net/issues/PROJ-123", false}, // no /browse/
		{"https://company.atlassian.net/browse/not-an-issue", false}, // no valid key
		{"https://slack.com/archives/C123", false},
	}

	for _, tt := range tests {
		got := r.CanResolve(tt.url)
		if got != tt.want {
			t.Errorf("CanResolve(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestJiraResolver_ExtractIssueKey(t *testing.T) {
	tests := []struct {
		url     string
		want    string
		wantErr bool
	}{
		{"https://company.atlassian.net/browse/PROJ-123", "PROJ-123", false},
		{"https://company.atlassian.net/browse/MY-42", "MY-42", false},
		{"https://company.atlassian.net/browse/ABC-9999", "ABC-9999", false},
		{"https://company.atlassian.net/browse/", "", true},
		{"not-a-url", "", true},
	}

	for _, tt := range tests {
		got, err := extractIssueKey(tt.url)
		if tt.wantErr {
			if err == nil {
				t.Errorf("extractIssueKey(%q) expected error, got nil", tt.url)
			}

			continue
		}

		if err != nil {
			t.Errorf("extractIssueKey(%q) unexpected error: %v", tt.url, err)
			continue
		}

		if got != tt.want {
			t.Errorf("extractIssueKey(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestJiraResolver_Resolve_CallsFetcherWithCorrectKey(t *testing.T) {
	fetcher := &mockJiraFetcher{
		item: models.NewBasicItem("jira_PROJ-123", "PROJ-123"),
	}

	r, err := NewJiraResolver(fetcher, "https://company.atlassian.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	item, err := r.Resolve(context.Background(), "https://company.atlassian.net/browse/PROJ-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fetcher.lastKey != "PROJ-123" {
		t.Errorf("FetchIssue called with key %q, want %q", fetcher.lastKey, "PROJ-123")
	}

	if item == nil {
		t.Fatal("expected a resolved item, got nil")
	}
}

func TestJiraResolver_Resolve_FetchError(t *testing.T) {
	fetcher := &mockJiraFetcher{err: errors.New("API error")}

	r, err := NewJiraResolver(fetcher, "https://company.atlassian.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = r.Resolve(context.Background(), "https://company.atlassian.net/browse/PROJ-1")
	if err == nil {
		t.Fatal("expected error from fetch failure, got nil")
	}
}

func TestJiraResolver_Resolve_TaggedAsResolved(t *testing.T) {
	baseItem := models.NewBasicItem("jira_PROJ-5", "PROJ-5")
	baseItem.SetTags([]string{"status:open"})

	fetcher := &mockJiraFetcher{item: baseItem}

	r, err := NewJiraResolver(fetcher, "https://company.atlassian.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	item, err := r.Resolve(context.Background(), "https://company.atlassian.net/browse/PROJ-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false

	for _, tag := range item.GetTags() {
		if tag == "resolved" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("resolved item should have 'resolved' tag, got: %v", item.GetTags())
	}
}

func TestNewJiraResolver_InvalidURL(t *testing.T) {
	_, err := NewJiraResolver(&mockJiraFetcher{}, "://bad-url")
	if err == nil {
		t.Fatal("expected error for invalid instance URL, got nil")
	}
}

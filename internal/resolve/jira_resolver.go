package resolve

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"pkm-sync/pkg/models"
)

// jiraFetcher is the subset of JiraSource used by JiraResolver.
// Defined as an interface so tests can inject a mock without a live Jira API.
type jiraFetcher interface {
	FetchIssue(ctx context.Context, issueKey string) (models.FullItem, error)
}

// issueKeyRe matches standard Jira issue keys like PROJ-123, MY-PROJ-42, or A-1.
// Single-letter project keys are supported by some Jira instances.
var issueKeyRe = regexp.MustCompile(`[A-Z][A-Z0-9]*-\d+`)

// JiraResolver resolves Jira issue URLs to FullItems.
type JiraResolver struct {
	fetcher  jiraFetcher
	baseHost string // lowercased host of the Jira instance, e.g. "company.atlassian.net"
}

// NewJiraResolver creates a JiraResolver for the given Jira instance URL and source.
// instanceURL should be the full base URL, e.g. "https://company.atlassian.net".
func NewJiraResolver(fetcher jiraFetcher, instanceURL string) (*JiraResolver, error) {
	parsed, err := url.Parse(instanceURL)
	if err != nil {
		return nil, fmt.Errorf("jira resolver: invalid instance URL %q: %w", instanceURL, err)
	}

	return &JiraResolver{
		fetcher:  fetcher,
		baseHost: strings.ToLower(parsed.Host),
	}, nil
}

// Name implements interfaces.Resolver.
func (r *JiraResolver) Name() string { return "jira" }

// CanResolve implements interfaces.Resolver. Returns true for URLs on the
// configured Jira instance whose path is exactly /browse/<ISSUE-KEY>.
// Only the segment immediately after "/browse/" is validated against the
// issue key regex to avoid false positives from longer paths.
func (r *JiraResolver) CanResolve(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	if strings.ToLower(parsed.Host) != r.baseHost {
		return false
	}

	segment, ok := browseSegment(parsed.Path)
	if !ok {
		return false
	}

	return issueKeyRe.MatchString(segment)
}

// browseSegment extracts the path segment immediately after "/browse/".
// Returns ("", false) if the path does not contain "/browse/".
func browseSegment(path string) (string, bool) {
	const prefix = "/browse/"

	idx := strings.Index(path, prefix)
	if idx < 0 {
		return "", false
	}

	segment := path[idx+len(prefix):]

	// Trim any trailing path components (e.g. /edit, /comment).
	if slash := strings.IndexByte(segment, '/'); slash >= 0 {
		segment = segment[:slash]
	}

	return segment, true
}

// Resolve implements interfaces.Resolver.
func (r *JiraResolver) Resolve(ctx context.Context, rawURL string) (models.FullItem, error) {
	key, err := extractIssueKey(rawURL)
	if err != nil {
		return nil, fmt.Errorf("jira resolver: %w", err)
	}

	item, err := r.fetcher.FetchIssue(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("jira resolver: fetch %s: %w", key, err)
	}

	// Tag as resolved so sinks can distinguish it from directly-synced items.
	item.SetTags(append(item.GetTags(), "resolved"))

	return item, nil
}

// extractIssueKey pulls the Jira issue key out of a /browse/<KEY> URL path.
func extractIssueKey(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	key := issueKeyRe.FindString(parsed.Path)
	if key == "" {
		return "", fmt.Errorf("no issue key found in URL %q", rawURL)
	}

	return key, nil
}

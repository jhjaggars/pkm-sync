package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	jiraclient "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"pkm-sync/pkg/models"
)

// JiraSource implements interfaces.Source for Jira.
type JiraSource struct {
	sourceID     string
	cfg          models.JiraSourceConfig
	client       *jiraclient.Client
	serverURL    string
	currentUser  string
	installation string // "Cloud" or "Local"
}

// isCloud returns true when the Jira instance uses the v3 Cloud API.
// jira-cli defaults to Cloud when no installation type is set.
func (s *JiraSource) isCloud() bool {
	return s.installation != jiraclient.InstallationTypeLocal
}

// NewJiraSource creates a new JiraSource from a SourceConfig.
func NewJiraSource(sourceID string, sourceCfg models.SourceConfig) *JiraSource {
	return &JiraSource{
		sourceID: sourceID,
		cfg:      sourceCfg.Jira,
	}
}

// Name implements interfaces.Source.
func (s *JiraSource) Name() string {
	return s.sourceID
}

// Configure implements interfaces.Source.
func (s *JiraSource) Configure(_ map[string]any, _ *http.Client) error {
	cliCfg, err := loadJiraConfig()
	if err != nil {
		return fmt.Errorf("failed to load jira-cli config: %w", err)
	}

	serverURL := cliCfg.Server
	if s.cfg.InstanceURL != "" {
		serverURL = s.cfg.InstanceURL
	}

	if serverURL == "" {
		return fmt.Errorf("jira server URL not configured: set instance_url in config or run 'jira init'")
	}

	token, err := resolveToken(cliCfg)
	if err != nil {
		return err
	}

	authType := jiraclient.AuthTypeBearer
	if cliCfg.AuthType == "basic" {
		authType = jiraclient.AuthTypeBasic
	}

	cfg := jiraclient.Config{
		Server:   serverURL,
		Login:    cliCfg.Login,
		APIToken: token,
		AuthType: &authType,
	}

	s.client = jiraclient.NewClient(cfg)
	s.serverURL = serverURL

	// Determine installation type: explicit config overrides jira-cli value.
	s.installation = cliCfg.Installation
	if s.cfg.Installation != "" {
		s.installation = s.cfg.Installation
	}

	if s.cfg.AssigneeFilter == "me" {
		me, meErr := s.client.Me()
		if meErr != nil {
			return fmt.Errorf("failed to resolve current Jira user: %w", meErr)
		}

		s.currentUser = me.Login
		if s.currentUser == "" {
			s.currentUser = cliCfg.Login
		}
	}

	return nil
}

// SupportsRealtime implements interfaces.Source.
func (s *JiraSource) SupportsRealtime() bool {
	return false
}

// Fetch implements interfaces.Source.
func (s *JiraSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	jql := buildJQL(s.cfg, since, s.currentUser)

	const pageSize = 50

	if s.isCloud() {
		return s.fetchCloud(jql, limit, pageSize)
	}

	return s.fetchLocal(jql, limit, pageSize)
}

// fetchCloud paginates using cursor-based nextPageToken (v3 /search/jql).
func (s *JiraSource) fetchCloud(jql string, limit, pageSize int) ([]models.FullItem, error) {
	var (
		allItems      []models.FullItem
		nextPageToken string
	)

	for {
		remaining := limit - len(allItems)
		if remaining <= 0 {
			break
		}

		batch := uint(pageSize)
		if remaining < pageSize {
			batch = uint(remaining)
		}

		result, err := s.searchCloudWithAllFields(jql, batch, nextPageToken)
		if err != nil {
			return nil, fmt.Errorf("jira search failed: %w", err)
		}

		for _, issue := range result.Issues {
			allItems = append(allItems, issueToItem(issue, s.serverURL, s.cfg))
		}

		if result.IsLast || len(result.Issues) == 0 {
			break
		}

		nextPageToken = result.NextPageToken
	}

	return allItems, nil
}

// fetchLocal paginates using offset-based startAt (v2 /search).
func (s *JiraSource) fetchLocal(jql string, limit, pageSize int) ([]models.FullItem, error) {
	var allItems []models.FullItem

	startAt := uint(0)

	for {
		remaining := limit - len(allItems)
		if remaining <= 0 {
			break
		}

		batch := uint(pageSize)
		if remaining < pageSize {
			batch = uint(remaining)
		}

		result, err := s.searchLocalWithAllFields(jql, startAt, batch)
		if err != nil {
			return nil, fmt.Errorf("jira search failed: %w", err)
		}

		for _, issue := range result.Issues {
			allItems = append(allItems, issueToItem(issue, s.serverURL, s.cfg))
		}

		if len(result.Issues) == 0 || result.IsLast {
			break
		}

		startAt += uint(len(result.Issues))
	}

	return allItems, nil
}

// FetchOne implements interfaces.Fetcher. key is a Jira issue key such as
// "PROJ-123". This makes JiraSource usable with the `pkm-sync fetch jira/KEY`
// verb without going through the bulk Fetch pipeline.
func (s *JiraSource) FetchOne(ctx context.Context, key string) (models.FullItem, error) {
	return s.FetchIssue(ctx, key)
}

// FetchIssue retrieves a single Jira issue by key (e.g. "PROJ-123") and converts
// it to a FullItem. Used by the cross-source reference resolver.
func (s *JiraSource) FetchIssue(ctx context.Context, issueKey string) (models.FullItem, error) {
	path := fmt.Sprintf("/issue/%s?fields=*all", url.PathEscape(issueKey))

	var (
		res *http.Response
		err error
	)

	if s.isCloud() {
		res, err = s.client.Get(ctx, path, nil)
	} else {
		res, err = s.client.GetV2(ctx, path, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("jira: fetch issue %s: %w", issueKey, err)
	}

	if res == nil {
		return nil, fmt.Errorf("jira: empty response for issue %s", issueKey)
	}

	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		var errs jiraclient.Errors

		_ = json.NewDecoder(res.Body).Decode(&errs)

		return nil, fmt.Errorf("jira: issue %s returned %s: %s", issueKey, res.Status, errs.String())
	}

	var issue jiraclient.Issue

	if err := json.NewDecoder(res.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("jira: decode issue %s: %w", issueKey, err)
	}

	return issueToItem(&issue, s.serverURL, s.cfg), nil
}

// searchCloudWithAllFields performs a v3 search with fields=*all.
// Uses cursor-based pagination via nextPageToken (Cloud /search/jql API).
func (s *JiraSource) searchCloudWithAllFields(
	jql string, limit uint, pageToken string,
) (*jiraclient.SearchResult, error) {
	path := fmt.Sprintf(
		"/search/jql?jql=%s&maxResults=%d&fields=*all",
		url.QueryEscape(jql), limit,
	)

	if pageToken != "" {
		path += "&nextPageToken=" + url.QueryEscape(pageToken)
	}

	res, err := s.client.Get(context.Background(), path, nil)
	if err != nil {
		return nil, fmt.Errorf("jira cloud search: %w", err)
	}

	return decodeSearchResult(res)
}

// searchV2Result extends SearchResult with v2-specific offset pagination fields.
type searchV2Result struct {
	jiraclient.SearchResult

	StartAt    int `json:"startAt"`
	MaxResults int `json:"maxResults"`
	Total      int `json:"total"`
}

// searchLocalWithAllFields performs a v2 search with fields=*all.
// Uses offset-based pagination via startAt (Server/DC /search API).
func (s *JiraSource) searchLocalWithAllFields(jql string, startAt, limit uint) (*jiraclient.SearchResult, error) {
	path := fmt.Sprintf(
		"/search?jql=%s&startAt=%d&maxResults=%d&fields=*all",
		url.QueryEscape(jql), startAt, limit,
	)

	res, err := s.client.GetV2(context.Background(), path, nil)
	if err != nil {
		return nil, fmt.Errorf("jira local search: %w", err)
	}

	if res == nil {
		return nil, jiraclient.ErrEmptyResponse
	}

	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		var errs jiraclient.Errors

		_ = json.NewDecoder(res.Body).Decode(&errs)

		return nil, fmt.Errorf("jira search returned %s: %s", res.Status, errs.String())
	}

	var v2 searchV2Result

	if err := json.NewDecoder(res.Body).Decode(&v2); err != nil {
		return nil, fmt.Errorf("failed to decode search result: %w", err)
	}

	// Translate v2 offset pagination into the IsLast flag used by Fetch().
	v2.IsLast = (v2.StartAt + len(v2.Issues)) >= v2.Total

	return &v2.SearchResult, nil
}

// decodeSearchResult reads and decodes a raw HTTP response into a SearchResult.
func decodeSearchResult(res *http.Response) (*jiraclient.SearchResult, error) {
	if res == nil {
		return nil, jiraclient.ErrEmptyResponse
	}

	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		var errs jiraclient.Errors

		_ = json.NewDecoder(res.Body).Decode(&errs)

		return nil, fmt.Errorf("jira search returned %s: %s", res.Status, errs.String())
	}

	var result jiraclient.SearchResult

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search result: %w", err)
	}

	return &result, nil
}

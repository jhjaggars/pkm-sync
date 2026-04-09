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
	sourceID    string
	cfg         models.JiraSourceConfig
	client      *jiraclient.Client
	serverURL   string
	currentUser string
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

	var allItems []models.FullItem

	var from uint

	for {
		remaining := limit - len(allItems)
		if remaining <= 0 {
			break
		}

		batch := uint(pageSize)
		if remaining < pageSize {
			batch = uint(remaining)
		}

		result, err := s.searchWithAllFields(jql, from, batch)
		if err != nil {
			return nil, fmt.Errorf("jira search failed: %w", err)
		}

		for _, issue := range result.Issues {
			item := issueToItem(issue, s.serverURL, s.cfg.IncludeComments)
			allItems = append(allItems, item)
		}

		if uint(len(result.Issues)) < batch {
			break
		}

		from += uint(len(result.Issues))
	}

	return allItems, nil
}

// FetchIssue retrieves a single Jira issue by key (e.g. "PROJ-123") and converts
// it to a FullItem. Used by the cross-source reference resolver.
func (s *JiraSource) FetchIssue(ctx context.Context, issueKey string) (models.FullItem, error) {
	path := fmt.Sprintf("/issue/%s?fields=*all", url.PathEscape(issueKey))

	res, err := s.client.Get(ctx, path, nil)
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

	return issueToItem(&issue, s.serverURL, s.cfg.IncludeComments), nil
}

// searchWithAllFields performs a search with fields=*all to retrieve all issue fields.
func (s *JiraSource) searchWithAllFields(jql string, from, limit uint) (*jiraclient.SearchResult, error) {
	path := fmt.Sprintf(
		"/search/jql?jql=%s&startAt=%d&maxResults=%d&fields=*all",
		url.QueryEscape(jql), from, limit,
	)

	res, err := s.client.Get(context.Background(), path, nil)
	if err != nil {
		return nil, err
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

	var result jiraclient.SearchResult

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search result: %w", err)
	}

	return &result, nil
}

package servicenow

import (
	"fmt"
	"net/http"
	"time"

	"pkm-sync/internal/config"
	"pkm-sync/pkg/models"
)

// defaultTables is the list of ServiceNow tables synced when none are configured.
var defaultTables = []string{"sc_req_item"}

// defaultQuery fetches tickets assigned to or opened by the current user.
const defaultQuery = "assigned_to=javascript:gs.getUserID()^ORopened_by=javascript:gs.getUserID()"

// scReqItemFields are the fields fetched for sc_req_item records.
var scReqItemFields = []string{
	"sys_id", "number", "short_description", "description", "state",
	"priority", "assignment_group", "assigned_to", "opened_by",
	"opened_at", "sys_updated_on", "comments", "approval", "cat_item",
}

// genericFields are the fields fetched for other table types.
var genericFields = []string{
	"sys_id", "number", "short_description", "description", "state",
	"priority", "assignment_group", "assigned_to", "opened_by",
	"opened_at", "sys_updated_on", "comments",
}

// ServiceNowSource implements interfaces.Source for ServiceNow.
type ServiceNowSource struct {
	sourceID  string
	cfg       models.ServiceNowSourceConfig
	configDir string
	client    *Client
}

// NewServiceNowSource creates a new ServiceNowSource from a SourceConfig.
func NewServiceNowSource(sourceID string, sourceCfg models.SourceConfig) *ServiceNowSource {
	return &ServiceNowSource{
		sourceID: sourceID,
		cfg:      sourceCfg.ServiceNow,
	}
}

// Name implements interfaces.Source.
func (s *ServiceNowSource) Name() string {
	return s.sourceID
}

// SupportsRealtime implements interfaces.Source.
func (s *ServiceNowSource) SupportsRealtime() bool {
	return false
}

// Configure implements interfaces.Source.
func (s *ServiceNowSource) Configure(_ map[string]any, _ *http.Client) error {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	s.configDir = configDir

	instance := instanceName(s.cfg.InstanceURL)

	td, err := LoadToken(configDir, instance)
	if err != nil {
		return fmt.Errorf("failed to load ServiceNow token: %w", err)
	}

	if td == nil {
		return fmt.Errorf("no ServiceNow token found for %s: run 'pkm-sync servicenow auth --instance %s' to authenticate",
			s.cfg.InstanceURL, s.cfg.InstanceURL)
	}

	s.client = NewClient(td.GCK, td.CookieHeader, s.cfg.InstanceURL, s.cfg.RequestDelay)

	return nil
}

// Fetch implements interfaces.Source.
func (s *ServiceNowSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	tables := s.cfg.Tables
	if len(tables) == 0 {
		tables = defaultTables
	}

	baseQuery := s.cfg.Query
	if baseQuery == "" {
		baseQuery = defaultQuery
	}

	var allItems []models.FullItem

	for _, table := range tables {
		items, err := s.fetchTable(table, baseQuery, since, limit-len(allItems))
		if err != nil {
			return nil, fmt.Errorf("failed to fetch table %s: %w", table, err)
		}

		allItems = append(allItems, items...)

		if len(allItems) >= limit {
			break
		}
	}

	return allItems, nil
}

// fetchTable fetches all matching records from a single ServiceNow table.
func (s *ServiceNowSource) fetchTable(table, baseQuery string, since time.Time, limit int) ([]models.FullItem, error) {
	query := buildQuery(baseQuery, since)
	fields := fieldsForTable(table)

	const pageSize = 50

	var allItems []models.FullItem

	for offset := 0; len(allItems) < limit; offset += pageSize {
		remaining := limit - len(allItems)
		fetchSize := pageSize

		if remaining < pageSize {
			fetchSize = remaining
		}

		records, err := s.client.QueryTable(table, query, fields, fetchSize, offset)
		if err != nil {
			return nil, err
		}

		for _, record := range records {
			allItems = append(allItems, recordToItem(record, table, s.cfg.InstanceURL))
		}

		if len(records) < fetchSize {
			break // no more pages
		}
	}

	return allItems, nil
}

// buildQuery constructs the sysparm_query string from a base query and a since time.
func buildQuery(baseQuery string, since time.Time) string {
	if since.IsZero() {
		return baseQuery
	}

	// ServiceNow date filter format.
	sinceStr := since.UTC().Format("2006-01-02 15:04:05")
	sinceFilter := fmt.Sprintf("sys_updated_on>=%s", sinceStr)

	if baseQuery == "" {
		return sinceFilter
	}

	return baseQuery + "^" + sinceFilter
}

// fieldsForTable returns the field list for a given table.
func fieldsForTable(table string) []string {
	if table == "sc_req_item" {
		return scReqItemFields
	}

	return genericFields
}

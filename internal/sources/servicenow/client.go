package servicenow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is an HTTP client for the ServiceNow REST Table API.
type Client struct {
	gck          string
	cookieHeader string
	instanceURL  string
	httpClient   *http.Client
	requestDelay time.Duration
}

// NewClient creates a ServiceNow API client using session credentials.
func NewClient(gck, cookieHeader, instanceURL string, requestDelay time.Duration) *Client {
	if requestDelay == 0 {
		requestDelay = 200 * time.Millisecond
	}

	return &Client{
		gck:          gck,
		cookieHeader: cookieHeader,
		instanceURL:  strings.TrimRight(instanceURL, "/"),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		requestDelay: requestDelay,
	}
}

// QueryTable fetches records from a ServiceNow table via the REST Table API.
// Returns the parsed result array from {"result": [...]}.
func (c *Client) QueryTable(table, query string, fields []string, limit, offset int) ([]map[string]any, error) {
	params := url.Values{}
	params.Set("sysparm_display_value", "true")
	params.Set("sysparm_limit", fmt.Sprintf("%d", limit))
	params.Set("sysparm_offset", fmt.Sprintf("%d", offset))

	if query != "" {
		params.Set("sysparm_query", query)
	}

	if len(fields) > 0 {
		params.Set("sysparm_fields", strings.Join(fields, ","))
	}

	endpoint := fmt.Sprintf("%s/api/now/table/%s?%s", c.instanceURL, table, params.Encode())

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-UserToken", c.gck)
	req.Header.Set("Cookie", c.cookieHeader)

	if c.requestDelay > 0 {
		time.Sleep(c.requestDelay)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf(
			"ServiceNow authentication expired (HTTP %d): run 'pkm-sync servicenow auth' to re-authenticate",
			resp.StatusCode,
		)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ServiceNow API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Result []map[string]any `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ServiceNow response: %w", err)
	}

	return result.Result, nil
}

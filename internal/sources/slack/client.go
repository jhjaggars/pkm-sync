package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// SlackChannel represents a Slack channel or DM.
type SlackChannel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsIM    bool   `json:"is_im"`
	IsGroup bool   `json:"is_group"`
	User    string `json:"user"` // For IMs: the other user's ID
}

// RawMessage is a raw Slack API message object.
type RawMessage struct {
	Type       string            `json:"type"`
	Subtype    string            `json:"subtype"`
	Text       string            `json:"text"`
	User       string            `json:"user"`
	BotID      string            `json:"bot_id"`
	Username   string            `json:"username"`
	Ts         string            `json:"ts"`
	ThreadTs   string            `json:"thread_ts"`
	ReplyCount int               `json:"reply_count"`
	Blocks     []json.RawMessage `json:"blocks"`
}

// Client calls the Slack internal web API.
type Client struct {
	token        string
	cookieHeader string
	apiBaseURL   string
	httpClient   *http.Client
	rateLimitMs  int
}

// NewClient creates a new Slack API client.
func NewClient(token, cookieHeader, apiBaseURL string, rateLimitMs int) *Client {
	if apiBaseURL == "" {
		apiBaseURL = "https://slack.com"
	}

	if rateLimitMs <= 0 {
		rateLimitMs = 500
	}

	return &Client{
		token:        token,
		cookieHeader: cookieHeader,
		apiBaseURL:   apiBaseURL,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		rateLimitMs:  rateLimitMs,
	}
}

// CallAPI calls a Slack API method using multipart form data.
func (c *Client) CallAPI(method string, params map[string]string) (map[string]any, error) {
	var body bytes.Buffer

	w := multipart.NewWriter(&body)

	if err := w.WriteField("token", c.token); err != nil {
		return nil, fmt.Errorf("failed to write token field: %w", err)
	}

	for k, v := range params {
		if err := w.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("failed to write field %s: %w", k, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	apiURL := fmt.Sprintf("%s/api/%s", c.apiBaseURL, method)

	req, err := http.NewRequest(http.MethodPost, apiURL, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", w.FormDataContentType())

	if c.cookieHeader != "" {
		req.Header.Set("Cookie", c.cookieHeader)
	}

	backoffMs := c.rateLimitMs

	for {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("HTTP request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		var result map[string]any
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("failed to parse response JSON: %w", err)
		}

		// Handle rate limiting
		if errVal, _ := result["error"].(string); errVal == "ratelimited" {
			time.Sleep(time.Duration(backoffMs) * time.Millisecond)

			backoffMs = min(backoffMs*2, 30000)

			// Re-create the request body for retry
			body.Reset()
			w = multipart.NewWriter(&body)

			_ = w.WriteField("token", c.token)

			for k, v := range params {
				_ = w.WriteField(k, v)
			}

			_ = w.Close()

			req, err = http.NewRequest(http.MethodPost, apiURL, &body)
			if err != nil {
				return nil, fmt.Errorf("failed to recreate request: %w", err)
			}

			req.Header.Set("Content-Type", w.FormDataContentType())

			if c.cookieHeader != "" {
				req.Header.Set("Cookie", c.cookieHeader)
			}

			continue
		}

		return result, nil
	}
}

// bootData calls client.userBoot and returns the raw response.
func (c *Client) bootData() (map[string]any, error) {
	return c.CallAPI("client.userBoot", map[string]string{
		"_x_reason":                 "webapp_start",
		"version_all_channels":      "0",
		"return_all_relevant_mpdms": "true",
		"min_channel_updated":       "0",
	})
}

// BootKeys returns the top-level keys from the client.userBoot response.
// Useful for diagnosing Enterprise Slack response layouts.
func (c *Client) BootKeys() ([]string, error) {
	boot, err := c.bootData()
	if err != nil {
		return nil, fmt.Errorf("failed to get boot data: %w", err)
	}

	keys := make([]string, 0, len(boot))
	for k := range boot {
		keys = append(keys, k)
	}

	return keys, nil
}

// BootChannelSample returns up to n raw channel objects from the boot response.
// Useful for inspecting the raw channel structure returned by the API.
func (c *Client) BootChannelSample(n int) ([]map[string]any, error) {
	boot, err := c.bootData()
	if err != nil {
		return nil, fmt.Errorf("failed to get boot data: %w", err)
	}

	raw, _ := boot["channels"].([]any)

	if n > len(raw) {
		n = len(raw)
	}

	samples := make([]map[string]any, 0, n)

	for _, item := range raw[:n] {
		if m, ok := item.(map[string]any); ok {
			samples = append(samples, m)
		}
	}

	return samples, nil
}

// GetChannels returns all channels and groups from the workspace.
func (c *Client) GetChannels() ([]SlackChannel, error) {
	boot, err := c.bootData()
	if err != nil {
		return nil, fmt.Errorf("failed to get boot data: %w", err)
	}

	if ok, _ := boot["ok"].(bool); !ok {
		errMsg, _ := boot["error"].(string)

		return nil, fmt.Errorf("client.userBoot failed: %s", errMsg)
	}

	var channels []SlackChannel

	if raw, ok := boot["channels"].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				channels = append(channels, mapToChannel(m, false))
			}
		}
	}

	if raw, ok := boot["groups"].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				channels = append(channels, mapToChannel(m, false))
			}
		}
	}

	return channels, nil
}

// GetDMs returns all direct message conversations.
func (c *Client) GetDMs() ([]SlackChannel, error) {
	boot, err := c.bootData()
	if err != nil {
		return nil, fmt.Errorf("failed to get boot data: %w", err)
	}

	if ok, _ := boot["ok"].(bool); !ok {
		errMsg, _ := boot["error"].(string)

		return nil, fmt.Errorf("client.userBoot failed: %s", errMsg)
	}

	var dms []SlackChannel

	if raw, ok := boot["ims"].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				dms = append(dms, mapToChannel(m, true))
			}
		}
	}

	return dms, nil
}

// FindChannel resolves a channel name to a SlackChannel.
func (c *Client) FindChannel(name string) (*SlackChannel, error) {
	channels, err := c.GetChannels()
	if err != nil {
		return nil, err
	}

	for i, ch := range channels {
		if ch.Name == name {
			return &channels[i], nil
		}
	}

	return nil, fmt.Errorf("channel #%s not found", name)
}

// GetHistory fetches paginated message history for a channel.
func (c *Client) GetHistory(channelID, oldest, latest, cursor string, limit int) ([]RawMessage, string, error) {
	params := map[string]string{
		"channel": channelID,
		"limit":   fmt.Sprintf("%d", limit),
	}

	if oldest != "" {
		params["oldest"] = oldest
	}

	if latest != "" {
		params["latest"] = latest
	}

	if cursor != "" {
		params["cursor"] = cursor
	}

	result, err := c.CallAPI("conversations.history", params)
	if err != nil {
		return nil, "", err
	}

	if ok, _ := result["ok"].(bool); !ok {
		errMsg, _ := result["error"].(string)

		return nil, "", fmt.Errorf("conversations.history failed: %s", errMsg)
	}

	msgs, err := parseMessages(result["messages"])
	if err != nil {
		return nil, "", err
	}

	nextCursor := ""

	if meta, ok := result["response_metadata"].(map[string]any); ok {
		nextCursor, _ = meta["next_cursor"].(string)
	}

	return msgs, nextCursor, nil
}

// GetReplies fetches all replies for a thread.
func (c *Client) GetReplies(channelID, threadTS string) ([]RawMessage, error) {
	result, err := c.CallAPI("conversations.replies", map[string]string{
		"channel": channelID,
		"ts":      threadTS,
	})
	if err != nil {
		return nil, err
	}

	if ok, _ := result["ok"].(bool); !ok {
		errMsg, _ := result["error"].(string)

		return nil, fmt.Errorf("conversations.replies failed: %s", errMsg)
	}

	return parseMessages(result["messages"])
}

// GetUserInfo fetches profile information for a user.
func (c *Client) GetUserInfo(userID string) (string, error) {
	result, err := c.CallAPI("users.info", map[string]string{"user": userID})
	if err != nil {
		return userID, err
	}

	if ok, _ := result["ok"].(bool); !ok {
		return userID, nil
	}

	if user, ok := result["user"].(map[string]any); ok {
		if realName, ok := user["real_name"].(string); ok && realName != "" {
			return realName, nil
		}

		if name, ok := user["name"].(string); ok && name != "" {
			return name, nil
		}
	}

	return userID, nil
}

func mapToChannel(m map[string]any, isIM bool) SlackChannel {
	ch := SlackChannel{IsIM: isIM}

	if id, ok := m["id"].(string); ok {
		ch.ID = id
	}

	if name, ok := m["name"].(string); ok {
		ch.Name = name
	}

	if user, ok := m["user"].(string); ok {
		ch.User = user
	}

	if v, ok := m["is_im"].(bool); ok {
		ch.IsIM = v
	}

	if v, ok := m["is_group"].(bool); ok {
		ch.IsGroup = v
	}

	return ch
}

func parseMessages(raw any) ([]RawMessage, error) {
	rawSlice, ok := raw.([]any)
	if !ok {
		return nil, nil
	}

	msgs := make([]RawMessage, 0, len(rawSlice))

	for _, item := range rawSlice {
		jsonBytes, err := json.Marshal(item)
		if err != nil {
			continue
		}

		var msg RawMessage
		if err := json.Unmarshal(jsonBytes, &msg); err != nil {
			continue
		}

		msgs = append(msgs, msg)
	}

	return msgs, nil
}

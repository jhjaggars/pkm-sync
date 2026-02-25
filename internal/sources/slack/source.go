package slack

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"pkm-sync/internal/config"
	"pkm-sync/pkg/models"
)

// systemSubtypes are message subtypes that should be filtered out.
var systemSubtypes = map[string]bool{
	"channel_join":    true,
	"channel_leave":   true,
	"channel_topic":   true,
	"channel_purpose": true,
	"channel_archive": true,
	"channel_name":    true,
}

// SlackSource implements interfaces.Source for Slack.
type SlackSource struct {
	sourceID    string
	cfg         models.SlackSourceConfig
	configDir   string
	client      *Client
	userCache   *UserCache
	rateLimitMs int
}

// NewSlackSource creates a new SlackSource from a SourceConfig.
func NewSlackSource(sourceID string, sourceCfg models.SourceConfig) *SlackSource {
	return &SlackSource{
		sourceID: sourceID,
		cfg:      sourceCfg.Slack,
	}
}

// Name implements interfaces.Source.
func (s *SlackSource) Name() string {
	return s.sourceID
}

// Configure implements interfaces.Source.
func (s *SlackSource) Configure(_ map[string]any, _ *http.Client) error {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	s.configDir = configDir

	workspace := workspaceName(s.cfg.WorkspaceURL)

	td, err := LoadToken(configDir, workspace)
	if err != nil {
		return fmt.Errorf("failed to load Slack token: %w", err)
	}

	if td == nil {
		return fmt.Errorf("no Slack token found for workspace %q — run 'pkm-sync slack auth' first", workspace)
	}

	apiURL := s.cfg.APIUrl

	rateLimitMs := s.cfg.RateLimitMs
	if rateLimitMs <= 0 {
		rateLimitMs = 500
	}

	s.rateLimitMs = rateLimitMs
	s.client = NewClient(td.Token, td.CookieHeader, apiURL, rateLimitMs)
	s.userCache = NewUserCache(configDir)

	return nil
}

// Client returns the underlying API client (used for diagnostics).
func (s *SlackSource) Client() *Client {
	return s.client
}

// SupportsRealtime implements interfaces.Source.
func (s *SlackSource) SupportsRealtime() bool {
	return false
}

// Fetch implements interfaces.Source.
func (s *SlackSource) Fetch(since time.Time, limit int) ([]models.FullItem, error) {
	oldest := ""
	if !since.IsZero() {
		oldest = fmt.Sprintf("%d", since.Unix())
	}

	maxPerChannel := s.cfg.MaxMessagesPerChannel
	if maxPerChannel <= 0 || (limit > 0 && limit < maxPerChannel) {
		maxPerChannel = limit
	}

	if maxPerChannel <= 0 {
		maxPerChannel = 1000
	}

	var allItems []models.FullItem

	// Resolve configured channels.
	channelsToSync := make([]SlackChannel, 0, len(s.cfg.Channels))

	for _, name := range s.cfg.Channels {
		ch, err := s.client.FindChannel(name)
		if err != nil {
			fmt.Printf("Warning: could not find Slack channel #%s: %v\n", name, err)

			continue
		}

		channelsToSync = append(channelsToSync, *ch)
	}

	// Optionally include DMs.
	if s.cfg.IncludeDMs {
		dms, err := s.client.GetDMs()
		if err != nil {
			fmt.Printf("Warning: failed to fetch Slack DMs: %v\n", err)
		} else {
			channelsToSync = append(channelsToSync, dms...)
		}
	}

	for _, ch := range channelsToSync {
		items, err := s.fetchChannel(ch, oldest, maxPerChannel)
		if err != nil {
			fmt.Printf("Warning: failed to fetch Slack channel %s: %v\n", ch.Name, err)

			continue
		}

		allItems = append(allItems, items...)
	}

	if err := s.userCache.Save(); err != nil {
		fmt.Printf("Warning: failed to save user cache: %v\n", err)
	}

	return allItems, nil
}

// fetchChannel fetches all messages for a channel and returns one daily-note FullItem per calendar day.
// Thread replies are fetched and embedded inline within each daily note.
func (s *SlackSource) fetchChannel(ch SlackChannel, oldest string, maxMessages int) ([]models.FullItem, error) {
	channelName := ch.Name
	if ch.IsIM && channelName == "" {
		channelName = s.userCache.ResolveUser(ch.User, s.client)
	}

	// Paginate through message history.
	pageSize := 200
	cursor := ""

	var rawMsgs []RawMessage

	fetched := 0

	for {
		remaining := maxMessages - fetched
		if remaining <= 0 {
			break
		}

		if remaining < pageSize {
			pageSize = remaining
		}

		msgs, nextCursor, err := s.client.GetHistory(ch.ID, oldest, "", cursor, pageSize)
		if err != nil {
			return nil, fmt.Errorf("GetHistory failed: %w", err)
		}

		rawMsgs = append(rawMsgs, msgs...)
		fetched += len(msgs)

		if nextCursor == "" {
			break
		}

		cursor = nextCursor

		time.Sleep(time.Duration(s.rateLimitMs) * time.Millisecond)
	}

	// Filter, resolve authors, and fetch thread replies.
	entries := make([]messageEntry, 0, len(rawMsgs))

	for i := range rawMsgs {
		msg := &rawMsgs[i]

		if systemSubtypes[msg.Subtype] {
			continue
		}

		if s.cfg.ExcludeBots && (msg.BotID != "" || msg.Subtype == "bot_message") {
			continue
		}

		content := ExtractMessageText(msg)
		if s.cfg.MinLength > 0 && len(strings.TrimSpace(content)) < s.cfg.MinLength {
			continue
		}

		entry := messageEntry{
			msg:    *msg,
			author: resolveAuthor(msg, s.userCache, s.client),
		}

		// Fetch and embed thread replies inline.
		isThreadRoot := msg.ThreadTs == msg.Ts && msg.ReplyCount > 0

		if s.cfg.IncludeThreads && isThreadRoot {
			replies, err := s.client.GetReplies(ch.ID, msg.Ts)
			if err != nil {
				fmt.Printf("Warning: failed to fetch thread replies for %s: %v\n", msg.Ts, err)
			} else {
				for j := range replies {
					if replies[j].Ts == msg.Ts {
						continue // skip parent included in reply list
					}

					entry.replies = append(entry.replies, replyEntry{
						msg:    replies[j],
						author: resolveAuthor(&replies[j], s.userCache, s.client),
					})
				}
			}

			time.Sleep(time.Duration(s.rateLimitMs) * time.Millisecond)
		}

		entries = append(entries, entry)
	}

	// Group entries by calendar date (UTC).
	byDate := make(map[string][]messageEntry)

	var dateOrder []string

	seen := make(map[string]bool)

	for _, e := range entries {
		dateStr := tsToTime(e.msg.Ts).UTC().Format("2006-01-02")
		if !seen[dateStr] {
			dateOrder = append(dateOrder, dateStr)
			seen[dateStr] = true
		}

		byDate[dateStr] = append(byDate[dateStr], e)
	}

	// Build one daily note per date, preserving chronological order.
	items := make([]models.FullItem, 0, len(byDate))

	for _, dateStr := range dateOrder {
		date, _ := time.Parse("2006-01-02", dateStr)
		item := BuildDailyNote(date, byDate[dateStr], ch.ID, channelName, s.cfg.WorkspaceURL)

		// Tag DMs additionally.
		if ch.IsIM {
			tags := item.GetTags()
			item.SetTags(append(tags, fmt.Sprintf("dm:%s", channelName)))
		}

		items = append(items, item)
	}

	return items, nil
}

// resolveAuthor returns the best display name for a message sender.
func resolveAuthor(msg *RawMessage, cache *UserCache, client *Client) string {
	if msg.User != "" {
		return cache.ResolveUser(msg.User, client)
	}

	if msg.Username != "" {
		return msg.Username
	}

	if msg.BotID != "" {
		return msg.BotID
	}

	return "Unknown"
}

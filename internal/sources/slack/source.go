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

	// Resolve configured channels
	channelNames := s.cfg.Channels
	channelsToSync := make([]SlackChannel, 0, len(channelNames))

	for _, name := range channelNames {
		ch, err := s.client.FindChannel(name)
		if err != nil {
			fmt.Printf("Warning: could not find Slack channel #%s: %v\n", name, err)

			continue
		}

		channelsToSync = append(channelsToSync, *ch)
	}

	// Optionally include DMs
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

// fetchChannel fetches messages from a single channel or DM.
func (s *SlackSource) fetchChannel(ch SlackChannel, oldest string, maxMessages int) ([]models.FullItem, error) {
	channelName := ch.Name
	if ch.IsIM && channelName == "" {
		// For DMs, resolve the other user's name
		channelName = s.userCache.ResolveUser(ch.User, s.client)
	}

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

	items := make([]models.FullItem, 0, len(rawMsgs))

	for i := range rawMsgs {
		msg := &rawMsgs[i]

		// Skip system messages
		if systemSubtypes[msg.Subtype] {
			continue
		}

		// Skip bots if configured
		if s.cfg.ExcludeBots && (msg.BotID != "" || msg.Subtype == "bot_message") {
			continue
		}

		content := ExtractMessageText(msg)

		// Skip short messages
		if s.cfg.MinLength > 0 && len(strings.TrimSpace(content)) < s.cfg.MinLength {
			continue
		}

		// Resolve author
		authorName := msg.User
		if authorName == "" {
			authorName = msg.Username
		}

		if authorName == "" {
			authorName = msg.BotID
		}

		if msg.User != "" {
			authorName = s.userCache.ResolveUser(msg.User, s.client)
		}

		// Fetch thread replies if this is a thread root
		isThreadRoot := msg.ThreadTs == msg.Ts && msg.ReplyCount > 0

		threadItems := s.processMessage(msg, ch.ID, channelName, authorName, isThreadRoot)
		items = append(items, threadItems...)
	}

	// Tag DMs with the other user's name
	if ch.IsIM {
		for _, item := range items {
			tags := item.GetTags()
			tags = append(tags, fmt.Sprintf("dm:%s", channelName))
			item.SetTags(tags)
		}
	}

	return items, nil
}

// processMessage converts a single raw message into FullItems, fetching thread replies when applicable.
func (s *SlackSource) processMessage(
	msg *RawMessage, channelID, channelName, authorName string, isThreadRoot bool,
) []models.FullItem {
	if !s.cfg.IncludeThreads || !isThreadRoot {
		return []models.FullItem{FromSlackMessage(msg, channelID, channelName, s.cfg.WorkspaceURL, authorName)}
	}

	replies, err := s.client.GetReplies(channelID, msg.Ts)
	if err != nil {
		fmt.Printf("Warning: failed to fetch thread replies: %v\n", err)

		return []models.FullItem{FromSlackMessage(msg, channelID, channelName, s.cfg.WorkspaceURL, authorName)}
	}

	time.Sleep(time.Duration(s.rateLimitMs) * time.Millisecond)

	switch s.cfg.ThreadMode {
	case "consolidated":
		thread := FromSlackThread(msg, replies, channelID, channelName, s.cfg.WorkspaceURL, authorName, s.userCache, s.client)

		return []models.FullItem{thread}

	case "summary":
		summaryLength := s.cfg.ThreadSummaryLength
		if summaryLength <= 0 {
			summaryLength = 5
		}

		if len(replies) > summaryLength+1 {
			replies = replies[:summaryLength+1]
		}

		thread := FromSlackThread(msg, replies, channelID, channelName, s.cfg.WorkspaceURL, authorName, s.userCache, s.client)
		thread.SetItemType("slack_thread_summary")

		return []models.FullItem{thread}

	default: // "individual" or unset
		return []models.FullItem{FromSlackMessage(msg, channelID, channelName, s.cfg.WorkspaceURL, authorName)}
	}
}

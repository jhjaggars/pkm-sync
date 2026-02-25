package slack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// UserCache resolves Slack user IDs to display names.
type UserCache struct {
	configDir string
	entries   map[string]string // userID -> display name
	dirty     bool
}

// NewUserCache creates a user cache backed by a JSON file.
func NewUserCache(configDir string) *UserCache {
	uc := &UserCache{
		configDir: configDir,
		entries:   make(map[string]string),
	}

	uc.load()

	return uc
}

func (uc *UserCache) cachePath() string {
	return filepath.Join(uc.configDir, "slack-user-cache.json")
}

func (uc *UserCache) load() {
	data, err := os.ReadFile(uc.cachePath())
	if err != nil {
		return
	}

	_ = json.Unmarshal(data, &uc.entries)
}

// Save writes the cache to disk if it has been modified.
func (uc *UserCache) Save() error {
	if !uc.dirty {
		return nil
	}

	if err := os.MkdirAll(uc.configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	data, err := json.MarshalIndent(uc.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal user cache: %w", err)
	}

	if err := os.WriteFile(uc.cachePath(), data, 0600); err != nil {
		return fmt.Errorf("failed to write user cache: %w", err)
	}

	uc.dirty = false

	return nil
}

// ResolveUser returns the display name for a user ID, fetching from the API if needed.
func (uc *UserCache) ResolveUser(userID string, client *Client) string {
	if name, ok := uc.entries[userID]; ok {
		return name
	}

	name, err := client.GetUserInfo(userID)
	if err != nil {
		name = userID
	}

	uc.entries[userID] = name
	uc.dirty = true

	return name
}

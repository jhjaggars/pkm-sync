package slack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pkm-sync/internal/keystore"
)

// secretStore is the active secret store; nil means use legacy file behavior.
var secretStore keystore.Store

// SetStore configures the secret store used for Slack tokens.
// Call this once in PersistentPreRun before any token operations.
func SetStore(s keystore.Store) {
	secretStore = s
}

func slackKeyForWorkspace(workspace string) string {
	return "slack-token-" + workspace
}

// TokenData holds the extracted Slack session token and cookies.
type TokenData struct {
	Token        string        `json:"token"`
	Cookies      []CookieEntry `json:"cookies"`
	CookieHeader string        `json:"cookie_header"`
	Timestamp    time.Time     `json:"timestamp"`
	Workspace    string        `json:"workspace"`
}

// CookieEntry represents a single browser cookie.
type CookieEntry struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

func tokenFilePath(configDir, workspace string) string {
	return filepath.Join(configDir, fmt.Sprintf("slack-token-%s.json", workspace))
}

// LoadToken loads token data from the configured store (or file fallback).
func LoadToken(configDir, workspace string) (*TokenData, error) {
	if secretStore != nil {
		raw, err := secretStore.Get(slackKeyForWorkspace(workspace))
		if err != nil {
			if err == keystore.ErrNotFound {
				return nil, nil
			}

			return nil, fmt.Errorf("failed to read token from store: %w", err)
		}

		var td TokenData
		if err := json.Unmarshal([]byte(raw), &td); err != nil {
			return nil, fmt.Errorf("failed to parse stored token: %w", err)
		}

		return &td, nil
	}

	path := tokenFilePath(configDir, workspace)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var td TokenData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &td, nil
}

// SaveToken writes token data to the configured store (or file fallback).
func SaveToken(configDir string, td *TokenData) error {
	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if secretStore != nil {
		if err := secretStore.Set(slackKeyForWorkspace(td.Workspace), string(data)); err != nil {
			return fmt.Errorf("failed to save token to store: %w", err)
		}

		return nil
	}

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	path := tokenFilePath(configDir, td.Workspace)

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// DeleteToken removes a token from the configured store (or file fallback).
func DeleteToken(configDir, workspace string) error {
	if secretStore != nil {
		return secretStore.Delete(slackKeyForWorkspace(workspace))
	}

	path := tokenFilePath(configDir, workspace)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}

	return nil
}

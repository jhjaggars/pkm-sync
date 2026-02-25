package slack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

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

// LoadToken loads token data from disk.
func LoadToken(configDir, workspace string) (*TokenData, error) {
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

// SaveToken writes token data to disk.
func SaveToken(configDir string, td *TokenData) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	path := tokenFilePath(configDir, td.Workspace)

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// DeleteToken removes a token file from disk.
func DeleteToken(configDir, workspace string) error {
	path := tokenFilePath(configDir, workspace)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}

	return nil
}

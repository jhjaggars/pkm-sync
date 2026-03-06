package keystore

import (
	"fmt"
	"os"
	"path/filepath"
)

// legacyFilenames maps logical key names to their legacy on-disk filenames.
var legacyFilenames = map[string]string{
	"google-oauth-token": "token.json",
}

// slackTokenFilename returns the legacy filename for a Slack workspace token key.
func slackTokenFilename(key string) (string, bool) {
	// Slack keys are "slack-token-<workspace>"
	const prefix = "slack-token-"
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		workspace := key[len(prefix):]

		return fmt.Sprintf("slack-token-%s.json", workspace), true
	}

	return "", false
}

// FileStore stores secrets as plain files in configDir, preserving legacy behavior.
type FileStore struct {
	configDir string
}

func newFileStore(configDir string) *FileStore {
	return &FileStore{configDir: configDir}
}

func (f *FileStore) filePath(key string) string {
	// Check legacy mapping first.
	if name, ok := legacyFilenames[key]; ok {
		return filepath.Join(f.configDir, name)
	}

	// Slack tokens.
	if name, ok := slackTokenFilename(key); ok {
		return filepath.Join(f.configDir, name)
	}

	// Generic fallback.
	return filepath.Join(f.configDir, key+".secret")
}

func (f *FileStore) Get(key string) (string, error) {
	path := f.filePath(key)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}

		return "", fmt.Errorf("keystore file read: %w", err)
	}

	return string(data), nil
}

func (f *FileStore) Set(key, value string) error {
	if err := os.MkdirAll(f.configDir, 0700); err != nil {
		return fmt.Errorf("keystore mkdir: %w", err)
	}

	path := f.filePath(key)

	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		return fmt.Errorf("keystore file write: %w", err)
	}

	return nil
}

func (f *FileStore) Delete(key string) error {
	path := f.filePath(key)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("keystore file delete: %w", err)
	}

	return nil
}

func (f *FileStore) Backend() string { return "file" }

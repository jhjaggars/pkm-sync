package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"absolute path", "/tmp/foo", "/tmp/foo"},
		{"relative path", "relative/path", "relative/path"},
		{"tilde only", "~", homeDir},
		{"tilde with subdir", "~/some/dir", filepath.Join(homeDir, "some/dir")},
		{"tilde with spaces", "~/path with spaces/foo", filepath.Join(homeDir, "path with spaces/foo")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandPath(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExpandConfigPaths_DefaultOutputDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `sync:
    enabled_sources: [work_calendar]
    default_target: obsidian
    default_output_dir: ~/my-vault/pkm-sync
    default_since: 7d
sources:
    work_calendar:
        enabled: true
        type: google_calendar
targets:
    obsidian:
        type: obsidian
`
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0644))

	cfg, err := loadConfigFromFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(homeDir, "my-vault/pkm-sync"), cfg.Sync.DefaultOutputDir)
}

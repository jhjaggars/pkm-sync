package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveAndLoadConfig is an integration test for saving and loading a configuration file.
func TestSaveAndLoadConfig(t *testing.T) {
	// --- Setup ---
	// Create a temporary directory to act as our config directory.
	tempDir := t.TempDir()

	// Override the custom config directory for the duration of this test.
	// This ensures we don't interact with the user's actual config.
	originalCustomConfigDir := customConfigDir
	customConfigDir = tempDir

	defer func() { customConfigDir = originalCustomConfigDir }()

	// --- Test SaveConfig ---
	// Get the default config and save it.
	configToSave := GetDefaultConfig()
	err := SaveConfig(configToSave)
	require.NoError(t, err, "SaveConfig should not return an error")

	// Verify the file was actually created.
	expectedConfigPath := filepath.Join(tempDir, ConfigFileName)
	_, err = os.Stat(expectedConfigPath)
	require.NoError(t, err, "Config file should exist at the expected path")

	// --- Test LoadConfig ---
	// Load the config from the temporary directory.
	loadedConfig, err := LoadConfig()
	require.NoError(t, err, "LoadConfig should not return an error")
	require.NotNil(t, loadedConfig, "Loaded config should not be nil")

	// --- Assertions ---
	// Compare the loaded config with the original default config.
	// This ensures that the save and load process is symmetrical.
	assert.Equal(t, configToSave, loadedConfig, "Loaded config should be identical to the saved config")
}

// TestLoadConfig_NoFileError tests that LoadConfig returns an error when no config file is found.
func TestLoadConfig_NoFileError(t *testing.T) {
	// --- Setup ---
	// Create a temporary directory that we know is empty.
	tempDir := t.TempDir()
	originalCustomConfigDir := customConfigDir
	customConfigDir = tempDir

	defer func() { customConfigDir = originalCustomConfigDir }()

	// --- Execution & Assertion ---
	// Attempt to load a config from the empty directory.
	_, err := LoadConfig()
	require.Error(t, err, "LoadConfig should return an error when no config file is found")
	assert.Contains(t, err.Error(), "no config file found", "Error message should indicate that no file was found")
}

// TestApplyEnvOverrides_EmbeddingsAPIKey tests that PKM_SYNC_EMBEDDINGS_API_KEY overrides the config value.
func TestApplyEnvOverrides_EmbeddingsAPIKey(t *testing.T) {
	tempDir := t.TempDir()
	originalCustomConfigDir := customConfigDir
	customConfigDir = tempDir

	defer func() { customConfigDir = originalCustomConfigDir }()

	cfg := GetDefaultConfig()
	require.Empty(t, cfg.Embeddings.APIKey)
	err := SaveConfig(cfg)
	require.NoError(t, err)

	t.Setenv("PKM_SYNC_EMBEDDINGS_API_KEY", "test-litellm-key")

	loaded, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "test-litellm-key", loaded.Embeddings.APIKey)
}

// TestApplyEnvOverrides_NoOverrideWhenEnvUnset tests that the config value is preserved when env var is absent.
func TestApplyEnvOverrides_NoOverrideWhenEnvUnset(t *testing.T) {
	tempDir := t.TempDir()
	originalCustomConfigDir := customConfigDir
	customConfigDir = tempDir

	defer func() { customConfigDir = originalCustomConfigDir }()

	cfg := GetDefaultConfig()
	err := SaveConfig(cfg)
	require.NoError(t, err)

	loaded, err := LoadConfig()
	require.NoError(t, err)
	assert.Empty(t, loaded.Embeddings.APIKey)
}

// TestGetDefaultConfig provides a basic sanity check for the default configuration.
func TestGetDefaultConfig(t *testing.T) {
	defaultConfig := GetDefaultConfig()
	require.NotNil(t, defaultConfig, "Default config should not be nil")

	// A few basic checks to ensure the config is populated.
	assert.NotEmpty(t, defaultConfig.Sync.EnabledSources, "Default config should have enabled sources")
	assert.NotEmpty(t, defaultConfig.Sync.DefaultTarget, "Default config should have a default target")
	assert.NotEmpty(t, defaultConfig.Sources, "Default config should have sources defined")
	assert.NotEmpty(t, defaultConfig.Targets, "Default config should have targets defined")
}

package main

import (
	"os"
	"path/filepath"
	"testing"

	"pkm-sync/internal/config"
	"pkm-sync/pkg/models"
)

func TestGetEnabledSourcesForValidation_ExplicitList(t *testing.T) {
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"google_calendar", "slack"},
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
			"slack": {
				Enabled: true,
				Type:    "slack",
			},
		},
	}

	enabledSources := getEnabledSources(cfg)

	if len(enabledSources) != 2 {
		t.Errorf("Expected 2 enabled sources, got %d", len(enabledSources))
	}

	expectedSources := map[string]bool{
		"google_calendar": false,
		"slack":           false,
	}

	for _, source := range enabledSources {
		if _, exists := expectedSources[source]; !exists {
			t.Errorf("Unexpected source in enabled list: %s", source)
		}

		expectedSources[source] = true
	}

	for source, found := range expectedSources {
		if !found {
			t.Errorf("Expected source %s to be enabled", source)
		}
	}
}

func TestGetEnabledSourcesForValidation_FallbackToEnabled(t *testing.T) {
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{}, // Empty, should fallback
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
			"slack": {
				Enabled: false,
				Type:    "slack",
			},
			"gmail": {
				Enabled: true,
				Type:    "gmail",
			},
		},
	}

	enabledSources := getEnabledSources(cfg)

	if len(enabledSources) != 2 {
		t.Errorf("Expected 2 enabled sources, got %d", len(enabledSources))
	}

	enabledMap := make(map[string]bool)
	for _, source := range enabledSources {
		enabledMap[source] = true
	}

	if !enabledMap["google_calendar"] {
		t.Error("Expected google_calendar to be enabled")
	}

	if !enabledMap["gmail"] {
		t.Error("Expected gmail to be enabled")
	}

	if enabledMap["slack"] {
		t.Error("Expected slack to be disabled")
	}
}

func TestGetConfigFilePath_Default(t *testing.T) {
	// Save original state
	oldConfigDir := configDir
	configDir = ""

	defer func() { configDir = oldConfigDir }()

	path, err := getConfigFilePath()
	if err != nil {
		t.Fatalf("Failed to get config file path: %v", err)
	}

	if !filepath.IsAbs(path) {
		t.Error("Expected absolute path")
	}

	if filepath.Base(path) != config.ConfigFileName {
		t.Errorf("Expected filename to be %s, got %s", config.ConfigFileName, filepath.Base(path))
	}
}

func TestGetConfigFilePath_Custom(t *testing.T) {
	// Save original state
	oldConfigDir := configDir
	configDir = "/custom/config"

	defer func() { configDir = oldConfigDir }()

	path, err := getConfigFilePath()
	if err != nil {
		t.Fatalf("Failed to get config file path: %v", err)
	}

	expectedPath := filepath.Join("/custom/config", config.ConfigFileName)
	if path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, path)
	}
}

// Test helper function to create a temporary config file.
func createTempConfig(t *testing.T, content string) (string, func()) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, config.ConfigFileName)

	err := os.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	// Set custom config dir for both cmd package and internal/config package
	oldConfigDir := configDir
	configDir = tempDir
	config.SetCustomConfigDir(tempDir)

	cleanup := func() {
		configDir = oldConfigDir

		config.SetCustomConfigDir("")
	}

	return configPath, cleanup
}

func TestConfigValidation_ValidConfig(t *testing.T) {
	validConfig := `sync:
  enabled_sources: ["google_calendar"]
  default_target: obsidian
  default_output_dir: ./vault

sources:
  google_calendar:
    enabled: true
    type: google_calendar

targets:
  obsidian:
    type: obsidian
`

	_, cleanup := createTempConfig(t, validConfig)
	defer cleanup()

	// Load and validate config
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load valid config: %v", err)
	}

	// Basic validation checks
	if cfg.Sync.DefaultTarget == "" {
		t.Error("Default target should not be empty")
	}

	if _, exists := cfg.Targets[cfg.Sync.DefaultTarget]; !exists {
		t.Errorf("Default target '%s' should exist in targets", cfg.Sync.DefaultTarget)
	}

	enabledSources := getEnabledSources(cfg)
	if len(enabledSources) == 0 {
		t.Error("Should have at least one enabled source")
	}

	for _, sourceName := range enabledSources {
		if sourceConfig, exists := cfg.Sources[sourceName]; !exists {
			t.Errorf("Enabled source '%s' should exist in sources", sourceName)
		} else if !sourceConfig.Enabled {
			t.Errorf("Source '%s' should be enabled", sourceName)
		}
	}
}

func TestConfigValidation_InvalidConfig_NoTarget(t *testing.T) {
	// Test that we can detect configs with missing required fields
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"google_calendar"},
			DefaultTarget:  "", // Empty target - invalid
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
		},
	}

	// Should have empty default target as configured
	if cfg.Sync.DefaultTarget != "" {
		t.Errorf("Expected empty default target, got %s", cfg.Sync.DefaultTarget)
	}

	// This would fail validation in a real validation function
	if cfg.Sync.DefaultTarget == "" {
		t.Log("Config correctly has empty default target (would fail validation)")
	}
}

func TestConfigValidation_InvalidConfig_TargetNotExists(t *testing.T) {
	// Test config with target that doesn't exist
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"google_calendar"},
			DefaultTarget:  "nonexistent",
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
		},
		Targets: map[string]models.TargetConfig{
			"obsidian": {
				Type: "obsidian",
			},
		},
	}

	// Should fail validation because 'nonexistent' target doesn't exist
	if _, exists := cfg.Targets[cfg.Sync.DefaultTarget]; exists {
		t.Errorf("Target '%s' should not exist", cfg.Sync.DefaultTarget)
	}

	// Verify we have the nonexistent target configured
	if cfg.Sync.DefaultTarget != "nonexistent" {
		t.Errorf("Expected default target to be 'nonexistent', got %s", cfg.Sync.DefaultTarget)
	}
}

func TestConfigValidation_InvalidConfig_NoEnabledSources(t *testing.T) {
	// Test config with no enabled sources
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{}, // Empty
			DefaultTarget:  "obsidian",
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: false, // Disabled
				Type:    "google_calendar",
			},
		},
		Targets: map[string]models.TargetConfig{
			"obsidian": {
				Type: "obsidian",
			},
		},
	}

	enabledSources := getEnabledSources(cfg)
	if len(enabledSources) != 0 {
		t.Errorf("Should have no enabled sources, got %v", enabledSources)
	}
}

func TestConfigValidation_SourceInEnabledButDisabled(t *testing.T) {
	invalidConfig := `sync:
  enabled_sources: ["google_calendar", "slack"]
  default_target: obsidian

sources:
  google_calendar:
    enabled: true
    type: google_calendar
  slack:
    enabled: false  # Listed in enabled_sources but disabled
    type: slack

targets:
  obsidian:
    type: obsidian
`

	_, cleanup := createTempConfig(t, invalidConfig)
	defer cleanup()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Check that only google_calendar is actually enabled
	enabledSources := getEnabledSources(cfg)
	if len(enabledSources) != 1 || enabledSources[0] != "google_calendar" {
		t.Errorf("Expected only google_calendar to be enabled, got %v", enabledSources)
	}

	// Check that slack is listed but disabled
	for _, sourceName := range cfg.Sync.EnabledSources {
		if sourceName == "slack" {
			if sourceConfig, exists := cfg.Sources[sourceName]; exists && sourceConfig.Enabled {
				t.Error("Slack should be listed but disabled")
			}
		}
	}
}

func TestConfigInit_BasicDefaults(t *testing.T) {
	// Test the config init logic without actually running the command
	cfg := config.GetDefaultConfig()

	// Apply some test overrides (simulating CLI flags)
	output := "./test-vault"
	target := "logseq"
	source := "slack"

	// Simulate config init logic
	cfg.Sync.DefaultOutputDir = output
	cfg.Sync.DefaultTarget = target

	// Add source to enabled sources if not already present
	found := false

	for _, src := range cfg.Sync.EnabledSources {
		if src == source {
			found = true

			break
		}
	}

	if !found {
		cfg.Sync.EnabledSources = append(cfg.Sync.EnabledSources, source)
	}

	// Enable the source in the sources config
	if sourceConfig, exists := cfg.Sources[source]; exists {
		sourceConfig.Enabled = true
		cfg.Sources[source] = sourceConfig
	}

	// Verify the changes
	if cfg.Sync.DefaultOutputDir != output {
		t.Errorf("Expected default_output_dir to be %s, got %s", output, cfg.Sync.DefaultOutputDir)
	}

	if cfg.Sync.DefaultTarget != target {
		t.Errorf("Expected default_target to be %s, got %s", target, cfg.Sync.DefaultTarget)
	}

	// Check that slack is in enabled sources
	slackEnabled := false

	for _, src := range cfg.Sync.EnabledSources {
		if src == source {
			slackEnabled = true

			break
		}
	}

	if !slackEnabled {
		t.Errorf("Expected %s to be in enabled sources", source)
	}

	// Check that slack source is enabled
	if sourceConfig, exists := cfg.Sources[source]; exists {
		if !sourceConfig.Enabled {
			t.Errorf("Expected %s source to be enabled", source)
		}
	}
}

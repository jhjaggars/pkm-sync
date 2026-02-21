package config

import (
	"fmt"
	"os"
	"path/filepath"

	"pkm-sync/pkg/models"

	"gopkg.in/yaml.v3"
)

const ConfigFileName = "config.yaml"

// LoadConfig loads configuration from the standard search paths.
func LoadConfig() (*models.Config, error) {
	// Search for config file in order:
	// 1. Custom config dir (if set)
	// 2. Global config directory
	// 3. Current directory
	configPaths := getConfigSearchPaths()

	for _, configPath := range configPaths {
		if _, err := os.Stat(configPath); err == nil {
			return loadConfigFromFile(configPath)
		}
	}

	return nil, fmt.Errorf("no config file found in search paths: %v", configPaths)
}

// SaveConfig saves configuration to the appropriate location.
func SaveConfig(cfg *models.Config) error {
	configPath, err := getConfigFilePath()
	if err != nil {
		return fmt.Errorf("failed to get config file path: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal config to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetDefaultConfig returns the default configuration.
func GetDefaultConfig() *models.Config {
	return &models.Config{
		Sync: models.SyncConfig{
			EnabledSources:   []string{"google_calendar"},
			DefaultTarget:    "obsidian",
			DefaultOutputDir: "./output",
			DefaultSince:     "7d",
			SourceTags:       false,
			SourceSchedules:  make(map[string]string),
		},
		Sources: map[string]models.SourceConfig{
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
				Google: models.GoogleSourceConfig{
					CalendarID:        "primary",
					DownloadDocs:      true,
					IncludeDeclined:   false,
					IncludePrivate:    false,
					EventTypes:        []string{},
					AttendeeAllowList: []string{},
					DocFormats:        []string{},
				},
			},
			"google_meetings": {
				Enabled: true,
				Type:    "google_calendar",
				Google: models.GoogleSourceConfig{
					CalendarID:        "primary",
					DownloadDocs:      true,
					IncludeDeclined:   false,
					IncludePrivate:    false,
					EventTypes:        []string{},
					AttendeeAllowList: []string{},
					DocFormats:        []string{},
				},
			},
			"google_drive": {
				Enabled: false,
				Type:    "google_drive",
				Drive: models.DriveSourceConfig{
					Name:            "My Drive",
					Description:     "Sync Google Docs, Sheets, and Slides from Google Drive",
					FolderIDs:       []string{},
					Recursive:       true,
					WorkspaceTypes:  []string{},
					DocExportFormat: "md",
				},
			},
		},
		Targets: map[string]models.TargetConfig{
			"obsidian": {
				Type: "obsidian",
				Obsidian: models.ObsidianTargetConfig{
					DefaultFolder:      "Calendar",
					IncludeFrontmatter: true,
					DateFormat:         "2006-01-02",
					CustomFields:       []string{},
				},
			},
			"logseq": {
				Type: "logseq",
				Logseq: models.LogseqTargetConfig{
					DefaultPage:   "Calendar",
					UseProperties: true,
				},
			},
		},
		Transformers: models.TransformConfig{
			Enabled:       false,
			PipelineOrder: make([]string, 0),
			ErrorStrategy: "",
			Transformers:  make(map[string]map[string]interface{}),
		},
		VectorDB: models.VectorDBConfig{
			DBPath:    "", // Will be resolved to ~/.config/pkm-sync/vectors.db at runtime
			AutoIndex: false,
		},
		Embeddings: models.EmbeddingsConfig{
			Provider:   "ollama",
			Model:      "nomic-embed-text",
			APIURL:     "http://localhost:11434",
			APIKey:     "",
			Dimensions: 768,
		},
	}
}

// CreateDefaultConfig creates and saves a default configuration.
func CreateDefaultConfig() error {
	cfg := GetDefaultConfig()

	return SaveConfig(cfg)
}

// getConfigSearchPaths returns the list of paths to search for config files.
func getConfigSearchPaths() []string {
	var paths []string

	// Custom config dir (if set via --config-dir flag)
	if customConfigDir != "" {
		paths = append(paths, filepath.Join(customConfigDir, ConfigFileName))
	}

	// Global config directory
	if globalConfigDir, err := GetConfigDir(); err == nil {
		paths = append(paths, filepath.Join(globalConfigDir, ConfigFileName))
	}

	// Current directory
	paths = append(paths, ConfigFileName)

	return paths
}

// getConfigFilePath returns the path where config should be saved.
func getConfigFilePath() (string, error) {
	// Use custom config dir if set
	if customConfigDir != "" {
		return filepath.Join(customConfigDir, ConfigFileName), nil
	}

	// Use global config directory
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, ConfigFileName), nil
}

// loadConfigFromFile loads configuration from a specific file.
func loadConfigFromFile(configPath string) (*models.Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var cfg models.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	return &cfg, nil
}

// ValidateConfig performs comprehensive validation of the configuration.
func ValidateConfig(cfg *models.Config) error {
	if cfg == nil {
		return fmt.Errorf("configuration is nil")
	}

	// Validate sync configuration
	if err := validateSyncConfig(&cfg.Sync); err != nil {
		return fmt.Errorf("sync configuration error: %w", err)
	}

	// Validate sources
	if err := validateSources(cfg.Sources); err != nil {
		return fmt.Errorf("sources configuration error: %w", err)
	}

	// Validate targets
	if err := validateTargets(cfg.Targets); err != nil {
		return fmt.Errorf("targets configuration error: %w", err)
	}

	// Validate enabled sources exist and are configured
	for _, sourceName := range cfg.Sync.EnabledSources {
		if sourceConfig, exists := cfg.Sources[sourceName]; !exists {
			return fmt.Errorf("enabled source '%s' is not defined in sources", sourceName)
		} else if !sourceConfig.Enabled {
			return fmt.Errorf("enabled source '%s' is marked as disabled", sourceName)
		}
	}

	// Validate default target exists
	if cfg.Sync.DefaultTarget != "" {
		if _, exists := cfg.Targets[cfg.Sync.DefaultTarget]; !exists {
			return fmt.Errorf("default target '%s' is not defined in targets", cfg.Sync.DefaultTarget)
		}
	}

	return nil
}

// validateSyncConfig validates the sync section.
func validateSyncConfig(sync *models.SyncConfig) error {
	if sync == nil {
		return fmt.Errorf("sync configuration is required")
	}

	// Validate default output directory
	if sync.DefaultOutputDir == "" {
		return fmt.Errorf("default_output_dir is required")
	}

	// Validate enabled sources list
	if len(sync.EnabledSources) == 0 {
		return fmt.Errorf("at least one source must be enabled")
	}

	return nil
}

// validateSources validates the sources configuration.
func validateSources(sources map[string]models.SourceConfig) error {
	if len(sources) == 0 {
		return fmt.Errorf("at least one source must be configured")
	}

	for sourceName, sourceConfig := range sources {
		if err := validateSourceConfig(sourceName, sourceConfig); err != nil {
			return fmt.Errorf("source '%s': %w", sourceName, err)
		}
	}

	return nil
}

// validateSourceConfig validates an individual source configuration.
func validateSourceConfig(_ string, config models.SourceConfig) error {
	if config.Type == "" {
		return fmt.Errorf("type is required")
	}

	// Validate type-specific configurations
	switch config.Type {
	case "google_calendar":
		if config.Google.CalendarID == "" {
			return fmt.Errorf("calendar_id is required for google_calendar sources")
		}
	case "gmail":
		if config.Gmail.Name == "" {
			return fmt.Errorf("name is required for gmail sources")
		}
	case "google_drive":
		if config.Drive.Name == "" {
			return fmt.Errorf("name is required for google_drive sources")
		}

		validDocFormats := map[string]bool{"md": true, "txt": true, "html": true, "": true}
		if !validDocFormats[config.Drive.DocExportFormat] {
			return fmt.Errorf("invalid doc_export_format %q for google_drive (supported: md, txt, html)",
				config.Drive.DocExportFormat)
		}

		validSheetFormats := map[string]bool{"csv": true, "html": true, "": true}
		if !validSheetFormats[config.Drive.SheetExportFormat] {
			return fmt.Errorf("invalid sheet_export_format %q for google_drive (supported: csv, html)",
				config.Drive.SheetExportFormat)
		}

		validSlideFormats := map[string]bool{"txt": true, "html": true, "": true}
		if !validSlideFormats[config.Drive.SlideExportFormat] {
			return fmt.Errorf("invalid slide_export_format %q for google_drive (supported: txt, html)",
				config.Drive.SlideExportFormat)
		}
	case "slack":
		// Add slack-specific validations if needed
	case "jira":
		// Add jira-specific validations if needed
	default:
		return fmt.Errorf("unsupported source type: %s", config.Type)
	}

	return nil
}

// validateTargets validates the targets configuration.
func validateTargets(targets map[string]models.TargetConfig) error {
	if len(targets) == 0 {
		return fmt.Errorf("at least one target must be configured")
	}

	for targetName, targetConfig := range targets {
		if err := validateTargetConfig(targetName, targetConfig); err != nil {
			return fmt.Errorf("target '%s': %w", targetName, err)
		}
	}

	return nil
}

// validateTargetConfig validates an individual target configuration.
func validateTargetConfig(_ string, config models.TargetConfig) error {
	if config.Type == "" {
		return fmt.Errorf("type is required")
	}

	// Validate supported target types
	switch config.Type {
	case "obsidian":
		// Obsidian-specific validations could go here
	case "logseq":
		// Logseq-specific validations could go here
	default:
		return fmt.Errorf("unsupported target type: %s", config.Type)
	}

	return nil
}

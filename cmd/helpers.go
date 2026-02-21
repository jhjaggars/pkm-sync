package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"pkm-sync/internal/sources/google"
	syncer "pkm-sync/internal/sync"
	"pkm-sync/internal/targets/logseq"
	"pkm-sync/internal/targets/obsidian"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// sourceResult is a package-level alias for syncer.SourceResult kept for backward compat.
type sourceResult = syncer.SourceResult

// createSource creates a named source with an HTTP client (no source config).
func createSource(name string, client *http.Client) (interfaces.Source, error) {
	switch name {
	case "google_calendar":
		source := google.NewGoogleSource()
		if err := source.Configure(nil, client); err != nil {
			return nil, err
		}

		return source, nil
	default:
		return nil, fmt.Errorf("unknown source '%s': supported sources are 'google_calendar' (others like slack, gmail, jira are planned for future releases)", name)
	}
}

// createSourceWithConfig creates a source from a SourceConfig.
func createSourceWithConfig(sourceID string, sourceConfig models.SourceConfig, client *http.Client) (interfaces.Source, error) {
	switch sourceConfig.Type {
	case "google_calendar":
		source := google.NewGoogleSourceWithConfig(sourceID, sourceConfig)
		if err := source.Configure(nil, client); err != nil {
			return nil, err
		}

		return source, nil
	case "gmail":
		source := google.NewGoogleSourceWithConfig(sourceID, sourceConfig)
		if err := source.Configure(nil, client); err != nil {
			return nil, err
		}

		return source, nil
	case "google_drive":
		source := google.NewGoogleSourceWithConfig(sourceID, sourceConfig)
		if err := source.Configure(nil, client); err != nil {
			return nil, err
		}

		return source, nil
	default:
		return nil, fmt.Errorf("unknown source type '%s': supported types are 'google_calendar', 'gmail', 'google_drive' (others like slack, jira are planned for future releases)", sourceConfig.Type)
	}
}

// createTarget creates a named target without config.
func createTarget(name string) (interfaces.Target, error) {
	switch name {
	case "obsidian":
		target := obsidian.NewObsidianTarget()
		if err := target.Configure(nil); err != nil {
			return nil, err
		}

		return target, nil
	case "logseq":
		target := logseq.NewLogseqTarget()
		if err := target.Configure(nil); err != nil {
			return nil, err
		}

		return target, nil
	default:
		return nil, fmt.Errorf("unknown target '%s': supported targets are 'obsidian' and 'logseq'", name)
	}
}

// createTargetWithConfig creates a target configured from the application config.
func createTargetWithConfig(name string, cfg *models.Config) (interfaces.Target, error) {
	switch name {
	case "obsidian":
		target := obsidian.NewObsidianTarget()

		configMap := make(map[string]interface{})
		if targetConfig, exists := cfg.Targets[name]; exists {
			configMap["template_dir"] = targetConfig.Obsidian.DefaultFolder
			configMap["daily_notes_format"] = targetConfig.Obsidian.DateFormat
		}

		if err := target.Configure(configMap); err != nil {
			return nil, err
		}

		return target, nil

	case "logseq":
		target := logseq.NewLogseqTarget()

		configMap := make(map[string]interface{})
		if targetConfig, exists := cfg.Targets[name]; exists {
			configMap["default_page"] = targetConfig.Logseq.DefaultPage
		}

		if err := target.Configure(configMap); err != nil {
			return nil, err
		}

		return target, nil

	default:
		return nil, fmt.Errorf("unknown target '%s': supported targets are 'obsidian' and 'logseq'", name)
	}
}

// parseSinceTime delegates to the unified date parser.
func parseSinceTime(since string) (time.Time, error) {
	return parseDateTime(since)
}

// getEnabledSources returns enabled source names from config.
func getEnabledSources(cfg *models.Config) []string {
	var enabledSources []string

	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

// getEnabledGmailSources returns enabled Gmail source names from config.
func getEnabledGmailSources(cfg *models.Config) []string {
	var enabledSources []string

	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled && sourceConfig.Type == "gmail" {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled && sourceConfig.Type == "gmail" {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

// getEnabledDriveSources returns enabled Google Drive source names from config.
func getEnabledDriveSources(cfg *models.Config) []string {
	var enabledSources []string

	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled && sourceConfig.Type == "google_drive" {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled && sourceConfig.Type == "google_drive" {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

// getSourceOutputDirectory calculates output directory for a source.
func getSourceOutputDirectory(baseOutputDir string, sourceConfig models.SourceConfig) string {
	if sourceConfig.OutputSubdir != "" {
		return filepath.Join(baseOutputDir, sourceConfig.OutputSubdir)
	}

	return baseOutputDir
}

// DryRunOutput is the complete JSON output structure for dry-run mode.
type DryRunOutput struct {
	Target       string                    `json:"target"`
	OutputDir    string                    `json:"output_dir"`
	Sources      []string                  `json:"sources"`
	TotalItems   int                       `json:"total_items"`
	Summary      DryRunSummary             `json:"summary"`
	Items        []models.FullItem         `json:"items"`
	FilePreviews []*interfaces.FilePreview `json:"file_previews"`
}

// DryRunSummary summarizes dry-run file operations.
type DryRunSummary struct {
	CreateCount   int `json:"create_count"`
	UpdateCount   int `json:"update_count"`
	SkipCount     int `json:"skip_count"`
	ConflictCount int `json:"conflict_count"`
}

func outputDryRunJSON(items []models.FullItem, previews []*interfaces.FilePreview, target, outputDir string, sources []string) error {
	summary := calculateSummary(previews)

	output := DryRunOutput{
		Target:       target,
		OutputDir:    outputDir,
		Sources:      sources,
		TotalItems:   len(items),
		Summary:      summary,
		Items:        items,
		FilePreviews: previews,
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(jsonData))

	return nil
}

func outputDryRunSummary(items []models.FullItem, previews []*interfaces.FilePreview, target, outputDir string, _ []string) error {
	fmt.Printf("=== DRY RUN: Preview of sync operation ===\n")
	fmt.Printf("Target: %s\nOutput directory: %s\nTotal items: %d\n\n", target, outputDir, len(items))

	summary := calculateSummary(previews)

	fmt.Printf("Summary:\n")
	fmt.Printf("  📝 %d files would be created\n", summary.CreateCount)
	fmt.Printf("  ✏️  %d files would be updated\n", summary.UpdateCount)
	fmt.Printf("  ⏭️  %d files would be skipped (no changes)\n", summary.SkipCount)

	if summary.ConflictCount > 0 {
		fmt.Printf("  ⚠️  %d files have potential conflicts\n", summary.ConflictCount)
	}

	fmt.Printf("\n")

	fmt.Printf("Detailed file operations:\n")

	for _, preview := range previews {
		var emoji string

		switch preview.Action {
		case "update":
			emoji = "✏️"
		case "skip":
			emoji = "⏭️"
		default:
			emoji = "📝"
		}

		if preview.Conflict {
			emoji = "⚠️"
		}

		fmt.Printf("  %s %s %s\n", emoji, preview.Action, preview.FilePath)
	}

	fmt.Printf("\nWould you like to see content previews? This will show the first few lines of each file that would be created/updated.\n")
	fmt.Printf("Note: Use --format json to see complete data model including full content\n")

	return nil
}

func calculateSummary(previews []*interfaces.FilePreview) DryRunSummary {
	summary := DryRunSummary{}

	for _, preview := range previews {
		switch preview.Action {
		case "create":
			summary.CreateCount++
		case "update":
			summary.UpdateCount++
		case "skip":
			summary.SkipCount++
		}

		if preview.Conflict {
			summary.ConflictCount++
		}
	}

	return summary
}

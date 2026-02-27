package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sinks"
	"pkm-sync/internal/sources/google"
	slacksource "pkm-sync/internal/sources/slack"
	syncer "pkm-sync/internal/sync"
	"pkm-sync/internal/transform"
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
	case "slack":
		source := slacksource.NewSlackSource(sourceID, sourceConfig)
		if err := source.Configure(nil, nil); err != nil {
			return nil, err
		}

		return source, nil
	default:
		return nil, fmt.Errorf("unknown source type '%s': supported types are 'google_calendar', 'gmail', 'google_drive', 'slack' (others like jira are planned for future releases)", sourceConfig.Type)
	}
}

// createFileSink creates a FileSink for the given formatter name and output directory.
func createFileSink(name string, outputDir string) (*sinks.FileSink, error) {
	return sinks.NewFileSink(name, outputDir, nil)
}

// createFileSinkWithConfig creates a FileSink configured from the application config.
func createFileSinkWithConfig(name string, outputDir string, cfg *models.Config) (*sinks.FileSink, error) {
	fmtConfig := make(map[string]any)

	if targetConfig, exists := cfg.Targets[name]; exists {
		switch name {
		case "obsidian":
			fmtConfig["template_dir"] = targetConfig.Obsidian.DefaultFolder
			fmtConfig["daily_notes_format"] = targetConfig.Obsidian.DateFormat
		case "logseq":
			fmtConfig["default_page"] = targetConfig.Logseq.DefaultPage
		}
	}

	return sinks.NewFileSink(name, outputDir, fmtConfig)
}

// parseSinceTime delegates to the unified date parser.
func parseSinceTime(since string) (time.Time, error) {
	return parseDateTime(since)
}

// maybeCreateArchiveSink creates an ArchiveSink when archive.enabled is true in config.
// Returns nil, nil when archive is disabled or source type is not gmail.
// The caller must call Close() on non-nil results.
func maybeCreateArchiveSink(cfg *models.Config, fetcher sinks.RawMessageFetcher) (*sinks.ArchiveSink, error) {
	if !cfg.Archive.Enabled || fetcher == nil {
		return nil, nil
	}

	emlDir := cfg.Archive.EMLDir
	if emlDir == "" {
		configDir, err := config.GetConfigDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get config directory: %w", err)
		}

		emlDir = filepath.Join(configDir, "archive", "eml")
	}

	dbPath := cfg.Archive.DBPath
	if dbPath == "" {
		configDir, err := config.GetConfigDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get config directory: %w", err)
		}

		dbPath = filepath.Join(configDir, "archive.db")
	}

	return sinks.NewArchiveSink(sinks.ArchiveSinkConfig{
		EMLDir:       emlDir,
		DBPath:       dbPath,
		RequestDelay: cfg.Archive.RequestDelay,
		MaxPerSync:   cfg.Archive.MaxPerSync,
	}, fetcher)
}

// maybeCreateSlackArchiveSink creates a SlackArchiveSink at the given path (or the default path).
// The caller must call Close() on non-nil results.
func maybeCreateSlackArchiveSink(dbPath string) (*sinks.SlackArchiveSink, error) {
	if dbPath == "" {
		configDir, err := config.GetConfigDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get config directory: %w", err)
		}

		dbPath = filepath.Join(configDir, "slack.db")
	}

	return sinks.NewSlackArchiveSink(dbPath)
}

// gmailFetcherFromEntries returns the first RawMessageFetcher found among the source entries.
// Returns nil if no Gmail source with an initialized service is found.
func gmailFetcherFromEntries(entries []syncer.SourceEntry) sinks.RawMessageFetcher {
	for _, entry := range entries {
		gs, ok := entry.Src.(*google.GoogleSource)
		if !ok {
			continue
		}

		if svc := gs.GetGmailService(); svc != nil {
			return svc
		}
	}

	return nil
}

// maybeCreateVectorSink creates a VectorSink when auto_index is enabled in config.
// Returns nil, nil when auto_index is false. The caller must call Close() on non-nil results.
func maybeCreateVectorSink(cfg *models.Config) (*sinks.VectorSink, error) {
	if !cfg.VectorDB.AutoIndex {
		return nil, nil
	}

	dbPath := cfg.VectorDB.DBPath
	if dbPath == "" {
		configDir, err := config.GetConfigDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get config directory: %w", err)
		}

		dbPath = filepath.Join(configDir, "vectors.db")
	}

	return sinks.NewVectorSink(sinks.VectorSinkConfig{
		DBPath:        dbPath,
		EmbeddingsCfg: cfg.Embeddings,
	})
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

// sourceSyncConfig holds all parameters for running a source-type-specific sync.
type sourceSyncConfig struct {
	SourceType   string   // e.g. "gmail", "google_drive"
	Sources      []string // resolved list of source names to sync
	TargetName   string
	OutputDir    string
	Since        string // display/default value
	SinceFlag    string // raw --since CLI flag value (empty = not set by user)
	DefaultLimit int
	DryRun       bool
	OutputFormat string
	SourceKind   string // e.g. "Gmail", "Drive" — used in log messages
	ItemKind     string // e.g. "emails", "documents" — used in success message
	SlackDBPath  string // override for slack archive DB path (empty = default)

	// SharedVectorSink is an optional pre-created VectorSink shared across concurrent
	// runSourceSync calls. When set, runSourceSync uses it instead of creating its own
	// and does NOT close it — the caller owns the lifetime.
	SharedVectorSink *sinks.VectorSink
}

// runSourceSync executes the full sync pipeline for a specific source type.
// It is the shared implementation used by the gmail, drive, slack, and sync commands.
func runSourceSync(cfg *models.Config, ssc sourceSyncConfig) error {
	defaultSinceTime, err := parseSinceTime(ssc.Since)
	if err != nil {
		return fmt.Errorf("invalid since parameter: %w", err)
	}

	fmt.Printf("Syncing %s from sources [%s] to %s (output: %s, since: %s)\n",
		ssc.SourceKind, strings.Join(ssc.Sources, ", "), ssc.TargetName, ssc.OutputDir, ssc.Since)

	entries := make([]syncer.SourceEntry, 0, len(ssc.Sources))

	for _, srcName := range ssc.Sources {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: %s source '%s' not configured, skipping\n", ssc.SourceKind, srcName)

			continue
		}

		if !sourceConfig.Enabled {
			fmt.Printf("%s source '%s' is disabled, skipping\n", ssc.SourceKind, srcName)

			continue
		}

		if sourceConfig.Type != ssc.SourceType {
			fmt.Printf("Warning: source '%s' is not a %s source (type: %s), skipping\n", srcName, ssc.SourceKind, sourceConfig.Type)

			continue
		}

		src, err := createSourceWithConfig(srcName, sourceConfig, nil)
		if err != nil {
			fmt.Printf("Warning: failed to create %s source '%s': %v, skipping\n", ssc.SourceKind, srcName, err)

			continue
		}

		entry := syncer.SourceEntry{Name: srcName, Src: src}

		// Per-source since: config overrides default, but CLI flag takes precedence.
		if sourceConfig.Since != "" && ssc.SinceFlag == "" {
			t, err := parseSinceTime(sourceConfig.Since)
			if err != nil {
				fmt.Printf("Warning: invalid since time for source '%s': %v, using default\n", srcName, err)
			} else {
				entry.Since = t
			}
		}

		// Per-source limit (cap at 2500).
		if sourceConfig.Google.MaxResults > 0 {
			if sourceConfig.Google.MaxResults > 2500 {
				fmt.Printf("Warning: max_results for source '%s' is %d (maximum: 2500), capping\n", srcName, sourceConfig.Google.MaxResults)

				entry.Limit = 2500
			} else {
				entry.Limit = sourceConfig.Google.MaxResults
			}
		}

		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return fmt.Errorf("no valid %s sources could be initialized", ssc.SourceKind)
	}

	// Apply output_subdir: use the common subdir if all sources agree, else warn and use base dir.
	effectiveOutputDir := ssc.OutputDir
	if len(entries) == 1 {
		effectiveOutputDir = getSourceOutputDirectory(ssc.OutputDir, cfg.Sources[entries[0].Name])
	} else {
		first := getSourceOutputDirectory(ssc.OutputDir, cfg.Sources[entries[0].Name])
		allSame := true

		for _, e := range entries[1:] {
			if getSourceOutputDirectory(ssc.OutputDir, cfg.Sources[e.Name]) != first {
				allSame = false

				break
			}
		}

		if allSame {
			effectiveOutputDir = first
		} else {
			fmt.Printf("Warning: sources have different output_subdir settings; using base output dir %s\n", ssc.OutputDir)
		}
	}

	// Slack uses SlackArchiveSink only — no file export to vault.
	var fileSink *sinks.FileSink
	if ssc.SourceType != "slack" {
		fileSink, err = createFileSinkWithConfig(ssc.TargetName, effectiveOutputDir, cfg)
		if err != nil {
			return fmt.Errorf("failed to create sink: %w", err)
		}
	}

	var sinksSlice []interfaces.Sink
	if fileSink != nil {
		sinksSlice = append(sinksSlice, fileSink)
	}

	// Use a shared VectorSink when one is provided (concurrent sync command),
	// otherwise create a dedicated one for single-source commands.
	vectorSink := ssc.SharedVectorSink
	if vectorSink == nil {
		vectorSink, err = maybeCreateVectorSink(cfg)
		if err != nil {
			return fmt.Errorf("failed to create vector sink: %w", err)
		}

		if vectorSink != nil {
			defer vectorSink.Close()
		}
	}

	if vectorSink != nil {
		sinksSlice = append(sinksSlice, vectorSink)
	}

	// Wire ArchiveSink for Gmail sources when archive is enabled.
	if ssc.SourceType == "gmail" && cfg.Archive.Enabled {
		archiveSink, archiveErr := maybeCreateArchiveSink(cfg, gmailFetcherFromEntries(entries))
		if archiveErr != nil {
			return fmt.Errorf("failed to create archive sink: %w", archiveErr)
		}

		if archiveSink != nil {
			defer archiveSink.Close()

			sinksSlice = append(sinksSlice, archiveSink)
		}
	}

	// Wire SlackArchiveSink for Slack sources.
	if ssc.SourceType == "slack" {
		slackArchiveSink, slackErr := maybeCreateSlackArchiveSink(ssc.SlackDBPath)
		if slackErr != nil {
			return fmt.Errorf("failed to create slack archive sink: %w", slackErr)
		}

		defer slackArchiveSink.Close()

		sinksSlice = append(sinksSlice, slackArchiveSink)
	}

	pipeline := transform.NewPipeline()
	for _, t := range transform.GetAllContentProcessingTransformers() {
		if err := pipeline.AddTransformer(t); err != nil {
			return fmt.Errorf("failed to add transformer %s: %w", t.Name(), err)
		}
	}

	s := syncer.NewMultiSyncer(pipeline)

	// Enable source tags when auto-indexing so VectorSink can extract source names for dedup
	sourceTags := cfg.Sync.SourceTags || vectorSink != nil

	syncResult, err := s.SyncAll(
		context.Background(),
		entries,
		sinksSlice,
		syncer.MultiSyncOptions{
			DefaultSince: defaultSinceTime,
			DefaultLimit: ssc.DefaultLimit,
			SourceTags:   sourceTags,
			TransformCfg: cfg.Transformers,
			DryRun:       ssc.DryRun,
		},
	)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	if ssc.DryRun {
		if ssc.SourceType == "slack" {
			dbPath := ssc.SlackDBPath
			if dbPath == "" {
				configDir, _ := config.GetConfigDir()
				dbPath = filepath.Join(configDir, "slack.db")
			}

			printSlackDryRunSummary(syncResult.Items, dbPath)

			return nil
		}

		previews, err := fileSink.Preview(syncResult.Items)
		if err != nil {
			return fmt.Errorf("failed to generate preview: %w", err)
		}

		switch ssc.OutputFormat {
		case "json":
			return outputDryRunJSON(syncResult.Items, previews, ssc.TargetName, ssc.OutputDir, ssc.Sources)
		case "summary":
			return outputDryRunSummary(syncResult.Items, previews, ssc.TargetName, ssc.OutputDir, ssc.Sources)
		default:
			return fmt.Errorf("unknown format '%s': supported formats are 'summary' and 'json'", ssc.OutputFormat)
		}
	}

	fmt.Printf("Successfully exported %d %s\n", len(syncResult.Items), ssc.ItemKind)

	return nil
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

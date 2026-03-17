package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sinks"
	"pkm-sync/internal/sources/google"
	jirasource "pkm-sync/internal/sources/jira"
	slacksource "pkm-sync/internal/sources/slack"
	"pkm-sync/internal/state"
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
	case "jira":
		source := jirasource.NewJiraSource(sourceID, sourceConfig)
		if err := source.Configure(nil, nil); err != nil {
			return nil, err
		}

		return source, nil
	default:
		return nil, fmt.Errorf("unknown source type '%s': supported types are 'google_calendar', 'gmail', 'google_drive', 'slack', 'jira'", sourceConfig.Type)
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

// maybeCreateSlackArchiveSink creates a SlackArchiveSink using the fallback chain:
// explicit dbPath arg (CLI flag) → cfg.Slack.DBPath (config file) → platform default.
// The caller must call Close() on non-nil results.
func maybeCreateSlackArchiveSink(dbPath string, cfg *models.Config) (*sinks.SlackArchiveSink, error) {
	if dbPath == "" && cfg != nil {
		dbPath = cfg.Slack.DBPath
	}

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

// createVectorSink creates the VectorSink that is always active during syncs.
// When no embedding provider is configured the sink runs in metadata-only mode:
// document rows (including timestamps) are still written to vectors.db so that
// inferLastSynced can determine the incremental since window.
// The caller must call Close() on the returned sink.
func createVectorSink(cfg *models.Config) (*sinks.VectorSink, error) {
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

// resolveVectorDBPath returns the configured path to vectors.db (or the default).
func resolveVectorDBPath(cfg *models.Config) (string, error) {
	if cfg.VectorDB.DBPath != "" {
		return cfg.VectorDB.DBPath, nil
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}

	return filepath.Join(configDir, "vectors.db"), nil
}

// inferLastSynced queries vectors.db for the maximum item timestamp recorded
// for sourceName. Returns zero time when no documents exist for the source yet.
func inferLastSynced(dbPath, sourceName string) (time.Time, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return time.Time{}, fmt.Errorf("opening vector db: %w", err)
	}

	defer db.Close()

	var tsStr sql.NullString

	if err := db.QueryRow(
		"SELECT MAX(updated_at) FROM documents WHERE source_name = ?",
		sourceName,
	).Scan(&tsStr); err != nil {
		return time.Time{}, fmt.Errorf("querying max timestamp for %s: %w", sourceName, err)
	}

	if !tsStr.Valid {
		return time.Time{}, nil // no documents yet — caller will use default since
	}

	t, err := time.Parse(time.RFC3339, tsStr.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing timestamp %q: %w", tsStr.String, err)
	}

	return t, nil
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

// getSourceSubItems returns the identifiable sub-item keys for a source that
// represent distinct data scopes (project keys, channel IDs, folder IDs, …).
// Returning a non-empty slice enables sub-item change detection: if the current
// config contains keys absent from the state's KnownSubItems list, those new
// keys trigger a full-window lookback instead of an incremental sync.
// Returns nil for source types where sub-item tracking is not applicable.
func getSourceSubItems(sourceType string, sourceConfig models.SourceConfig) []string {
	var items []string

	switch sourceType {
	case "jira":
		items = append(items, sourceConfig.Jira.ProjectKeys...)

	case "slack":
		items = append(items, sourceConfig.Slack.Channels...)
		items = append(items, sourceConfig.Slack.ChannelGroups...)

	case "gmail":
		items = append(items, sourceConfig.Gmail.Labels...)
		if q := sourceConfig.Gmail.Query; q != "" {
			items = append(items, "query:"+q)
		}

	case "google_calendar":
		if calID := sourceConfig.Google.CalendarID; calID != "" {
			items = append(items, calID)
		} else {
			items = append(items, "primary")
		}

	case "google_drive":
		items = append(items, sourceConfig.Drive.FolderIDs...)
	}

	if len(items) == 0 {
		return nil
	}

	sort.Strings(items)

	return items
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

	// SyncState is an optional pre-loaded sync state shared across concurrent
	// runSourceSync calls (used by the sync command). When non-nil, runSourceSync
	// reads from and writes to this state but does NOT save it — the caller owns
	// the save. When nil, runSourceSync loads and saves its own state.
	SyncState *state.SyncState
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

	// Resolve the vector DB path for incremental since-time inference and for
	// sub-item state tracking.
	configDir, configDirErr := config.GetConfigDir()
	vectorDBPath, vectorDBPathErr := resolveVectorDBPath(cfg)

	// Load sub-item state. When a shared SyncState is provided by the caller
	// (sync command), use it directly; the caller saves it. Otherwise load and
	// save our own copy.
	syncState := ssc.SyncState
	ownedState := false // true = we loaded this state ourselves

	if syncState == nil && configDirErr == nil {
		var loadErr error

		syncState, loadErr = state.Load(configDir)
		if loadErr != nil {
			fmt.Printf("Warning: failed to load sync state: %v; starting fresh\n", loadErr)

			syncState = state.New()
		}

		ownedState = true
	}

	entries := make([]syncer.SourceEntry, 0, len(ssc.Sources))
	// sourceSubItems maps each source name to its current config sub-items
	// (project keys, channel IDs, etc.). Populated during entry building and
	// used after the sync to persist the current set in state.
	sourceSubItems := make(map[string][]string, len(ssc.Sources))

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

		// Record current sub-items for post-sync state update.
		currentSubItems := getSourceSubItems(ssc.SourceType, sourceConfig)
		sourceSubItems[srcName] = currentSubItems

		// Per-source since: config overrides default, but CLI flag takes precedence.
		if sourceConfig.Since != "" && ssc.SinceFlag == "" {
			t, err := parseSinceTime(sourceConfig.Since)
			if err != nil {
				fmt.Printf("Warning: invalid since time for source '%s': %v, using default\n", srcName, err)
			} else {
				entry.Since = t
			}
		}

		// Fall back to data-inferred incremental since when no explicit CLI or
		// config per-source override is set. We query vectors.db for the maximum
		// item timestamp already stored for this source — anchoring the window to
		// the actual data rather than to the wall-clock time of a previous sync.
		if entry.Since.IsZero() && ssc.SinceFlag == "" && vectorDBPathErr == nil {
			if lastSynced, err := inferLastSynced(vectorDBPath, srcName); err != nil {
				fmt.Printf("  → %s: could not infer last sync time: %v; using default window\n", srcName, err)
			} else if !lastSynced.IsZero() {
				entry.Since = lastSynced.Add(-state.SinceOverlap)
				fmt.Printf("  → %s: incremental sync from %s\n", srcName, lastSynced.UTC().Format(time.RFC3339))
			}
		}

		// If we set an incremental since time, check whether new sub-items have
		// been added to the source config since the last sync. New sub-items
		// (e.g. a newly added Jira project or Slack channel) have no history in
		// the incremental window, so we reset to zero and let the source fetch
		// from the full default lookback window instead.
		if !entry.Since.IsZero() && syncState != nil {
			if newItems := syncState.NewSubItems(srcName, currentSubItems); len(newItems) > 0 {
				entry.Since = time.Time{} // zero → use defaultSinceTime

				fmt.Printf("  → %s: new sub-items %v detected, using full lookback window\n", srcName, newItems)
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

	// Slack and Gmail use archive sinks only — no file export to vault.
	var fileSink *sinks.FileSink
	if ssc.SourceType != "slack" && ssc.SourceType != "gmail" {
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
		vectorSink, err = createVectorSink(cfg)
		if err != nil {
			return fmt.Errorf("failed to create vector sink: %w", err)
		}

		defer vectorSink.Close()
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
		slackArchiveSink, slackErr := maybeCreateSlackArchiveSink(ssc.SlackDBPath, cfg)
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
		return handleDryRun(ssc, fileSink, syncResult.Items, cfg)
	}

	// Update sub-item membership in state for each successfully synced source.
	// Timestamps are NOT stored here — they are inferred at the next sync by
	// querying vectors.db (MAX(updated_at) per source_name), which is always
	// written by the VectorSink.
	if syncState == nil {
		fmt.Printf("Successfully exported %d %s\n", len(syncResult.Items), ssc.ItemKind)

		return nil
	}

	for _, r := range syncResult.SourceResults {
		if r.Err != nil {
			continue
		}

		if subItems, ok := sourceSubItems[r.Name]; ok {
			syncState.UpdateSubItems(r.Name, subItems)
		}
	}

	// Save only when we own the state (individual command path).
	// The sync command saves its shared state after all groups complete.
	if ownedState {
		if saveErr := syncState.Save(configDir); saveErr != nil {
			fmt.Printf("Warning: failed to save sync state: %v\n", saveErr)
		}
	}

	fmt.Printf("Successfully exported %d %s\n", len(syncResult.Items), ssc.ItemKind)

	return nil
}

// handleDryRun prints a dry-run summary appropriate for the source type.
func handleDryRun(ssc sourceSyncConfig, fileSink *sinks.FileSink, items []models.FullItem, cfg *models.Config) error {
	if ssc.SourceType == "slack" {
		dbPath := ssc.SlackDBPath
		if dbPath == "" && cfg != nil {
			dbPath = cfg.Slack.DBPath
		}

		if dbPath == "" {
			configDir, _ := config.GetConfigDir()
			dbPath = filepath.Join(configDir, "slack.db")
		}

		printSlackDryRunSummary(items, dbPath)

		return nil
	}

	if ssc.SourceType == "gmail" {
		configDir, _ := config.GetConfigDir()
		dbPath := filepath.Join(configDir, "archive.db")
		fmt.Printf("Would archive %d emails to %s\n", len(items), dbPath)

		return nil
	}

	previews, err := fileSink.Preview(items)
	if err != nil {
		return fmt.Errorf("failed to generate preview: %w", err)
	}

	switch ssc.OutputFormat {
	case "json":
		return outputDryRunJSON(items, previews, ssc.TargetName, ssc.OutputDir, ssc.Sources)
	case "summary":
		return outputDryRunSummary(items, previews, ssc.TargetName, ssc.OutputDir, ssc.Sources)
	default:
		return fmt.Errorf("unknown format '%s': supported formats are 'summary' and 'json'", ssc.OutputFormat)
	}
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

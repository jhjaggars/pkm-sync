package main

import (
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"pkm-sync/internal/config"
	"pkm-sync/internal/state"
	"pkm-sync/pkg/models"
	"pkm-sync/pkg/routing"

	"github.com/spf13/cobra"
)

var (
	syncSourceName   string
	syncTargetName   string
	syncOutputDir    string
	syncSince        string
	syncDryRun       bool
	syncLimit        int
	syncOutputFormat string
)

var syncCmd = &cobra.Command{
	Use:   "sync [source]",
	Short: "Sync all enabled sources to PKM systems",
	Long: `Sync all enabled sources (Gmail, Google Calendar, Drive, Slack, Jira) to PKM targets in a single operation.

An optional positional argument can filter to a specific source type or source
name. Source type aliases like "gmail", "drive", "jira", "slack" are accepted:

  pkm-sync sync gmail           # all enabled Gmail sources
  pkm-sync sync gmail_work      # specific source by name
  pkm-sync sync drive           # all enabled Drive sources

The --source flag is also accepted for backward compatibility.

Examples:
  pkm-sync sync
  pkm-sync sync gmail
  pkm-sync sync gmail_work
  pkm-sync sync --source gmail_work
  pkm-sync sync --target obsidian --output ./vault
  pkm-sync sync --since 7d --dry-run
  pkm-sync sync gmail --dry-run --format json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSyncCommand,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().StringVar(&syncSourceName, "source", "", "Filter to a specific source by name")
	syncCmd.Flags().StringVar(&syncTargetName, "target", "", "PKM target (obsidian, logseq)")
	syncCmd.Flags().StringVarP(&syncOutputDir, "output", "o", "", "Output directory")
	syncCmd.Flags().StringVar(&syncSince, "since", "", "Sync items since (7d, 2006-01-02, today)")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "Show what would be synced without making changes")
	syncCmd.Flags().IntVar(&syncLimit, "limit", 1000, "Maximum number of items per source")
	syncCmd.Flags().StringVar(&syncOutputFormat, "format", "summary", "Output format for dry-run (summary, json)")
}

func runSyncCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.GetDefaultConfig()
	}

	// The optional positional arg can be a source name ("gmail_work") or a source
	// type alias ("gmail", "drive"). Resolve into a local to avoid mutating the
	// flag-backed global (which persists across in-process invocations).
	resolvedSource := syncSourceName
	if len(args) == 1 && resolvedSource == "" {
		resolvedSource = resolveSyncPositionalArg(cfg, args[0])
	}

	// Determine which sources to sync.
	// resolvedSource may be a source name ("gmail_work") or a canonical type
	// ("gmail", "google_drive") when set via the positional arg.
	var sourcesToSync []string

	switch {
	case resolvedSource == "":
		sourcesToSync = getEnabledSources(cfg)
	case isSourceType(resolvedSource):
		// Filter all enabled sources that match this canonical type.
		for _, name := range getEnabledSources(cfg) {
			if sc, ok := cfg.Sources[name]; ok && sc.Type == resolvedSource {
				sourcesToSync = append(sourcesToSync, name)
			}
		}

		if len(sourcesToSync) == 0 {
			return fmt.Errorf("no enabled sources of type %q found", resolvedSource)
		}
	default:
		// Treat as a specific source name.
		sourcesToSync = []string{resolvedSource}
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no enabled sources found. Configure sources in your config file or use --source flag")
	}

	// Resolve target, output, since from CLI flags with config fallbacks
	finalTargetName := cfg.Sync.DefaultTarget
	if syncTargetName != "" {
		finalTargetName = syncTargetName
	}

	finalOutputDir := cfg.Sync.DefaultOutputDir
	if syncOutputDir != "" {
		finalOutputDir = syncOutputDir
	}

	finalSince := cfg.Sync.DefaultSince
	if syncSince != "" {
		finalSince = syncSince
	}

	// Group enabled sources by type for dispatch to runSourceSync.
	typeGroups := map[string][]string{}

	for _, srcName := range sourcesToSync {
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: source '%s' not configured, skipping\n", srcName)

			continue
		}

		switch sourceConfig.Type {
		case "gmail", "google_calendar", "google_drive", "slack", "jira", "servicenow":
			typeGroups[sourceConfig.Type] = append(typeGroups[sourceConfig.Type], srcName)
		default:
			fmt.Printf("Warning: source '%s' has unsupported type '%s', skipping\n", srcName, sourceConfig.Type)
		}
	}

	if len(typeGroups) == 0 {
		return fmt.Errorf("no valid sources could be initialized")
	}

	type typeGroupCfg struct {
		sourceType string
		sourceKind string
		itemKind   string
	}

	allGroups := []typeGroupCfg{
		{"gmail", "Gmail", "emails"},
		{"google_calendar", "Calendar", "events"},
		{"google_drive", "Drive", "documents"},
		{"slack", "Slack", "messages"},
		{"jira", "Jira", "issues"},
		{"servicenow", "ServiceNow", "tickets"},
	}

	// Filter to groups that have at least one configured source.
	type activeGroup struct {
		typeGroupCfg

		sources []string
	}

	active := make([]activeGroup, 0, len(allGroups))

	for _, grp := range allGroups {
		sources, ok := typeGroups[grp.sourceType]
		if !ok || len(sources) == 0 {
			continue
		}

		active = append(active, activeGroup{grp, sources})
	}

	// Create a single shared VectorSink for all concurrent type-group goroutines.
	// The VectorSink is always active: it writes document metadata (timestamps,
	// source name) unconditionally, enabling data-inferred incremental syncs,
	// and additionally stores embeddings when a provider is configured.
	sharedVectorSink, vsErr := createVectorSink(cfg)
	if vsErr != nil {
		return fmt.Errorf("failed to create vector sink: %w", vsErr)
	}

	defer sharedVectorSink.Close()

	// Load a single shared SyncState so all concurrent goroutines update the
	// same in-memory object (its mutex keeps it safe). We save once after all
	// groups finish to avoid concurrent writes to the same file.
	var sharedSyncState *state.SyncState

	stateConfigDir, stateConfigDirErr := config.GetConfigDir()
	if stateConfigDirErr == nil {
		if syncSince == "" {
			// Only read persisted state when --since was not explicitly set.
			var loadErr error

			sharedSyncState, loadErr = state.Load(stateConfigDir)
			if loadErr != nil {
				fmt.Printf("Warning: failed to load sync state: %v; using default since window\n", loadErr)
			}
		}

		if sharedSyncState == nil {
			sharedSyncState = state.New()
		}
	}

	// Run each type group concurrently. Goroutines always return nil so that
	// one failing group does not cancel the others.
	groupErrs := make([]error, len(active))
	eg := new(errgroup.Group)

	for i, ag := range active {
		eg.Go(func() error {
			if err := runSourceSync(cfg, sourceSyncConfig{
				SourceType:       ag.sourceType,
				Sources:          ag.sources,
				TargetName:       finalTargetName,
				OutputDir:        finalOutputDir,
				Since:            finalSince,
				SinceFlag:        syncSince,
				DefaultLimit:     syncLimit,
				DryRun:           syncDryRun,
				OutputFormat:     syncOutputFormat,
				SourceKind:       ag.sourceKind,
				ItemKind:         ag.itemKind,
				SharedVectorSink: sharedVectorSink,
				SyncState:        sharedSyncState,
			}); err != nil {
				fmt.Printf("Warning: %s sync failed: %v\n", ag.sourceKind, err)
				groupErrs[i] = err
			}

			return nil
		})
	}

	eg.Wait() //nolint:errcheck // goroutines always return nil

	// Save the shared sync state after all groups have finished updating it.
	if !syncDryRun && sharedSyncState != nil && stateConfigDirErr == nil {
		if saveErr := sharedSyncState.Save(stateConfigDir); saveErr != nil {
			fmt.Printf("Warning: failed to save sync state: %v\n", saveErr)
		}
	}

	var failedGroups []string

	for i, ag := range active {
		if groupErrs[i] != nil {
			failedGroups = append(failedGroups, ag.sourceKind)
		}
	}

	if len(failedGroups) > 0 {
		return fmt.Errorf("sync failed for: %s", strings.Join(failedGroups, ", "))
	}

	return nil
}

// knownSourceTypes is the set of canonical config type strings.
var knownSourceTypes = map[string]bool{
	"gmail": true, "google_calendar": true, "google_drive": true,
	"slack": true, "jira": true, "servicenow": true,
}

// isSourceType returns true if s is a canonical source type string (not a name).
func isSourceType(s string) bool {
	return knownSourceTypes[s]
}

// resolveSyncPositionalArg maps a positional arg to a source name or type.
// If arg matches a configured source name, it is returned as-is.
// If arg matches a type alias (e.g. "gmail", "drive"), the canonical type is returned.
// If neither matches, the arg is returned unchanged (will fail later with a clear error).
func resolveSyncPositionalArg(cfg *models.Config, arg string) string {
	// Exact source name match takes priority.
	if _, ok := cfg.Sources[arg]; ok {
		return arg
	}

	// Type alias → canonical type.
	canonical := routing.CanonicalSourceType(arg)
	if isSourceType(canonical) {
		return canonical
	}

	// Return unchanged; the caller will produce a helpful error.
	return arg
}

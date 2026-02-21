package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sources/google"
	"pkm-sync/internal/targets/logseq"
	"pkm-sync/internal/targets/obsidian"
	"pkm-sync/internal/transform"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var (
	gmailSourceName   string
	gmailTargetName   string
	gmailOutputDir    string
	gmailSince        string
	gmailDryRun       bool
	gmailLimit        int
	gmailOutputFormat string
)

var gmailCmd = &cobra.Command{
	Use:   "gmail",
	Short: "Sync Gmail emails to PKM systems",
	Long: `Sync Gmail emails to PKM targets (obsidian, logseq, etc.)
    
Examples:
  pkm-sync gmail --source gmail_work --target obsidian --output ./vault
  pkm-sync gmail --source gmail_personal --target logseq --output ./graph --since 7d
  pkm-sync gmail --source gmail_work --target obsidian --dry-run`,
	RunE: runGmailCommand,
}

func init() {
	rootCmd.AddCommand(gmailCmd)
	gmailCmd.Flags().StringVar(&gmailSourceName, "source", "", "Gmail source (gmail_work, gmail_personal, etc.)")
	gmailCmd.Flags().StringVar(&gmailTargetName, "target", "", "PKM target (obsidian, logseq)")
	gmailCmd.Flags().StringVarP(&gmailOutputDir, "output", "o", "", "Output directory")
	gmailCmd.Flags().StringVar(&gmailSince, "since", "", "Sync emails since (7d, 2006-01-02, today)")
	gmailCmd.Flags().BoolVar(&gmailDryRun, "dry-run", false, "Show what would be synced without making changes")
	gmailCmd.Flags().IntVar(&gmailLimit, "limit", 1000, "Maximum number of emails to fetch (default: 1000)")
	gmailCmd.Flags().StringVar(&gmailOutputFormat, "format", "summary", "Output format for dry-run (summary, json)")
}

func runGmailCommand(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		// If no config exists, use defaults
		cfg = config.GetDefaultConfig()
	}

	// Determine which Gmail sources to sync
	var sourcesToSync []string
	if gmailSourceName != "" {
		// CLI override: sync specific Gmail source
		sourcesToSync = []string{gmailSourceName}
	} else {
		// Use enabled Gmail sources from config
		sourcesToSync = getEnabledGmailSources(cfg)
	}

	if len(sourcesToSync) == 0 {
		return fmt.Errorf("no Gmail sources configured. Please configure Gmail sources in your config file or use --source flag")
	}

	// Apply config defaults, then CLI overrides
	finalTargetName := cfg.Sync.DefaultTarget
	if gmailTargetName != "" {
		finalTargetName = gmailTargetName
	}

	finalOutputDir := cfg.Sync.DefaultOutputDir
	if gmailOutputDir != "" {
		finalOutputDir = gmailOutputDir
	}

	finalSince := cfg.Sync.DefaultSince
	if gmailSince != "" {
		finalSince = gmailSince
	}

	// Parse since parameter
	sinceTime, err := parseSinceTime(finalSince)
	if err != nil {
		return fmt.Errorf("invalid since parameter: %w", err)
	}

	fmt.Printf("Syncing Gmail from sources [%s] to %s (output: %s, since: %s)\n",
		strings.Join(sourcesToSync, ", "), finalTargetName, finalOutputDir, finalSince)

	// Create target with config
	target, err := createTargetWithConfig(finalTargetName, cfg)
	if err != nil {
		return fmt.Errorf("failed to create target: %w", err)
	}

	// Collect all items from all Gmail sources for unified processing
	var allItems []models.ItemInterface

	// Process each Gmail source independently to support per-source customization
	for _, srcName := range sourcesToSync {
		// Get source-specific config
		sourceConfig, exists := cfg.Sources[srcName]
		if !exists {
			fmt.Printf("Warning: Gmail source '%s' not configured, skipping\n", srcName)

			continue
		}

		if !sourceConfig.Enabled {
			fmt.Printf("Gmail source '%s' is disabled, skipping\n", srcName)

			continue
		}

		// Verify this is a Gmail source
		if sourceConfig.Type != "gmail" {
			fmt.Printf("Warning: source '%s' is not a Gmail source (type: %s), skipping\n", srcName, sourceConfig.Type)

			continue
		}

		// Create source with config
		source, err := createSourceWithConfig(srcName, sourceConfig, nil)
		if err != nil {
			fmt.Printf("Warning: failed to create Gmail source '%s': %v, skipping\n", srcName, err)

			continue
		}

		// Use source-specific since time if configured, but CLI flag takes precedence
		sourceSince := finalSince
		if sourceConfig.Since != "" && gmailSince == "" {
			// Only use config since if no CLI override was provided
			sourceSince = sourceConfig.Since
		}

		sourceSinceTime, err := parseSinceTime(sourceSince)
		if err != nil {
			fmt.Printf("Warning: invalid since time for Gmail source '%s': %v, using default\n", srcName, err)

			sourceSinceTime = sinceTime
		}

		// Use source-specific max results if configured, otherwise default to 1000
		maxResults := 1000 // default value

		if sourceConfig.Google.MaxResults > 0 {
			// Validate max results range
			if sourceConfig.Google.MaxResults > 2500 {
				fmt.Printf("Warning: max_results for Gmail source '%s' is %d (maximum allowed: 2500), using 2500\n", srcName, sourceConfig.Google.MaxResults)

				maxResults = 2500
			} else {
				maxResults = sourceConfig.Google.MaxResults
			}
		}

		// Fetch items from this Gmail source
		fmt.Printf("Fetching emails from %s...\n", srcName)

		items, err := source.Fetch(sourceSinceTime, maxResults)
		if err != nil {
			fmt.Printf("Warning: failed to fetch from Gmail source '%s': %v, skipping\n", srcName, err)

			continue
		}

		// Add source tags if enabled
		if cfg.Sync.SourceTags {
			for _, item := range items {
				currentTags := item.GetTags()
				newTags := append(currentTags, "source:"+srcName)
				item.SetTags(newTags)
			}
		}

		fmt.Printf("Found %d emails from %s\n", len(items), srcName)

		// Add items to the collection
		allItems = append(allItems, items...)
	}

	fmt.Printf("Total emails collected: %d\n", len(allItems))

	// Initialize and apply transformer pipeline if configured
	if cfg.Transformers.Enabled {
		pipeline := transform.NewPipeline()

		// Register all available transformers
		for _, t := range transform.GetAllExampleTransformers() {
			if err := pipeline.AddTransformer(t); err != nil {
				return fmt.Errorf("failed to add transformer %s: %w", t.Name(), err)
			}
		}

		// Configure the pipeline from the config file
		if err := pipeline.Configure(cfg.Transformers); err != nil {
			return fmt.Errorf("failed to configure transformer pipeline: %w", err)
		}

		// Transform items
		transformedItems, err := pipeline.Transform(allItems)
		if err != nil {
			return fmt.Errorf("failed to transform items: %w", err)
		}

		fmt.Printf("Transformed to %d items\n", len(transformedItems))
		allItems = transformedItems
	}

	if gmailDryRun {
		// Generate preview of what would be done
		previews, err := target.Preview(allItems, finalOutputDir)
		if err != nil {
			return fmt.Errorf("failed to generate preview: %w", err)
		}

		switch gmailOutputFormat {
		case "json":
			return outputDryRunJSON(allItems, previews, finalTargetName, finalOutputDir, sourcesToSync)
		case "summary":
			return outputDryRunSummary(allItems, previews, finalTargetName, finalOutputDir, sourcesToSync)
		default:
			return fmt.Errorf("unknown format '%s': supported formats are 'summary' and 'json'", gmailOutputFormat)
		}
	}

	// Export all items to target
	if err := target.Export(allItems, finalOutputDir); err != nil {
		return fmt.Errorf("failed to export to target: %w", err)
	}

	fmt.Printf("Successfully exported %d emails\n", len(allItems))

	return nil
}

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
	default:
		return nil, fmt.Errorf("unknown source type '%s': supported types are 'google_calendar', 'gmail' (others like slack, jira are planned for future releases)", sourceConfig.Type)
	}
}

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

func createTargetWithConfig(name string, cfg *models.Config) (interfaces.Target, error) {
	switch name {
	case "obsidian":
		target := obsidian.NewObsidianTarget()

		// Apply configuration
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

		// Apply configuration
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

func parseSinceTime(since string) (time.Time, error) {
	// Delegate to the unified date parser
	return parseDateTime(since)
}

// getEnabledSources returns list of sources that are enabled in the configuration.
func getEnabledSources(cfg *models.Config) []string {
	var enabledSources []string

	// Use explicit enabled_sources list if provided
	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	// Fallback: find all enabled sources in config
	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

// getEnabledGmailSources returns list of Gmail sources that are enabled in the configuration.
func getEnabledGmailSources(cfg *models.Config) []string {
	var enabledSources []string

	// Use explicit enabled_sources list if provided
	if len(cfg.Sync.EnabledSources) > 0 {
		for _, srcName := range cfg.Sync.EnabledSources {
			if sourceConfig, exists := cfg.Sources[srcName]; exists && sourceConfig.Enabled && sourceConfig.Type == "gmail" {
				enabledSources = append(enabledSources, srcName)
			}
		}

		return enabledSources
	}

	// Fallback: find all enabled Gmail sources in config
	for srcName, sourceConfig := range cfg.Sources {
		if sourceConfig.Enabled && sourceConfig.Type == "gmail" {
			enabledSources = append(enabledSources, srcName)
		}
	}

	return enabledSources
}

// getSourceOutputDirectory calculates the output directory for a source based on configuration.
func getSourceOutputDirectory(baseOutputDir string, sourceConfig models.SourceConfig) string {
	if sourceConfig.OutputSubdir != "" {
		return filepath.Join(baseOutputDir, sourceConfig.OutputSubdir)
	}

	return baseOutputDir
}

// DryRunOutput represents the complete output structure for JSON format.
type DryRunOutput struct {
	Target       string                    `json:"target"`
	OutputDir    string                    `json:"output_dir"`
	Sources      []string                  `json:"sources"`
	TotalItems   int                       `json:"total_items"`
	Summary      DryRunSummary             `json:"summary"`
	Items        []models.ItemInterface    `json:"items"`
	FilePreviews []*interfaces.FilePreview `json:"file_previews"`
}

type DryRunSummary struct {
	CreateCount   int `json:"create_count"`
	UpdateCount   int `json:"update_count"`
	SkipCount     int `json:"skip_count"`
	ConflictCount int `json:"conflict_count"`
}

func outputDryRunJSON(items []models.ItemInterface, previews []*interfaces.FilePreview, target, outputDir string, sources []string) error {
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

func outputDryRunSummary(items []models.ItemInterface, previews []*interfaces.FilePreview, target, outputDir string, _ []string) error {
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

	// Show detailed file operations
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

	// Ask if user wants to see file content previews
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pkm-sync/internal/archive"
	"pkm-sync/internal/config"
	"pkm-sync/internal/vectorstore"
	"pkm-sync/pkg/routing"

	"github.com/spf13/cobra"
)

var (
	searchLimit      int
	searchSourceType string
	searchSourceName string
	searchFormat     string
	searchMinScore   float64
)

var searchCmd = &cobra.Command{
	Use:   "search [type[/source]] <query>",
	Short: "Search indexed items using semantic search or Gmail full-text search",
	Long: `Search indexed items using semantic (vector) search or Gmail full-text search.

An optional first argument scopes the search to a source type or a specific
source instance. The query is always the last argument.

  Bare query — vector (semantic) search across all sources:
    pkm-sync search "kubernetes deployment issues"

  Source type — routes to the appropriate backend:
    pkm-sync search gmail "meeting with alice"      # Gmail FTS (archive.db)
    pkm-sync search slack "deploy failed"           # vector search, slack filter
    pkm-sync search jira "authentication error"     # vector search, jira filter

  Type/source — narrows to a specific configured source:
    pkm-sync search gmail/work_gmail "rosa boundary"
    pkm-sync search jira/jira_work "auth error"

Examples:
  pkm-sync search "kubernetes deployment issues"
  pkm-sync search gmail "rosa boundary"
  pkm-sync search gmail/work_gmail "rosa boundary"
  pkm-sync search slack "deploy failed" --limit 5
  pkm-sync search "project status" --format json`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSearchCommand,
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().IntVar(&searchLimit, "limit", 10, "Maximum number of results to return")
	searchCmd.Flags().StringVar(&searchSourceType, "source-type", "", "Filter by source type (gmail)")
	searchCmd.Flags().StringVar(&searchSourceName, "source-name", "", "Filter by source name (gmail_work, etc.)")
	searchCmd.Flags().StringVar(&searchFormat, "format", "text", "Output format (text, json)")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0.0, "Minimum similarity score (0.0-1.0)")
}

func runSearchCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Two-arg form: search <type[/source]> <query>
	// One-arg form: search <query>
	var query string

	sourceTypeFilter := searchSourceType
	sourceName := searchSourceName

	if len(args) == 2 {
		query = args[1]
		// Parse the source specifier: "gmail" or "gmail/work_gmail"
		specifier := args[0]
		if idx := strings.Index(specifier, "/"); idx >= 0 {
			sourceName = specifier[idx+1:]
			specifier = specifier[:idx]
		}

		if sourceTypeFilter == "" {
			sourceTypeFilter = routing.CanonicalSourceType(specifier)
		}
	} else {
		query = args[0]
	}

	// Route gmail queries to the FTS archive when available.
	if sourceTypeFilter == "gmail" {
		if handled, err := runGmailFTSSearch(query, sourceName); handled {
			return err
		}
		// Fall through to vector search if archive.db is not available.
	}

	// Default path: vector (semantic) search.
	return runVectorSearch(ctx, query, sourceTypeFilter, sourceName)
}

// runGmailFTSSearch performs full-text search against archive.db.
// sourceName optionally filters to a specific source (e.g. "work_gmail").
// Returns (true, err) when the archive was found and queried (even on query error).
// Returns (false, nil) when archive.db doesn't exist, so the caller can fall through.
func runGmailFTSSearch(query, sourceName string) (bool, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return false, nil
	}

	dbPath := filepath.Join(configDir, "archive.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return false, nil // archive not set up, fall through to vector search
	}

	store, err := archive.NewStore(dbPath)
	if err != nil {
		return true, fmt.Errorf("failed to open archive: %w", err)
	}
	defer store.Close()

	results, err := store.Search(query, searchLimit)
	if err != nil {
		return true, fmt.Errorf("archive search failed: %w", err)
	}

	// Filter by source name when specified.
	if sourceName != "" {
		filtered := results[:0]
		for _, r := range results {
			if r.SourceName == sourceName {
				filtered = append(filtered, r)
			}
		}

		results = filtered
	}

	return true, outputArchiveResults(query, results)
}

// runVectorSearch performs semantic (KNN) search against vectors.db.
func runVectorSearch(ctx context.Context, query, sourceTypeFilter, sourceName string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	vectorSink, err := createVectorSink(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vector sink: %w", err)
	}
	defer vectorSink.Close()

	filters := vectorstore.SearchFilters{
		SourceType: sourceTypeFilter,
		SourceName: sourceName,
		MinScore:   searchMinScore,
	}

	results, err := vectorSink.Search(ctx, query, searchLimit, filters)
	if err != nil {
		return fmt.Errorf("failed to search: %w", err)
	}

	switch searchFormat {
	case "json":
		return outputJSON(query, results)
	case "text":
		return outputText(query, results)
	default:
		return fmt.Errorf("unsupported format: %s (supported: text, json)", searchFormat)
	}
}

// outputArchiveResults prints Gmail FTS results.
func outputArchiveResults(query string, results []archive.FTSResult) error {
	if len(results) == 0 {
		fmt.Printf("No Gmail archive results for %q\n", query)

		return nil
	}

	fmt.Printf("Found %d Gmail archive result(s) for %q:\n\n", len(results), query)

	for i, r := range results {
		fmt.Printf("%d. %s\n", i+1, r.Subject)
		fmt.Printf("   From: %s | Source: %s | Date: %s\n",
			r.FromAddr, r.SourceName, r.DateSent.Format("2006-01-02"))
		fmt.Println()
	}

	return nil
}

// outputText outputs search results in human-readable text format.
func outputText(query string, results []vectorstore.SearchResult) error {
	if len(results) == 0 {
		fmt.Printf("No results found for \"%s\"\n", query)

		return nil
	}

	fmt.Printf("Found %d thread(s) for \"%s\":\n\n", len(results), query)

	for i, result := range results {
		fmt.Printf("%d. [%.2f] %s (%d message%s)\n",
			i+1,
			result.Score,
			result.Title,
			result.MessageCount,
			pluralize(result.MessageCount))

		fmt.Printf("   Source: %s | %s - %s\n",
			result.SourceName,
			result.CreatedAt.Format("2006-01-02"),
			result.UpdatedAt.Format("2006-01-02"))

		// Extract participants from metadata
		if participants, ok := result.Metadata["participants"].([]interface{}); ok && len(participants) > 0 {
			participantStrs := make([]string, 0, len(participants))
			for _, p := range participants {
				if pStr, ok := p.(string); ok {
					participantStrs = append(participantStrs, pStr)
				}
			}

			if len(participantStrs) > 0 {
				fmt.Printf("   Participants: %s\n", strings.Join(participantStrs[:minInt(3, len(participantStrs))], ", "))
			}
		}

		// Show snippet of latest message
		lines := strings.Split(result.Content, "\n")
		contentPreview := ""

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "Thread:") && !strings.HasPrefix(line, "From:") && !strings.HasPrefix(line, "To:") && !strings.HasPrefix(line, "Cc:") && !strings.HasPrefix(line, "Bcc:") {
				contentPreview = line
				if len(contentPreview) > 100 {
					contentPreview = contentPreview[:100] + "..."
				}

				break
			}
		}

		if contentPreview != "" {
			fmt.Printf("   Preview: %s\n", contentPreview)
		}

		fmt.Println()
	}

	return nil
}

// outputJSON outputs search results in JSON format.
func outputJSON(query string, results []vectorstore.SearchResult) error {
	// Build JSON output
	output := map[string]interface{}{
		"query":         query,
		"total_results": len(results),
		"results":       make([]map[string]interface{}, len(results)),
	}

	for i, result := range results {
		output["results"].([]map[string]interface{})[i] = map[string]interface{}{
			"score":         result.Score,
			"thread_id":     result.ThreadID,
			"title":         result.Title,
			"content":       result.Content,
			"source_type":   result.SourceType,
			"source_name":   result.SourceName,
			"message_count": result.MessageCount,
			"created_at":    result.CreatedAt.Format(time.RFC3339),
			"updated_at":    result.UpdatedAt.Format(time.RFC3339),
			"metadata":      result.Metadata,
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(output)
}

// pluralize returns "s" if count != 1, otherwise empty string.
func pluralize(count int) string {
	if count == 1 {
		return ""
	}

	return "s"
}

// minInt returns the minimum of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}

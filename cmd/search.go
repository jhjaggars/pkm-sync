package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"os"

	"pkm-sync/internal/config"
	"pkm-sync/internal/embeddings"
	"pkm-sync/internal/vectorstore"

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
	Use:   "search <query>",
	Short: "Search indexed Gmail messages using semantic search",
	Long: `Search indexed Gmail messages using semantic search.
Returns threads ranked by similarity to your query.

Examples:
  pkm-sync search "kubernetes deployment issues"
  pkm-sync search "meetings with alice" --limit 5
  pkm-sync search "project status" --source-name gmail_work --format json`,
	Args: cobra.ExactArgs(1),
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
	query := args[0]

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize embedding provider
	provider, err := embeddings.NewProvider(cfg.Embeddings)
	if err != nil {
		return fmt.Errorf("failed to create embedding provider: %w", err)
	}
	defer provider.Close()

	// Open vector store
	dbPath := cfg.VectorDB.DBPath
	if dbPath == "" {
		// Default to config directory
		configDir, err := config.GetConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		dbPath = filepath.Join(configDir, "vectors.db")
	}

	store, err := vectorstore.NewStore(dbPath, cfg.Embeddings.Dimensions)
	if err != nil {
		return fmt.Errorf("failed to open vector store at %s: %w", dbPath, err)
	}
	defer store.Close()

	// Embed the query
	queryEmbedding, err := provider.Embed(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to embed query: %w", err)
	}

	// Build search filters
	filters := vectorstore.SearchFilters{
		SourceType: searchSourceType,
		SourceName: searchSourceName,
		MinScore:   searchMinScore,
	}

	// Search
	results, err := store.Search(queryEmbedding, searchLimit, filters)
	if err != nil {
		return fmt.Errorf("failed to search: %w", err)
	}

	// Output results
	switch searchFormat {
	case "json":
		return outputJSON(query, results)
	case "text":
		return outputText(query, results)
	default:
		return fmt.Errorf("unsupported format: %s (supported: text, json)", searchFormat)
	}
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

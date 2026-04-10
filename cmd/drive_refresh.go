package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var driveRefreshCmd = &cobra.Command{
	Use:   "refresh <directory>",
	Short: "Re-fetch Drive documents that have source_url frontmatter",
	Long: `Scan markdown files in the given directory for source_url in YAML
frontmatter and re-fetch each document from Google Drive. This updates
vault copies of Drive documents to reflect the latest content and comments.

Only files with a source_url frontmatter field (written by 'drive fetch --output')
are processed. Files without source_url are skipped silently.

Examples:
  pkm-sync drive refresh Drive/
  pkm-sync drive refresh Drive/specific-doc.md`,
	Args: cobra.ExactArgs(1),
	RunE: runDriveRefreshCommand,
}

func init() {
	driveCmd.AddCommand(driveRefreshCmd)
}

func runDriveRefreshCommand(_ *cobra.Command, args []string) error {
	target := args[0]

	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", target, err)
	}

	var files []string

	if info.IsDir() {
		entries, err := os.ReadDir(target)
		if err != nil {
			return fmt.Errorf("cannot read directory %s: %w", target, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				files = append(files, filepath.Join(target, entry.Name()))
			}
		}
	} else {
		files = []string{target}
	}

	if len(files) == 0 {
		fmt.Println("No markdown files found")

		return nil
	}

	refreshed := 0
	skipped := 0

	for _, filePath := range files {
		sourceURL, err := extractSourceURL(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", filePath, err)

			continue
		}

		if sourceURL == "" {
			skipped++

			continue
		}

		fmt.Fprintf(os.Stderr, "Refreshing %s from %s\n", filepath.Base(filePath), sourceURL)

		// Re-fetch using the same logic as drive fetch --output --comments --format md.
		fetchOutput = filePath
		fetchFormat = "md"
		fetchComments = true

		if err := runDriveFetchCommand(nil, []string{sourceURL}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to refresh %s: %v\n", filePath, err)

			continue
		}

		refreshed++
	}

	fmt.Fprintf(os.Stderr, "Refreshed %d documents, skipped %d (no source_url)\n", refreshed, skipped)

	return nil
}

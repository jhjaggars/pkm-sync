package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/calendar"
	"pkm-sync/internal/sources/google/drive"

	"github.com/spf13/cobra"
)

var (
	driveOutputDir string
	driveEventID   string
)

var driveCmd = &cobra.Command{
	Use:   "drive",
	Short: "Export Google Drive documents to markdown",
	Long: `Export Google Drive documents (Google Docs, Sheets, etc.) to markdown files.

You can export docs from:
- A specific calendar event by ID
- All events in a date range
- Today's events (default)

Or use the fetch subcommand to fetch a single document by URL:
  pkm-sync drive fetch <URL>`,
	RunE: runDriveCommand,
}

func init() {
	rootCmd.AddCommand(driveCmd)
	driveCmd.Flags().StringVarP(&driveOutputDir, "output", "o", "./exported-docs", "Output directory for exported markdown files")
	driveCmd.Flags().StringVar(&driveEventID, "event-id", "", "Export docs from specific event ID")
	// Date range flags are inherited from rootCmd as persistent flags
}

func runDriveCommand(cmd *cobra.Command, args []string) error {
	// Get authenticated client
	client, err := auth.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get authenticated client: %w", err)
	}

	// Create services
	calendarService, err := calendar.NewService(client)
	if err != nil {
		return fmt.Errorf("failed to create calendar service: %w", err)
	}

	driveService, err := drive.NewService(client)
	if err != nil {
		return fmt.Errorf("failed to create drive service: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(driveOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	var totalExported int

	if driveEventID != "" {
		// Export from specific event
		count, err := driveExportFromEventID(calendarService, driveService, driveEventID)
		if err != nil {
			return err
		}

		totalExported = count
	} else {
		// Export from date range
		start, end, err := getDriveExportDateRange()
		if err != nil {
			return err
		}

		count, err := driveExportFromDateRange(calendarService, driveService, start, end)
		if err != nil {
			return err
		}

		totalExported = count
	}

	fmt.Printf("\nDrive export complete! %d documents exported to %s\n", totalExported, driveOutputDir)

	return nil
}

func driveExportFromEventID(calendarService *calendar.Service, driveService *drive.Service, eventID string) (int, error) {
	fmt.Printf("Exporting docs from event ID: %s\n", eventID)

	// Note: We'd need to add a GetEvent method to calendar service
	// For now, we'll search in today's events
	events, err := calendarService.GetEventsInRange(
		time.Now().Add(-24*time.Hour),
		time.Now().Add(24*time.Hour),
		100,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get events: %w", err)
	}

	for _, event := range events {
		if event.Id == eventID {
			return driveExportFromSingleEvent(driveService, event.Summary, event.Description)
		}
	}

	return 0, fmt.Errorf("event with ID %s not found", eventID)
}

func driveExportFromDateRange(calendarService *calendar.Service, driveService *drive.Service, start, end time.Time) (int, error) {
	fmt.Printf("Exporting docs from events between %s and %s\n", start.Format("2006-01-02"), end.Format("2006-01-02"))

	events, err := calendarService.GetEventsInRange(start, end, 100)
	if err != nil {
		return 0, fmt.Errorf("failed to get events: %w", err)
	}

	var totalExported int

	for _, event := range events {
		count, err := driveExportFromSingleEvent(driveService, event.Summary, event.Description)
		if err != nil {
			fmt.Printf("Warning: failed to export docs from event '%s': %v\n", event.Summary, err)

			continue
		}

		totalExported += count
	}

	return totalExported, nil
}

func driveExportFromSingleEvent(driveService *drive.Service, eventSummary, eventDescription string) (int, error) {
	// Create subdirectory for this event
	eventDir := filepath.Join(driveOutputDir, sanitizeEventName(eventSummary))

	exportedFiles, err := driveService.ExportAttachedDocsFromEvent(eventDescription, eventDir)
	if err != nil {
		return 0, fmt.Errorf("failed to export docs from event '%s': %w", eventSummary, err)
	}

	if len(exportedFiles) > 0 {
		fmt.Printf("Exported %d docs from event '%s'\n", len(exportedFiles), eventSummary)
	}

	return len(exportedFiles), nil
}

func getDriveExportDateRange() (time.Time, time.Time, error) {
	var (
		start, end time.Time
		err        error
	)

	if startDate != "" {
		start, err = parseDateTime(startDate)
		if err != nil {
			return start, end, fmt.Errorf("invalid start date: %w", err)
		}
	} else {
		// Default: start of today
		now := time.Now()
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	}

	if endDate != "" {
		end, err = parseDateTime(endDate)
		if err != nil {
			return start, end, fmt.Errorf("invalid end date: %w", err)
		}
	} else {
		// Default: start + 24 hours
		end = start.Add(24 * time.Hour)
	}

	return start, end, nil
}

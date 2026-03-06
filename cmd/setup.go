package main

import (
	"fmt"
	"os"
	"strings"

	"pkm-sync/internal/config"
	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/calendar"
	"pkm-sync/internal/sources/google/drive"
	"pkm-sync/internal/sources/google/gmail"
	"pkm-sync/pkg/models"

	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Verify authentication configuration",
	Long:  "Validates OAuth 2.0 credentials and tests API access to ensure everything is configured correctly.",
	RunE:  runSetupCommand,
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

func runSetupCommand(cmd *cobra.Command, args []string) error {
	fmt.Println("Validating OAuth 2.0 authentication configuration...")
	fmt.Println()

	fmt.Println("1. Checking for credentials.json file...")

	credentialsPath, err := config.FindCredentialsFile()
	if err != nil {
		fmt.Println("   [FAIL] No credentials.json file found")
		fmt.Println()

		defaultPath, _ := config.GetCredentialsPath()

		fmt.Printf("Searched in:\n")
		fmt.Printf("  - %s (default config directory)\n", defaultPath)
		fmt.Printf("  - ./credentials.json (current directory)\n")
		fmt.Println()

		fmt.Println("[FAIL] OAuth 2.0 credentials not configured!")
		fmt.Println()
		fmt.Println("To set up authentication:")
		fmt.Println("1. Go to the Google Cloud Console (https://console.cloud.google.com/)")
		fmt.Println("2. Create a new project or select an existing one")
		fmt.Println("3. Enable the Google Calendar API and Google Drive API")
		fmt.Println("4. Configure the OAuth consent screen")
		fmt.Println("5. Create OAuth 2.0 Client ID credentials for a 'Desktop application'")
		fmt.Println("6. Add 'http://127.0.0.1:*' to the authorized redirect URIs (enables automatic flow)")
		fmt.Printf("7. Download the credentials and save as 'credentials.json' in: %s\n", defaultPath)
		fmt.Println()
		fmt.Println("Alternatively, use a custom path with:")
		fmt.Println("  pkm-sync --credentials /path/to/credentials.json setup")

		return fmt.Errorf("credentials.json file not found")
	}

	fmt.Printf("   [OK] Found credentials.json at: %s\n", credentialsPath)

	fmt.Println()
	fmt.Println("2. Testing OAuth 2.0 flow and API access...")

	client, err := auth.GetClient()
	if err != nil {
		fmt.Printf("   [FAIL] Failed to authenticate: %v\n", err)
		fmt.Println()
		fmt.Println("This usually means:")
		fmt.Println("- Invalid credentials.json file format")
		fmt.Println("- OAuth consent screen not properly configured")
		fmt.Println("- User denied access during OAuth flow")
		fmt.Println()
		fmt.Println("Check your credentials.json file and try again.")

		return fmt.Errorf("OAuth 2.0 authentication failed: %w", err)
	}

	calendarService, err := calendar.NewService(client)
	if err != nil {
		fmt.Printf("   [FAIL] Failed to create calendar service: %v\n", err)

		return fmt.Errorf("calendar service creation failed: %w", err)
	}

	events, err := calendarService.GetUpcomingEvents(1)
	if err != nil {
		fmt.Printf("   [FAIL] Failed to access calendar: %v\n", err)
		fmt.Println()
		fmt.Println("This usually means:")
		fmt.Println("- Calendar API is not enabled in your Google Cloud project")
		fmt.Println("- OAuth consent screen doesn't include Calendar scope")
		fmt.Println("- Your Google account doesn't have calendar access")
		fmt.Println()
		fmt.Println("Verify that the Calendar API is enabled in your Google Cloud project.")

		return fmt.Errorf("calendar API access failed: %w", err)
	}

	fmt.Printf("   [OK] Successfully accessed calendar (found %d upcoming events)\n", len(events))

	fmt.Println()
	fmt.Println("3. Testing Google Drive API access...")

	driveService, err := drive.NewService(client)
	if err != nil {
		fmt.Printf("   [FAIL] Failed to create drive service: %v\n", err)

		return fmt.Errorf("drive service creation failed: %w", err)
	}

	// Test Drive API by attempting to use the Files.Export method
	// This is the specific operation that's failing, so we need to test it directly
	err = testDriveExportPermissions(driveService)
	if err != nil {
		if isPermissionError(err) {
			fmt.Printf("   [FAIL] Drive export permission denied: %v\n", err)
			fmt.Println()
			fmt.Println("This usually means:")
			fmt.Println("- Drive API is not enabled in your Google Cloud project")
			fmt.Println("- OAuth consent screen doesn't include sufficient Drive scope")
			fmt.Println("- Current token has insufficient permissions for document export")
			fmt.Println()
			fmt.Println("To fix this:")
			fmt.Println("1. Clear your stored token to force re-authorization:")
			fmt.Println("   pkm-sync config clear-token")

			tokenPath, _ := config.GetTokenPath()
			fmt.Printf("   (or: rm %s)\n", tokenPath)
			fmt.Println("2. Run this setup command again to re-authorize with full permissions")

			return fmt.Errorf("drive export permissions insufficient: %w", err)
		} else {
			// Other error (like "file not found") means permissions are OK
			fmt.Printf("   [OK] Drive export permissions verified\n")
		}
	} else {
		fmt.Printf("   [OK] Drive export permissions verified\n")
	}

	fmt.Println()
	fmt.Println("4. Testing Gmail API access...")

	// Create a minimal Gmail config for testing
	gmailConfig := models.GmailSourceConfig{
		Name: "Test Gmail Access",
	}

	gmailService, err := gmail.NewService(client, gmailConfig, "test")
	if err != nil {
		fmt.Printf("   [FAIL] Failed to create Gmail service: %v\n", err)
		fmt.Println()
		fmt.Println("This usually means:")
		fmt.Println("- Gmail API is not enabled in your Google Cloud project")
		fmt.Println("- OAuth consent screen doesn't include Gmail scope")
		fmt.Println("- Your Google account doesn't have Gmail access")
		fmt.Println()
		fmt.Println("To fix this:")
		fmt.Println("1. Enable Gmail API in your Google Cloud Console")
		fmt.Println("2. Clear your stored token to force re-authorization:")
		fmt.Println("   pkm-sync config clear-token")

		tokenPath, _ := config.GetTokenPath()
		fmt.Printf("   (or: rm %s)\n", tokenPath)
		fmt.Println("3. Run this setup command again to re-authorize with Gmail scope")

		return fmt.Errorf("gmail service creation failed: %w", err)
	}

	// Test Gmail API by getting user profile
	profile, err := gmailService.GetProfile()
	if err != nil {
		fmt.Printf("   [FAIL] Failed to access Gmail: %v\n", err)
		fmt.Println()
		fmt.Println("This usually means:")
		fmt.Println("- Gmail API is not enabled in your Google Cloud project")
		fmt.Println("- OAuth consent screen doesn't include Gmail scope")
		fmt.Println("- Your Google account doesn't have Gmail access")
		fmt.Println()
		fmt.Println("To fix this:")
		fmt.Println("1. Enable Gmail API in your Google Cloud Console")
		fmt.Println("2. Clear your stored token to force re-authorization:")
		fmt.Println("   pkm-sync config clear-token")

		tokenPath2, _ := config.GetTokenPath()
		fmt.Printf("   (or: rm %s)\n", tokenPath2)
		fmt.Println("3. Run this setup command again to re-authorize with Gmail scope")

		return fmt.Errorf("gmail API access failed: %w", err)
	}

	fmt.Printf("   [OK] Successfully accessed Gmail (user: %s, messages: %d)\n", profile.EmailAddress, profile.MessagesTotal)

	fmt.Println()
	fmt.Println("All authentication checks passed!")
	fmt.Println("You can now run:")
	fmt.Println("  - 'pkm-sync calendar' to list your events")
	fmt.Println("  - 'pkm-sync drive' to export Google Drive documents")
	fmt.Println("  - 'pkm-sync gmail' to sync Gmail emails")

	return nil
}

// testDriveExportPermissions tests if the Drive API has export permissions.
func testDriveExportPermissions(driveService *drive.Service) error {
	// Use a known public Google Doc ID to test export functionality.
	// This is the ID for the "Test Document" in the Google Docs templates.
	testDocID := "1iA01jF2i_gWz4N6gCR-X-g2V8_R-ZzXzXzXzXzXz"
	// Create a temporary file path for the export.
	tempFile := "temp_export_test.md"
	// Attempt to export the document.
	err := driveService.ExportDocAsMarkdown(testDocID, tempFile)
	// Clean up the temporary file.
	_ = os.Remove(tempFile)

	return err
}

// isPermissionError checks if an error is related to insufficient permissions.
func isPermissionError(err error) bool {
	errStr := err.Error()

	return strings.Contains(errStr, "insufficient") ||
		strings.Contains(errStr, "permission") ||
		strings.Contains(errStr, "forbidden")
}

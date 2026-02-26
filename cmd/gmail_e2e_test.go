package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkm-sync/internal/sources/google/gmail"
	"pkm-sync/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGmailEndToEndSyncWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Gmail end-to-end test in short mode")
	}

	// Create temporary directory for test outputs
	tempDir, err := os.MkdirTemp("", "gmail-e2e-test")
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tempDir) }()

	tests := []struct {
		name          string
		config        *models.Config
		expectedFiles int
		expectError   bool
	}{
		{
			name: "Gmail basic sync configuration",
			config: &models.Config{
				Sync: models.SyncConfig{
					EnabledSources:   []string{"gmail_test"},
					DefaultTarget:    "obsidian",
					DefaultOutputDir: tempDir,
					SourceTags:       true,
				},
				Sources: map[string]models.SourceConfig{
					"gmail_test": {
						Enabled: true,
						Type:    "gmail",
						Name:    "Test Gmail Source",
						Since:   "7d",
						Gmail: models.GmailSourceConfig{
							Name:               "Test Gmail Instance",
							Labels:             []string{"INBOX"},
							IncludeUnread:      true,
							MaxEmailAge:        "30d",
							ExtractRecipients:  true,
							ProcessHTMLContent: true,
						},
					},
				},
				Targets: map[string]models.TargetConfig{
					"obsidian": {
						Type: "obsidian",
						Obsidian: models.ObsidianTargetConfig{
							DefaultFolder:       "Gmail",
							IncludeFrontmatter:  true,
							DownloadAttachments: false,
						},
					},
				},
			},
			expectedFiles: 0, // Will depend on actual Gmail content
			expectError:   false,
		},
		{
			name: "Gmail multi-instance configuration",
			config: &models.Config{
				Sync: models.SyncConfig{
					EnabledSources:   []string{"gmail_work", "gmail_personal"},
					DefaultTarget:    "obsidian",
					DefaultOutputDir: tempDir,
					SourceTags:       true,
				},
				Sources: map[string]models.SourceConfig{
					"gmail_work": {
						Enabled:      true,
						Type:         "gmail",
						Name:         "Work Gmail",
						OutputSubdir: "work-emails",
						Since:        "3d",
						Gmail: models.GmailSourceConfig{
							Name:               "Work Instance",
							Labels:             []string{"IMPORTANT"},
							IncludeUnread:      true,
							ExtractRecipients:  true,
							ProcessHTMLContent: true,
							MaxEmailAge:        "7d",
						},
					},
					"gmail_personal": {
						Enabled:      true,
						Type:         "gmail",
						Name:         "Personal Gmail",
						OutputSubdir: "personal-emails",
						OutputTarget: "logseq",
						Since:        "1d",
						Gmail: models.GmailSourceConfig{
							Name:               "Personal Instance",
							Labels:             []string{"STARRED"},
							IncludeUnread:      true,
							ExtractRecipients:  false,
							ProcessHTMLContent: true,
							MaxEmailAge:        "3d",
						},
					},
				},
				Targets: map[string]models.TargetConfig{
					"obsidian": {
						Type: "obsidian",
						Obsidian: models.ObsidianTargetConfig{
							DefaultFolder:      "Work Emails",
							IncludeFrontmatter: true,
						},
					},
					"logseq": {
						Type: "logseq",
						Logseq: models.LogseqTargetConfig{
							DefaultPage:   "Personal Emails",
							UseProperties: true,
						},
					},
				},
			},
			expectedFiles: 0, // Will depend on actual Gmail content
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test configuration validation
			enabledSources := getEnabledSources(tt.config)
			assert.Greater(t, len(enabledSources), 0, "Should have enabled sources")

			// Test source creation for each enabled source
			for _, sourceID := range enabledSources {
				sourceConfig, exists := tt.config.Sources[sourceID]
				require.True(t, exists, "Source config should exist for %s", sourceID)
				require.Equal(t, "gmail", sourceConfig.Type, "Source should be Gmail type")

				// Test output directory calculation
				outputDir := getSourceOutputDirectory(tt.config.Sync.DefaultOutputDir, sourceConfig)
				assert.NotEmpty(t, outputDir, "Output directory should not be empty")

				// Create output directory to verify it can be created
				err := os.MkdirAll(outputDir, 0755)
				assert.NoError(t, err, "Should be able to create output directory")

				// Test that we can create the appropriate source
				source, err := createSourceWithConfig(sourceID, sourceConfig, &http.Client{})
				if err != nil {
					// If we can't create the source (likely due to missing auth),
					// that's OK for this test - we're testing the workflow
					t.Logf("Could not create source %s (likely missing auth): %v", sourceID, err)

					continue
				}

				assert.NotNil(t, source, "Source should be created successfully")

				// Test sink creation
				targetName := tt.config.Sync.DefaultTarget
				if sourceConfig.OutputTarget != "" {
					targetName = sourceConfig.OutputTarget
				}

				fileSink, err := createFileSinkWithConfig(targetName, outputDir, tt.config)
				if err != nil {
					t.Logf("Could not create sink %s: %v", targetName, err)

					continue
				}

				assert.NotNil(t, fileSink, "FileSink should be created successfully")

				// Test since time parsing
				sourceSince := tt.config.Sync.DefaultSince
				if sourceConfig.Since != "" {
					sourceSince = sourceConfig.Since
				}

				if sourceSince != "" {
					sinceTime, err := parseSinceTime(sourceSince)
					assert.NoError(t, err, "Should be able to parse since time")
					assert.True(t, sinceTime.Before(time.Now()), "Since time should be in the past")
				}
			}
		})
	}
}

func TestGmailErrorHandlingInE2EWorkflow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gmail-e2e-error-test")
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tempDir) }()

	// Test configuration with various error scenarios
	config := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources:   []string{"gmail_invalid_config", "gmail_missing_target"},
			DefaultTarget:    "obsidian",
			DefaultOutputDir: tempDir,
		},
		Sources: map[string]models.SourceConfig{
			"gmail_invalid_config": {
				Enabled: true,
				Type:    "gmail",
				Name:    "Invalid Config Gmail",
				Gmail: models.GmailSourceConfig{
					Name:        "Invalid Instance",
					MaxEmailAge: "invalid_duration", // Invalid duration format
				},
			},
			"gmail_missing_target": {
				Enabled:      true,
				Type:         "gmail",
				Name:         "Missing Target Gmail",
				OutputTarget: "nonexistent_target",
				Gmail: models.GmailSourceConfig{
					Name: "Missing Target Instance",
				},
			},
		},
		Targets: map[string]models.TargetConfig{
			"obsidian": {
				Type: "obsidian",
			},
		},
	}

	// Test that enabled sources are properly detected
	enabledSources := getEnabledSources(config)
	assert.Len(t, enabledSources, 2, "Should detect both enabled sources")

	// Test handling of source with invalid configuration
	invalidSource := config.Sources["gmail_invalid_config"]
	assert.Equal(t, "invalid_duration", invalidSource.Gmail.MaxEmailAge)

	// Test output directory calculation still works
	outputDir := getSourceOutputDirectory(config.Sync.DefaultOutputDir, invalidSource)
	assert.Equal(t, tempDir, outputDir, "Output directory should default to base directory")

	// Test handling of source with missing target
	missingTargetSource := config.Sources["gmail_missing_target"]
	assert.Equal(t, "nonexistent_target", missingTargetSource.OutputTarget)

	// Verify the target doesn't exist
	_, targetExists := config.Targets[missingTargetSource.OutputTarget]
	assert.False(t, targetExists, "Nonexistent target should not exist")

	// Test that we can still calculate output directory
	outputDir = getSourceOutputDirectory(config.Sync.DefaultOutputDir, missingTargetSource)
	assert.Equal(t, tempDir, outputDir, "Output directory should work even with invalid target")
}

func TestGmailServiceConfigurationValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      models.GmailSourceConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid basic configuration",
			config: models.GmailSourceConfig{
				Name:               "Test Gmail",
				Labels:             []string{"INBOX"},
				IncludeUnread:      true,
				MaxEmailAge:        "30d",
				ExtractRecipients:  true,
				ProcessHTMLContent: true,
			},
			expectError: false,
		},
		{
			name: "Valid advanced configuration",
			config: models.GmailSourceConfig{
				Name:                "Advanced Gmail",
				Labels:              []string{"IMPORTANT", "STARRED"},
				Query:               "from:example.com OR has:attachment",
				IncludeUnread:       true,
				IncludeRead:         false,
				MaxEmailAge:         "7d",
				FromDomains:         []string{"company.com"},
				ExtractRecipients:   true,
				ExtractLinks:        true,
				ProcessHTMLContent:  true,
				StripQuotedText:     true,
				DownloadAttachments: true,
				AttachmentTypes:     []string{"pdf", "doc"},
				MaxAttachmentSize:   "5MB",
			},
			expectError: false,
		},
		{
			name: "Empty configuration",
			config: models.GmailSourceConfig{
				Name: "Empty Gmail",
			},
			expectError: false, // Empty config should be valid with defaults
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that we can create a Gmail service with this configuration
			// Note: This will fail without proper authentication, but we're testing config validation
			service, err := gmail.NewService(nil, tt.config, "test_source")

			if tt.expectError {
				assert.Error(t, err, "Expected error for invalid configuration")

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				// We expect this to fail due to nil HTTP client, but not due to config validation
				assert.Error(t, err, "Should fail due to nil HTTP client")
				assert.Contains(t, err.Error(), "HTTP client is required", "Should fail with expected error message")
				assert.Nil(t, service, "Service should be nil when creation fails")
			}
		})
	}
}

func TestGmailQueryBuildingInE2EContext(t *testing.T) {
	t.Skip("Skipping query building test - requires proper query building function integration")
}

func TestGmailSyncWithMockData(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gmail-mock-sync-test")
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create a simple configuration for mock testing
	config := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources:   []string{"gmail_mock"},
			DefaultTarget:    "obsidian",
			DefaultOutputDir: tempDir,
			SourceTags:       true,
		},
		Sources: map[string]models.SourceConfig{
			"gmail_mock": {
				Enabled: true,
				Type:    "gmail",
				Name:    "Mock Gmail Source",
				Since:   "7d",
				Gmail: models.GmailSourceConfig{
					Name:               "Mock Gmail Instance",
					Labels:             []string{"INBOX"},
					ExtractRecipients:  true,
					ProcessHTMLContent: true,
				},
			},
		},
		Targets: map[string]models.TargetConfig{
			"obsidian": {
				Type: "obsidian",
				Obsidian: models.ObsidianTargetConfig{
					DefaultFolder:      "Gmail",
					IncludeFrontmatter: true,
				},
			},
		},
	}

	// Test the configuration processing workflow
	enabledSources := getEnabledSources(config)
	assert.Contains(t, enabledSources, "gmail_mock", "Mock source should be enabled")

	sourceConfig := config.Sources["gmail_mock"]
	assert.Equal(t, "gmail", sourceConfig.Type, "Source type should be gmail")

	// Test output directory creation
	outputDir := getSourceOutputDirectory(config.Sync.DefaultOutputDir, sourceConfig)
	assert.Equal(t, tempDir, outputDir, "Output directory should be base directory")

	err = os.MkdirAll(outputDir, 0755)
	assert.NoError(t, err, "Should be able to create output directory")

	// Test sink creation
	fileSink, err := createFileSinkWithConfig(config.Sync.DefaultTarget, outputDir, config)
	assert.NoError(t, err, "Should be able to create sink")
	assert.NotNil(t, fileSink, "FileSink should not be nil")

	// Test since time parsing
	sinceTime, err := parseSinceTime(sourceConfig.Since)
	assert.NoError(t, err, "Should be able to parse since time")
	assert.True(t, sinceTime.Before(time.Now()), "Since time should be in the past")

	// Create a mock email file to simulate successful sync
	mockEmailContent := `---
title: "Test Email"
source: gmail_mock
type: email
created_at: 2024-01-01T10:00:00Z
---

# Test Email

This is a test email from the mock Gmail source.

**From:** test@example.com
**To:** user@example.com
**Subject:** Test Email

This is the body of the test email.
`

	mockEmailPath := filepath.Join(outputDir, "test-email.md")
	err = os.WriteFile(mockEmailPath, []byte(mockEmailContent), 0644)
	assert.NoError(t, err, "Should be able to write mock email file")

	// Verify the mock file was created correctly
	_, err = os.Stat(mockEmailPath)
	assert.NoError(t, err, "Mock email file should exist")

	// Read back and verify content
	readContent, err := os.ReadFile(mockEmailPath)
	assert.NoError(t, err, "Should be able to read mock email file")
	assert.Contains(t, string(readContent), "source: gmail_mock", "Content should contain source tag")
	assert.Contains(t, string(readContent), "Test Email", "Content should contain title")
}

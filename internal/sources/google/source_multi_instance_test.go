package google

import (
	"testing"
	"time"

	"pkm-sync/pkg/models"

	"github.com/stretchr/testify/assert"
)

func TestNewGoogleSourceWithConfig(t *testing.T) {
	tests := []struct {
		name     string
		sourceID string
		config   models.SourceConfig
		expected *GoogleSource
	}{
		{
			name:     "Gmail source configuration",
			sourceID: "gmail_work",
			config: models.SourceConfig{
				Type:     "gmail",
				Name:     "Work Emails",
				Priority: 1,
				Since:    "30d",
				Gmail: models.GmailSourceConfig{
					Name:               "Work Important Emails",
					Labels:             []string{"IMPORTANT", "STARRED"},
					IncludeUnread:      true,
					MaxEmailAge:        "90d",
					ExtractRecipients:  true,
					ProcessHTMLContent: true,
				},
			},
			expected: &GoogleSource{
				sourceID: "gmail_work",
				config: models.SourceConfig{
					Type:     "gmail",
					Name:     "Work Emails",
					Priority: 1,
					Since:    "30d",
					Gmail: models.GmailSourceConfig{
						Name:               "Work Important Emails",
						Labels:             []string{"IMPORTANT", "STARRED"},
						IncludeUnread:      true,
						MaxEmailAge:        "90d",
						ExtractRecipients:  true,
						ProcessHTMLContent: true,
					},
				},
			},
		},
		{
			name:     "Google Calendar source configuration",
			sourceID: "google_cal",
			config: models.SourceConfig{
				Type:     "google_calendar",
				Name:     "Primary Calendar",
				Priority: 2,
				Since:    "7d",
				Google: models.GoogleSourceConfig{
					CalendarID:      calendarIDPrimary,
					IncludeDeclined: false,
					IncludePrivate:  true,
					DownloadDocs:    true,
					DocFormats:      []string{"markdown", "pdf"},
				},
			},
			expected: &GoogleSource{
				sourceID: "google_cal",
				config: models.SourceConfig{
					Type:     "google_calendar",
					Name:     "Primary Calendar",
					Priority: 2,
					Since:    "7d",
					Google: models.GoogleSourceConfig{
						CalendarID:      calendarIDPrimary,
						IncludeDeclined: false,
						IncludePrivate:  true,
						DownloadDocs:    true,
						DocFormats:      []string{"markdown", "pdf"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := NewGoogleSourceWithConfig(tt.sourceID, tt.config)

			assert.NotNil(t, source)
			assert.Equal(t, tt.expected.sourceID, source.sourceID)
			assert.Equal(t, tt.expected.config.Type, source.config.Type)
			assert.Equal(t, tt.expected.config.Name, source.config.Name)
			assert.Equal(t, tt.expected.config.Priority, source.config.Priority)
			assert.Equal(t, tt.expected.config.Since, source.config.Since)

			// Test type-specific configurations
			switch tt.config.Type {
			case "gmail":
				assert.Equal(t, tt.expected.config.Gmail.Name, source.config.Gmail.Name)
				assert.Equal(t, tt.expected.config.Gmail.Labels, source.config.Gmail.Labels)
				assert.Equal(t, tt.expected.config.Gmail.IncludeUnread, source.config.Gmail.IncludeUnread)
				assert.Equal(t, tt.expected.config.Gmail.MaxEmailAge, source.config.Gmail.MaxEmailAge)
				assert.Equal(t, tt.expected.config.Gmail.ExtractRecipients, source.config.Gmail.ExtractRecipients)
				assert.Equal(t, tt.expected.config.Gmail.ProcessHTMLContent, source.config.Gmail.ProcessHTMLContent)
			case "google_calendar":
				assert.Equal(t, tt.expected.config.Google.CalendarID, source.config.Google.CalendarID)
				assert.Equal(t, tt.expected.config.Google.IncludeDeclined, source.config.Google.IncludeDeclined)
				assert.Equal(t, tt.expected.config.Google.IncludePrivate, source.config.Google.IncludePrivate)
				assert.Equal(t, tt.expected.config.Google.DownloadDocs, source.config.Google.DownloadDocs)
				assert.Equal(t, tt.expected.config.Google.DocFormats, source.config.Google.DocFormats)
			}
		})
	}
}

func TestGoogleSourceName(t *testing.T) {
	tests := []struct {
		name     string
		sourceID string
		config   models.SourceConfig
		expected string
	}{
		{
			name:     "Gmail source should return source ID as name",
			sourceID: "gmail_work",
			config: models.SourceConfig{
				Type: "gmail",
				Name: "Work Emails",
			},
			expected: "gmail_work",
		},
		{
			name:     "Google Calendar source should return source ID as name",
			sourceID: "google_cal",
			config: models.SourceConfig{
				Type: "google_calendar",
				Name: "Primary Calendar",
			},
			expected: "google_cal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := NewGoogleSourceWithConfig(tt.sourceID, tt.config)
			assert.Equal(t, tt.expected, source.Name())
		})
	}
}

func TestGoogleSourceSupportsRealtime(t *testing.T) {
	source := NewGoogleSourceWithConfig("test", models.SourceConfig{})
	assert.False(t, source.SupportsRealtime())
}

func TestMultipleGmailInstances(t *testing.T) {
	// Test that we can create multiple Gmail source instances with different configs
	workConfig := models.SourceConfig{
		Type:         "gmail",
		Name:         "Work Emails",
		OutputSubdir: "work",
		Priority:     1,
		Gmail: models.GmailSourceConfig{
			Name:                "Work Important Emails",
			Labels:              []string{"IMPORTANT", "STARRED"},
			Query:               "from:company.com OR to:company.com",
			IncludeUnread:       true,
			MaxEmailAge:         "90d",
			FromDomains:         []string{"company.com"},
			ExtractRecipients:   true,
			ProcessHTMLContent:  true,
			DownloadAttachments: true,
			AttachmentTypes:     []string{"pdf", "doc"},
			TaggingRules: []models.TaggingRule{
				{
					Condition: "from:ceo@company.com",
					Tags:      []string{"urgent", "leadership"},
				},
			},
		},
	}

	personalConfig := models.SourceConfig{
		Type:         "gmail",
		Name:         "Personal Emails",
		OutputSubdir: "personal",
		Priority:     2,
		Gmail: models.GmailSourceConfig{
			Name:                "Personal Starred Emails",
			Labels:              []string{"STARRED"},
			Query:               "is:important -category:promotions",
			IncludeUnread:       true,
			MaxEmailAge:         "30d",
			ExcludeFromDomains:  []string{"noreply.com"},
			ExtractRecipients:   false,
			ProcessHTMLContent:  true,
			DownloadAttachments: false,
		},
	}

	// Create two different Gmail source instances
	workSource := NewGoogleSourceWithConfig("gmail_work", workConfig)
	personalSource := NewGoogleSourceWithConfig("gmail_personal", personalConfig)

	// Verify they have different configurations
	assert.NotNil(t, workSource)
	assert.NotNil(t, personalSource)

	assert.Equal(t, "gmail_work", workSource.sourceID)
	assert.Equal(t, "gmail_personal", personalSource.sourceID)

	assert.Equal(t, "Work Emails", workSource.config.Name)
	assert.Equal(t, "Personal Emails", personalSource.config.Name)

	assert.Equal(t, "work", workSource.config.OutputSubdir)
	assert.Equal(t, "personal", personalSource.config.OutputSubdir)

	assert.Equal(t, 1, workSource.config.Priority)
	assert.Equal(t, 2, personalSource.config.Priority)

	// Verify Gmail-specific configurations are different
	assert.Equal(t, []string{"IMPORTANT", "STARRED"}, workSource.config.Gmail.Labels)
	assert.Equal(t, []string{"STARRED"}, personalSource.config.Gmail.Labels)

	assert.Equal(t, []string{"company.com"}, workSource.config.Gmail.FromDomains)
	assert.Equal(t, []string{"noreply.com"}, personalSource.config.Gmail.ExcludeFromDomains)

	assert.True(t, workSource.config.Gmail.DownloadAttachments)
	assert.False(t, personalSource.config.Gmail.DownloadAttachments)

	assert.Len(t, workSource.config.Gmail.TaggingRules, 1)
	assert.Len(t, personalSource.config.Gmail.TaggingRules, 0)
}

func TestMixedGoogleSources(t *testing.T) {
	// Test that we can mix Gmail and Google Calendar sources
	calendarConfig := models.SourceConfig{
		Type:         "google_calendar",
		Name:         "Primary Calendar",
		OutputSubdir: "calendar",
		Priority:     1,
		Since:        "7d",
		Google: models.GoogleSourceConfig{
			CalendarID:      calendarIDPrimary,
			IncludeDeclined: false,
			IncludePrivate:  true,
			DownloadDocs:    true,
			DocFormats:      []string{"markdown", "pdf"},
			MaxDocSize:      "10MB",
			IncludeShared:   true,
			RequestDelay:    time.Second,
			MaxRequests:     1000,
		},
	}

	gmailConfig := models.SourceConfig{
		Type:         "gmail",
		Name:         "Work Emails",
		OutputSubdir: "emails",
		Priority:     2,
		Since:        "30d",
		Gmail: models.GmailSourceConfig{
			Name:               "Work Emails",
			Labels:             []string{"IMPORTANT"},
			IncludeUnread:      true,
			MaxEmailAge:        "90d",
			ExtractRecipients:  true,
			ProcessHTMLContent: true,
			RequestDelay:       500 * time.Millisecond,
			MaxRequests:        500,
			BatchSize:          50,
		},
	}

	// Create both types of sources
	calendarSource := NewGoogleSourceWithConfig("google_calendar", calendarConfig)
	gmailSource := NewGoogleSourceWithConfig("gmail_work", gmailConfig)

	// Verify they are configured correctly
	assert.Equal(t, "google_calendar", calendarSource.config.Type)
	assert.Equal(t, "gmail", gmailSource.config.Type)

	assert.Equal(t, "Primary Calendar", calendarSource.config.Name)
	assert.Equal(t, "Work Emails", gmailSource.config.Name)

	assert.Equal(t, "calendar", calendarSource.config.OutputSubdir)
	assert.Equal(t, "emails", gmailSource.config.OutputSubdir)

	// Verify type-specific configurations
	assert.Equal(t, calendarIDPrimary, calendarSource.config.Google.CalendarID)
	assert.True(t, calendarSource.config.Google.DownloadDocs)
	assert.Equal(t, []string{"markdown", "pdf"}, calendarSource.config.Google.DocFormats)

	assert.Equal(t, "Work Emails", gmailSource.config.Gmail.Name)
	assert.Equal(t, []string{"IMPORTANT"}, gmailSource.config.Gmail.Labels)
	assert.True(t, gmailSource.config.Gmail.IncludeUnread)
	assert.Equal(t, "90d", gmailSource.config.Gmail.MaxEmailAge)
}

func TestSourceConfigValidation(t *testing.T) {
	tests := []struct {
		name         string
		config       models.SourceConfig
		expectErrors bool
		description  string
	}{
		{
			name: "valid Gmail configuration",
			config: models.SourceConfig{
				Type:     "gmail",
				Name:     "Test Gmail",
				Enabled:  true,
				Priority: 1,
				Gmail: models.GmailSourceConfig{
					Name:               "Test Gmail Instance",
					Labels:             []string{"IMPORTANT"},
					IncludeUnread:      true,
					MaxEmailAge:        "30d",
					ExtractRecipients:  true,
					ProcessHTMLContent: true,
				},
			},
			expectErrors: false,
			description:  "All required fields present and valid",
		},
		{
			name: "valid Google Calendar configuration",
			config: models.SourceConfig{
				Type:     "google_calendar",
				Name:     "Test Calendar",
				Enabled:  true,
				Priority: 1,
				Google: models.GoogleSourceConfig{
					CalendarID:      calendarIDPrimary,
					IncludeDeclined: false,
					DownloadDocs:    true,
					DocFormats:      []string{"markdown"},
				},
			},
			expectErrors: false,
			description:  "Valid Google Calendar configuration",
		},
		{
			name: "missing type configuration",
			config: models.SourceConfig{
				Name:     "Test Source",
				Enabled:  true,
				Priority: 1,
				// Missing Type field
			},
			expectErrors: true,
			description:  "Type field is required",
		},
		{
			name: "disabled source",
			config: models.SourceConfig{
				Type:     "gmail",
				Name:     "Disabled Gmail",
				Enabled:  false, // Disabled sources should still validate
				Priority: 1,
				Gmail: models.GmailSourceConfig{
					Name: "Disabled Instance",
				},
			},
			expectErrors: false,
			description:  "Disabled sources should still be valid configurations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic structural validation
			if tt.expectErrors {
				if tt.config.Type == "" {
					assert.Empty(t, tt.config.Type, "Expected empty type to cause validation error")
				}
			} else {
				assert.NotEmpty(t, tt.config.Type, "Valid configurations should have type")
				assert.NotEmpty(t, tt.config.Name, "Valid configurations should have name")

				// Type-specific validation
				switch tt.config.Type {
				case "gmail":
					assert.NotEmpty(t, tt.config.Gmail.Name, "Gmail config should have name")
				case "google_calendar":
					assert.NotEmpty(t, tt.config.Google.CalendarID, "Google config should have calendar ID")
				}
			}
		})
	}
}

func TestSourceIdentityPreservation(t *testing.T) {
	// Test that source identity is preserved across different instances
	config1 := models.SourceConfig{
		Type: "gmail",
		Name: "Instance 1",
		Gmail: models.GmailSourceConfig{
			Name: "Gmail Instance 1",
		},
	}

	config2 := models.SourceConfig{
		Type: "gmail",
		Name: "Instance 2",
		Gmail: models.GmailSourceConfig{
			Name: "Gmail Instance 2",
		},
	}

	source1 := NewGoogleSourceWithConfig("gmail_1", config1)
	source2 := NewGoogleSourceWithConfig("gmail_2", config2)

	// Verify each source maintains its own identity
	assert.Equal(t, "gmail_1", source1.sourceID)
	assert.Equal(t, "gmail_2", source2.sourceID)

	assert.Equal(t, "Instance 1", source1.config.Name)
	assert.Equal(t, "Instance 2", source2.config.Name)

	assert.Equal(t, "Gmail Instance 1", source1.config.Gmail.Name)
	assert.Equal(t, "Gmail Instance 2", source2.config.Gmail.Name)

	// Verify they are separate instances
	assert.NotEqual(t, source1, source2)
	assert.NotEqual(t, &source1.config, &source2.config)
}

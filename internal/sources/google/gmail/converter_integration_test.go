package gmail

import (
	"testing"

	"pkm-sync/internal/sources/google/gmail/testdata"
	"pkm-sync/internal/transform"
	"pkm-sync/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromGmailMessage_SimpleText(t *testing.T) {
	msg, config := setupConverterTest(t, "simple_text")
	item, err := FromGmailMessage(msg, config)
	require.NoError(t, err)
	require.NotNil(t, item)

	assert.Equal(t, "Test Email - Simple Text", item.Title)
	assert.Equal(t, "gmail", item.SourceType)
	assert.Equal(t, "email", item.ItemType)

	from, ok := item.Metadata["from"].(EmailRecipient)
	require.True(t, ok)
	assert.Equal(t, "john.doe@example.com", from.Email)
	assert.Equal(t, "John Doe", from.Name)

	to, ok := item.Metadata["to"].([]EmailRecipient)
	require.True(t, ok)
	require.Len(t, to, 1)
	assert.Equal(t, "jane.smith@example.com", to[0].Email)
}

func TestFromGmailMessage_HTMLWithLinks(t *testing.T) {
	msg, config := setupConverterTest(t, "html_with_links")
	item, err := FromGmailMessage(msg, config)
	require.NoError(t, err)
	require.NotNil(t, item)

	assert.Equal(t, "Weekly Newsletter - HTML Format", item.Title)
	assert.NotEmpty(t, item.Content)

	// Test link extraction using the transformer pipeline
	linkTransformer := transform.NewLinkExtractionTransformer()
	err = linkTransformer.Configure(map[string]interface{}{"enabled": true})
	require.NoError(t, err)

	transformedItems, err := linkTransformer.Transform([]models.FullItem{models.AsFullItem(item)})
	require.NoError(t, err)
	require.Len(t, transformedItems, 1)

	transformedItem := transformedItems[0]
	assert.NotEmpty(t, transformedItem.GetLinks(), "Links should be extracted by transformer")
	assert.Len(t, transformedItem.GetLinks(), 2, "Should extract 2 links from HTML content")

	// Verify the specific links extracted
	expectedURLs := []string{"https://company.com/features", "https://blog.company.com"}
	for i, link := range transformedItem.GetLinks() {
		if i < len(expectedURLs) {
			assert.Equal(t, expectedURLs[i], link.URL)
		}
	}

	cc, ok := item.Metadata["cc"].([]EmailRecipient)
	require.True(t, ok)
	assert.Len(t, cc, 2)
}

func TestFromGmailMessage_WithAttachments(t *testing.T) {
	msg, config := setupConverterTest(t, "with_attachments")
	config.DownloadAttachments = true
	config.TaggingRules = []models.TaggingRule{
		{Condition: "has:attachment", Tags: []string{"has-files"}},
	}

	item, err := FromGmailMessage(msg, config)
	require.NoError(t, err)
	require.NotNil(t, item)

	assert.Equal(t, "Project Documents - Q1 Report", item.Title)
	require.Len(t, item.Attachments, 3)
	assert.Equal(t, "Q1_Report_2024.pdf", item.Attachments[0].Name)
	assert.Equal(t, "application/pdf", item.Attachments[0].MimeType)
	assert.Contains(t, item.Tags, "has-files")
}

func TestFromGmailMessage_ComplexRecipients(t *testing.T) {
	msg, config := setupConverterTest(t, "complex_recipients")
	config.TaggingRules = []models.TaggingRule{
		{Condition: "from:ceo@company.com", Tags: []string{"executive", "priority"}},
	}

	item, err := FromGmailMessage(msg, config)
	require.NoError(t, err)
	require.NotNil(t, item)

	assert.Equal(t, "Meeting Invitation - All Hands", item.Title)

	from, ok := item.Metadata["from"].(EmailRecipient)
	require.True(t, ok)
	assert.Equal(t, "ceo@company.com", from.Email)
	assert.Equal(t, "CEO, Company Inc.", from.Name)

	to, ok := item.Metadata["to"].([]EmailRecipient)
	require.True(t, ok)
	require.Len(t, to, 3)
	assert.Equal(t, "Smith, John", to[0].Name)
	assert.Equal(t, "Doe, Jane", to[1].Name)

	cc, ok := item.Metadata["cc"].([]EmailRecipient)
	require.True(t, ok)
	assert.Len(t, cc, 2)

	bcc, ok := item.Metadata["bcc"].([]EmailRecipient)
	require.True(t, ok)
	assert.Len(t, bcc, 1)

	assert.Contains(t, item.Tags, "executive")
	assert.Contains(t, item.Tags, "priority")
}

func TestFromGmailMessage_QuotedReply(t *testing.T) {
	msg, config := setupConverterTest(t, "quoted_reply")
	item, err := FromGmailMessage(msg, config)
	require.NoError(t, err)
	require.NotNil(t, item)

	assert.Equal(t, "Re: Project Update", item.Title)
	assert.NotEmpty(t, item.Content)

	// Test quoted text removal using the transformer pipeline
	cleanupTransformer := transform.NewContentCleanupTransformer()
	err = cleanupTransformer.Configure(map[string]interface{}{
		"strip_quoted_text": true,
	})
	require.NoError(t, err)

	transformedItems, err := cleanupTransformer.Transform([]models.FullItem{models.AsFullItem(item)})
	require.NoError(t, err)
	require.Len(t, transformedItems, 1)

	transformedItem := transformedItems[0]
	assert.NotContains(t, transformedItem.GetContent(), ">", "Quoted text should be stripped by transformer")

	assert.Equal(t, "<reply005@company.com>", item.Metadata["message_id"])
}

func TestGmailConverterPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	testEmail, err := testdata.LoadTestEmail("with_attachments")
	require.NoError(t, err)

	config := models.GmailSourceConfig{
		ProcessHTMLContent: true,
		ExtractLinks:       true,
		ExtractRecipients:  true,
		StripQuotedText:    true,
		TaggingRules: []models.TaggingRule{
			{Condition: "has:attachment", Tags: []string{"files"}},
			{Condition: "from:company.com", Tags: []string{"internal"}},
		},
	}

	for i := 0; i < 1000; i++ {
		_, err := FromGmailMessage(testEmail, config)
		require.NoError(t, err)
	}
}

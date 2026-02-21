package gmail

import (
	"fmt"
	"log/slog"
	"net/mail"
	"sort"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
)

// threadIDPrefix is the prefix used when constructing item IDs from Gmail thread IDs.
const threadIDPrefix = "thread_"

// EmailRecipient represents an email recipient with name and email.
type EmailRecipient struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// FromGmailMessage converts a Gmail message to the universal Item format.
func FromGmailMessage(msg *gmail.Message, config models.GmailSourceConfig) (*models.Item, error) {
	return FromGmailMessageWithService(msg, config, nil)
}

// FromGmailMessageWithService converts a Gmail message to the universal Item format
// with optional service for attachments.
func FromGmailMessageWithService(
	msg *gmail.Message,
	config models.GmailSourceConfig,
	service *Service,
) (*models.Item, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}

	// Extract basic information
	subject := getSubject(msg)

	content, err := getProcessedBody(msg, config)
	if err != nil {
		return nil, fmt.Errorf("failed to process email body: %w", err)
	}

	createdAt, err := getDate(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse email date: %w", err)
	}

	// Build the universal item
	item := &models.Item{
		ID:         msg.Id,
		Title:      subject,
		Content:    content,
		SourceType: "gmail",
		ItemType:   "email",
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt, // Gmail doesn't track modifications, use creation date
		Metadata:   make(map[string]interface{}),
		Tags:       buildTags(msg, config),
	}

	// Extract comprehensive metadata
	addBasicMetadata(item, msg)

	// Add recipient information if enabled
	if config.ExtractRecipients {
		addRecipientMetadata(item, msg)
	}

	// Add header information if enabled
	if config.IncludeFullHeaders {
		addHeaderMetadata(item, msg)
	}

	// Links extraction is now handled by LinkExtractionTransformer

	// Process attachments
	if config.DownloadAttachments {
		var processor *ContentProcessor
		if service != nil {
			processor = NewContentProcessorWithService(config, service)
		} else {
			processor = NewContentProcessor(config)
		}

		item.Attachments = processor.ProcessEmailAttachments(msg)
	}

	return item, nil
}

// getSubject extracts the subject from Gmail message headers.
func getSubject(msg *gmail.Message) string {
	if msg.Payload == nil {
		return ""
	}

	for _, header := range msg.Payload.Headers {
		if strings.ToLower(header.Name) == "subject" {
			return header.Value
		}
	}

	return ""
}

// getDate extracts and parses the date from Gmail message.
func getDate(msg *gmail.Message) (time.Time, error) {
	if msg.Payload != nil {
		if date, err := parseDateFromHeaders(msg.Payload.Headers); err == nil {
			return date, nil
		}
	}

	// Fallback to internal date (timestamp in milliseconds)
	if msg.InternalDate > 0 {
		return time.Unix(msg.InternalDate/1000, (msg.InternalDate%1000)*1000000), nil
	}

	return time.Now(), fmt.Errorf("could not parse date from message")
}

func parseDateFromHeaders(headers []*gmail.MessagePartHeader) (time.Time, error) {
	for _, header := range headers {
		if strings.ToLower(header.Name) == "date" {
			// Try parsing with multiple formats
			formats := []string{
				time.RFC1123Z,
				time.RFC1123,
				"Mon, 2 Jan 2006 15:04:05 -0700",
				"2 Jan 2006 15:04:05 -0700",
				"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
			}
			for _, format := range formats {
				if date, err := time.Parse(format, header.Value); err == nil {
					return date, nil
				}
			}
		}
	}

	return time.Time{}, fmt.Errorf("date header not found or could not be parsed")
}

// getProcessedBody extracts and processes the email body based on configuration.
func getProcessedBody(msg *gmail.Message, config models.GmailSourceConfig) (string, error) {
	processor := NewContentProcessor(config)

	return processor.ProcessEmailBody(msg)
}

// addBasicMetadata adds basic email metadata to the item.
func addBasicMetadata(item *models.Item, msg *gmail.Message) {
	item.Metadata["message_id"] = getHeader(msg, "message-id")
	item.Metadata["thread_id"] = msg.ThreadId
	item.Metadata["labels"] = msg.LabelIds
	item.Metadata["snippet"] = msg.Snippet
	item.Metadata["size"] = msg.SizeEstimate

	// Add reply-to if present
	if replyTo := getHeader(msg, "reply-to"); replyTo != "" {
		item.Metadata["reply_to"] = replyTo
	}
}

// addRecipientMetadata extracts and adds recipient information to metadata.
func addRecipientMetadata(item *models.Item, msg *gmail.Message) {
	item.Metadata["from"] = extractSender(msg)
	item.Metadata["to"] = extractRecipients(msg, "to")
	item.Metadata["cc"] = extractRecipients(msg, "cc")
	item.Metadata["bcc"] = extractRecipients(msg, "bcc")
}

// addHeaderMetadata adds all email headers to metadata if enabled.
func addHeaderMetadata(item *models.Item, msg *gmail.Message) {
	if msg.Payload == nil {
		return
	}

	headers := make(map[string]string)
	for _, header := range msg.Payload.Headers {
		headers[strings.ToLower(header.Name)] = header.Value
	}

	item.Metadata["headers"] = headers
}

// extractSender extracts the sender information.
func extractSender(msg *gmail.Message) EmailRecipient {
	from := getHeader(msg, "from")

	return parseEmailAddress(from)
}

// extractRecipients extracts recipients for the specified field (to, cc, bcc).
func extractRecipients(msg *gmail.Message, field string) []EmailRecipient {
	headerValue := getHeader(msg, field)
	if headerValue == "" {
		return []EmailRecipient{}
	}

	return parseEmailAddressList(headerValue)
}

// getHeader gets a header value by name (case-insensitive).
func getHeader(msg *gmail.Message, name string) string {
	if msg.Payload == nil {
		return ""
	}

	name = strings.ToLower(name)
	for _, header := range msg.Payload.Headers {
		if strings.ToLower(header.Name) == name {
			return header.Value
		}
	}

	return ""
}

// parseEmailAddress parses a single email address with optional name using net/mail.
func parseEmailAddress(addr string) EmailRecipient {
	if addr == "" {
		return EmailRecipient{}
	}

	parsed, err := mail.ParseAddress(strings.TrimSpace(addr))
	if err != nil {
		// Fallback for malformed addresses - just use the input as email.
		return EmailRecipient{
			Name:  "",
			Email: strings.TrimSpace(addr),
		}
	}

	return EmailRecipient{
		Name:  parsed.Name,
		Email: parsed.Address,
	}
}

// parseEmailAddressList parses a comma-separated list of email addresses using net/mail.
func parseEmailAddressList(addressList string) []EmailRecipient {
	if addressList == "" {
		return []EmailRecipient{}
	}

	parsed, err := mail.ParseAddressList(addressList)
	if err != nil {
		// Fallback to manual parsing if standard library fails.
		return parseEmailAddressListFallback(addressList)
	}

	recipients := make([]EmailRecipient, 0, len(parsed))
	for _, addr := range parsed {
		recipients = append(recipients, EmailRecipient{
			Name:  addr.Name,
			Email: addr.Address,
		})
	}

	return recipients
}

// parseEmailAddressListFallback parses email addresses manually when net/mail fails.
func parseEmailAddressListFallback(addressList string) []EmailRecipient {
	// Split by comma, but be careful about commas inside quoted names.
	addresses := splitEmailAddresses(addressList)
	recipients := make([]EmailRecipient, 0, len(addresses))

	for _, addr := range addresses {
		if recipient := parseEmailAddress(addr); recipient.Email != "" {
			recipients = append(recipients, recipient)
		}
	}

	return recipients
}

// splitEmailAddresses splits email addresses handling quoted names with commas.
func splitEmailAddresses(addressList string) []string {
	var (
		addresses []string
		current   strings.Builder
	)

	inQuotes := false
	inAngleBrackets := false

	for _, char := range addressList {
		switch char {
		case '"':
			inQuotes = !inQuotes

			current.WriteRune(char)
		case '<':
			inAngleBrackets = true

			current.WriteRune(char)
		case '>':
			inAngleBrackets = false

			current.WriteRune(char)
		case ',':
			if !inQuotes && !inAngleBrackets {
				// This comma is a separator.
				if addr := strings.TrimSpace(current.String()); addr != "" {
					addresses = append(addresses, addr)
				}

				current.Reset()
			} else {
				current.WriteRune(char)
			}
		default:
			current.WriteRune(char)
		}
	}

	// Add the last address.
	if addr := strings.TrimSpace(current.String()); addr != "" {
		addresses = append(addresses, addr)
	}

	return addresses
}

// buildTags builds tags for the email based on configuration and message properties.
func buildTags(msg *gmail.Message, config models.GmailSourceConfig) []string {
	var tags []string

	// Add source identifier.
	tags = append(tags, "gmail")

	// Add labels as tags.
	for _, labelID := range msg.LabelIds {
		// Convert system labels to readable tags.
		switch labelID {
		case "IMPORTANT":
			tags = append(tags, "important")
		case "STARRED":
			tags = append(tags, "starred")
		case "UNREAD":
			tags = append(tags, "unread")
		case "INBOX":
			tags = append(tags, "inbox")
		case "SENT":
			tags = append(tags, "sent")
		case "DRAFT":
			tags = append(tags, "draft")
		default:
			// Use label as-is for custom labels.
			tags = append(tags, labelID)
		}
	}

	// Apply custom tagging rules.
	for _, rule := range config.TaggingRules {
		if matchesCondition(msg, rule.Condition) {
			tags = append(tags, rule.Tags...)
		}
	}

	// Add instance name as tag if specified.
	if config.Name != "" {
		tags = append(tags, "source:"+strings.ToLower(strings.ReplaceAll(config.Name, " ", "-")))
	}

	return tags
}

// matchesCondition checks if a message matches a tagging rule condition.
func matchesCondition(msg *gmail.Message, condition string) bool {
	// Simple condition matching - could be enhanced.
	condition = strings.ToLower(condition)

	if strings.HasPrefix(condition, "from:") {
		fromEmail := getHeader(msg, "from")
		targetEmail := strings.TrimPrefix(condition, "from:")

		return strings.Contains(strings.ToLower(fromEmail), targetEmail)
	}

	if strings.HasPrefix(condition, "subject:") {
		subject := getSubject(msg)
		targetSubject := strings.TrimPrefix(condition, "subject:")

		return strings.Contains(strings.ToLower(subject), targetSubject)
	}

	if condition == "has:attachment" {
		return hasAttachments(msg)
	}

	if strings.HasPrefix(condition, "label:") {
		targetLabel := strings.TrimPrefix(condition, "label:")
		for _, label := range msg.LabelIds {
			if strings.ToLower(label) == targetLabel {
				return true
			}
		}
	}

	return false
}

// hasAttachments checks if a message has attachments.
func hasAttachments(msg *gmail.Message) bool {
	if msg.Payload == nil {
		return false
	}

	return hasAttachmentsInPart(msg.Payload)
}

// FromGmailThread converts a Gmail thread to the universal Item format.
// It aggregates all messages in the thread chronologically into a single item.
func FromGmailThread(thread *gmail.Thread, config models.GmailSourceConfig, service *Service) (*models.Item, error) {
	if thread == nil {
		return nil, fmt.Errorf("thread is nil")
	}

	if len(thread.Messages) == 0 {
		return nil, fmt.Errorf("thread %s has no messages", thread.Id)
	}

	// Sort messages chronologically by internal date.
	messages := make([]*gmail.Message, len(thread.Messages))
	copy(messages, thread.Messages)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].InternalDate < messages[j].InternalDate
	})

	firstMsg := messages[0]
	subject := getSubject(firstMsg)

	createdAt, err := getDate(firstMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse thread start date: %w", err)
	}

	lastMsg := messages[len(messages)-1]

	updatedAt, err := getDate(lastMsg)
	if err != nil {
		updatedAt = createdAt
	}

	// Build aggregated content from all messages.
	var contentBuilder strings.Builder

	for i, msg := range messages {
		processor := NewContentProcessor(config)

		msgContent, err := processor.ProcessEmailBody(msg)
		if err != nil {
			slog.Warn("Failed to process message body in thread", "thread_id", thread.Id, "message_id", msg.Id, "error", err)
			msgContent = msg.Snippet
		}

		if i > 0 {
			contentBuilder.WriteString("\n\n---\n\n")
		}

		msgDate, _ := getDate(msg)
		contentBuilder.WriteString(fmt.Sprintf("**From:** %s  \n", getHeader(msg, "from")))
		contentBuilder.WriteString(fmt.Sprintf("**Date:** %s  \n\n", msgDate.Format("2006-01-02 15:04:05")))
		contentBuilder.WriteString(msgContent)
	}

	// Collect all labels across messages (deduplicated).
	labelSet := make(map[string]bool)

	for _, msg := range messages {
		for _, label := range msg.LabelIds {
			labelSet[label] = true
		}
	}

	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}

	item := &models.Item{
		ID:         threadIDPrefix + thread.Id,
		Title:      subject,
		Content:    contentBuilder.String(),
		SourceType: "gmail",
		ItemType:   "email_thread",
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Metadata:   make(map[string]interface{}),
		Tags:       buildThreadItemTags(firstMsg, config, len(messages)),
	}

	item.Metadata["thread_id"] = thread.Id
	item.Metadata["message_count"] = len(messages)
	item.Metadata["labels"] = labels
	item.Metadata["snippet"] = thread.Snippet

	// Process attachments if enabled.
	if config.DownloadAttachments {
		var processor *ContentProcessor
		if service != nil {
			processor = NewContentProcessorWithService(config, service)
		} else {
			processor = NewContentProcessor(config)
		}

		item.Attachments = processor.ProcessThreadAttachments(thread)
	}

	return item, nil
}

// buildThreadItemTags builds tags for a thread item, reusing per-message tag logic.
func buildThreadItemTags(firstMsg *gmail.Message, config models.GmailSourceConfig, messageCount int) []string {
	tags := buildTags(firstMsg, config)
	tags = append(tags, "thread")

	if messageCount > 5 {
		tags = append(tags, "long-thread")
	}

	return tags
}

// hasAttachmentsInPart recursively checks for attachments in message parts.
func hasAttachmentsInPart(part *gmail.MessagePart) bool {
	if part == nil {
		return false
	}

	// Check if this part is an attachment.
	if part.Filename != "" && part.Body != nil && part.Body.AttachmentId != "" {
		return true
	}

	// Recursively check parts.
	for _, subPart := range part.Parts {
		if hasAttachmentsInPart(subPart) {
			return true
		}
	}

	return false
}

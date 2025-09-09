package gmail

import (
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"pkm-sync/pkg/models"

	"google.golang.org/api/gmail/v1"
)

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

// FromGmailThread converts a Gmail thread to a unified Item.
func FromGmailThread(thread *gmail.Thread, config models.GmailSourceConfig, service *Service) (*models.Item, error) {
	if thread == nil {
		return nil, fmt.Errorf("thread is nil")
	}

	if thread.Messages == nil || len(thread.Messages) == 0 {
		return nil, fmt.Errorf("thread %s contains no messages", thread.Id)
	}

	// Use the first message for basic item properties
	firstMessage := thread.Messages[0]

	// Extract thread subject (cleaned from first message)
	subject := getSubject(firstMessage)
	subject = cleanThreadSubject(subject)

	// Aggregate content from all messages in chronological order
	content, err := aggregateThreadContent(thread, config)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate thread content: %w", err)
	}

	// Get thread timing from first and last messages
	threadStart, err := getDate(firstMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to parse thread start date: %w", err)
	}

	lastMessage := thread.Messages[len(thread.Messages)-1]

	threadEnd, err := getDate(lastMessage)
	if err != nil {
		threadEnd = threadStart // Fallback to start time
	}

	// Build the universal item for the thread
	item := &models.Item{
		ID:         thread.Id,
		Title:      subject,
		Content:    content,
		SourceType: "gmail",
		ItemType:   "thread",
		CreatedAt:  threadStart,
		UpdatedAt:  threadEnd,
		Metadata:   make(map[string]interface{}),
		Tags:       buildThreadTags(thread, config),
	}

	// Add thread-specific metadata
	addThreadMetadata(item, thread, config)

	// Add aggregated recipient information if enabled
	if config.ExtractRecipients {
		addThreadRecipientMetadata(item, thread)
	}

	// Add aggregated header information if enabled
	if config.IncludeFullHeaders {
		addThreadHeaderMetadata(item, thread)
	}

	// Process attachments from all messages in thread
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

// cleanThreadSubject removes common email prefixes from thread subjects.
func cleanThreadSubject(subject string) string {
	subject = strings.TrimSpace(subject)

	// Remove common prefixes
	prefixes := []string{"Re: ", "RE: ", "Fwd: ", "FWD: ", "Fw: ", "FW: "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(subject, prefix) {
			subject = strings.TrimSpace(subject[len(prefix):])

			break // Only remove one prefix
		}
	}

	return subject
}

// aggregateThreadContent combines all messages in a thread chronologically.
func aggregateThreadContent(thread *gmail.Thread, config models.GmailSourceConfig) (string, error) {
	if len(thread.Messages) == 0 {
		return "", fmt.Errorf("no messages in thread")
	}

	// Sort messages by internal date (chronological order)
	sortedMessages := make([]*gmail.Message, len(thread.Messages))
	copy(sortedMessages, thread.Messages)

	sort.Slice(sortedMessages, func(i, j int) bool {
		dateI, _ := getDate(sortedMessages[i])
		dateJ, _ := getDate(sortedMessages[j])

		return dateI.Before(dateJ)
	})

	contentParts := make([]string, 0, len(sortedMessages))

	for i, message := range sortedMessages {
		// Get message header info
		from := extractSender(message)
		date, _ := getDate(message)

		// Format message header
		messageHeader := fmt.Sprintf("From: %s | Date: %s", from, date.Format("2006-01-02 15:04"))

		// Get processed message content
		messageContent, err := getProcessedBody(message, config)
		if err != nil {
			// Log warning but continue with other messages
			messageContent = fmt.Sprintf("[Error processing message content: %v]", err)
		}

		// Combine header and content
		var messagePart string
		if i == 0 {
			// First message - no separator above
			messagePart = fmt.Sprintf("%s\n\n%s", messageHeader, messageContent)
		} else {
			// Subsequent messages - add separator
			messagePart = fmt.Sprintf("---\n\n%s\n\n%s", messageHeader, messageContent)
		}

		contentParts = append(contentParts, messagePart)
	}

	return strings.Join(contentParts, "\n\n"), nil
}

// addThreadMetadata adds thread-specific metadata to the item.
func addThreadMetadata(item *models.Item, thread *gmail.Thread, config models.GmailSourceConfig) {
	// Basic thread information
	item.Metadata["thread_id"] = thread.Id
	item.Metadata["message_count"] = len(thread.Messages)

	// Thread timing
	if len(thread.Messages) > 0 {
		firstMessage := thread.Messages[0]
		lastMessage := thread.Messages[len(thread.Messages)-1]

		if threadStart, err := getDate(firstMessage); err == nil {
			item.Metadata["thread_start"] = threadStart
		}

		if threadEnd, err := getDate(lastMessage); err == nil {
			item.Metadata["thread_end"] = threadEnd
		}
	}

	// Snippet from Gmail (if available)
	if thread.Snippet != "" {
		item.Metadata["snippet"] = thread.Snippet
	}

	// History ID for change tracking
	if thread.HistoryId != 0 {
		item.Metadata["history_id"] = thread.HistoryId
	}

	// Aggregate labels from all messages
	labelSet := make(map[string]bool)

	for _, message := range thread.Messages {
		for _, labelId := range message.LabelIds {
			labelSet[labelId] = true
		}
	}

	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}

	item.Metadata["labels"] = labels

	// Thread statistics
	item.Metadata["has_attachments"] = threadHasAttachments(thread)
	item.Metadata["unique_senders"] = countUniqueSenders(thread)
}

// addThreadRecipientMetadata aggregates recipient information from all messages in thread.
func addThreadRecipientMetadata(item *models.Item, thread *gmail.Thread) {
	participantSet := make(map[string]bool)

	var allRecipients []EmailRecipient

	for _, message := range thread.Messages {
		// Add sender
		sender := extractSender(message)
		if sender.Email != "" {
			participantSet[sender.Email] = true
		}

		// Add all recipients from this message
		toRecipients := extractRecipients(message, "to")
		ccRecipients := extractRecipients(message, "cc")
		bccRecipients := extractRecipients(message, "bcc")

		allMessageRecipients := append(toRecipients, ccRecipients...)
		allMessageRecipients = append(allMessageRecipients, bccRecipients...)

		for _, recipient := range allMessageRecipients {
			if recipient.Email != "" {
				participantSet[recipient.Email] = true
				allRecipients = append(allRecipients, recipient)
			}
		}
	}

	// Convert to slice for metadata
	participants := make([]string, 0, len(participantSet))
	for participant := range participantSet {
		participants = append(participants, participant)
	}

	item.Metadata["participants"] = participants
	item.Metadata["all_recipients"] = allRecipients
	item.Metadata["participant_count"] = len(participants)
}

// addThreadHeaderMetadata aggregates header information from all messages if enabled.
func addThreadHeaderMetadata(item *models.Item, thread *gmail.Thread) {
	var allHeaders []map[string]string

	for _, message := range thread.Messages {
		if message.Payload != nil && message.Payload.Headers != nil {
			messageHeaders := make(map[string]string)
			messageHeaders["message_id"] = message.Id

			for _, header := range message.Payload.Headers {
				messageHeaders[strings.ToLower(header.Name)] = header.Value
			}

			allHeaders = append(allHeaders, messageHeaders)
		}
	}

	item.Metadata["all_headers"] = allHeaders
}

// buildThreadTags builds tags for a thread based on configuration rules.
func buildThreadTags(thread *gmail.Thread, config models.GmailSourceConfig) []string {
	var tags []string

	// Use first message for tag building (most thread rules apply to the thread as a whole)
	if len(thread.Messages) > 0 {
		tags = buildTags(thread.Messages[0], config)
	}

	// Add thread-specific tags
	tags = append(tags, "thread")

	if len(thread.Messages) > 5 {
		tags = append(tags, "long-thread")
	}

	if threadHasAttachments(thread) {
		tags = append(tags, "has-attachments")
	}

	return tags
}

// threadHasAttachments checks if any message in the thread has attachments.
func threadHasAttachments(thread *gmail.Thread) bool {
	for _, message := range thread.Messages {
		if hasAttachments(message) {
			return true
		}
	}

	return false
}

// countUniqueSenders counts the number of unique senders in a thread.
func countUniqueSenders(thread *gmail.Thread) int {
	senderSet := make(map[string]bool)

	for _, message := range thread.Messages {
		sender := extractSender(message)
		if sender.Email != "" {
			senderSet[sender.Email] = true
		}
	}

	return len(senderSet)
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

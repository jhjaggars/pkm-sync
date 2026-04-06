package servicenow

import (
	"fmt"
	"strings"
	"time"

	"pkm-sync/pkg/models"
)

// servicenowTimeLayout is the timestamp format used by the ServiceNow REST API
// when sysparm_display_value=true is set.
const servicenowTimeLayout = "2006-01-02 15:04:05"

// recordToItem converts a ServiceNow table record (map from JSON) to a BasicItem.
// Title is set to the ticket number (e.g. "RITM2300969") so the output filename
// is "RITM2300969.md", matching the Obsidian vault convention.
func recordToItem(record map[string]any, table, instanceURL string) models.FullItem {
	sysID := stringField(record, "sys_id")
	number := stringField(record, "number")

	if number == "" {
		number = sysID
	}

	item := &models.BasicItem{
		ID:         fmt.Sprintf("servicenow_%s_%s", table, sysID),
		Title:      number,
		SourceType: "servicenow",
		ItemType:   table,
		Tags:       make([]string, 0),
		Metadata:   make(map[string]any),
		Links:      make([]models.Link, 0),
	}

	// Parse timestamps.
	item.CreatedAt = parseServiceNowTime(stringField(record, "opened_at"))
	item.UpdatedAt = parseServiceNowTime(stringField(record, "sys_updated_on"))

	// Build content.
	shortDesc := stringField(record, "short_description")
	description := stringField(record, "description")
	comments := stringField(record, "comments")

	var sb strings.Builder

	if shortDesc != "" {
		sb.WriteString(shortDesc)
		sb.WriteString("\n\n")
	}

	if description != "" && description != shortDesc {
		sb.WriteString(description)
		sb.WriteString("\n\n")
	}

	if comments != "" {
		sb.WriteString("## Comments\n\n")
		sb.WriteString(comments)
	}

	item.Content = strings.TrimSpace(sb.String())

	// Build tags.
	state := resolveDisplayValue(record, "state")
	priority := resolveDisplayValue(record, "priority")

	if state != "" {
		item.Tags = append(item.Tags, "state:"+normalizeTag(state))
	}

	if priority != "" {
		item.Tags = append(item.Tags, "priority:"+normalizeTag(priority))
	}

	item.Tags = append(item.Tags, "table:"+table)

	// Build metadata.
	meta := map[string]any{
		"summary":          shortDesc,
		"number":           number,
		"state":            state,
		"priority":         priority,
		"table":            table,
		"assignment_group": resolveDisplayValue(record, "assignment_group"),
		"assigned_to":      resolveDisplayValue(record, "assigned_to"),
		"opened_by":        resolveDisplayValue(record, "opened_by"),
		"sys_id":           sysID,
	}

	// Table-specific metadata.
	if catItem := resolveDisplayValue(record, "cat_item"); catItem != "" {
		meta["cat_item"] = catItem
	}

	if approval := stringField(record, "approval"); approval != "" {
		meta["approval"] = approval
	}

	item.Metadata = meta

	// Set browse URL.
	if instanceURL != "" && sysID != "" {
		browseURL := fmt.Sprintf("%s/nav_to.do?uri=/%s.do?sys_id=%s",
			strings.TrimRight(instanceURL, "/"), table, sysID)
		item.Links = append(item.Links, models.Link{
			URL:   browseURL,
			Title: number,
			Type:  "external",
		})
	}

	return item
}

// stringField extracts a string value from a record field.
// ServiceNow returns display values as plain strings when sysparm_display_value=true.
func stringField(record map[string]any, key string) string {
	v, ok := record[key]
	if !ok || v == nil {
		return ""
	}

	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		// Reference field with display_value/link structure.
		if dv, ok := val["display_value"].(string); ok {
			return dv
		}
	}

	return fmt.Sprintf("%v", v)
}

// resolveDisplayValue extracts the display_value from a reference field,
// or falls back to a plain string.
func resolveDisplayValue(record map[string]any, key string) string {
	v, ok := record[key]
	if !ok || v == nil {
		return ""
	}

	if m, ok := v.(map[string]any); ok {
		if dv, ok := m["display_value"].(string); ok {
			return dv
		}
	}

	if s, ok := v.(string); ok {
		return s
	}

	return ""
}

// parseServiceNowTime parses a ServiceNow timestamp.
func parseServiceNowTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	if t, err := time.ParseInLocation(servicenowTimeLayout, s, time.UTC); err == nil {
		return t
	}

	// Fallback to RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}

	return time.Time{}
}

// normalizeTag converts a display value to a lowercase tag-safe string.
func normalizeTag(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", "-"))
}

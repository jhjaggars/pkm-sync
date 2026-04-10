// Package routing provides argument parsing utilities for verb-centric CLI commands.
package routing

import "strings"

// ParsedIdentifier is the result of parsing a `fetch` or `search` argument.
// The argument can be a bare URL, a source-qualified key ("jira/PROJ-123"), or
// a bare key (requires --source to disambiguate).
type ParsedIdentifier struct {
	// Raw is the original unparsed argument.
	Raw string

	// IsURL is true when the argument is an HTTP(S) URL (or the key part of a
	// source-prefixed argument is a URL).
	IsURL bool

	// URL holds the URL string when IsURL is true.
	URL string

	// SourceType is the source type parsed from the prefix (e.g. "drive",
	// "gmail", "jira"). Empty when the argument is a bare URL or a bare key
	// without a prefix.
	SourceType string

	// Key is the source-native identifier after the source-type prefix. For bare
	// URLs this is empty; use URL instead. For bare keys this equals Raw.
	Key string
}

// Parse parses a fetch/search argument into its components.
//
// Parsing rules (checked in order):
//  1. If raw starts with "http://" or "https://" → bare URL, IsURL=true.
//  2. If raw contains "/" → split on the first "/". Left side is SourceType,
//     right side is Key. If Key looks like a URL ("http…"), IsURL=true and URL
//     is set to the Key portion (the source-type prefix is retained as context).
//  3. Otherwise → bare key, SourceType and URL are empty.
func Parse(raw string) ParsedIdentifier {
	id := ParsedIdentifier{Raw: raw}

	// Rule 1: bare URL
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		id.IsURL = true
		id.URL = raw

		return id
	}

	// Rule 2: source-type/key prefix
	if idx := strings.Index(raw, "/"); idx >= 0 {
		id.SourceType = raw[:idx]
		id.Key = raw[idx+1:]

		// Key might itself be a URL (e.g. "drive/https://docs.google.com/...")
		if strings.HasPrefix(id.Key, "http://") || strings.HasPrefix(id.Key, "https://") {
			id.IsURL = true
			id.URL = id.Key
		}

		return id
	}

	// Rule 3: bare key — leave SourceType and URL empty
	id.Key = raw

	return id
}

// sourceTypeAliases maps common short names to canonical source type strings
// as used in config (e.g. "google_drive", "gmail", etc.).
var sourceTypeAliases = map[string]string{
	"drive":      "google_drive",
	"calendar":   "google_calendar",
	"cal":        "google_calendar",
	"gmail":      "gmail",
	"jira":       "jira",
	"slack":      "slack",
	"snow":       "servicenow",
	"servicenow": "servicenow",
}

// CanonicalSourceType converts a short alias (e.g. "drive") to the canonical
// config source type string (e.g. "google_drive"). Returns the input unchanged
// if no alias is found.
func CanonicalSourceType(alias string) string {
	if canonical, ok := sourceTypeAliases[strings.ToLower(alias)]; ok {
		return canonical
	}

	return alias
}

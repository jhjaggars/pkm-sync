package main

import (
	"fmt"
	"strings"

	"pkm-sync/pkg/models"
)

// findSourceByType searches the config for an enabled source matching
// canonicalType (e.g. "google_drive", "jira"). If sourceName is non-empty it
// must match exactly. Returns the source name and config on success.
//
// For sources that work without a config entry (Google Drive via OAuth), the
// caller should fall back to a direct auth flow when this returns an error.
func findSourceByType(cfg *models.Config, canonicalType, sourceName string) (string, models.SourceConfig, error) {
	if sourceName != "" {
		sc, ok := cfg.Sources[sourceName]
		if !ok {
			return "", models.SourceConfig{}, fmt.Errorf("source %q not found in config", sourceName)
		}

		if sc.Type != canonicalType {
			return "", models.SourceConfig{}, fmt.Errorf("source %q has type %q, expected %q", sourceName, sc.Type, canonicalType)
		}

		return sourceName, sc, nil
	}

	var matches []string

	for name, sc := range cfg.Sources {
		if sc.Type == canonicalType && sc.Enabled {
			matches = append(matches, name)
		}
	}

	switch len(matches) {
	case 0:
		return "", models.SourceConfig{}, fmt.Errorf("no enabled %q source found in config", canonicalType)
	case 1:
		return matches[0], cfg.Sources[matches[0]], nil
	default:
		return "", models.SourceConfig{}, fmt.Errorf(
			"multiple %q sources configured, use --source to specify one: %s",
			canonicalType, strings.Join(matches, ", "),
		)
	}
}

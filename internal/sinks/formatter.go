package sinks

import (
	"fmt"

	"pkm-sync/pkg/models"
)

// formatter is an unexported interface for PKM-specific formatting differences.
// Implementations live in this package and are created via newFormatter.
type formatter interface {
	name() string
	configure(config map[string]any)
	formatContent(item models.FullItem) string
	formatFilename(title string) string
	fileExtension() string
	formatMetadata(metadata map[string]any) string
}

// newFormatter creates the named formatter ("obsidian" or "logseq").
func newFormatter(n string) (formatter, error) {
	switch n {
	case "obsidian":
		return newObsidianFormatter(), nil
	case "logseq":
		return newLogseqFormatter(), nil
	default:
		return nil, fmt.Errorf("unknown formatter '%s': supported formatters are 'obsidian' and 'logseq'", n)
	}
}

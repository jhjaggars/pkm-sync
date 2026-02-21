package sinks

import (
	"context"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// FileSink writes items to a file-based target (Obsidian, Logseq).
// It wraps a Target with an output directory, implementing the general Sink interface.
type FileSink struct {
	target    interfaces.Target
	outputDir string
}

// NewFileSink creates a FileSink for the given target and output directory.
func NewFileSink(target interfaces.Target, outputDir string) *FileSink {
	return &FileSink{
		target:    target,
		outputDir: outputDir,
	}
}

// Name returns the name of the underlying target.
func (s *FileSink) Name() string {
	return s.target.Name()
}

// Write exports items to the file-based target.
func (s *FileSink) Write(_ context.Context, items []models.FullItem) error {
	return s.target.Export(items, s.outputDir)
}

// Target returns the underlying Target (for dry-run preview support).
func (s *FileSink) Target() interfaces.Target {
	return s.target
}

// OutputDir returns the configured output directory.
func (s *FileSink) OutputDir() string {
	return s.outputDir
}

// Ensure FileSink implements Sink.
var _ interfaces.Sink = (*FileSink)(nil)

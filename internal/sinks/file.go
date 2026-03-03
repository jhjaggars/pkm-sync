package sinks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time" //nolint:gci

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// FileSink writes items to the file system using a PKM-specific formatter
// (Obsidian or Logseq). It implements the Sink interface.
type FileSink struct {
	fmt       formatter
	outputDir string
}

// NewFileSink creates a FileSink for the given formatter name and output directory.
// config is passed to the underlying formatter (may be nil).
func NewFileSink(formatterName string, outputDir string, config map[string]any) (*FileSink, error) {
	f, err := newFormatter(formatterName)
	if err != nil {
		return nil, err
	}

	f.configure(config)

	return &FileSink{fmt: f, outputDir: outputDir}, nil
}

// Name returns the name of the underlying formatter.
func (s *FileSink) Name() string {
	return s.fmt.name()
}

// Write exports items to the file system.
func (s *FileSink) Write(_ context.Context, items []models.FullItem) error {
	for _, item := range items {
		if err := s.writeItem(item); err != nil {
			return fmt.Errorf("failed to write item %s: %w", item.GetID(), err)
		}
	}

	return nil
}

func (s *FileSink) writeItem(item models.FullItem) error {
	filename := s.fmt.formatFilename(item.GetTitle())
	filePath := filepath.Join(s.outputDir, dateSubdirForItem(item), filename)

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	content := s.fmt.formatContent(item)

	return os.WriteFile(filePath, []byte(content), 0644)
}

// Preview generates a description of what files would be created/modified
// without actually writing them.
func (s *FileSink) Preview(items []models.FullItem) ([]*interfaces.FilePreview, error) {
	previews := make([]*interfaces.FilePreview, 0, len(items))

	for _, item := range items {
		filename := s.fmt.formatFilename(item.GetTitle())
		filePath := filepath.Join(s.outputDir, dateSubdirForItem(item), filename)
		content := s.fmt.formatContent(item)

		action, existingContent, err := logseqDetermineFileAction(filePath, content)
		if err != nil {
			return nil, fmt.Errorf("could not determine action for %s: %w", filePath, err)
		}

		conflict := action == "update"

		previews = append(previews, &interfaces.FilePreview{
			FilePath:        filePath,
			Action:          action,
			Content:         content,
			ExistingContent: existingContent,
			Conflict:        conflict,
		})
	}

	return previews, nil
}

// dateSubdirForItem returns a YYYY/MM-Month/DD-Weekday path component when the
// item has a parseable start_time metadata field (calendar events), and an
// empty string for all other items.
func dateSubdirForItem(item models.FullItem) string {
	meta := item.GetMetadata()
	if meta == nil {
		return ""
	}

	raw, ok := meta["start_time"]
	if !ok {
		return ""
	}

	var t time.Time

	switch v := raw.(type) {
	case time.Time:
		t = v
	case string:
		var err error

		for _, layout := range []string{
			"2006-01-02 15:04:05 -0700 MST",
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02",
		} {
			t, err = time.Parse(layout, v)
			if err == nil {
				break
			}
		}

		if t.IsZero() {
			return ""
		}
	default:
		return ""
	}

	return filepath.Join(
		t.Format("2006"),
		t.Format("01-January"),
		t.Format("02-Monday"),
	)
}

// Ensure FileSink implements Sink.
var _ interfaces.Sink = (*FileSink)(nil)

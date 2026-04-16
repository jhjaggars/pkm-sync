package sinks

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time" //nolint:gci

	"pkm-sync/internal/formatters"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// FileSink writes items to the file system using a PKM-specific formatter
// (Obsidian or Logseq). It implements the Sink interface.
type FileSink struct {
	fmt       formatter
	outputDir string

	// registry holds compiled template-based formatters (may be nil).
	registry *formatters.Registry
	// typeFormatters maps item type (e.g. "event") to a formatter name.
	typeFormatters map[string]string
	idIndex        map[string]string // id → existing file path
}

// NewFileSink creates a FileSink for the given formatter name and output directory.
// config is passed to the underlying formatter (may be nil).
func NewFileSink(formatterName string, outputDir string, config map[string]any) (*FileSink, error) {
	f, err := newFormatter(formatterName)
	if err != nil {
		return nil, err
	}

	f.configure(config)

	sink := &FileSink{fmt: f, outputDir: outputDir}
	sink.buildIDIndex()

	return sink, nil
}

// WithFormatters attaches a compiled formatter registry and a type-to-name
// mapping to the sink.  When an item's ItemType matches a key in typeMap the
// corresponding named formatter in reg is used for directory, filename and
// content rendering (falling back to the PKM-specific formatter for any field
// whose template is empty).
func (s *FileSink) WithFormatters(reg *formatters.Registry, typeMap map[string]string) {
	s.registry = reg
	s.typeFormatters = typeMap
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
	dir, filename, content, err := s.renderItem(item)
	if err != nil {
		return err
	}

	defaultPath := filepath.Join(s.outputDir, dir, filename)

	// Use existing path if a file with this ID was found during indexing.
	filePath := defaultPath
	if existing, ok := s.idIndex[item.GetID()]; ok {
		filePath = existing
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	// Skip writing if file content is unchanged to avoid bumping mtime.
	ondisk, err := os.ReadFile(filePath)
	if err == nil && string(ondisk) == content {
		slog.Debug("Skipping unchanged file", "path", filePath)

		return nil
	}

	return os.WriteFile(filePath, []byte(content), 0644)
}

// renderItem returns the (directory, filename, content) triple for an item.
// It applies a configured template formatter when one is registered for the
// item's type, falling back to the built-in PKM formatter for any field whose
// template is empty.
func (s *FileSink) renderItem(item models.FullItem) (dir, filename, content string, err error) {
	// Resolve the optional template formatter for this item type.
	var tf *formatters.TemplateFormatter

	if s.registry != nil && len(s.typeFormatters) > 0 {
		if fmtName, ok := s.typeFormatters[item.GetItemType()]; ok {
			var found bool

			tf, found = s.registry.Lookup(fmtName)
			if !found {
				slog.Warn("configured formatter not found; using default",
					"formatter", fmtName,
					"item_type", item.GetItemType(),
				)
			}
		}
	}

	// --- directory ---
	if tf != nil && tf.HasDirectoryPattern() {
		dir, err = tf.FormatDirectory(item)
		if err != nil {
			return "", "", "", fmt.Errorf("template formatter directory: %w", err)
		}
	} else {
		dir = dateSubdirForItem(item)
	}

	// --- filename ---
	if tf != nil && tf.HasFilenamePattern() {
		filename, err = tf.FormatFilename(item)
		if err != nil {
			return "", "", "", fmt.Errorf("template formatter filename: %w", err)
		}
		// Ensure the file extension is appended if not already present.
		if ext := s.fmt.fileExtension(); ext != "" && !hasExtension(filename, ext) {
			filename += ext
		}
	} else {
		filename = s.fmt.formatFilename(item.GetTitle())
	}

	// --- content ---
	if tf != nil && tf.HasContentTemplate() {
		content, err = tf.FormatContent(item)
		if err != nil {
			return "", "", "", fmt.Errorf("template formatter content: %w", err)
		}
	} else {
		content = s.fmt.formatContent(item)
	}

	return dir, filename, content, nil
}

// hasExtension reports whether filename already ends with ext (case-insensitive).
func hasExtension(filename, ext string) bool {
	if len(filename) < len(ext) {
		return false
	}

	suffix := filename[len(filename)-len(ext):]

	return strings.EqualFold(suffix, ext)
}

// buildIDIndex scans the output directory for existing markdown files and
// builds a map from frontmatter id values to file paths. This allows files
// that have been moved to subdirectories to be updated in place.
func (s *FileSink) buildIDIndex() {
	s.idIndex = make(map[string]string)

	err := filepath.Walk(s.outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		id := extractFrontmatterID(path)
		if id != "" {
			s.idIndex[id] = path
		}

		return nil
	})
	if err != nil {
		slog.Debug("Failed to build ID index", "dir", s.outputDir, "error", err)
	}

	if len(s.idIndex) > 0 {
		slog.Debug("Built file ID index", "dir", s.outputDir, "entries", len(s.idIndex))
	}
}

// extractFrontmatterID reads the first lines of a markdown file and returns
// the value of the "id:" frontmatter field, or empty string if not found.
func extractFrontmatterID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}

	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false

	for i := 0; i < 30 && scanner.Scan(); i++ {
		line := scanner.Text()
		if line == "---" {
			if inFrontmatter {
				return "" // end of frontmatter, no id found
			}

			inFrontmatter = true

			continue
		}

		if inFrontmatter && strings.HasPrefix(line, "id: ") {
			return strings.TrimPrefix(line, "id: ")
		}
	}

	return ""
}

// Preview generates a description of what files would be created/modified
// without actually writing them.
func (s *FileSink) Preview(items []models.FullItem) ([]*interfaces.FilePreview, error) {
	previews := make([]*interfaces.FilePreview, 0, len(items))

	for _, item := range items {
		dir, filename, content, err := s.renderItem(item)
		if err != nil {
			return nil, fmt.Errorf("failed to render item %s: %w", item.GetID(), err)
		}

		filePath := filepath.Join(s.outputDir, dir, filename)

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

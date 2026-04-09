// Package formatters provides configurable, Go-template-based formatters for
// PKM sink outputs.  Each formatter is defined in the configuration file and
// referenced by name from a target's Formatters map.
//
// Template data
//
// All three template kinds (directory, filename, content) receive the same
// [ItemData] struct as their dot value.  The following template functions are
// available:
//
//   - formatDate "layout"   – format a time.Time with the given Go layout
//   - sanitize              – sanitize a string for use in a filename
//   - truncate N            – truncate a string to at most N runes
package formatters

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"pkm-sync/internal/utils"
	"pkm-sync/pkg/models"
)

// ItemData is the template context passed to every formatter template.
type ItemData struct {
	ID          string
	Title       string
	Content     string
	SourceType  string
	ItemType    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Tags        []string
	Attachments []models.Attachment
	Metadata    map[string]interface{}
	Links       []models.Link
}

// itemDataFromFullItem converts a FullItem into an ItemData for template rendering.
func itemDataFromFullItem(item models.FullItem) ItemData {
	return ItemData{
		ID:          item.GetID(),
		Title:       item.GetTitle(),
		Content:     item.GetContent(),
		SourceType:  item.GetSourceType(),
		ItemType:    item.GetItemType(),
		CreatedAt:   item.GetCreatedAt(),
		UpdatedAt:   item.GetUpdatedAt(),
		Tags:        item.GetTags(),
		Attachments: item.GetAttachments(),
		Metadata:    item.GetMetadata(),
		Links:       item.GetLinks(),
	}
}

// templateFuncs returns the template.FuncMap available to all formatter templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// formatDate formats a time.Time value with the given Go time layout.
		// Usage: {{.CreatedAt | formatDate "2006-01-02"}}
		"formatDate": func(layout string, t time.Time) string {
			return t.Format(layout)
		},
		// sanitize converts a string to a safe filename component.
		// Usage: {{.Title | sanitize}}
		"sanitize": func(s string) string {
			return utils.SanitizeFilename(s)
		},
		// truncate limits a string to at most n runes.
		// Usage: {{.Title | truncate 50}}
		"truncate": func(n int, s string) string {
			runes := []rune(s)
			if len(runes) <= n {
				return s
			}
			return string(runes[:n])
		},
	}
}

// TemplateFormatter is a formatter backed by three compiled Go templates: one
// each for the directory path, the filename (without extension), and the full
// file content.
type TemplateFormatter struct {
	name      string
	itemType  string
	dirTmpl   *template.Template // may be nil when DirectoryPattern is empty
	fileTmpl  *template.Template // may be nil when FilenamePattern is empty
	contTmpl  *template.Template // may be nil when ContentTemplate is empty
}

// New compiles a TemplateFormatter from a [models.FormatterConfig].
// Returns an error if any non-empty template fails to parse.
func New(cfg models.FormatterConfig) (*TemplateFormatter, error) {
	funcs := templateFuncs()

	tf := &TemplateFormatter{
		name:     cfg.Name,
		itemType: cfg.Type,
	}

	if p := cfg.Spec.DirectoryPattern; p != "" {
		t, err := template.New("dir").Funcs(funcs).Parse(p)
		if err != nil {
			return nil, fmt.Errorf("formatter %q: directory_pattern: %w", cfg.Name, err)
		}
		tf.dirTmpl = t
	}

	if p := cfg.Spec.FilenamePattern; p != "" {
		t, err := template.New("file").Funcs(funcs).Parse(p)
		if err != nil {
			return nil, fmt.Errorf("formatter %q: filename_pattern: %w", cfg.Name, err)
		}
		tf.fileTmpl = t
	}

	if p := cfg.Spec.ContentTemplate; p != "" {
		t, err := template.New("content").Funcs(funcs).Parse(p)
		if err != nil {
			return nil, fmt.Errorf("formatter %q: content_template: %w", cfg.Name, err)
		}
		tf.contTmpl = t
	}

	return tf, nil
}

// Name returns the formatter's name as declared in the configuration.
func (tf *TemplateFormatter) Name() string { return tf.name }

// ItemType returns the item type this formatter targets (e.g. "event", "thread").
func (tf *TemplateFormatter) ItemType() string { return tf.itemType }

// HasDirectoryPattern reports whether the formatter defines a directory template.
func (tf *TemplateFormatter) HasDirectoryPattern() bool { return tf.dirTmpl != nil }

// HasFilenamePattern reports whether the formatter defines a filename template.
func (tf *TemplateFormatter) HasFilenamePattern() bool { return tf.fileTmpl != nil }

// HasContentTemplate reports whether the formatter defines a content template.
func (tf *TemplateFormatter) HasContentTemplate() bool { return tf.contTmpl != nil }

// FormatDirectory renders the directory_pattern template for the given item.
// Returns an empty string if no template was configured.
func (tf *TemplateFormatter) FormatDirectory(item models.FullItem) (string, error) {
	if tf.dirTmpl == nil {
		return "", nil
	}
	return tf.render(tf.dirTmpl, item)
}

// FormatFilename renders the filename_pattern template for the given item.
// Returns an empty string if no template was configured.
func (tf *TemplateFormatter) FormatFilename(item models.FullItem) (string, error) {
	if tf.fileTmpl == nil {
		return "", nil
	}
	return tf.render(tf.fileTmpl, item)
}

// FormatContent renders the content_template template for the given item.
// Returns an empty string if no template was configured.
func (tf *TemplateFormatter) FormatContent(item models.FullItem) (string, error) {
	if tf.contTmpl == nil {
		return "", nil
	}
	return tf.render(tf.contTmpl, item)
}

func (tf *TemplateFormatter) render(t *template.Template, item models.FullItem) (string, error) {
	data := itemDataFromFullItem(item)

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("formatter %q: render %q: %w", tf.name, t.Name(), err)
	}

	return strings.TrimSpace(buf.String()), nil
}

// Registry maps formatter names to their compiled TemplateFormatters.
type Registry struct {
	byName map[string]*TemplateFormatter
}

// BuildRegistry compiles every FormatterConfig in cfgs and returns a Registry.
// The first compilation error aborts and is returned to the caller.
func BuildRegistry(cfgs []models.FormatterConfig) (*Registry, error) {
	r := &Registry{byName: make(map[string]*TemplateFormatter, len(cfgs))}

	for _, cfg := range cfgs {
		tf, err := New(cfg)
		if err != nil {
			return nil, err
		}

		r.byName[cfg.Name] = tf
	}

	return r, nil
}

// Lookup returns the named formatter, or (nil, false) if it does not exist.
func (r *Registry) Lookup(name string) (*TemplateFormatter, bool) {
	if r == nil {
		return nil, false
	}

	tf, ok := r.byName[name]

	return tf, ok
}

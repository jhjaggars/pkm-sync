package formatters_test

import (
	"testing"
	"time"

	"pkm-sync/internal/formatters"
	"pkm-sync/pkg/models"
)

// fixedTime is a stable timestamp used across tests.
var fixedTime = time.Date(2024, 3, 15, 9, 30, 0, 0, time.UTC)

// makeItem builds a minimal FullItem for testing.
func makeItem(id, title, itemType string) models.FullItem {
	item := models.NewBasicItem(id, title)
	item.SetItemType(itemType)
	item.SetCreatedAt(fixedTime)
	return item
}

func cfg(name, typ, dir, file, content string) models.FormatterConfig {
	return models.FormatterConfig{
		Name: name,
		Type: typ,
		Spec: models.FormatterSpec{
			DirectoryPattern: dir,
			FilenamePattern:  file,
			ContentTemplate:  content,
		},
	}
}

// --- TemplateFormatter ---

func TestNew_EmptySpec(t *testing.T) {
	tf, err := formatters.New(cfg("empty", "event", "", "", ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tf.HasDirectoryPattern() {
		t.Error("expected no directory pattern")
	}

	if tf.HasFilenamePattern() {
		t.Error("expected no filename pattern")
	}

	if tf.HasContentTemplate() {
		t.Error("expected no content template")
	}
}

func TestNew_InvalidTemplate(t *testing.T) {
	_, err := formatters.New(cfg("bad", "event", "{{.Missing}", "", ""))
	if err == nil {
		t.Fatal("expected parse error for invalid template, got nil")
	}
}

func TestFormatDirectory(t *testing.T) {
	tf, err := formatters.New(cfg("dir_test", "event",
		`Meetings/{{.CreatedAt | formatDate "2006"}}/{{.CreatedAt | formatDate "01-January"}}`,
		"", ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	item := makeItem("1", "Standup", "event")
	got, err := tf.FormatDirectory(item)
	if err != nil {
		t.Fatalf("FormatDirectory: %v", err)
	}

	want := "Meetings/2024/03-March"
	if got != want {
		t.Errorf("FormatDirectory = %q, want %q", got, want)
	}
}

func TestFormatFilename(t *testing.T) {
	tf, err := formatters.New(cfg("file_test", "event", "",
		`{{.CreatedAt | formatDate "2006-01-02"}} - {{.Title | sanitize}}`,
		""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	item := makeItem("2", "Team Meeting", "event")
	got, err := tf.FormatFilename(item)
	if err != nil {
		t.Fatalf("FormatFilename: %v", err)
	}

	want := "2024-03-15 - Team-Meeting"
	if got != want {
		t.Errorf("FormatFilename = %q, want %q", got, want)
	}
}

func TestFormatContent(t *testing.T) {
	tf, err := formatters.New(cfg("content_test", "event", "", "",
		`# {{.Title}}
Date: {{.CreatedAt | formatDate "January 2, 2006"}}`))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	item := makeItem("3", "Sprint Review", "event")
	got, err := tf.FormatContent(item)
	if err != nil {
		t.Fatalf("FormatContent: %v", err)
	}

	want := "# Sprint Review\nDate: March 15, 2024"
	if got != want {
		t.Errorf("FormatContent = %q, want %q", got, want)
	}
}

func TestTruncateFunction(t *testing.T) {
	tf, err := formatters.New(cfg("trunc_test", "thread", "",
		`{{.Title | truncate 5}}`, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	item := makeItem("4", "Hello World", "thread")
	got, err := tf.FormatFilename(item)
	if err != nil {
		t.Fatalf("FormatFilename: %v", err)
	}

	if got != "Hello" {
		t.Errorf("truncate(5) = %q, want %q", got, "Hello")
	}
}

func TestFormatDirectory_Empty_WhenNoTemplate(t *testing.T) {
	tf, err := formatters.New(cfg("nodir", "event", "", "{{.Title}}", ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	item := makeItem("5", "X", "event")
	got, err := tf.FormatDirectory(item)
	if err != nil {
		t.Fatalf("FormatDirectory: %v", err)
	}

	if got != "" {
		t.Errorf("expected empty dir, got %q", got)
	}
}

// --- Registry ---

func TestBuildRegistry(t *testing.T) {
	cfgs := []models.FormatterConfig{
		cfg("alpha", "event", "", "{{.Title}}", ""),
		cfg("beta", "thread", `{{.CreatedAt | formatDate "2006"}}`, "", ""),
	}

	reg, err := formatters.BuildRegistry(cfgs)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}

	if tf, ok := reg.Lookup("alpha"); !ok || tf == nil {
		t.Error("expected 'alpha' in registry")
	}

	if tf, ok := reg.Lookup("beta"); !ok || tf == nil {
		t.Error("expected 'beta' in registry")
	}

	if _, ok := reg.Lookup("missing"); ok {
		t.Error("expected 'missing' to be absent from registry")
	}
}

func TestBuildRegistry_DuplicateName_LastWins(t *testing.T) {
	cfgs := []models.FormatterConfig{
		cfg("dup", "event", "", "first", ""),
		cfg("dup", "event", "", "second", ""),
	}

	reg, err := formatters.BuildRegistry(cfgs)
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}

	tf, ok := reg.Lookup("dup")
	if !ok {
		t.Fatal("expected 'dup' in registry")
	}

	item := makeItem("6", "T", "event")
	got, err := tf.FormatFilename(item)
	if err != nil {
		t.Fatalf("FormatFilename: %v", err)
	}

	if got != "second" {
		t.Errorf("expected last-registered formatter to win, got %q", got)
	}
}

func TestBuildRegistry_InvalidTemplate_ReturnsError(t *testing.T) {
	cfgs := []models.FormatterConfig{
		cfg("broken", "event", "{{bad", "", ""),
	}

	_, err := formatters.BuildRegistry(cfgs)
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestNilRegistry_Lookup(t *testing.T) {
	var reg *formatters.Registry
	tf, ok := reg.Lookup("anything")

	if ok || tf != nil {
		t.Error("nil Registry.Lookup should return (nil, false)")
	}
}

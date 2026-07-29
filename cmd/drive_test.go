package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"pkm-sync/pkg/models"
)

func TestGetEnabledDriveSources_ExplicitList(t *testing.T) {
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"drive1", "drive2", "gmail_work"},
		},
		Sources: map[string]models.SourceConfig{
			"drive1": {
				Enabled: true,
				Type:    "google_drive",
			},
			"drive2": {
				Enabled: true,
				Type:    "google_drive",
			},
			"gmail_work": {
				Enabled: true,
				Type:    "gmail",
			},
			"drive_disabled": {
				Enabled: false,
				Type:    "google_drive",
			},
		},
	}

	sources := getEnabledDriveSources(cfg)

	if len(sources) != 2 {
		t.Errorf("getEnabledDriveSources() returned %d sources, want 2", len(sources))
	}

	sourceSet := make(map[string]bool)
	for _, s := range sources {
		sourceSet[s] = true
	}

	if !sourceSet["drive1"] {
		t.Error("expected drive1 to be included")
	}

	if !sourceSet["drive2"] {
		t.Error("expected drive2 to be included")
	}

	if sourceSet["gmail_work"] {
		t.Error("expected gmail_work to be excluded (wrong type)")
	}
}

func TestGetEnabledDriveSources_Fallback(t *testing.T) {
	// No explicit enabled_sources list → fall back to scanning all sources
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{},
		},
		Sources: map[string]models.SourceConfig{
			"my_drive": {
				Enabled: true,
				Type:    "google_drive",
			},
			"calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
			"drive_off": {
				Enabled: false,
				Type:    "google_drive",
			},
		},
	}

	sources := getEnabledDriveSources(cfg)

	if len(sources) != 1 {
		t.Errorf("getEnabledDriveSources() returned %d sources, want 1", len(sources))
	}

	if sources[0] != "my_drive" {
		t.Errorf("getEnabledDriveSources() = %v, want [my_drive]", sources)
	}
}

func TestGetEnabledDriveSources_Empty(t *testing.T) {
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{},
		},
		Sources: map[string]models.SourceConfig{
			"calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
		},
	}

	sources := getEnabledDriveSources(cfg)

	if len(sources) != 0 {
		t.Errorf("getEnabledDriveSources() returned %d sources, want 0", len(sources))
	}
}

func TestGetEnabledDriveSources_DisabledInExplicitList(t *testing.T) {
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"drive1"},
		},
		Sources: map[string]models.SourceConfig{
			"drive1": {
				Enabled: false, // disabled
				Type:    "google_drive",
			},
		},
	}

	sources := getEnabledDriveSources(cfg)

	if len(sources) != 0 {
		t.Errorf("getEnabledDriveSources() returned %d sources, want 0 (disabled source)", len(sources))
	}
}

// --- extractFrontmatterTimestamp tests ---

func TestExtractFrontmatterTimestamp_BasicRFC3339(t *testing.T) {
	content := "---\ncreated: 2026-04-16T09:29:38-04:00\ntitle: Test\n---\n\n# Body"
	got := extractFrontmatterTimestamp(content, "created")
	want, _ := time.Parse(time.RFC3339, "2026-04-16T09:29:38-04:00")

	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractFrontmatterTimestamp_QuotedValue(t *testing.T) {
	content := "---\ncreated: \"2026-04-16T09:29:38-04:00\"\n---\n"
	got := extractFrontmatterTimestamp(content, "created")
	want, _ := time.Parse(time.RFC3339, "2026-04-16T09:29:38-04:00")

	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractFrontmatterTimestamp_FieldAbsent(t *testing.T) {
	content := "---\ntitle: No timestamp here\n---\n\n# Body"
	got := extractFrontmatterTimestamp(content, "created")

	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}

func TestExtractFrontmatterTimestamp_NoFrontmatter(t *testing.T) {
	content := "# Just a heading\n\nSome body text."
	got := extractFrontmatterTimestamp(content, "created")

	if !got.IsZero() {
		t.Errorf("expected zero time for file with no frontmatter, got %v", got)
	}
}

func TestExtractFrontmatterTimestamp_HorizontalRuleInBody(t *testing.T) {
	// "---" in the body should NOT be treated as a second frontmatter block.
	content := "---\ntitle: Doc\n---\n\n# Heading\n\nSome text\n\n---\n\ncreated: 2099-01-01T00:00:00Z\n"
	got := extractFrontmatterTimestamp(content, "created")

	if !got.IsZero() {
		t.Errorf("horizontal rule in body should not produce a timestamp, got %v", got)
	}
}

func TestExtractFrontmatterTimestamp_EmptyContent(t *testing.T) {
	got := extractFrontmatterTimestamp("", "created")
	if !got.IsZero() {
		t.Errorf("expected zero time for empty content, got %v", got)
	}
}

func TestExtractFrontmatterTimestamp_UnparsableValue(t *testing.T) {
	content := "---\ncreated: not-a-timestamp\n---\n"
	got := extractFrontmatterTimestamp(content, "created")

	if !got.IsZero() {
		t.Errorf("expected zero time for unparsable value, got %v", got)
	}
}

// --- writeDocToFile skip-when-unchanged tests ---

func TestWriteDocToFile_SkipsWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/test-doc.md"
	fixedTime, _ := time.Parse(time.RFC3339, "2026-04-16T09:00:00-04:00")

	meta := &models.DriveFile{
		ID:           "abc123",
		Name:         "Test Doc",
		ModifiedTime: fixedTime,
	}

	// First write.
	err := writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# Hello", filePath)
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	info1, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat after first write: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Second write with identical markdown — should skip.
	err = writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# Hello", filePath)
	if err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	info2, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat after second write: %v", err)
	}

	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("mtime changed on second write: %v -> %v", info1.ModTime(), info2.ModTime())
	}
}

func TestWriteDocToFile_WritesWhenContentChanges(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/test-doc.md"
	fixedTime, _ := time.Parse(time.RFC3339, "2026-04-16T09:00:00-04:00")

	meta := &models.DriveFile{
		ID:           "abc123",
		Name:         "Test Doc",
		ModifiedTime: fixedTime,
	}

	err := writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# Original", filePath)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	info1, _ := os.Stat(filePath)

	time.Sleep(50 * time.Millisecond)

	err = writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# Updated", filePath)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}

	info2, _ := os.Stat(filePath)
	if info1.ModTime().Equal(info2.ModTime()) {
		t.Error("mtime should have changed when content changed")
	}

	ondisk, _ := os.ReadFile(filePath)
	if !strings.Contains(string(ondisk), "Updated") {
		t.Error("file should contain updated content")
	}
}

func TestWriteDocToFile_PreservesCreatedAt(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/test-doc.md"
	fixedTime, _ := time.Parse(time.RFC3339, "2026-04-16T09:00:00-04:00")

	meta := &models.DriveFile{
		ID:           "abc123",
		Name:         "Test Doc",
		ModifiedTime: fixedTime,
	}

	// First write establishes the created timestamp.
	if err := writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# v1", filePath); err != nil {
		t.Fatalf("first write: %v", err)
	}

	first, _ := os.ReadFile(filePath)
	createdFirst := extractFrontmatterTimestamp(string(first), "created")

	time.Sleep(50 * time.Millisecond)

	// Second write with different content — created should be preserved.
	if err := writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# v2", filePath); err != nil {
		t.Fatalf("second write: %v", err)
	}

	second, _ := os.ReadFile(filePath)
	createdSecond := extractFrontmatterTimestamp(string(second), "created")

	if !createdFirst.Equal(createdSecond) {
		t.Errorf("created timestamp changed: %v -> %v", createdFirst, createdSecond)
	}
}

func TestWriteDocToFile_UsesModifiedTimeForUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/test-doc.md"
	modTime, _ := time.Parse(time.RFC3339, "2026-07-01T12:00:00Z")

	meta := &models.DriveFile{
		ID:           "abc123",
		Name:         "Test Doc",
		ModifiedTime: modTime,
	}

	if err := writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# Body", filePath); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Second call with same ModifiedTime and same content should skip writing.
	info1, _ := os.Stat(filePath)

	time.Sleep(50 * time.Millisecond)

	if err := writeDocToFile("https://docs.google.com/document/d/abc123/edit", meta, "# Body", filePath); err != nil {
		t.Fatalf("second write: %v", err)
	}

	info2, _ := os.Stat(filePath)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("mtime changed even though Drive ModifiedTime and content were identical")
	}
}

func TestWriteDocToFile_NewFile(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/new-doc.md"
	fixedTime, _ := time.Parse(time.RFC3339, "2026-04-16T09:00:00-04:00")

	meta := &models.DriveFile{
		ID:           "newfile",
		Name:         "New Doc",
		ModifiedTime: fixedTime,
	}

	if err := writeDocToFile("https://docs.google.com/document/d/newfile/edit", meta, "# New", filePath); err != nil {
		t.Fatalf("write: %v", err)
	}

	ondisk, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !strings.Contains(string(ondisk), "New") {
		t.Error("new file should contain written content")
	}
}

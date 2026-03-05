package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewState(t *testing.T) {
	s := New()
	if s.Sources == nil {
		t.Fatal("expected non-nil Sources map")
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}

	if s == nil || s.Sources == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := New()
	s.UpdateSubItems("gmail_work", []string{"INBOX", "STARRED"})
	s.UpdateSubItems("slack", []string{"general"})

	if err := s.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file has restricted permissions.
	info, err := os.Stat(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %o, want 0600", info.Mode().Perm())
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	subItems := loaded.Sources["gmail_work"].KnownSubItems
	if len(subItems) != 2 || subItems[0] != "INBOX" || subItems[1] != "STARRED" {
		t.Errorf("gmail_work sub-items: got %v, want [INBOX STARRED]", subItems)
	}
}

func TestLegacyBareTimestampMigration(t *testing.T) {
	dir := t.TempDir()
	// Write the oldest legacy format: sources as map[string]time.Time.
	legacy := `{"sources":{"jira_redhat":"2024-01-15T10:30:00Z","slack":"2024-01-14T08:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Sources should be present in the map (with empty sub-items — timestamp is dropped).
	if _, ok := s.Sources["jira_redhat"]; !ok {
		t.Error("jira_redhat missing after legacy migration")
	}
}

func TestLegacyObjectWithLastSyncedMigration(t *testing.T) {
	dir := t.TempDir()
	// Previous format: source objects had a last_synced field.
	prev := `{"sources":{"jira_redhat":{"last_synced":"2024-01-15T10:30:00Z","known_sub_items":["AAP","KONFLUX"]}}}`
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte(prev), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Sub-items should survive migration; last_synced is silently dropped.
	items := s.Sources["jira_redhat"].KnownSubItems
	if len(items) != 2 || items[0] != "AAP" || items[1] != "KONFLUX" {
		t.Errorf("sub-items: got %v, want [AAP KONFLUX]", items)
	}
}

func TestNewSubItems(t *testing.T) {
	s := New()

	// No baseline yet — should not flag anything as new.
	result := s.NewSubItems("jira", []string{"PROJECT1", "PROJECT2"})
	if result != nil {
		t.Errorf("expected nil with no baseline, got %v", result)
	}

	// Establish a baseline.
	s.UpdateSubItems("jira", []string{"PROJECT1", "PROJECT2"})

	// Same set — no new items.
	result = s.NewSubItems("jira", []string{"PROJECT1", "PROJECT2"})
	if len(result) != 0 {
		t.Errorf("expected no new items, got %v", result)
	}

	// New project added.
	result = s.NewSubItems("jira", []string{"PROJECT1", "PROJECT2", "PROJECT3"})
	if len(result) != 1 || result[0] != "PROJECT3" {
		t.Errorf("expected [PROJECT3], got %v", result)
	}

	// Removed project — not flagged (we only detect additions, not removals).
	result = s.NewSubItems("jira", []string{"PROJECT1"})
	if len(result) != 0 {
		t.Errorf("expected no new items when project removed, got %v", result)
	}
}

func TestNewSubItemsEmptyCurrent(t *testing.T) {
	s := New()
	s.UpdateSubItems("slack", []string{"general", "engineering"})

	result := s.NewSubItems("slack", nil)
	if result != nil {
		t.Errorf("expected nil for empty current, got %v", result)
	}
}

func TestSinceOverlap(t *testing.T) {
	if SinceOverlap <= 0 {
		t.Error("SinceOverlap should be positive")
	}
}

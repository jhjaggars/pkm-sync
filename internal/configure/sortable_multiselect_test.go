package configure

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func makeTestOpts() []DiscoverableOption {
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2022, 6, 15, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	return []DiscoverableOption{
		{ID: "zebra", Name: "#zebra", Created: t2, Updated: t3, Selected: true},
		{ID: "alpha", Name: "#alpha", Created: t3, Updated: t1},
		{ID: "mango", Name: "#mango", Created: t1, Updated: t2},
		{ID: "empty", Name: "#empty"}, // zero timestamps
	}
}

func makeTestOptsWithOwners() []DiscoverableOption {
	return []DiscoverableOption{
		{ID: "f1", Name: "Reports", Owner: "Carol"},
		{ID: "f2", Name: "Projects", Owner: "Alice"},
		{ID: "f3", Name: "Archives", Owner: "Bob"},
		{ID: "f4", Name: "Shared", Owner: ""},
	}
}

// sendKey simulates a key press on a SortableMultiSelect and returns the new model.
func sendKey(m SortableMultiSelect, k string) SortableMultiSelect {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	next, _ := m.Update(msg)

	return next.(SortableMultiSelect)
}

func sendSpecialKey(m SortableMultiSelect, kt tea.KeyType) SortableMultiSelect {
	msg := tea.KeyMsg{Type: kt}
	next, _ := m.Update(msg)

	return next.(SortableMultiSelect)
}

func sortedNames(m SortableMultiSelect) []string {
	names := make([]string, len(m.sorted))
	for i, idx := range m.sorted {
		names[i] = m.options[idx].Name
	}

	return names
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}

	return false
}

func TestSortByNameAscending(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	names := sortedNames(m)
	want := []string{"#alpha", "#empty", "#mango", "#zebra"}

	for i, n := range want {
		if names[i] != n {
			t.Errorf("pos %d: want %q got %q", i, n, names[i])
		}
	}
}

func TestSortByNameToggleDescending(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "N") // already on Name, so toggle to desc
	names := sortedNames(m)
	want := []string{"#zebra", "#mango", "#empty", "#alpha"}

	for i, n := range want {
		if names[i] != n {
			t.Errorf("pos %d: want %q got %q", i, n, names[i])
		}
	}
}

func TestSortByCreatedDefaultsDescending(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "C") // switch to created; default is desc (newest first)

	if m.ascending {
		t.Error("expected descending default for created sort")
	}

	names := sortedNames(m)
	// newest created first: alpha(t3), zebra(t2), mango(t1), empty(zero→last)
	want := []string{"#alpha", "#zebra", "#mango", "#empty"}

	for i, n := range want {
		if names[i] != n {
			t.Errorf("pos %d: want %q got %q", i, n, names[i])
		}
	}
}

func TestSortByCreatedZeroAtEnd(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "C")
	names := sortedNames(m)

	if names[len(names)-1] != "#empty" {
		t.Errorf("zero-time item should be last, got %q", names[len(names)-1])
	}
}

func TestSortByModifiedDefaultsDescending(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "M") // most-recently-modified first
	names := sortedNames(m)
	// updated: zebra=t3, mango=t2, alpha=t1, empty=zero
	want := []string{"#zebra", "#mango", "#alpha", "#empty"}

	for i, n := range want {
		if names[i] != n {
			t.Errorf("pos %d: want %q got %q", i, n, names[i])
		}
	}
}

func TestInitialSelection(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	ids := m.SelectedIDs()

	if len(ids) != 1 || ids[0] != "zebra" {
		t.Errorf("expected [zebra], got %v", ids)
	}
}

func TestToggleSelection(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	// cursor=0, after name-sort = "#alpha"
	m = sendSpecialKey(m, tea.KeySpace)
	ids := m.SelectedIDs()

	if !containsStr(ids, "zebra") || !containsStr(ids, "alpha") {
		t.Errorf("expected zebra and alpha selected, got %v", ids)
	}
}

func TestSelectAll(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "a")
	ids := m.SelectedIDs()

	if len(ids) != 4 {
		t.Errorf("expected 4 selected, got %d: %v", len(ids), ids)
	}
}

func TestSelectNone(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "n")
	ids := m.SelectedIDs()

	if len(ids) != 0 {
		t.Errorf("expected 0 selected, got %d: %v", len(ids), ids)
	}
}

func TestSelectionPreservedAcrossResort(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	// toggle alpha (cursor=0 in name-sort)
	m = sendSpecialKey(m, tea.KeySpace)
	// re-sort by created
	m = sendKey(m, "C")
	ids := m.SelectedIDs()

	if !containsStr(ids, "zebra") || !containsStr(ids, "alpha") {
		t.Errorf("selection lost after re-sort; got %v", ids)
	}
}

func TestAbortEsc(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendSpecialKey(m, tea.KeyEsc)

	if !m.Aborted() {
		t.Error("expected Aborted() after esc")
	}
}

func TestSortByOwnerAscending(t *testing.T) {
	m := NewSortableMultiSelect("Folders", "", makeTestOptsWithOwners())
	m = sendKey(m, "O")
	names := sortedNames(m)
	// ascending: Alice, Bob, Carol, then empty-owner last? Actually empty string sorts first alphabetically.
	// "" < "Alice" < "Bob" < "Carol"
	want := []string{"Shared", "Projects", "Archives", "Reports"}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("pos %d: want %q got %q", i, n, names[i])
		}
	}
}

func TestFilterByName(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	// Type "/" then "ma" to filter for "mango"
	m = sendKey(m, "/")
	if !m.filtering {
		t.Fatal("expected filtering mode after /")
	}

	m = sendKey(m, "m")
	m = sendKey(m, "a")

	names := sortedNames(m)
	if len(names) != 1 || names[0] != "#mango" {
		t.Errorf("expected [#mango] after filter 'ma', got %v", names)
	}
}

func TestFilterClearedByEsc(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "/")
	m = sendKey(m, "z")
	// exit filter mode
	m = sendSpecialKey(m, tea.KeyEsc)
	if m.filtering {
		t.Error("should no longer be filtering after esc")
	}

	// esc again clears the filter (not abort, since filter is set)
	m = sendSpecialKey(m, tea.KeyEsc)
	if m.filter != "" {
		t.Errorf("expected filter cleared, got %q", m.filter)
	}

	if m.Aborted() {
		t.Error("should not abort when filter was set")
	}
}

func TestFilterBackspace(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendKey(m, "/")
	m = sendKey(m, "z")
	m = sendKey(m, "e")

	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	next, _ := m.Update(msg)
	m = next.(SortableMultiSelect)

	if m.filter != "z" {
		t.Errorf("expected filter 'z' after backspace, got %q", m.filter)
	}
}

func TestPageDown(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m.height = 2 // only 2 visible rows
	m = sendSpecialKey(m, tea.KeyPgDown)

	if m.cursor != 2 {
		t.Errorf("expected cursor 2 after pgdown with height=2, got %d", m.cursor)
	}
}

func TestHasOwners(t *testing.T) {
	m1 := NewSortableMultiSelect("Folders", "", makeTestOptsWithOwners())
	if !m1.hasOwners() {
		t.Error("expected hasOwners() true")
	}

	m2 := NewSortableMultiSelect("Channels", "", makeTestOpts())
	if m2.hasOwners() {
		t.Error("expected hasOwners() false for opts without owners")
	}
}

func TestAbortCtrlC(t *testing.T) {
	m := NewSortableMultiSelect("Channels", "", makeTestOpts())
	m = sendSpecialKey(m, tea.KeyCtrlC)

	if !m.Aborted() {
		t.Error("expected Aborted() after ctrl+c")
	}
}

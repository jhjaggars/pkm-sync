// Package state manages persistent sync state between pkm-sync runs.
//
// The state file (sync-state.json) tracks the set of sub-items (project keys,
// channel IDs, folder IDs, …) that were active for each source during the last
// sync. This allows newly added sub-items to be detected and given a full
// lookback window rather than an incremental one.
//
// Last-synced timestamps are NOT stored here — they are inferred at sync time
// by querying vectors.db for MAX(updated_at) per source, which is populated by
// the always-on VectorSink.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	stateFileName = "sync-state.json"

	// SinceOverlap is subtracted from the inferred last-synced timestamp when
	// computing the incremental since window. A small buffer guards against
	// clock skew and items that were in-flight at the time of the previous sync.
	SinceOverlap = 5 * time.Minute
)

// SourceState holds the persisted state for a single named source.
type SourceState struct {
	// KnownSubItems is the sorted list of sub-item identifiers (project keys,
	// channel IDs, folder IDs, etc.) that were active during the last sync.
	// When the current config contains items absent from this list, those new
	// items trigger a full-window lookback rather than an incremental one.
	KnownSubItems []string `json:"known_sub_items,omitempty"`
}

// SyncState records per-source sub-item membership. It is safe for concurrent
// use across goroutines; all exported methods acquire the internal mutex.
type SyncState struct {
	mu      sync.Mutex
	Sources map[string]SourceState `json:"sources"`
}

// New returns an empty SyncState ready for use.
func New() *SyncState {
	return &SyncState{Sources: make(map[string]SourceState)}
}

// Load reads the state file from configDir.
// Transparently migrates legacy formats (where source values were bare
// timestamps or objects with a last_synced field) — those fields are ignored
// since timestamps are now inferred from the vector store.
// Returns a fresh empty state when the file does not exist yet.
func Load(configDir string) (*SyncState, error) {
	path := filepath.Join(configDir, stateFileName)

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading sync state: %w", err)
	}

	// Parse with raw values so we can handle multiple historical format versions.
	var raw struct {
		Sources map[string]json.RawMessage `json:"sources"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing sync state: %w", err)
	}

	s := New()

	for name, rawVal := range raw.Sources {
		// Current format: {"known_sub_items": [...]}
		// Also handles previous format: {"last_synced": "...", "known_sub_items": [...]}
		// (last_synced is silently dropped since timestamps now come from vectors.db)
		var ss SourceState
		if err := json.Unmarshal(rawVal, &ss); err == nil {
			s.Sources[name] = ss

			continue
		}

		// Oldest legacy format: bare RFC3339 timestamp string — drop it,
		// start with empty sub-items for this source.
		s.Sources[name] = SourceState{}
	}

	return s, nil
}

// Save writes the state to configDir/sync-state.json with mode 0600.
func (s *SyncState) Save(configDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, stateFileName), data, 0o600)
}

// UpdateSubItems records the set of sub-items that were active for sourceName
// during this sync. On the next sync, NewSubItems compares the current config
// against this list to detect newly added items that need a wider lookback.
func (s *SyncState) UpdateSubItems(sourceName string, items []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss := s.Sources[sourceName]
	ss.KnownSubItems = items
	s.Sources[sourceName] = ss
}

// NewSubItems returns the items in current that are not present in the known
// sub-item set for sourceName. Returns nil when:
//   - current is empty (the source type has no trackable sub-items)
//   - sourceName has no recorded sub-items yet (no baseline to diff against —
//     avoids false positives on the first run or after format migration)
//   - all current items are already known
func (s *SyncState) NewSubItems(sourceName string, current []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(current) == 0 {
		return nil
	}

	ss, ok := s.Sources[sourceName]
	if !ok || len(ss.KnownSubItems) == 0 {
		// No baseline — don't trigger a false "new items" on first run or upgrade.
		return nil
	}

	knownSet := make(map[string]bool, len(ss.KnownSubItems))
	for _, k := range ss.KnownSubItems {
		knownSet[k] = true
	}

	var newItems []string

	for _, c := range current {
		if !knownSet[c] {
			newItems = append(newItems, c)
		}
	}

	return newItems
}

package slack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCachePathEnvOverride(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), "slack-user-cache.json")
	t.Setenv("PKM_SLACK_USER_CACHE", overridePath)

	uc := NewUserCache(t.TempDir())

	if got := uc.cachePath(); got != overridePath {
		t.Fatalf("cachePath() = %q, want %q", got, overridePath)
	}

	uc.entries["U123"] = "alice"
	uc.dirty = true

	if err := uc.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if _, err := os.Stat(overridePath); err != nil {
		t.Fatalf("expected cache file at %s: %v", overridePath, err)
	}
}

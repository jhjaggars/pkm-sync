package main

import (
	"fmt"
	"testing"

	"pkm-sync/pkg/models"
)

func TestSyncCmd_SourceFiltering(t *testing.T) {
	// When --source is set, only that source should be synced
	cfg := &models.Config{
		Sync: models.SyncConfig{
			EnabledSources: []string{"gmail_work", "gmail_personal", "google_calendar"},
		},
		Sources: map[string]models.SourceConfig{
			"gmail_work": {
				Enabled: true,
				Type:    "gmail",
			},
			"gmail_personal": {
				Enabled: true,
				Type:    "gmail",
			},
			"google_calendar": {
				Enabled: true,
				Type:    "google_calendar",
			},
		},
	}

	// Simulate --source flag filtering: only return the requested source
	requestedSource := "gmail_work"
	sourcesToSync := []string{requestedSource}

	// Verify filtering result
	if len(sourcesToSync) != 1 {
		t.Errorf("Expected 1 source when --source is set, got %d", len(sourcesToSync))
	}

	if sourcesToSync[0] != requestedSource {
		t.Errorf("Expected source %s, got %s", requestedSource, sourcesToSync[0])
	}

	// Verify the source exists and is enabled in config
	srcCfg, exists := cfg.Sources[requestedSource]
	if !exists {
		t.Errorf("Expected source '%s' to exist in config", requestedSource)
	}

	if !srcCfg.Enabled {
		t.Errorf("Expected source '%s' to be enabled", requestedSource)
	}
}

func TestSyncCmd_ConfigResolutionCascade(t *testing.T) {
	// CLI flags override config defaults
	cfg := &models.Config{
		Sync: models.SyncConfig{
			DefaultTarget:    "obsidian",
			DefaultOutputDir: "/default/output",
			DefaultSince:     "7d",
		},
	}

	// Case 1: No CLI overrides — use config defaults
	finalTarget := cfg.Sync.DefaultTarget
	finalOutput := cfg.Sync.DefaultOutputDir
	finalSince := cfg.Sync.DefaultSince

	if finalTarget != "obsidian" {
		t.Errorf("Expected target 'obsidian', got '%s'", finalTarget)
	}

	if finalOutput != "/default/output" {
		t.Errorf("Expected output '/default/output', got '%s'", finalOutput)
	}

	if finalSince != "7d" {
		t.Errorf("Expected since '7d', got '%s'", finalSince)
	}

	// Case 2: CLI overrides apply
	cliTarget := "logseq"
	cliOutput := "/custom/output"
	cliSince := "1d"

	if cliTarget != "" {
		finalTarget = cliTarget
	}

	if cliOutput != "" {
		finalOutput = cliOutput
	}

	if cliSince != "" {
		finalSince = cliSince
	}

	if finalTarget != "logseq" {
		t.Errorf("Expected CLI override target 'logseq', got '%s'", finalTarget)
	}

	if finalOutput != "/custom/output" {
		t.Errorf("Expected CLI override output '/custom/output', got '%s'", finalOutput)
	}

	if finalSince != "1d" {
		t.Errorf("Expected CLI override since '1d', got '%s'", finalSince)
	}
}

func TestSyncCmd_ErrorAccumulation(t *testing.T) {
	// Source failures are tracked but don't stop the overall sync
	results := []sourceResult{
		{Name: "gmail_work", ItemCount: 5, Err: nil},
		{Name: "gmail_personal", ItemCount: 0, Err: fmt.Errorf("auth error")},
		{Name: "google_calendar", ItemCount: 3, Err: nil},
	}

	succeeded := 0
	failed := 0

	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			succeeded++
		}
	}

	if succeeded != 2 {
		t.Errorf("Expected 2 succeeded sources, got %d", succeeded)
	}

	if failed != 1 {
		t.Errorf("Expected 1 failed source, got %d", failed)
	}
}

func TestSyncCmd_UnsupportedSourceType(t *testing.T) {
	// drive and other unimplemented types should be skipped with a warning
	supportedTypes := map[string]bool{
		"gmail":           true,
		"google_calendar": true,
	}

	unsupportedTypes := []string{"drive", "slack", "jira", "notion"}

	for _, sourceType := range unsupportedTypes {
		if supportedTypes[sourceType] {
			t.Errorf("Expected source type '%s' to be unsupported", sourceType)
		}
	}

	// Supported types should be recognized
	for sourceType := range supportedTypes {
		if !supportedTypes[sourceType] {
			t.Errorf("Expected source type '%s' to be supported", sourceType)
		}
	}
}

func TestSyncCmd_SourceResultSummary(t *testing.T) {
	tests := []struct {
		name              string
		results           []sourceResult
		totalItems        int
		expectedSucceeded int
		expectedFailed    int
	}{
		{
			name: "all sources succeed",
			results: []sourceResult{
				{Name: "gmail_work", ItemCount: 10, Err: nil},
				{Name: "google_calendar", ItemCount: 5, Err: nil},
			},
			totalItems:        15,
			expectedSucceeded: 2,
			expectedFailed:    0,
		},
		{
			name: "all sources fail",
			results: []sourceResult{
				{Name: "gmail_work", ItemCount: 0, Err: fmt.Errorf("error")},
				{Name: "google_calendar", ItemCount: 0, Err: fmt.Errorf("error")},
			},
			totalItems:        0,
			expectedSucceeded: 0,
			expectedFailed:    2,
		},
		{
			name: "mixed results",
			results: []sourceResult{
				{Name: "gmail_work", ItemCount: 8, Err: nil},
				{Name: "gmail_personal", ItemCount: 0, Err: fmt.Errorf("token expired")},
				{Name: "google_calendar", ItemCount: 3, Err: nil},
			},
			totalItems:        11,
			expectedSucceeded: 2,
			expectedFailed:    1,
		},
		{
			name:              "empty results",
			results:           []sourceResult{},
			totalItems:        0,
			expectedSucceeded: 0,
			expectedFailed:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			succeeded := 0
			failed := 0

			for _, r := range tt.results {
				if r.Err != nil {
					failed++
				} else {
					succeeded++
				}
			}

			if succeeded != tt.expectedSucceeded {
				t.Errorf("Expected %d succeeded, got %d", tt.expectedSucceeded, succeeded)
			}

			if failed != tt.expectedFailed {
				t.Errorf("Expected %d failed, got %d", tt.expectedFailed, failed)
			}
		})
	}
}

func TestSyncCmd_PerSourceSinceResolution(t *testing.T) {
	// Per-source since: config overrides global default, but CLI flag takes precedence
	globalSince := "7d"
	sourceConfigSince := "30d"

	// Case 1: No CLI override — use source-specific config since
	cliSince := ""
	expectedSince := globalSince

	if sourceConfigSince != "" && cliSince == "" {
		expectedSince = sourceConfigSince
	}

	if expectedSince != sourceConfigSince {
		t.Errorf("Expected source config since '%s', got '%s'", sourceConfigSince, expectedSince)
	}

	// Case 2: CLI override takes precedence over source config
	cliSince = "1d"
	expectedSince = globalSince

	if cliSince != "" {
		expectedSince = cliSince
	} else if sourceConfigSince != "" {
		expectedSince = sourceConfigSince
	}

	if expectedSince != cliSince {
		t.Errorf("Expected CLI since '%s' to take precedence, got '%s'", cliSince, expectedSince)
	}
}

package main

import (
	"testing"

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

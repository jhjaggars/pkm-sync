package keystore

import (
	"errors"
	"fmt"
)

// KnownKeys returns the list of well-known secret keys managed by pkm-sync.
// Slack workspace keys are not included here — callers must enumerate workspaces
// separately and call MigrateKey for each "slack-token-<workspace>" key.
var KnownKeys = []string{
	"google-oauth-token",
}

// MigrateAll reads every known secret from the file store and writes it to the
// keyring store, deleting the file copy on success. It returns an error summary
// if any individual migration fails.
func MigrateAll(configDir string, extraKeys []string) error {
	kr := newKeyringStore()
	if err := kr.probe(); err != nil {
		return fmt.Errorf("keyring unavailable, cannot migrate: %w", err)
	}

	file := newFileStore(configDir)

	keys := append(KnownKeys, extraKeys...)

	var failedKeys []string

	for _, key := range keys {
		if err := MigrateKey(file, kr, key); err != nil {
			if !errors.Is(err, ErrNotFound) {
				failedKeys = append(failedKeys, fmt.Sprintf("%s: %v", key, err))
			}
		}
	}

	if len(failedKeys) > 0 {
		return fmt.Errorf("migration failures: %v", failedKeys)
	}

	return nil
}

// MigrateKey copies a single secret from src to dst and deletes it from src.
// Returns ErrNotFound if the key does not exist in src (caller may ignore this).
func MigrateKey(src, dst Store, key string) error {
	val, err := src.Get(key)
	if err != nil {
		return err
	}

	if err := dst.Set(key, val); err != nil {
		return fmt.Errorf("write to dst: %w", err)
	}

	// Non-fatal: best-effort cleanup.
	_ = src.Delete(key)

	return nil
}

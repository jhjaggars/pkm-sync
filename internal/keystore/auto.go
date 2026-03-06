package keystore

import (
	"errors"
	"fmt"
	"log/slog"
)

// AutoStore tries the keyring first and falls back to file storage transparently.
// On first Get it auto-migrates secrets found in files to the keyring.
type AutoStore struct {
	kr   *KeyringStore
	file *FileStore
	// krOK is whether keyring is available.
	krOK bool
}

func newAutoStore(configDir string) *AutoStore {
	kr := newKeyringStore()

	krOK := kr.probe() == nil
	if !krOK {
		slog.Debug("keyring unavailable, using file-based secret storage")
	}

	return &AutoStore{
		kr:   kr,
		file: newFileStore(configDir),
		krOK: krOK,
	}
}

func (a *AutoStore) Get(key string) (string, error) {
	if !a.krOK {
		// Keyring unavailable — use file directly.
		return a.file.Get(key)
	}

	val, err := a.kr.Get(key)
	if err == nil {
		return val, nil
	}

	if !errors.Is(err, ErrNotFound) {
		slog.Debug("keyring get failed, falling back to file", "key", key, "err", err)
	}

	// Not in keyring — check file for migration.
	fileVal, fileErr := a.file.Get(key)
	if fileErr != nil {
		if errors.Is(fileErr, ErrNotFound) {
			return "", ErrNotFound
		}

		return "", fileErr
	}

	// Migrate: write to keyring, delete file.
	if migrateErr := a.kr.Set(key, fileVal); migrateErr != nil {
		slog.Debug("keyring migration failed, keeping file", "key", key, "err", migrateErr)
	} else {
		_ = a.file.Delete(key)
		slog.Debug("migrated secret to keyring", "key", key)
	}

	return fileVal, nil
}

func (a *AutoStore) Set(key, value string) error {
	if !a.krOK {
		return a.file.Set(key, value)
	}

	if err := a.kr.Set(key, value); err != nil {
		slog.Debug("keyring set failed, falling back to file", "key", key, "err", err)

		return a.file.Set(key, value)
	}

	// Ensure no stale file copy remains.
	_ = a.file.Delete(key)

	return nil
}

func (a *AutoStore) Delete(key string) error {
	var errs []error

	if a.krOK {
		if err := a.kr.Delete(key); err != nil {
			errs = append(errs, fmt.Errorf("keyring: %w", err))
		}
	}

	if err := a.file.Delete(key); err != nil {
		errs = append(errs, fmt.Errorf("file: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("delete failed: %v", errs)
	}

	return nil
}

func (a *AutoStore) Backend() string {
	if a.krOK {
		return "auto(keyring)"
	}

	return "auto(file)"
}

// Package keystore provides a unified interface for storing secrets securely.
// It supports macOS Keychain, Linux D-Bus Secret Service, and Windows Credential
// Manager via go-keyring, with automatic fallback to file-based storage.
package keystore

import (
	"fmt"
)

const (
	// ServiceName is the keyring service identifier for pkm-sync.
	ServiceName = "pkm-sync"

	// ModeAuto tries keyring first and falls back to file storage.
	ModeAuto = "auto"

	// ModeKeyring requires keyring storage; errors if unavailable.
	ModeKeyring = "keyring"

	// ModeFile uses legacy file-based storage only.
	ModeFile = "file"
)

// ErrNotFound is returned when a key does not exist in the store.
var ErrNotFound = fmt.Errorf("secret not found")

// Store is the interface for reading and writing secrets.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	// Backend returns a human-readable name of the active backend.
	Backend() string
}

// New creates a Store according to the requested mode.
//   - ModeAuto: AutoStore (keyring with file fallback + migration)
//   - ModeKeyring: KeyringStore only
//   - ModeFile: FileStore only
//
// configDir is the directory used by FileStore for legacy token files.
func New(mode, configDir string) (Store, error) {
	switch mode {
	case ModeKeyring:
		s := newKeyringStore()

		if err := s.probe(); err != nil {
			return nil, fmt.Errorf("keyring unavailable: %w", err)
		}

		return s, nil

	case ModeFile:
		return newFileStore(configDir), nil

	default: // ModeAuto and anything unrecognized
		return newAutoStore(configDir), nil
	}
}

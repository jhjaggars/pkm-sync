package keystore

import (
	"context"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

const keystoreTimeout = 3 * time.Second

// KeyringStore stores secrets in the OS keychain (macOS Keychain, GNOME Keyring, Windows Credential Manager).
type KeyringStore struct{}

func newKeyringStore() *KeyringStore {
	return &KeyringStore{}
}

// probe checks whether the keyring is available by performing a benign Get.
func (k *KeyringStore) probe() error {
	_, err := k.withTimeout(func() (string, error) {
		_, err := keyring.Get(ServiceName, "__probe__")
		if err == keyring.ErrNotFound {
			return "", nil // probe succeeded, key just absent
		}

		return "", err
	})

	return err
}

// withTimeout executes fn with a deadline so a blocked D-Bus or Keychain dialog
// doesn't hang the process (mirrors the gh CLI pattern).
func (k *KeyringStore) withTimeout(fn func() (string, error)) (string, error) {
	type result struct {
		val string
		err error
	}

	ctx, cancel := context.WithTimeout(context.Background(), keystoreTimeout)
	defer cancel()

	ch := make(chan result, 1)

	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()

	select {
	case r := <-ch:
		return r.val, r.err
	case <-ctx.Done():
		return "", fmt.Errorf("keyring operation timed out after %s", keystoreTimeout)
	}
}

func (k *KeyringStore) Get(key string) (string, error) {
	val, err := k.withTimeout(func() (string, error) {
		return keyring.Get(ServiceName, key)
	})
	if err == keyring.ErrNotFound {
		return "", ErrNotFound
	}

	return val, err
}

func (k *KeyringStore) Set(key, value string) error {
	_, err := k.withTimeout(func() (string, error) {
		return "", keyring.Set(ServiceName, key, value)
	})

	return err
}

func (k *KeyringStore) Delete(key string) error {
	_, err := k.withTimeout(func() (string, error) {
		err := keyring.Delete(ServiceName, key)
		if err == keyring.ErrNotFound {
			return "", nil
		}

		return "", err
	})

	return err
}

func (k *KeyringStore) Backend() string { return "keyring" }

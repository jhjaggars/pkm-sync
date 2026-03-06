package keystore

import (
	"errors"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeyringStore(t *testing.T) {
	keyring.MockInit()

	ks := newKeyringStore()

	// Get on missing key
	_, err := ks.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Set then Get
	if err := ks.Set("mykey", "myvalue"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := ks.Get("mykey")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if val != "myvalue" {
		t.Fatalf("expected 'myvalue', got %q", val)
	}

	// Delete
	if err := ks.Delete("mykey"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = ks.Get("mykey")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}

	// Delete of non-existent key is not an error
	if err := ks.Delete("nonexistent"); err != nil {
		t.Fatalf("Delete of nonexistent: %v", err)
	}

	if ks.Backend() != "keyring" {
		t.Fatalf("unexpected backend %q", ks.Backend())
	}
}

func TestFileStore(t *testing.T) {
	dir := t.TempDir()
	fs := newFileStore(dir)

	// Get on missing key
	_, err := fs.Get("google-oauth-token")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Set (uses legacy filename)
	if err := fs.Set("google-oauth-token", `{"access_token":"tok"}`); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify file exists with legacy name
	if _, err := os.Stat(dir + "/token.json"); err != nil {
		t.Fatalf("expected token.json: %v", err)
	}

	val, err := fs.Get("google-oauth-token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if val != `{"access_token":"tok"}` {
		t.Fatalf("unexpected value %q", val)
	}

	// Slack token key
	if err := fs.Set("slack-token-myworkspace", `{"token":"xoxs"}`); err != nil {
		t.Fatalf("Set slack: %v", err)
	}

	if _, err := os.Stat(dir + "/slack-token-myworkspace.json"); err != nil {
		t.Fatalf("expected slack-token-myworkspace.json: %v", err)
	}

	// Delete
	if err := fs.Delete("google-oauth-token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = fs.Get("google-oauth-token")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}

	if fs.Backend() != "file" {
		t.Fatalf("unexpected backend %q", fs.Backend())
	}
}

func TestAutoStore_Migration(t *testing.T) {
	keyring.MockInit()

	dir := t.TempDir()

	// Write a secret to the file store directly (simulates legacy token)
	fs := newFileStore(dir)
	if err := fs.Set("google-oauth-token", `{"access_token":"legacy"}`); err != nil {
		t.Fatal(err)
	}

	// AutoStore should migrate on first Get
	as := newAutoStore(dir)

	val, err := as.Get("google-oauth-token")
	if err != nil {
		t.Fatalf("AutoStore.Get: %v", err)
	}

	if val != `{"access_token":"legacy"}` {
		t.Fatalf("unexpected value %q", val)
	}

	// File should be gone
	if _, err := os.Stat(dir + "/token.json"); !os.IsNotExist(err) {
		t.Fatal("expected token.json to be deleted after migration")
	}

	// Keyring should now have the value
	krVal, err := as.kr.Get("google-oauth-token")
	if err != nil {
		t.Fatalf("expected value in keyring after migration: %v", err)
	}

	if krVal != `{"access_token":"legacy"}` {
		t.Fatalf("unexpected keyring value %q", krVal)
	}
}

func TestAutoStore_FallbackToFile(t *testing.T) {
	keyring.MockInit()

	dir := t.TempDir()
	as := newAutoStore(dir)

	// Set and Get round-trip
	if err := as.Set("google-oauth-token", `{"access_token":"new"}`); err != nil {
		t.Fatalf("AutoStore.Set: %v", err)
	}

	val, err := as.Get("google-oauth-token")
	if err != nil {
		t.Fatalf("AutoStore.Get: %v", err)
	}

	if val != `{"access_token":"new"}` {
		t.Fatalf("unexpected value %q", val)
	}

	// Delete
	if err := as.Delete("google-oauth-token"); err != nil {
		t.Fatalf("AutoStore.Delete: %v", err)
	}

	_, err = as.Get("google-oauth-token")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestMigrateAll(t *testing.T) {
	keyring.MockInit()

	dir := t.TempDir()
	fs := newFileStore(dir)

	// Pre-populate file store
	if err := fs.Set("google-oauth-token", `{"access_token":"t1"}`); err != nil {
		t.Fatal(err)
	}

	if err := MigrateAll(dir, nil); err != nil {
		t.Fatalf("MigrateAll: %v", err)
	}

	// File should be gone
	if _, err := os.Stat(dir + "/token.json"); !os.IsNotExist(err) {
		t.Fatal("expected token.json deleted after MigrateAll")
	}

	// Value in keyring
	kr := newKeyringStore()

	val, err := kr.Get("google-oauth-token")
	if err != nil {
		t.Fatalf("keyring Get: %v", err)
	}

	if val != `{"access_token":"t1"}` {
		t.Fatalf("unexpected value %q", val)
	}
}

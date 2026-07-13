// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

type fakeKeyring struct {
	values    map[string]string
	getError  map[string]error
	deleteErr map[string]error
	gets      int
}

func (backend *fakeKeyring) Get(_, user string) (string, error) {
	backend.gets++
	if err := backend.getError[user]; err != nil {
		return "", err
	}
	value, ok := backend.values[user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (backend *fakeKeyring) Delete(_, user string) error {
	if err := backend.deleteErr[user]; err != nil {
		return err
	}
	if _, ok := backend.values[user]; !ok {
		return keyring.ErrNotFound
	}
	delete(backend.values, user)
	return nil
}

func TestOSStorePersistsCredentialsAcrossProcesses(t *testing.T) {
	backend := newFakeKeyring()
	store, path := newTestOSStore(t, backend)

	assert.Error(t, store.Save(Credentials{}))
	require.NoError(t, store.Save(Credentials{Login: " account ", APIKey: "test-only-secret"}))
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		directoryInfo, statErr := os.Stat(filepath.Dir(path))
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o700), directoryInfo.Mode().Perm())
	}

	backend.getError[loginEntry] = errors.New("legacy keyring must not be used")
	secondProcess := testOSStore(path, backend)
	stored, err := secondProcess.Load()
	require.NoError(t, err)
	assert.Equal(t, Credentials{Login: "account", APIKey: "test-only-secret"}, stored)
	assert.Zero(t, backend.gets)

	delete(backend.getError, loginEntry)
	require.NoError(t, secondProcess.Delete())
	_, err = secondProcess.Load()
	assert.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, secondProcess.Delete(), "deleting absent credentials must be idempotent")
}

func TestOSStoreMigratesLegacyKeyringCredentials(t *testing.T) {
	backend := newFakeKeyring()
	backend.values[loginEntry] = "legacy-account"
	backend.values[apiKeyEntry] = "legacy-key"
	store, path := newTestOSStore(t, backend)

	stored, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, Credentials{Login: "legacy-account", APIKey: "legacy-key"}, stored)
	assert.FileExists(t, path)
	assert.Empty(t, backend.values, "legacy entries should be removed after migration")

	backend.getError[loginEntry] = errors.New("keyring unavailable")
	stored, err = testOSStore(path, backend).Load()
	require.NoError(t, err)
	assert.Equal(t, Credentials{Login: "legacy-account", APIKey: "legacy-key"}, stored)
}

func TestOSStoreWindowsReplacementFlow(t *testing.T) {
	store, _ := newTestOSStore(t, newFakeKeyring())
	store.goos = "windows"
	require.NoError(t, store.Save(Credentials{Login: "account", APIKey: "first-key"}))
	require.NoError(t, store.Save(Credentials{Login: "account", APIKey: "second-key"}))

	stored, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, Credentials{Login: "account", APIKey: "second-key"}, stored)
}

func TestOSStoreRejectsUnsafeOrInvalidCredentialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission validation does not apply on Windows")
	}
	store, path := newTestOSStore(t, newFakeKeyring())
	require.NoError(t, store.Save(Credentials{Login: "account", APIKey: "key"}))

	require.NoError(t, os.Chmod(path, 0o644))
	_, err := store.Load()
	assert.ErrorContains(t, err, "must not be accessible")
	require.NoError(t, os.Chmod(path, 0o600))

	require.NoError(t, os.Chmod(filepath.Dir(path), 0o755))
	_, err = store.Load()
	assert.ErrorContains(t, err, "directory")
	require.NoError(t, os.Chmod(filepath.Dir(path), 0o700))

	require.NoError(t, os.WriteFile(path, []byte(`{"format":1`), 0o600))
	_, err = store.Load()
	assert.ErrorContains(t, err, "decode Beget credential file")

	require.NoError(t, os.WriteFile(path, []byte(`{"format":2,"login":"account","api_key":"key"}`), 0o600))
	_, err = store.Load()
	assert.ErrorContains(t, err, "unsupported Beget credential file format")

	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", maxCredentialFileBytes+1)), 0o600))
	_, err = store.Load()
	assert.ErrorContains(t, err, "exceeds")

	require.NoError(t, os.Remove(path))
	target := filepath.Join(t.TempDir(), "target")
	require.NoError(t, os.WriteFile(target, []byte(`{}`), 0o600))
	require.NoError(t, os.Symlink(target, path))
	_, err = store.Load()
	assert.ErrorContains(t, err, "regular file")
}

func TestOSStoreReportsPathAndLegacyErrors(t *testing.T) {
	expected := errors.New("storage unavailable")
	backend := newFakeKeyring()
	backend.getError[loginEntry] = expected
	store, path := newTestOSStore(t, backend)
	_, err := store.Load()
	assert.ErrorIs(t, err, expected)

	delete(backend.getError, loginEntry)
	backend.values[loginEntry] = "account"
	backend.getError[apiKeyEntry] = expected
	_, err = store.Load()
	assert.ErrorIs(t, err, expected)

	_, err = testOSStore(path, nil).Load()
	assert.ErrorIs(t, err, ErrNotFound)

	store.credentialPath = func() (string, error) { return "", expected }
	assert.ErrorIs(t, store.Save(Credentials{Login: "account", APIKey: "key"}), expected)
	_, err = store.Load()
	assert.ErrorIs(t, err, expected)
	assert.ErrorIs(t, store.Delete(), expected)

	store.credentialPath = func() (string, error) { return "", nil }
	assert.ErrorContains(t, store.Save(Credentials{Login: "account", APIKey: "key"}), "path is empty")
	store.credentialPath = nil
	assert.ErrorContains(t, store.Save(Credentials{Login: "account", APIKey: "key"}), "path is unavailable")
}

func TestOSStoreDeleteReportsLegacyCleanupError(t *testing.T) {
	backend := newFakeKeyring()
	store, path := newTestOSStore(t, backend)
	require.NoError(t, store.Save(Credentials{Login: "account", APIKey: "key"}))
	expected := errors.New("keyring failed")
	backend.deleteErr[loginEntry] = expected

	assert.ErrorIs(t, store.Delete(), expected)
	assert.NoFileExists(t, path)
}

func TestOSStoreDeleteReportsCredentialFileError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.Mkdir(path, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "child"), []byte("occupied"), 0o600))

	err := testOSStore(path, nil).Delete()
	assert.ErrorContains(t, err, "delete Beget credential file")
}

func TestNewOSStore(t *testing.T) {
	store := NewOSStore()
	assert.NotNil(t, store)
	path, err := store.path()
	require.NoError(t, err)
	assert.Contains(t, path, serviceName)
}

func newTestOSStore(t *testing.T, backend *fakeKeyring) (*OSStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), serviceName, credentialFileName)
	return testOSStore(path, backend), path
}

func testOSStore(path string, backend keyringBackend) *OSStore {
	return &OSStore{
		keyring:        backend,
		credentialPath: func() (string, error) { return path, nil },
		goos:           runtime.GOOS,
	}
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{
		values:    make(map[string]string),
		getError:  make(map[string]error),
		deleteErr: make(map[string]error),
	}
}

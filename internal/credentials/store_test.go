// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package credentials

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

type fakeKeyring struct {
	values    map[string]string
	getError  map[string]error
	setError  map[string]error
	deleteErr map[string]error
}

func (backend *fakeKeyring) Get(_, user string) (string, error) {
	if err := backend.getError[user]; err != nil {
		return "", err
	}
	value, ok := backend.values[user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (backend *fakeKeyring) Set(_, user, password string) error {
	if err := backend.setError[user]; err != nil {
		return err
	}
	backend.values[user] = password
	return nil
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

func TestOSStoreLifecycle(t *testing.T) {
	backend := newFakeKeyring()
	store := &OSStore{keyring: backend}

	assert.Error(t, store.Save(Credentials{}))
	require.NoError(t, store.Save(Credentials{Login: " account ", APIKey: "test-only-secret"}))
	stored, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, Credentials{Login: "account", APIKey: "test-only-secret"}, stored)

	require.NoError(t, store.Delete())
	_, err = store.Load()
	assert.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, store.Delete(), "deleting absent credentials must be idempotent")
}

func TestOSStoreReportsBackendErrorsAndRollsBack(t *testing.T) {
	backend := newFakeKeyring()
	store := &OSStore{keyring: backend}
	expected := errors.New("keyring failed")

	backend.getError[loginEntry] = expected
	_, err := store.Load()
	assert.ErrorIs(t, err, expected)
	delete(backend.getError, loginEntry)

	backend.values[loginEntry] = "account"
	backend.getError[apiKeyEntry] = expected
	_, err = store.Load()
	assert.ErrorIs(t, err, expected)
	delete(backend.getError, apiKeyEntry)

	backend.setError[loginEntry] = expected
	assert.ErrorIs(t, store.Save(Credentials{Login: "account", APIKey: "key"}), expected)
	delete(backend.setError, loginEntry)

	delete(backend.values, loginEntry)
	backend.setError[apiKeyEntry] = expected
	assert.ErrorIs(t, store.Save(Credentials{Login: "account", APIKey: "key"}), expected)
	assert.NotContains(t, backend.values, loginEntry, "partially saved login was not rolled back")
	delete(backend.setError, apiKeyEntry)

	backend.values[loginEntry] = "previous-account"
	backend.setError[apiKeyEntry] = expected
	assert.ErrorIs(t, store.Save(Credentials{Login: "new-account", APIKey: "key"}), expected)
	assert.Equal(t, "previous-account", backend.values[loginEntry])
	delete(backend.setError, apiKeyEntry)

	backend.getError[loginEntry] = expected
	assert.ErrorIs(t, store.Save(Credentials{Login: "account", APIKey: "key"}), expected)
	delete(backend.getError, loginEntry)

	backend.deleteErr[loginEntry] = expected
	assert.ErrorIs(t, store.Delete(), expected)
}

func TestOSStoreRejectsEmptyStoredValues(t *testing.T) {
	backend := newFakeKeyring()
	backend.values[loginEntry] = " "
	backend.values[apiKeyEntry] = "key"
	store := &OSStore{keyring: backend}

	_, err := store.Load()
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestNewOSStore(t *testing.T) {
	assert.NotNil(t, NewOSStore())
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{
		values:    make(map[string]string),
		getError:  make(map[string]error),
		setError:  make(map[string]error),
		deleteErr: make(map[string]error),
	}
}

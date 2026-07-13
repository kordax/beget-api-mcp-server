// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"testing"

	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCredentialStore struct {
	value credentials.Credentials
	err   error
	loads int
}

func (store *fakeCredentialStore) Load() (credentials.Credentials, error) {
	store.loads++
	return store.value, store.err
}
func (*fakeCredentialStore) Save(credentials.Credentials) error { return nil }
func (*fakeCredentialStore) Delete() error                      { return nil }

func TestFromEnvironment(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "account")
	t.Setenv("BEGET_API_KEY", "test-only-secret")
	t.Setenv("BEGET_API_BASE_URL", "https://example.invalid/api/")

	store := &fakeCredentialStore{err: errors.New("must not load keyring")}
	config, err := FromSources(store)
	require.NoError(t, err)
	assert.Equal(t, "account", config.Login)
	assert.Equal(t, "test-only-secret", config.APIKey)
	assert.Equal(t, "https://example.invalid/api", config.BaseURL)
	assert.Equal(t, "environment", config.CredentialSource)
	assert.Zero(t, store.loads)
}

func TestFromSourcesUsesStoredCredentials(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "")
	t.Setenv("BEGET_API_KEY", "")
	t.Setenv("BEGET_API_BASE_URL", "")
	store := &fakeCredentialStore{value: credentials.Credentials{Login: "stored-account", APIKey: "stored-key"}}

	config, err := FromSources(store)
	require.NoError(t, err)
	assert.Equal(t, "stored-account", config.Login)
	assert.Equal(t, "stored-key", config.APIKey)
	assert.Equal(t, defaultBaseURL, config.BaseURL)
	assert.Equal(t, "system-keyring", config.CredentialSource)
	assert.Equal(t, 1, store.loads)
}

func TestFromSourcesUsesPartialEnvironmentOverride(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "environment-account")
	t.Setenv("BEGET_API_KEY", "")
	store := &fakeCredentialStore{value: credentials.Credentials{Login: "stored-account", APIKey: "stored-key"}}

	config, err := FromSources(store)
	require.NoError(t, err)
	assert.Equal(t, "environment-account", config.Login)
	assert.Equal(t, "stored-key", config.APIKey)
	assert.Equal(t, "environment-and-keyring", config.CredentialSource)
}

func TestFromSourcesAllowsMissingCredentials(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "")
	t.Setenv("BEGET_API_KEY", "")
	expected := errors.New("keyring unavailable")

	configuration, err := FromSources(&fakeCredentialStore{err: expected})
	require.NoError(t, err)
	assert.ErrorIs(t, configuration.CredentialError, expected)
	assert.Equal(t, "not-configured", configuration.CredentialSource)

	configuration, err = FromSources(nil)
	require.NoError(t, err)
	assert.ErrorIs(t, configuration.CredentialError, credentials.ErrNotFound)

	configuration, err = FromSources(&fakeCredentialStore{})
	require.NoError(t, err)
	assert.ErrorIs(t, configuration.CredentialError, credentials.ErrNotFound)
}

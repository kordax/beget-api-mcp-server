// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package credentials

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	value       Credentials
	loadError   error
	saveError   error
	deleteError error
	deleted     bool
	saved       bool
}

func (store *fakeStore) Load() (Credentials, error) { return store.value, store.loadError }
func (store *fakeStore) Save(value Credentials) error {
	if store.saveError != nil {
		return store.saveError
	}
	store.value = value
	store.saved = true
	return nil
}

type fakeValidator struct {
	values []Credentials
	err    error
}

func (validator *fakeValidator) Validate(_ context.Context, value Credentials) error {
	validator.values = append(validator.values, value)
	return validator.err
}
func (store *fakeStore) Delete() error {
	store.deleted = true
	return store.deleteError
}

func TestCommandRecognitionAndValidation(t *testing.T) {
	assert.True(t, IsCommand([]string{"credentials"}))
	assert.False(t, IsCommand(nil))
	assert.False(t, IsCommand([]string{"--stdio"}))

	command := newTestCommand(t, &fakeStore{}, "secret\n")
	assert.ErrorContains(t, command.Run(t.Context(), nil), "requires set, check, or delete")
	assert.ErrorContains(t, command.Run(t.Context(), []string{"unknown"}), "unknown credentials command")
	assert.ErrorContains(t, command.Run(t.Context(), []string{"check", "extra"}), "does not accept arguments")
	assert.ErrorContains(t, command.Run(t.Context(), []string{"delete", "extra"}), "does not accept arguments")
	assert.ErrorContains(t, command.Run(t.Context(), []string{"set"}), "requires --login")
	assert.ErrorContains(t, command.Run(t.Context(), []string{"set", "--login", "account", "extra"}), "accepts only --login")
}

func TestCommandSetCheckAndDelete(t *testing.T) {
	store := &fakeStore{}
	command := newTestCommand(t, store, "test-only-secret\n")
	validator := command.validator.(*fakeValidator)

	require.NoError(t, command.Run(t.Context(), []string{"set", "--login", " account "}))
	assert.Equal(t, Credentials{Login: "account", APIKey: "test-only-secret"}, store.value)
	assert.True(t, store.saved)
	assert.NotContains(t, command.output.(*bytes.Buffer).String(), "test-only-secret")

	require.NoError(t, command.Run(t.Context(), []string{"check"}))
	assert.Equal(t, []Credentials{store.value, store.value}, validator.values)
	assert.Contains(t, command.output.(*bytes.Buffer).String(), "valid and authorized")
	require.NoError(t, command.Run(t.Context(), []string{"delete"}))
	assert.True(t, store.deleted)
}

func TestCommandRejectsInvalidCredentialsBeforeSaving(t *testing.T) {
	expected := errors.New("Beget rejected credentials")
	store := &fakeStore{value: Credentials{Login: "previous-account", APIKey: "previous-key"}}
	command := newTestCommand(t, store, "invalid-key\n")
	command.validator = &fakeValidator{err: expected}

	err := command.Run(t.Context(), []string{"set", "--login", "account"})
	assert.ErrorIs(t, err, expected)
	assert.EqualError(t, err, "Beget rejected credentials")
	assert.False(t, store.saved)
	assert.Equal(t, Credentials{Login: "previous-account", APIKey: "previous-key"}, store.value)
	assert.NotContains(t, err.Error(), "invalid-key")

	err = command.Run(t.Context(), []string{"check"})
	assert.ErrorIs(t, err, expected)
	assert.EqualError(t, err, "Beget rejected credentials")
}

func TestCommandPropagatesStoreErrors(t *testing.T) {
	expected := errors.New("store failed")

	store := &fakeStore{saveError: expected}
	command := newTestCommand(t, store, "secret\n")
	assert.ErrorIs(t, command.Run(t.Context(), []string{"set", "--login", "account"}), expected)

	store = &fakeStore{loadError: expected}
	command = newTestCommand(t, store, "secret\n")
	assert.ErrorIs(t, command.Run(t.Context(), []string{"check"}), expected)

	store = &fakeStore{deleteError: expected}
	command = newTestCommand(t, store, "secret\n")
	assert.ErrorIs(t, command.Run(t.Context(), []string{"delete"}), expected)
}

func TestReadAPIKeyRejectsMissingInput(t *testing.T) {
	_, err := readAPIKey(nil, &bytes.Buffer{})
	assert.ErrorContains(t, err, "input is unavailable")

	command := newTestCommand(t, &fakeStore{}, "")
	assert.ErrorContains(t, command.Run(t.Context(), []string{"set", "--login", "account"}), "API key is required")
}

func newTestCommand(t *testing.T, store Store, input string) *Command {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	_, err = writer.WriteString(input)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	t.Cleanup(func() { _ = reader.Close() })
	return &Command{
		store:       store,
		validator:   &fakeValidator{},
		input:       reader,
		output:      &bytes.Buffer{},
		errorOutput: &bytes.Buffer{},
	}
}

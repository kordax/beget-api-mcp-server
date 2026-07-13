// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package credentials

import (
	"bytes"
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
}

func (store *fakeStore) Load() (Credentials, error) { return store.value, store.loadError }
func (store *fakeStore) Save(value Credentials) error {
	store.value = value
	return store.saveError
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
	assert.ErrorContains(t, command.Run(nil), "requires set, check, or delete")
	assert.ErrorContains(t, command.Run([]string{"unknown"}), "unknown credentials command")
	assert.ErrorContains(t, command.Run([]string{"check", "extra"}), "does not accept arguments")
	assert.ErrorContains(t, command.Run([]string{"delete", "extra"}), "does not accept arguments")
	assert.ErrorContains(t, command.Run([]string{"set"}), "requires --login")
	assert.ErrorContains(t, command.Run([]string{"set", "--login", "account", "extra"}), "accepts only --login")
}

func TestCommandSetCheckAndDelete(t *testing.T) {
	store := &fakeStore{}
	command := newTestCommand(t, store, "test-only-secret\n")

	require.NoError(t, command.Run([]string{"set", "--login", "account"}))
	assert.Equal(t, Credentials{Login: "account", APIKey: "test-only-secret"}, store.value)
	assert.NotContains(t, command.output.(*bytes.Buffer).String(), "test-only-secret")

	require.NoError(t, command.Run([]string{"check"}))
	require.NoError(t, command.Run([]string{"delete"}))
	assert.True(t, store.deleted)
}

func TestCommandPropagatesStoreErrors(t *testing.T) {
	expected := errors.New("store failed")

	store := &fakeStore{saveError: expected}
	command := newTestCommand(t, store, "secret\n")
	assert.ErrorIs(t, command.Run([]string{"set", "--login", "account"}), expected)

	store = &fakeStore{loadError: expected}
	command = newTestCommand(t, store, "secret\n")
	assert.ErrorIs(t, command.Run([]string{"check"}), expected)

	store = &fakeStore{deleteError: expected}
	command = newTestCommand(t, store, "secret\n")
	assert.ErrorIs(t, command.Run([]string{"delete"}), expected)
}

func TestReadAPIKeyRejectsMissingInput(t *testing.T) {
	_, err := readAPIKey(nil, &bytes.Buffer{})
	assert.ErrorContains(t, err, "input is unavailable")

	command := newTestCommand(t, &fakeStore{}, "")
	assert.ErrorContains(t, command.Run([]string{"set", "--login", "account"}), "API key is required")
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
		input:       reader,
		output:      &bytes.Buffer{},
		errorOutput: &bytes.Buffer{},
	}
}

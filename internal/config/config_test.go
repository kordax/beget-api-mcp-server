// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromEnvironment(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "account")
	t.Setenv("BEGET_API_KEY", "test-only-secret")
	t.Setenv("BEGET_API_BASE_URL", "https://example.invalid/api/")

	config, err := FromEnvironment()
	require.NoError(t, err)
	assert.Equal(t, "account", config.Login)
	assert.Equal(t, "test-only-secret", config.APIKey)
	assert.Equal(t, "https://example.invalid/api", config.BaseURL)
}

func TestFromEnvironmentRequiresCredentials(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "")
	t.Setenv("BEGET_API_KEY", "")

	_, err := FromEnvironment()
	require.Error(t, err)
}

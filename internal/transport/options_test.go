// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package transport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOptionsDefaultsToStdio(t *testing.T) {
	options, err := ParseOptions(nil)

	require.NoError(t, err)
	assert.Equal(t, ModeStdio, options.Mode)
	assert.Equal(t, "/mcp", options.HTTPPath)
}

func TestParseOptionsSelectsHTTPTransports(t *testing.T) {
	t.Run("streamable", func(t *testing.T) {
		options, err := ParseOptions(Arguments{
			"--streamable-http",
			"--http-address", "127.0.0.1:0",
			"--streamable-json-response",
			"--streamable-stateless",
			"--streamable-session-timeout", "1m",
		})

		require.NoError(t, err)
		assert.Equal(t, ModeStreamableHTTP, options.Mode)
		assert.Equal(t, "/mcp", options.HTTPPath)
		assert.True(t, options.JSONResponse)
		assert.True(t, options.StreamableStateless)
		assert.Equal(t, time.Minute, options.SessionTimeout)
	})

	t.Run("legacy sse", func(t *testing.T) {
		options, err := ParseOptions(Arguments{"--sse", "--http-address", "[::1]:0"})

		require.NoError(t, err)
		assert.Equal(t, ModeSSE, options.Mode)
		assert.Equal(t, "/sse", options.HTTPPath)
	})
}

func TestParseOptionsRejectsConflictsAndUnsafeHTTP(t *testing.T) {
	_, err := ParseOptions(Arguments{"--stdio", "--streamable-http"})
	assert.ErrorContains(t, err, "mutually exclusive")

	_, err = ParseOptions(Arguments{"--streamable-http", "--http-address", "0.0.0.0:8080"})
	assert.ErrorContains(t, err, "loopback")

	_, err = ParseOptions(Arguments{"--sse", "--http-path", "relative"})
	assert.ErrorContains(t, err, "clean absolute path")

	_, err = ParseOptions(Arguments{"--streamable-json-response"})
	assert.ErrorContains(t, err, "require --streamable-http")

	_, err = ParseOptions(Arguments{"--sse", "--streamable-session-timeout", "1m"})
	assert.ErrorContains(t, err, "require --streamable-http")
}

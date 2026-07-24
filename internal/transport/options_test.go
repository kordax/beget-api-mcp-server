// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package transport

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOptionsDefaultsToStdio(t *testing.T) {
	options, err := ParseOptions(nil)

	require.NoError(t, err)
	assert.Equal(t, ModeStdio, options.Mode)
	assert.Empty(t, options.ToolSections)
	assert.Equal(t, "/mcp", options.HTTPPath)
}

func TestParseOptionsSelectsToolSections(t *testing.T) {
	options, err := ParseOptions(Arguments{
		"--tool-sections", "DNS, site, dns, account, statistics",
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"dns", "site", "user", "stat"}, options.ToolSections)
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

func TestCommandLineArguments(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"beget-api-mcp-server", "--stdio", "value"}

	assert.Equal(t, Arguments{"--stdio", "value"}, CommandLineArguments())
}

func TestParseOptionsRejectsMalformedArguments(t *testing.T) {
	for name, testCase := range map[string]struct {
		arguments Arguments
		expected  string
	}{
		"unknown flag":        {Arguments{"--unknown"}, "flag provided but not defined"},
		"positional argument": {Arguments{"unexpected"}, "unexpected positional arguments"},
		"empty tool sections": {Arguments{"--tool-sections", ""}, "tool sections cannot be empty"},
		"empty tool section item": {
			Arguments{"--tool-sections", "dns,,site"}, "empty value",
		},
		"all combined with section": {
			Arguments{"--tool-sections", "all,dns"}, "cannot be combined",
		},
		"unknown tool section": {
			Arguments{"--tool-sections", "cloud"}, "unknown tool section",
		},
		"negative timeout": {Arguments{
			"--streamable-http", "--streamable-session-timeout", "-1s",
		}, "session timeout cannot be negative"},
		"malformed address": {Arguments{
			"--streamable-http", "--http-address", "localhost",
		}, "invalid HTTP address"},
		"invalid port": {Arguments{
			"--streamable-http", "--http-address", "localhost:not-a-port",
		}, "invalid HTTP port"},
		"non-loopback hostname": {Arguments{
			"--streamable-http", "--http-address", "example.com:8080",
		}, "loopback"},
		"root path": {Arguments{
			"--streamable-http", "--http-path", "/",
		}, "clean absolute path"},
		"unclean path": {Arguments{
			"--streamable-http", "--http-path", "/mcp/../other",
		}, "clean absolute path"},
		"query path": {Arguments{
			"--streamable-http", "--http-path", "/mcp?query=true",
		}, "query or fragment"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseOptions(testCase.arguments)
			assert.ErrorContains(t, err, testCase.expected)
		})
	}
}

func TestParseOptionsAcceptsLocalhostAndCustomPath(t *testing.T) {
	options, err := ParseOptions(Arguments{
		"--sse",
		"--http-address", "localhost:0",
		"--http-path", "/events",
	})

	require.NoError(t, err)
	assert.Equal(t, ModeSSE, options.Mode)
	assert.Equal(t, "/events", options.HTTPPath)
}

func TestParseOptionsHTTPAuthentication(t *testing.T) {
	t.Run("requires HTTP transport", func(t *testing.T) {
		_, err := ParseOptions(Arguments{"--http-auth"})
		assert.ErrorContains(t, err, "requires --streamable-http or --sse")
	})

	t.Run("requires token", func(t *testing.T) {
		t.Setenv("BEGET_MCP_HTTP_TOKEN", "")
		_, err := ParseOptions(Arguments{"--sse", "--http-auth"})
		assert.ErrorContains(t, err, "BEGET_MCP_HTTP_TOKEN is required")
	})

	t.Run("rejects weak token", func(t *testing.T) {
		t.Setenv("BEGET_MCP_HTTP_TOKEN", "too-short")
		_, err := ParseOptions(Arguments{"--sse", "--http-auth"})
		assert.ErrorContains(t, err, "at least 32 characters")
	})

	t.Run("accepts token from environment", func(t *testing.T) {
		token := "test-only-token-with-at-least-32-characters"
		t.Setenv("BEGET_MCP_HTTP_TOKEN", token)
		options, err := ParseOptions(Arguments{"--streamable-http", "--http-auth"})
		require.NoError(t, err)
		assert.Equal(t, token, options.HTTPBearerToken)
	})
}

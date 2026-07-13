// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kordax/beget-api-mcp-server/internal/buildinfo"
	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
)

func TestRunShowsHelp(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		contains  []string
	}{
		{name: "help command", arguments: []string{"help"}, contains: []string{"Usage:", "Transport options:", "credentials"}},
		{name: "long flag", arguments: []string{"--help"}, contains: []string{"Usage:", "--streamable-http"}},
		{name: "short flag after transport option", arguments: []string{"--stdio", "-h"}, contains: []string{"Usage:", "--stdio"}},
		{name: "credentials", arguments: []string{"help", "credentials"}, contains: []string{"credentials set", "credentials check", "credentials delete"}},
		{name: "credentials alias", arguments: []string{"credentials", "--help"}, contains: []string{"system keyring", "credentials set"}},
		{name: "credentials set", arguments: []string{"credentials", "set", "--help"}, contains: []string{"--login <login>", "never accepted"}},
		{name: "upgrade", arguments: []string{"upgrade", "help"}, contains: []string{"upgrade --check", "latest release"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			var errorOutput bytes.Buffer

			assert.Equal(t, 0, Run(test.arguments, &output, &errorOutput))
			assert.Empty(t, errorOutput.String())
			for _, expected := range test.contains {
				assert.Contains(t, output.String(), expected)
			}
		})
	}
}

func TestRunRejectsUnknownHelpTopic(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer

	assert.Equal(t, 2, Run([]string{"help", "missing"}, &output, &errorOutput))
	assert.Empty(t, output.String())
	assert.Contains(t, errorOutput.String(), `unknown help topic "missing"`)
	assert.Contains(t, errorOutput.String(), "beget-api-mcp-server help")
}

func TestRunValidatesCredentialCommands(t *testing.T) {
	var output bytes.Buffer
	assert.Equal(t, 1, Run([]string{"credentials"}, io.Discard, &output))
	assert.Contains(t, output.String(), "requires set, check, or delete")

	output.Reset()
	assert.Equal(t, 1, Run([]string{"credentials", "unknown"}, io.Discard, &output))
	assert.Contains(t, output.String(), "unknown credentials command")
}

func TestRunValidatesUpgradeCommand(t *testing.T) {
	var output bytes.Buffer
	assert.Equal(t, 1, Run([]string{"upgrade", "one", "two"}, io.Discard, &output))
	assert.Contains(t, output.String(), "at most one version")

	output.Reset()
	assert.Equal(t, 0, Run([]string{"upgrade", "v" + buildinfo.Version}, io.Discard, &output))
	assert.Empty(t, output.String())
}

func TestRunServerReportsConciseRootCause(t *testing.T) {
	var output bytes.Buffer
	expected := errors.New("stored Beget credentials were not found")

	exitCode := runServer(&output, fx.Invoke(func() error { return expected }))

	assert.Equal(t, 1, exitCode)
	assert.Equal(t, "start MCP server: stored Beget credentials were not found\n", output.String())
	assert.NotContains(t, output.String(), "[Fx]")
}

func TestRunServerReturnsAfterCleanShutdown(t *testing.T) {
	var output bytes.Buffer
	shutdownOnStart := fx.Invoke(func(lifecycle fx.Lifecycle, shutdowner fx.Shutdowner) {
		lifecycle.Append(fx.Hook{
			OnStart: func(context.Context) error {
				return shutdowner.Shutdown()
			},
		})
	})

	assert.Equal(t, 0, runServer(&output, shutdownOnStart))
	assert.Empty(t, output.String())
}

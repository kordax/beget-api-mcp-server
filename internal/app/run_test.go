// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
)

func TestRunValidatesCredentialCommands(t *testing.T) {
	var output bytes.Buffer
	assert.Equal(t, 1, Run([]string{"credentials"}, &output))
	assert.Contains(t, output.String(), "requires set, check, or delete")

	output.Reset()
	assert.Equal(t, 1, Run([]string{"credentials", "unknown"}, &output))
	assert.Contains(t, output.String(), "unknown credentials command")
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

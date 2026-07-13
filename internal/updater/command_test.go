// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package updater

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandChecksVersionsAndValidatesArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, `{"tag_name":"v0.3.1"}`)
	}))
	defer server.Close()
	instance := &Updater{client: server.Client(), currentVersion: "0.3.0", apiBaseURL: server.URL}
	var output bytes.Buffer
	command := &Command{updater: instance, output: &output}

	assert.NoError(t, command.Run(context.Background(), []string{"--check"}))
	assert.Contains(t, output.String(), "Current version: v0.3.0")
	assert.Contains(t, output.String(), "Latest version: v0.3.1")
	assert.Error(t, command.Run(context.Background(), []string{"--check", "v0.3.1"}))
	assert.Error(t, command.Run(context.Background(), []string{"one", "two"}))
	assert.Error(t, command.Run(context.Background(), []string{"--unknown"}))
	assert.True(t, IsCommand([]string{"upgrade"}))
	assert.False(t, IsCommand(nil))
}

func TestCommandReportsAlreadyInstalled(t *testing.T) {
	var output bytes.Buffer
	command := &Command{updater: &Updater{currentVersion: "0.3.0"}, output: &output}
	assert.NoError(t, command.Run(context.Background(), []string{"v0.3.0"}))
	assert.Contains(t, output.String(), "already installed")
}

// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCommandShowsInteractiveUpgradeProgress(t *testing.T) {
	binary := []byte("upgraded binary")
	archiveName := "beget-api-mcp-server_v0.3.1_linux_amd64.tar.gz"
	archive := makeTarball(t, "beget-api-mcp-server_v0.3.1_linux_amd64/beget-api-mcp-server", binary)
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(3 * time.Millisecond)
		switch request.URL.Path {
		case "/api/releases/latest":
			_, _ = io.WriteString(response, `{"tag_name":"v0.3.1"}`)
		case "/download/v0.3.1/" + archiveName:
			_, _ = response.Write(archive)
		case "/download/v0.3.1/checksums.txt":
			_, _ = fmt.Fprintf(response, "%x  %s\n", digest, archiveName)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "beget-api-mcp-server")
	require.NoError(t, os.WriteFile(executable, []byte("old binary"), 0o755))
	instance := &Updater{
		client: server.Client(), currentVersion: "0.3.0", goos: "linux", goarch: "amd64",
		executable: func() (string, error) { return executable, nil },
		apiBaseURL: server.URL + "/api", releaseBaseURL: server.URL + "/download",
	}
	var output bytes.Buffer
	command := &Command{
		updater: instance,
		output:  &output,
		spinner: &spinner{output: &output, enabled: true, interval: time.Millisecond},
	}

	require.NoError(t, command.Run(context.Background(), nil))
	assert.Contains(t, output.String(), "Checking for updates...")
	assert.Contains(t, output.String(), "Updating to v0.3.1...")
	assert.Contains(t, output.String(), "\x1b[2K")
	assert.Contains(t, output.String(), "Updated beget-api-mcp-server to v0.3.1")
	installed, err := os.ReadFile(executable)
	require.NoError(t, err)
	assert.Equal(t, binary, installed)
}

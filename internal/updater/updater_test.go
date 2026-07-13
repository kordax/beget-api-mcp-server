// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeDownloadsVerifiesAndReplacesExecutable(t *testing.T) {
	const version = "v0.3.1"
	binary := []byte("upgraded binary")
	archiveName := "beget-api-mcp-server_v0.3.1_linux_amd64.tar.gz"
	archive := makeTarball(t, "beget-api-mcp-server_v0.3.1_linux_amd64/beget-api-mcp-server", binary)
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Bearer private-test-token", request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/releases/latest":
			_, _ = io.WriteString(response, `{"tag_name":"v0.3.1"}`)
		case "/download/v0.3.1/" + archiveName:
			_, _ = response.Write(archive)
		case "/download/v0.3.1/checksums.txt":
			_, _ = fmt.Fprintf(response, "%x  ./%s\n", digest, archiveName)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "beget-api-mcp-server")
	require.NoError(t, os.WriteFile(executable, []byte("old binary"), 0o755))
	instance := &Updater{
		client:         server.Client(),
		currentVersion: "0.3.0",
		goos:           "linux",
		goarch:         "amd64",
		executable:     func() (string, error) { return executable, nil },
		apiBaseURL:     server.URL,
		releaseBaseURL: server.URL + "/download",
		token:          "private-test-token",
	}

	updated, err := instance.Upgrade(context.Background(), "latest")
	require.NoError(t, err)
	assert.Equal(t, version, updated)
	installed, err := os.ReadFile(executable)
	require.NoError(t, err)
	assert.Equal(t, binary, installed)
}

func TestUpgradeHandlesCurrentAndUnsupportedTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, `{"tag_name":"v0.3.0"}`)
	}))
	defer server.Close()
	instance := &Updater{client: server.Client(), currentVersion: "0.3.0", goos: "linux", goarch: "amd64", apiBaseURL: server.URL}

	version, err := instance.Upgrade(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "v0.3.0", version)

	instance.currentVersion = "0.2.0"
	instance.goos = "plan9"
	_, err = instance.Upgrade(context.Background(), "v0.3.0")
	assert.ErrorContains(t, err, "not supported on plan9")
	instance.goos = "linux"
	instance.goarch = "386"
	_, err = instance.Upgrade(context.Background(), "v0.3.0")
	assert.ErrorContains(t, err, "not supported on 386")
}

func TestUpgradeUsesWindowsZIPAndReportsIntegrityErrors(t *testing.T) {
	const version = "v0.3.1"
	binary := []byte("windows binary")
	archiveName := "beget-api-mcp-server_v0.3.1_windows_arm64.zip"
	archive := makeZIP(t, "beget-api-mcp-server_v0.3.1_windows_arm64/beget-api-mcp-server.exe", binary)
	digest := sha256.Sum256(archive)
	badChecksum := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/download/v0.3.1/" + archiveName:
			_, _ = response.Write(archive)
		case "/download/v0.3.1/checksums.txt":
			if badChecksum {
				_, _ = fmt.Fprintf(response, "%s  %s\n", strings.Repeat("0", 64), archiveName)
			} else {
				_, _ = fmt.Fprintf(response, "%x  %s\n", digest, archiveName)
			}
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "beget-api-mcp-server.exe")
	require.NoError(t, os.WriteFile(executable, []byte("old"), 0o755))
	instance := &Updater{
		client: server.Client(), currentVersion: "0.3.0", goos: "windows", goarch: "arm64",
		executable: func() (string, error) { return executable, nil }, releaseBaseURL: server.URL + "/download",
	}
	updated, err := instance.Upgrade(context.Background(), version)
	require.NoError(t, err)
	assert.Equal(t, version, updated)
	installed, err := os.ReadFile(executable)
	require.NoError(t, err)
	assert.Equal(t, binary, installed)

	badChecksum = true
	instance.currentVersion = "0.2.0"
	_, err = instance.Upgrade(context.Background(), version)
	assert.ErrorContains(t, err, "checksum verification failed")
}

func TestUpgradeReportsExecutableLookupFailure(t *testing.T) {
	const version = "v0.3.1"
	archiveName := "beget-api-mcp-server_v0.3.1_linux_amd64.tar.gz"
	archive := makeTarball(t, "beget-api-mcp-server_v0.3.1_linux_amd64/beget-api-mcp-server", []byte("binary"))
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "checksums.txt") {
			_, _ = fmt.Fprintf(response, "%x  %s\n", digest, archiveName)
			return
		}
		_, _ = response.Write(archive)
	}))
	defer server.Close()
	expected := errors.New("executable unavailable")
	instance := &Updater{
		client: server.Client(), currentVersion: "0.3.0", goos: "linux", goarch: "amd64",
		executable: func() (string, error) { return "", expected }, releaseBaseURL: server.URL,
	}
	_, err := instance.Upgrade(context.Background(), version)
	assert.ErrorIs(t, err, expected)
}

func TestVersionAndChecksumValidation(t *testing.T) {
	for input, expected := range map[string]string{"0.3.0": "v0.3.0", " v1.2.3 ": "v1.2.3"} {
		actual, err := normalizeVersion(input)
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
	for _, invalid := range []string{"", "v1", "v1.2.beta", "v1.2.3.4"} {
		_, err := normalizeVersion(invalid)
		assert.Error(t, err)
	}

	body := []byte("archive")
	digest := sha256.Sum256(body)
	assert.NoError(t, verifyChecksum("release.tar.gz", body, []byte(fmt.Sprintf("%x  release.tar.gz\n", digest))))
	assert.ErrorContains(t, verifyChecksum("missing", body, nil), "missing")
	assert.ErrorContains(t, verifyChecksum("release.tar.gz", body, []byte(strings.Repeat("0", 64)+"  release.tar.gz\n")), "failed")
}

func TestExtractBinaryFromTarAndZip(t *testing.T) {
	path := "package/beget-api-mcp-server"
	binary := []byte("binary")
	extracted, err := extractBinary("release.tar.gz", makeTarball(t, path, binary), path)
	require.NoError(t, err)
	assert.Equal(t, binary, extracted)
	extracted, err = extractBinary("release.zip", makeZIP(t, path, binary), path)
	require.NoError(t, err)
	assert.Equal(t, binary, extracted)

	_, err = extractBinary("release.tar.gz", makeTarball(t, "other", binary), path)
	assert.ErrorContains(t, err, "expected binary")
	_, err = extractBinary("release.zip", makeZIP(t, "other", binary), path)
	assert.ErrorContains(t, err, "expected binary")
	_, err = extractBinary("release.tar.gz", []byte("invalid"), path)
	assert.ErrorContains(t, err, "tarball")
	_, err = extractBinary("release.zip", []byte("invalid"), path)
	assert.ErrorContains(t, err, "ZIP")
}

func TestReplaceExecutableSupportsUnixAndWindowsFlow(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "server")
			require.NoError(t, os.WriteFile(executable, []byte("old"), 0o700))
			require.NoError(t, replaceExecutable(executable, []byte("new"), goos))
			body, err := os.ReadFile(executable)
			require.NoError(t, err)
			assert.Equal(t, []byte("new"), body)
		})
	}
	assert.Error(t, replaceExecutable(filepath.Join(t.TempDir(), "missing"), []byte("new"), "linux"))
}

func TestLatestVersionReportsHTTPAndJSONErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"HTTP": func(response http.ResponseWriter, _ *http.Request) { http.Error(response, "no", http.StatusNotFound) },
		"JSON": func(response http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(response, "not-json") },
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			_, err := (&Updater{client: server.Client(), apiBaseURL: server.URL}).LatestVersion(context.Background())
			assert.Error(t, err)
		})
	}
}

func makeTarball(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}))
	_, err := tarWriter.Write(body)
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return output.Bytes()
}

func makeZIP(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	file, err := writer.Create(name)
	require.NoError(t, err)
	_, err = file.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return output.Bytes()
}

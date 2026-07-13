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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kordax/beget-api-mcp-server/internal/buildinfo"
)

const (
	repository       = "kordax/beget-api-mcp-server"
	maxDownloadBytes = 64 << 20
)

type Updater struct {
	client         *http.Client
	currentVersion string
	goos           string
	goarch         string
	executable     func() (string, error)
	apiBaseURL     string
	releaseBaseURL string
	token          string
}

func New() *Updater {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return &Updater{
		client:         &http.Client{Timeout: 2 * time.Minute},
		currentVersion: buildinfo.Version,
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
		executable:     os.Executable,
		apiBaseURL:     "https://api.github.com/repos/" + repository,
		releaseBaseURL: "https://github.com/" + repository + "/releases/download",
		token:          token,
	}
}

func (updater *Updater) LatestVersion(ctx context.Context) (string, error) {
	body, err := updater.download(ctx, updater.apiBaseURL+"/releases/latest")
	if err != nil {
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("decode latest release: %w", err)
	}
	return normalizeVersion(release.TagName)
}

func (updater *Updater) Upgrade(ctx context.Context, requestedVersion string) (string, error) {
	version := requestedVersion
	var err error
	if version == "" || version == "latest" {
		version, err = updater.LatestVersion(ctx)
	} else {
		version, err = normalizeVersion(version)
	}
	if err != nil {
		return "", err
	}
	if version == "v"+strings.TrimPrefix(updater.currentVersion, "v") {
		return version, nil
	}
	if updater.goos != "linux" && updater.goos != "darwin" && updater.goos != "windows" {
		return "", fmt.Errorf("self-upgrade is not supported on %s", updater.goos)
	}
	if updater.goarch != "amd64" && updater.goarch != "arm64" {
		return "", fmt.Errorf("self-upgrade is not supported on %s", updater.goarch)
	}

	extension := ".tar.gz"
	binaryName := "beget-api-mcp-server"
	if updater.goos == "windows" {
		extension = ".zip"
		binaryName += ".exe"
	}
	archiveName := fmt.Sprintf("beget-api-mcp-server_%s_%s_%s%s", version, updater.goos, updater.goarch, extension)
	releaseURL := updater.releaseBaseURL + "/" + version
	archive, err := updater.download(ctx, releaseURL+"/"+archiveName)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", archiveName, err)
	}
	checksums, err := updater.download(ctx, releaseURL+"/checksums.txt")
	if err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyChecksum(archiveName, archive, checksums); err != nil {
		return "", err
	}

	packageDirectory := fmt.Sprintf("beget-api-mcp-server_%s_%s_%s", version, updater.goos, updater.goarch)
	binary, err := extractBinary(archiveName, archive, packageDirectory+"/"+binaryName)
	if err != nil {
		return "", err
	}
	executable, err := updater.executable()
	if err != nil {
		return "", fmt.Errorf("locate current executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	if err := replaceExecutable(executable, binary, updater.goos); err != nil {
		return "", err
	}
	return version, nil
}

func (updater *Updater) download(ctx context.Context, target string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	accept := "application/octet-stream"
	if strings.HasPrefix(target, updater.apiBaseURL+"/") {
		accept = "application/vnd.github+json"
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "beget-api-mcp-server/"+updater.currentVersion)
	if updater.token != "" {
		request.Header.Set("Authorization", "Bearer "+updater.token)
	}
	response, err := updater.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDownloadBytes {
		return nil, fmt.Errorf("download exceeds %d bytes", maxDownloadBytes)
	}
	return body, nil
}

func normalizeVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid release version %q", value)
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return "", fmt.Errorf("invalid release version %q", value)
		}
	}
	return value, nil
}

func verifyChecksum(name string, body, checksums []byte) error {
	expected := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "./") == name {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum is missing for %s", name)
	}
	actualBytes := sha256.Sum256(body)
	actual := hex.EncodeToString(actualBytes[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum verification failed for %s", name)
	}
	return nil
}

func extractBinary(archiveName string, body []byte, expectedPath string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return nil, fmt.Errorf("open release ZIP: %w", err)
		}
		for _, file := range reader.File {
			if filepath.ToSlash(file.Name) != expectedPath {
				continue
			}
			stream, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("open binary in release ZIP: %w", err)
			}
			binary, readErr := io.ReadAll(io.LimitReader(stream, maxDownloadBytes+1))
			closeErr := stream.Close()
			if readErr != nil || closeErr != nil {
				return nil, errors.Join(readErr, closeErr)
			}
			return binary, nil
		}
		return nil, errors.New("release ZIP does not contain the expected binary")
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("open release tarball: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release tarball: %w", err)
		}
		if filepath.ToSlash(header.Name) == expectedPath && header.Typeflag == tar.TypeReg {
			binary, err := io.ReadAll(io.LimitReader(tarReader, maxDownloadBytes+1))
			if err != nil {
				return nil, fmt.Errorf("read binary from release tarball: %w", err)
			}
			return binary, nil
		}
	}
	return nil, errors.New("release tarball does not contain the expected binary")
}

func replaceExecutable(executable string, binary []byte, goos string) error {
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	directory := filepath.Dir(executable)
	temporary, err := os.CreateTemp(directory, ".beget-api-mcp-server-upgrade-*")
	if err != nil {
		return fmt.Errorf("create upgrade file beside executable: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(binary); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write upgraded executable: %w", err)
	}
	if err := temporary.Chmod(info.Mode().Perm() | 0o500); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set upgraded executable permissions: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync upgraded executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close upgraded executable: %w", err)
	}

	if goos != "windows" {
		if err := os.Rename(temporaryName, executable); err != nil {
			return fmt.Errorf("replace current executable: %w", err)
		}
		return nil
	}

	backup := executable + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(executable, backup); err != nil {
		return fmt.Errorf("prepare Windows executable replacement: %w", err)
	}
	if err := os.Rename(temporaryName, executable); err != nil {
		_ = os.Rename(backup, executable)
		return fmt.Errorf("replace Windows executable: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

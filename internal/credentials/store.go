// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
	"go.uber.org/fx"
)

const (
	serviceName              = "beget-api-mcp-server"
	loginEntry               = "hosting-login"
	apiKeyEntry              = "hosting-api-key"
	credentialFileName       = "credentials.json"
	credentialDocumentFormat = 1
	maxCredentialFileBytes   = 64 << 10
)

var ErrNotFound = errors.New("stored Beget credentials were not found")

type Credentials struct {
	Login  string
	APIKey string
}

type Store interface {
	Load() (Credentials, error)
	Save(Credentials) error
	Delete() error
}

type keyringBackend interface {
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (systemKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

type credentialDocument struct {
	Format int    `json:"format"`
	Login  string `json:"login"`
	APIKey string `json:"api_key"`
}

type OSStore struct {
	keyring        keyringBackend
	credentialPath func() (string, error)
	goos           string
}

var Module = fx.Module("credentials",
	fx.Provide(fx.Annotate(NewOSStore, fx.As(new(Store)))),
)

func NewOSStore() *OSStore {
	return &OSStore{keyring: systemKeyring{}, credentialPath: defaultCredentialPath, goos: runtime.GOOS}
}

func (store *OSStore) Load() (Credentials, error) {
	value, err := store.loadFile()
	if err == nil || !errors.Is(err, ErrNotFound) {
		return value, err
	}

	value, err = store.loadLegacyKeyring()
	if err != nil {
		if concurrentValue, concurrentErr := store.loadFile(); concurrentErr == nil {
			return concurrentValue, nil
		}
		return Credentials{}, err
	}
	if err := store.saveFile(value); err != nil {
		return Credentials{}, fmt.Errorf("migrate Beget credentials from system keyring: %w", err)
	}
	_ = store.deleteLegacyKeyring()
	return value, nil
}

func (store *OSStore) Save(value Credentials) error {
	value, err := validCredentials(value)
	if err != nil {
		return err
	}
	if err := store.saveFile(value); err != nil {
		return err
	}
	return nil
}

func (store *OSStore) Delete() error {
	var result error
	path, err := store.path()
	if err != nil {
		result = errors.Join(result, err)
	} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		result = errors.Join(result, fmt.Errorf("delete Beget credential file: %w", err))
	}
	return errors.Join(result, store.deleteLegacyKeyring())
}

func (store *OSStore) loadFile() (Credentials, error) {
	path, err := store.path()
	if err != nil {
		return Credentials{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrNotFound
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("inspect Beget credential file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Credentials{}, errors.New("beget credential file must be a regular file")
	}
	if err := store.checkPermissions(path, info); err != nil {
		return Credentials{}, err
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return Credentials{}, fmt.Errorf("open Beget credential directory: %w", err)
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return Credentials{}, fmt.Errorf("open Beget credential file: %w", err)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxCredentialFileBytes+1))
	if err != nil {
		return Credentials{}, fmt.Errorf("read Beget credential file: %w", err)
	}
	if len(body) > maxCredentialFileBytes {
		return Credentials{}, fmt.Errorf("beget credential file exceeds %d bytes", maxCredentialFileBytes)
	}
	var document credentialDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return Credentials{}, fmt.Errorf("decode Beget credential file: %w", err)
	}
	if document.Format != credentialDocumentFormat {
		return Credentials{}, fmt.Errorf("unsupported Beget credential file format %d", document.Format)
	}
	return validCredentials(Credentials{Login: document.Login, APIKey: document.APIKey})
}

func (store *OSStore) saveFile(value Credentials) error {
	path, err := store.path()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Beget credential directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect Beget credential directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("beget credential directory must be a regular directory")
	}
	if store.goos != "windows" {
		// #nosec G302 -- directories need owner execute permission and reject all group and other access.
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("secure Beget credential directory: %w", err)
		}
	}

	// #nosec G117 -- this is the intentional serialization boundary for the owner-only credential file.
	body, err := json.Marshal(credentialDocument{
		Format: credentialDocumentFormat,
		Login:  value.Login,
		APIKey: value.APIKey,
	})
	if err != nil {
		return fmt.Errorf("encode Beget credential file: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create temporary Beget credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary Beget credential file: %w", err)
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary Beget credential file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary Beget credential file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Beget credential file: %w", err)
	}
	if store.goos == "windows" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("replace Beget credential file: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Beget credential file: %w", err)
	}
	return nil
}

func (store *OSStore) checkPermissions(path string, info os.FileInfo) error {
	if store.goos == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("beget credential file %s must not be accessible by group or other users", path)
	}
	directoryInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("inspect Beget credential directory: %w", err)
	}
	if !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("beget credential directory must be a regular directory")
	}
	if directoryInfo.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("beget credential directory %s must not be accessible by group or other users", filepath.Dir(path))
	}
	return nil
}

func (store *OSStore) loadLegacyKeyring() (Credentials, error) {
	if store.keyring == nil {
		return Credentials{}, ErrNotFound
	}
	login, err := store.keyring.Get(serviceName, loginEntry)
	if err != nil {
		return Credentials{}, loadError("login", err)
	}
	apiKey, err := store.keyring.Get(serviceName, apiKeyEntry)
	if err != nil {
		return Credentials{}, loadError("API key", err)
	}
	return validCredentials(Credentials{Login: login, APIKey: apiKey})
}

func (store *OSStore) deleteLegacyKeyring() error {
	if store.keyring == nil {
		return nil
	}
	var result error
	for _, entry := range []string{loginEntry, apiKeyEntry} {
		if err := store.keyring.Delete(serviceName, entry); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			result = errors.Join(result, fmt.Errorf("delete legacy system keyring entry: %w", err))
		}
	}
	return result
}

func (store *OSStore) path() (string, error) {
	if store.credentialPath == nil {
		return "", errors.New("beget credential file path is unavailable")
	}
	path, err := store.credentialPath()
	if err != nil {
		return "", fmt.Errorf("locate Beget credential file: %w", err)
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("beget credential file path is empty")
	}
	return path, nil
}

func defaultCredentialPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, serviceName, credentialFileName), nil
}

func validCredentials(value Credentials) (Credentials, error) {
	value.Login = strings.TrimSpace(value.Login)
	if value.Login == "" || value.APIKey == "" {
		return Credentials{}, errors.New("beget login and API key are required")
	}
	return value, nil
}

func loadError(name string, err error) error {
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("read Beget %s from system keyring: %w", name, err)
}

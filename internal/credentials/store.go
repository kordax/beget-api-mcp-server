// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package credentials

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
	"go.uber.org/fx"
)

const (
	serviceName = "beget-api-mcp-server"
	loginEntry  = "hosting-login"
	apiKeyEntry = "hosting-api-key"
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
	Set(service, user, password string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (systemKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

type OSStore struct {
	keyring keyringBackend
}

var Module = fx.Module("credentials",
	fx.Provide(fx.Annotate(NewOSStore, fx.As(new(Store)))),
)

func NewOSStore() *OSStore {
	return &OSStore{keyring: systemKeyring{}}
}

func (store *OSStore) Load() (Credentials, error) {
	login, err := store.keyring.Get(serviceName, loginEntry)
	if err != nil {
		return Credentials{}, loadError("login", err)
	}
	apiKey, err := store.keyring.Get(serviceName, apiKeyEntry)
	if err != nil {
		return Credentials{}, loadError("API key", err)
	}
	if strings.TrimSpace(login) == "" || apiKey == "" {
		return Credentials{}, ErrNotFound
	}
	return Credentials{Login: login, APIKey: apiKey}, nil
}

func (store *OSStore) Save(value Credentials) error {
	value.Login = strings.TrimSpace(value.Login)
	if value.Login == "" || value.APIKey == "" {
		return errors.New("beget login and API key are required")
	}
	previousLogin, previousErr := store.keyring.Get(serviceName, loginEntry)
	if previousErr != nil && !errors.Is(previousErr, keyring.ErrNotFound) {
		return fmt.Errorf("read existing Beget login from system keyring: %w", previousErr)
	}
	if err := store.keyring.Set(serviceName, loginEntry, value.Login); err != nil {
		return fmt.Errorf("store Beget login in system keyring: %w", err)
	}
	if err := store.keyring.Set(serviceName, apiKeyEntry, value.APIKey); err != nil {
		if previousErr == nil {
			_ = store.keyring.Set(serviceName, loginEntry, previousLogin)
		} else {
			_ = store.keyring.Delete(serviceName, loginEntry)
		}
		return fmt.Errorf("store Beget API key in system keyring: %w", err)
	}
	return nil
}

func (store *OSStore) Delete() error {
	var result error
	for _, entry := range []string{loginEntry, apiKeyEntry} {
		if err := store.keyring.Delete(serviceName, entry); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			result = errors.Join(result, fmt.Errorf("delete system keyring entry: %w", err))
		}
	}
	return result
}

func loadError(name string, err error) error {
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("read Beget %s from system keyring: %w", name, err)
}

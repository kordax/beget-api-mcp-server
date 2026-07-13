// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"strings"
	"time"

	"github.com/kordax/basic-utils/v3/uos"
	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"go.uber.org/fx"
)

const defaultBaseURL = "https://api.beget.com/api"

type Config struct {
	Login            string
	APIKey           string
	BaseURL          string
	Timeout          time.Duration
	CredentialSource string
	CredentialError  error
}

var Module = fx.Module("config", fx.Provide(FromSources))

func FromSources(store credentials.Store) (Config, error) {
	login := strings.TrimSpace(os.Getenv("BEGET_API_LOGIN"))
	apiKey := os.Getenv("BEGET_API_KEY")
	source := "environment"
	var credentialError error
	if login == "" || apiKey == "" {
		source = "system-keyring"
		if store != nil {
			stored, err := store.Load()
			if err == nil {
				if login == "" {
					login = stored.Login
				}
				if apiKey == "" {
					apiKey = stored.APIKey
				}
				if os.Getenv("BEGET_API_LOGIN") != "" || os.Getenv("BEGET_API_KEY") != "" {
					source = "environment-and-keyring"
				}
			} else {
				credentialError = err
			}
		} else {
			credentialError = credentials.ErrNotFound
		}
	}
	if login == "" || apiKey == "" {
		source = "not-configured"
		if credentialError == nil {
			credentialError = credentials.ErrNotFound
		}
	}
	baseURL := uos.GetEnvOptAs("BEGET_API_BASE_URL", uos.MapStringToTrimmed).OrElse(defaultBaseURL)
	config := Config{
		Login:            login,
		APIKey:           apiKey,
		BaseURL:          strings.TrimRight(baseURL, "/"),
		Timeout:          30 * time.Second,
		CredentialSource: source,
		CredentialError:  credentialError,
	}
	return config, nil
}

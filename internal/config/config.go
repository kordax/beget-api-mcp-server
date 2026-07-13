// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kordax/basic-utils/v3/uos"
	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"go.uber.org/fx"
)

const defaultBaseURL = "https://api.beget.com/api"

type Config struct {
	Login   string
	APIKey  string
	BaseURL string
	Timeout time.Duration
}

var Module = fx.Module("config", fx.Provide(FromSources))

func FromSources(store credentials.Store) (Config, error) {
	login := strings.TrimSpace(os.Getenv("BEGET_API_LOGIN"))
	apiKey := os.Getenv("BEGET_API_KEY")
	if login == "" || apiKey == "" {
		if store == nil {
			return Config{}, errors.New("beget credentials are required")
		}
		stored, err := store.Load()
		if err != nil {
			return Config{}, fmt.Errorf("load Beget credentials: %w; run credentials set or provide BEGET_API_LOGIN and BEGET_API_KEY", err)
		}
		if login == "" {
			login = stored.Login
		}
		if apiKey == "" {
			apiKey = stored.APIKey
		}
	}
	baseURL := uos.GetEnvOptAs("BEGET_API_BASE_URL", uos.MapStringToTrimmed).OrElse(defaultBaseURL)
	config := Config{
		Login:   login,
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Timeout: 30 * time.Second,
	}
	return config, nil
}

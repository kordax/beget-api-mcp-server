// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"strings"
	"time"

	"github.com/kordax/basic-utils/v3/uos"
	"go.uber.org/fx"
)

const defaultBaseURL = "https://api.beget.com/api"

type Config struct {
	Login   string
	APIKey  string
	BaseURL string
	Timeout time.Duration
}

var Module = fx.Module("config", fx.Provide(FromEnvironment))

func FromEnvironment() (Config, error) {
	login, err := uos.GetEnvAs("BEGET_API_LOGIN", uos.MapStringToTrimmed)
	if err != nil {
		return Config{}, errors.New("BEGET_API_LOGIN is required")
	}
	apiKey, err := uos.GetEnvAs("BEGET_API_KEY", uos.MapString)
	if err != nil {
		return Config{}, errors.New("BEGET_API_KEY is required")
	}
	baseURL := uos.GetEnvOptAs("BEGET_API_BASE_URL", uos.MapStringToTrimmed).OrElse(defaultBaseURL)
	config := Config{
		Login:   *login,
		APIKey:  *apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Timeout: 30 * time.Second,
	}
	return config, nil
}

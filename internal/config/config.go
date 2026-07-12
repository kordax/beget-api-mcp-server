package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.beget.com/api"

type Config struct {
	Login   string
	APIKey  string
	BaseURL string
	Timeout time.Duration
}

func FromEnvironment() (Config, error) {
	config := Config{
		Login:   strings.TrimSpace(os.Getenv("BEGET_API_LOGIN")),
		APIKey:  os.Getenv("BEGET_API_KEY"),
		BaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("BEGET_API_BASE_URL")), "/"),
		Timeout: 30 * time.Second,
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.Login == "" {
		return Config{}, errors.New("BEGET_API_LOGIN is required")
	}
	if config.APIKey == "" {
		return Config{}, errors.New("BEGET_API_KEY is required; launch through codex-keyring")
	}
	return config, nil
}

// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/kordax/beget-api-mcp-server/internal/config"
	"go.uber.org/fx"
)

const maxResponseBytes = 4 << 20

var pathPartPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Caller interface {
	Call(context.Context, string, string, any) (json.RawMessage, error)
}

type Client struct {
	baseURL string
	login   string
	apiKey  string
	http    HTTPClient
}

var Module = fx.Module("beget",
	fx.Provide(
		NewHTTPClient,
		fx.Annotate(NewFromConfig, fx.As(new(Caller))),
	),
)

type envelope struct {
	Status    string          `json:"status"`
	Answer    json.RawMessage `json:"answer"`
	ErrorText string          `json:"error_text"`
	ErrorCode any             `json:"error_code"`
}

type APIError struct {
	Section string
	Method  string
	Code    any
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Beget %s/%s failed: %s", e.Section, e.Method, e.Message)
	}
	return fmt.Sprintf("Beget %s/%s failed", e.Section, e.Method)
}

func NewClient(baseURL, login, apiKey string, httpClient HTTPClient) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || login == "" || apiKey == "" {
		return nil, errors.New("base URL, login, and API key are required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, login: login, apiKey: apiKey, http: httpClient}, nil
}

func NewHTTPClient(configuration config.Config) *http.Client {
	return &http.Client{Timeout: configuration.Timeout}
}

func NewFromConfig(configuration config.Config, httpClient *http.Client) (*Client, error) {
	return NewClient(configuration.BaseURL, configuration.Login, configuration.APIKey, httpClient)
}

func (c *Client) Call(ctx context.Context, section, method string, input any) (json.RawMessage, error) {
	if !pathPartPattern.MatchString(section) || !pathPartPattern.MatchString(method) {
		return nil, errors.New("invalid Beget API section or method")
	}

	data := []byte(`{}`)
	if input != nil {
		var err error
		data, err = json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("encode Beget input: %w", err)
		}
	}

	form := url.Values{
		"login":         {c.login},
		"passwd":        {c.apiKey},
		"input_format":  {"json"},
		"output_format": {"json"},
		"input_data":    {string(data)},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+section+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create Beget request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "beget-api-mcp-server/0.1")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call Beget %s/%s: %w", section, method, err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read Beget %s/%s response: %w", section, method, err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("Beget %s/%s response exceeds %d bytes", section, method, maxResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Beget %s/%s returned HTTP %d", section, method, response.StatusCode)
	}

	var result envelope
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode Beget %s/%s response: %w", section, method, err)
	}
	if result.Status != "success" {
		return nil, &APIError{Section: section, Method: method, Code: result.ErrorCode, Message: result.ErrorText}
	}
	if len(result.Answer) == 0 {
		return json.RawMessage("null"), nil
	}
	return result.Answer, nil
}

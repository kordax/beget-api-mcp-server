// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/kordax/beget-api-mcp-server/internal/config"
	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"go.uber.org/fx"
)

const maxResponseBytes = 4 << 20

var pathPartPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Caller interface {
	Call(context.Context, string, string, any) (json.RawMessage, error)
	AuthenticationStatus() AuthenticationStatus
}

type Client struct {
	baseURL               string
	http                  HTTPClient
	credentialMu          sync.Mutex
	login                 string
	apiKey                string
	source                string
	authErr               error
	credentialStore       credentials.Store
	loginFromEnvironment  bool
	apiKeyFromEnvironment bool
}

type AuthenticationStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
	Message    string `json:"message"`
}

type AuthenticationError struct {
	Cause error
}

func (e *AuthenticationError) Error() string {
	return "Beget credentials are not configured; set BEGET_API_LOGIN and BEGET_API_KEY in the MCP server environment, or run beget-api-mcp-server credentials set --login <login>; the server retries stored credentials automatically"
}

func (e *AuthenticationError) Unwrap() error { return e.Cause }

var Module = fx.Module("beget",
	CredentialValidationModule,
	fx.Provide(
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

func (e *APIError) IsCode(code int) bool {
	return fmt.Sprint(e.Code) == strconv.Itoa(code)
}

type MethodError struct {
	Section string
	Method  string
	Errors  []ProviderError
}

type ProviderError struct {
	Code    any    `json:"error_code"`
	Message string `json:"error_text"`
}

func (e *MethodError) Error() string {
	if len(e.Errors) > 0 && e.Errors[0].Message != "" {
		return fmt.Sprintf("Beget %s/%s rejected the operation: %s", e.Section, e.Method, e.Errors[0].Message)
	}
	return fmt.Sprintf("Beget %s/%s rejected the operation", e.Section, e.Method)
}

type TransportError struct {
	Stage          string
	OutcomeUnknown bool
	Cause          error
}

func (e *TransportError) Error() string { return e.Cause.Error() }
func (e *TransportError) Unwrap() error { return e.Cause }

type InputError struct{ Cause error }

func (e *InputError) Error() string { return e.Cause.Error() }
func (e *InputError) Unwrap() error { return e.Cause }

type HTTPError struct {
	Section string
	Method  string
	Status  int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Beget %s/%s returned HTTP %d", e.Section, e.Method, e.Status)
}

func NewClient(baseURL, login, apiKey string, httpClient HTTPClient) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || login == "" || apiKey == "" {
		return nil, errors.New("base URL, login, and API key are required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, login: login, apiKey: apiKey, http: httpClient, source: "explicit"}, nil
}

func NewHTTPClient(configuration config.Config) *http.Client {
	return &http.Client{Timeout: configuration.Timeout}
}

func NewFromConfig(configuration config.Config, httpClient *http.Client, store credentials.Store) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(configuration.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("base URL is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:               baseURL,
		login:                 configuration.Login,
		apiKey:                configuration.APIKey,
		http:                  httpClient,
		source:                configuration.CredentialSource,
		authErr:               configuration.CredentialError,
		credentialStore:       store,
		loginFromEnvironment:  configuration.LoginFromEnvironment,
		apiKeyFromEnvironment: configuration.APIKeyFromEnvironment,
	}, nil
}

func (c *Client) AuthenticationStatus() AuthenticationStatus {
	login, apiKey, source, authErr := c.credentials()
	if login != "" && apiKey != "" {
		return AuthenticationStatus{Configured: true, Source: source, Message: "Beget credentials are configured."}
	}
	return AuthenticationStatus{
		Source:  "not-configured",
		Message: (&AuthenticationError{Cause: authErr}).Error(),
	}
}

func (c *Client) Call(ctx context.Context, section, method string, input any) (json.RawMessage, error) {
	if !pathPartPattern.MatchString(section) || !pathPartPattern.MatchString(method) {
		return nil, errors.New("invalid Beget API section or method")
	}
	login, apiKey, _, authErr := c.credentials()
	if login == "" || apiKey == "" {
		return nil, &AuthenticationError{Cause: authErr}
	}

	data := []byte(`{}`)
	if input != nil {
		var err error
		data, err = json.Marshal(input)
		if err != nil {
			return nil, &InputError{Cause: fmt.Errorf("encode Beget input: %w", err)}
		}
	}

	form := url.Values{
		"login":         {login},
		"passwd":        {apiKey},
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
		return nil, &TransportError{Stage: "send", OutcomeUnknown: true, Cause: fmt.Errorf("call Beget %s/%s: %w", section, method, err)}
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, &TransportError{Stage: "read", OutcomeUnknown: true, Cause: fmt.Errorf("read Beget %s/%s response: %w", section, method, err)}
	}
	if len(body) > maxResponseBytes {
		return nil, &TransportError{Stage: "read", OutcomeUnknown: true, Cause: fmt.Errorf("beget %s/%s response exceeds %d bytes", section, method, maxResponseBytes)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPError{Section: section, Method: method, Status: response.StatusCode}
	}

	var result envelope
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, &TransportError{Stage: "decode", OutcomeUnknown: true, Cause: fmt.Errorf("decode Beget %s/%s response: %w", section, method, err)}
	}
	if result.Status != "success" {
		return nil, &APIError{Section: section, Method: method, Code: result.ErrorCode, Message: result.ErrorText}
	}
	if len(result.Answer) == 0 {
		return json.RawMessage("null"), nil
	}
	return unwrapMethodResult(section, method, result.Answer)
}

func unwrapMethodResult(section, method string, answer json.RawMessage) (json.RawMessage, error) {
	answer = bytes.TrimSpace(answer)
	var nested struct {
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
		Errors []ProviderError `json:"errors"`
	}
	if len(answer) == 0 || answer[0] != '{' || json.Unmarshal(answer, &nested) != nil || nested.Status == "" {
		return answer, nil
	}
	if nested.Status != "success" {
		return nil, &MethodError{Section: section, Method: method, Errors: nested.Errors}
	}
	if len(nested.Result) == 0 {
		return json.RawMessage("null"), nil
	}
	return nested.Result, nil
}

func (c *Client) credentials() (string, string, string, error) {
	c.credentialMu.Lock()
	defer c.credentialMu.Unlock()
	if c.credentialStore == nil || c.loginFromEnvironment && c.apiKeyFromEnvironment {
		return c.login, c.apiKey, c.source, c.authErr
	}

	stored, err := c.credentialStore.Load()
	stored.Login = strings.TrimSpace(stored.Login)
	if err == nil && (stored.Login == "" || stored.APIKey == "") {
		err = credentials.ErrNotFound
	}
	if err != nil {
		if !c.loginFromEnvironment {
			c.login = ""
		}
		if !c.apiKeyFromEnvironment {
			c.apiKey = ""
		}
		c.source = "not-configured"
		c.authErr = err
		return c.login, c.apiKey, c.source, c.authErr
	}

	if !c.loginFromEnvironment {
		c.login = stored.Login
	}
	if !c.apiKeyFromEnvironment {
		c.apiKey = stored.APIKey
	}
	c.source = "persistent-store"
	if c.loginFromEnvironment || c.apiKeyFromEnvironment {
		c.source = "environment-and-store"
	}
	c.authErr = nil
	return c.login, c.apiKey, c.source, nil
}

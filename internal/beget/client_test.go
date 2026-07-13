// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kordax/beget-api-mcp-server/internal/config"
	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type httpClientFunc func(*http.Request) (*http.Response, error)

func (f httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReadCloser) Close() error             { return nil }

type fakeCredentialStore struct {
	value credentials.Credentials
	err   error
	loads int
}

func (store *fakeCredentialStore) Load() (credentials.Credentials, error) {
	store.loads++
	return store.value, store.err
}

func (*fakeCredentialStore) Save(credentials.Credentials) error { return nil }
func (*fakeCredentialStore) Delete() error                      { return nil }

func TestClientUsesPOSTAndUnwrapsAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/api/domain/getList", request.URL.Path)
		assert.NoError(t, request.ParseForm())
		assert.Equal(t, "test-login", request.Form.Get("login"))
		assert.Equal(t, "test-key", request.Form.Get("passwd"))
		assert.Empty(t, request.URL.RawQuery, "credentials must not be placed in the URL")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"status":"success","answer":[{"fqdn":"example.com"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api", "test-login", "test-key", server.Client())
	require.NoError(t, err)
	answer, err := client.Call(context.Background(), "domain", "getList", nil)
	require.NoError(t, err)
	var domains []map[string]any
	require.NoError(t, json.Unmarshal(answer, &domains))
	assert.Len(t, domains, 1)
}

func TestClientReturnsSanitizedAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(`{"status":"error","error_code":7,"error_text":"denied"}`))
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "test-login", "secret-that-must-not-leak", server.Client())
	_, err := client.Call(context.Background(), "user", "getAccountInfo", nil)
	require.Error(t, err)
	var apiError *APIError
	require.True(t, errors.As(err, &apiError))
	assert.NotContains(t, err.Error(), "secret-that-must-not-leak")
}

func TestClientRejectsArbitraryPaths(t *testing.T) {
	client, _ := NewClient("https://example.invalid/api", "login", "key", nil)
	_, err := client.Call(context.Background(), "../user", "getAccountInfo", nil)
	require.Error(t, err)
}

func TestClientConstruction(t *testing.T) {
	for name, arguments := range map[string][]string{
		"base URL": {"", "login", "key"},
		"login":    {"https://example.com", "", "key"},
		"API key":  {"https://example.com", "login", ""},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewClient(arguments[0], arguments[1], arguments[2], nil)
			assert.Nil(t, client)
			assert.Error(t, err)
		})
	}

	client, err := NewClient(" https://example.com/api/// ", "login", "key", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/api", client.baseURL)
	assert.Same(t, http.DefaultClient, client.http)

	configuration := config.Config{
		BaseURL: "https://example.com/api",
		Login:   "login",
		APIKey:  "key",
		Timeout: 17 * time.Second,
	}
	httpClient := NewHTTPClient(configuration)
	assert.Equal(t, 17*time.Second, httpClient.Timeout)

	configured, err := NewFromConfig(configuration, httpClient, nil)
	require.NoError(t, err)
	assert.Same(t, httpClient, configured.http)
	assert.True(t, configured.AuthenticationStatus().Configured)
}

func TestConfiguredClientDefersMissingCredentialsUntilCall(t *testing.T) {
	configuration := config.Config{
		BaseURL:          "https://example.com/api",
		Timeout:          time.Second,
		CredentialSource: "not-configured",
		CredentialError:  errors.New("credential store unavailable"),
	}
	store := &fakeCredentialStore{err: errors.New("credential store unavailable")}
	client, err := NewFromConfig(configuration, NewHTTPClient(configuration), store)
	require.NoError(t, err)

	status := client.AuthenticationStatus()
	assert.False(t, status.Configured)
	assert.Equal(t, "not-configured", status.Source)
	assert.Contains(t, status.Message, "BEGET_API_LOGIN")

	_, err = client.Call(context.Background(), "domain", "getList", nil)
	var authenticationError *AuthenticationError
	require.ErrorAs(t, err, &authenticationError)
	assert.ErrorContains(t, err, "credentials set")
	assert.Equal(t, 2, store.loads)
}

func TestConfiguredClientValidatesBaseURLAndDefaultsHTTPClient(t *testing.T) {
	_, err := NewFromConfig(config.Config{}, nil, nil)
	assert.ErrorContains(t, err, "base URL is required")

	client, err := NewFromConfig(config.Config{BaseURL: "https://example.com", Login: "login", APIKey: "key"}, nil, nil)
	require.NoError(t, err)
	assert.Same(t, http.DefaultClient, client.http)

	cause := errors.New("credential store unavailable")
	assert.ErrorIs(t, &AuthenticationError{Cause: cause}, cause)
}

func TestConfiguredClientRecoversStoredCredentialsAndKeepsThemInMemory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.NoError(t, request.ParseForm())
		assert.Equal(t, "stored-login", request.Form.Get("login"))
		assert.Equal(t, "stored-key", request.Form.Get("passwd"))
		_, _ = io.WriteString(response, `{"status":"success","answer":{"ok":true}}`)
	}))
	defer server.Close()

	store := &fakeCredentialStore{value: credentials.Credentials{Login: "stored-login", APIKey: "stored-key"}}
	configuration := config.Config{BaseURL: server.URL, CredentialSource: "not-configured", CredentialError: errors.New("credential store was unavailable at startup")}
	client, err := NewFromConfig(configuration, server.Client(), store)
	require.NoError(t, err)

	status := client.AuthenticationStatus()
	assert.True(t, status.Configured)
	assert.Equal(t, "persistent-store", status.Source)
	assert.Equal(t, 1, store.loads)

	store.err = errors.New("credential store became unavailable again")
	_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, store.loads, "credentials must stay cached after a successful load")
}

func TestAPIErrorFormatting(t *testing.T) {
	assert.Equal(t, "Beget dns/getData failed", (&APIError{Section: "dns", Method: "getData"}).Error())
	assert.Equal(t, "Beget dns/getData failed: denied", (&APIError{Section: "dns", Method: "getData", Message: "denied"}).Error())
}

func TestClientCallFailuresAndEmptyAnswer(t *testing.T) {
	t.Run("encode input", func(t *testing.T) {
		client, err := NewClient("https://example.com", "login", "key", nil)
		require.NoError(t, err)
		_, err = client.Call(context.Background(), "user", "getAccountInfo", make(chan int))
		assert.ErrorContains(t, err, "encode Beget input")
	})

	t.Run("create request", func(t *testing.T) {
		client, err := NewClient("://invalid", "login", "key", nil)
		require.NoError(t, err)
		_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
		assert.ErrorContains(t, err, "create Beget request")
	})

	t.Run("perform request", func(t *testing.T) {
		client, err := NewClient("https://example.com", "login", "key", httpClientFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network failed")
		}))
		require.NoError(t, err)
		_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
		assert.ErrorContains(t, err, "call Beget user/getAccountInfo")
	})

	for name, testCase := range map[string]struct {
		response *http.Response
		expected string
	}{
		"read response": {
			response: &http.Response{StatusCode: http.StatusOK, Body: failingReadCloser{}},
			expected: "read Beget user/getAccountInfo response",
		},
		"oversized response": {
			response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxResponseBytes+1)))},
			expected: "response exceeds",
		},
		"HTTP status": {
			response: &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{}`))},
			expected: "returned HTTP 503",
		},
		"decode response": {
			response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`not-json`))},
			expected: "decode Beget user/getAccountInfo response",
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewClient("https://example.com", "login", "key", httpClientFunc(func(*http.Request) (*http.Response, error) {
				return testCase.response, nil
			}))
			require.NoError(t, err)
			_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
			assert.ErrorContains(t, err, testCase.expected)
		})
	}

	t.Run("empty answer", func(t *testing.T) {
		client, err := NewClient("https://example.com", "login", "key", httpClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"success"}`))}, nil
		}))
		require.NoError(t, err)
		answer, err := client.Call(context.Background(), "user", "getAccountInfo", map[string]any{"value": true})
		require.NoError(t, err)
		assert.JSONEq(t, "null", string(answer))
	})
}

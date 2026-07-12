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

	configured, err := NewFromConfig(configuration, httpClient)
	require.NoError(t, err)
	assert.Same(t, httpClient, configured.http)
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

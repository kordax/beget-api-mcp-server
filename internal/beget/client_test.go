// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

type countingReadCloser struct {
	remaining int
	read      int
}

func (reader *countingReadCloser) Read(buffer []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	count := min(len(buffer), reader.remaining)
	for index := range count {
		buffer[index] = 'x'
	}
	reader.remaining -= count
	reader.read += count
	return count, nil
}

func (*countingReadCloser) Close() error { return nil }

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

func TestClientReusesHTTPConnections(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, `{"status":"success","answer":true}`)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	httpClient := NewHTTPClient(config.Config{Timeout: time.Second})
	httpClient.Transport = server.Client().Transport
	client, err := NewClient(server.URL, "login", "key", httpClient)
	require.NoError(t, err)

	for range 2 {
		_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, connections.Load(), "fully read and closed responses must keep the connection reusable")
}

func TestClientCancelsHTTPRequestWithContext(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		started <- struct{}{}
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	client, err := NewClient(server.URL, "login", "key", server.Client())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, callErr := client.Call(ctx, "user", "getAccountInfo", nil)
		result <- callErr
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Beget request did not start")
	}
	cancel()
	select {
	case callErr := <-result:
		require.Error(t, callErr)
		assert.ErrorIs(t, callErr, context.Canceled)
		var transportError *TransportError
		assert.ErrorAs(t, callErr, &transportError)
	case <-time.After(time.Second):
		t.Fatal("cancelled Beget request did not return")
	}
	assert.EqualValues(t, 1, requests.Load())
}

func TestClientStopsReadingOversizedResponseAtLimit(t *testing.T) {
	body := &countingReadCloser{remaining: maxResponseBytes * 2}
	client, err := NewClient("https://example.com", "login", "key", httpClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}))
	require.NoError(t, err)

	_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
	assert.ErrorContains(t, err, "response exceeds")
	assert.Equal(t, maxResponseBytes+1, body.read, "the client must not buffer bytes beyond the configured response limit")
}

func TestClientUnwrapsNestedMethodEnvelope(t *testing.T) {
	client, err := NewClient("https://example.com", "login", "key", httpClientFunc(func(*http.Request) (*http.Response, error) {
		body := `{"status":"success","answer":{"status":"success","result":{"row_number":42}}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	require.NoError(t, err)
	answer, err := client.Call(context.Background(), "cron", "add", nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"row_number":42}`, string(answer))
}

func TestClientReturnsNestedProviderErrors(t *testing.T) {
	client, err := NewClient("https://example.com", "login", "key", httpClientFunc(func(*http.Request) (*http.Response, error) {
		body := `{"status":"success","answer":{"status":"error","errors":[{"error_code":"INVALID_DATA","error_text":"invalid suffix"}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	require.NoError(t, err)
	_, err = client.Call(context.Background(), "ftp", "add", nil)
	require.Error(t, err)
	var methodError *MethodError
	require.ErrorAs(t, err, &methodError)
	assert.Equal(t, "INVALID_DATA", methodError.Errors[0].Code)
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

func TestConfiguredClientRefreshesStoredCredentials(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		assert.NoError(t, request.ParseForm())
		assert.Equal(t, "updated-login", request.Form.Get("login"))
		assert.Equal(t, "updated-key", request.Form.Get("passwd"))
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

	store.value = credentials.Credentials{Login: "updated-login", APIKey: "updated-key"}
	_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, store.loads)
	assert.EqualValues(t, 1, requests.Load())

	store.err = errors.New("credential store became unavailable again")
	_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
	var authenticationError *AuthenticationError
	require.ErrorAs(t, err, &authenticationError)
	assert.Equal(t, 3, store.loads)
	assert.EqualValues(t, 1, requests.Load(), "a stale credential must not be sent after the store becomes unavailable")
}

func TestConfiguredClientPreservesEnvironmentOverridesDuringRefresh(t *testing.T) {
	tests := map[string]struct {
		configuration config.Config
		expectedLogin string
		expectedKey   string
	}{
		"login": {
			configuration: config.Config{
				BaseURL:              "https://example.invalid/api",
				Login:                "environment-login",
				APIKey:               "initial-stored-key",
				CredentialSource:     "environment-and-store",
				LoginFromEnvironment: true,
			},
			expectedLogin: "environment-login",
			expectedKey:   "updated-stored-key",
		},
		"API key": {
			configuration: config.Config{
				BaseURL:               "https://example.invalid/api",
				Login:                 "initial-stored-login",
				APIKey:                "environment-key",
				CredentialSource:      "environment-and-store",
				APIKeyFromEnvironment: true,
			},
			expectedLogin: "updated-stored-login",
			expectedKey:   "environment-key",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			store := &fakeCredentialStore{value: credentials.Credentials{Login: "updated-stored-login", APIKey: "updated-stored-key"}}
			client, err := NewFromConfig(test.configuration, nil, store)
			require.NoError(t, err)
			client.http = httpClientFunc(func(request *http.Request) (*http.Response, error) {
				require.NoError(t, request.ParseForm())
				assert.Equal(t, test.expectedLogin, request.Form.Get("login"))
				assert.Equal(t, test.expectedKey, request.Form.Get("passwd"))
				body := io.NopCloser(strings.NewReader(`{"status":"success","answer":true}`))
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			})

			_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
			require.NoError(t, err)
			assert.Equal(t, 1, store.loads)
		})
	}
}

func TestConfiguredClientDoesNotReloadCompleteEnvironmentCredentials(t *testing.T) {
	configuration := config.Config{
		BaseURL:               "https://example.invalid/api",
		Login:                 "environment-login",
		APIKey:                "environment-key",
		CredentialSource:      "environment",
		LoginFromEnvironment:  true,
		APIKeyFromEnvironment: true,
	}
	store := &fakeCredentialStore{err: errors.New("credential store must not be loaded")}
	client, err := NewFromConfig(configuration, nil, store)
	require.NoError(t, err)
	client.http = httpClientFunc(func(request *http.Request) (*http.Response, error) {
		require.NoError(t, request.ParseForm())
		assert.Equal(t, "environment-login", request.Form.Get("login"))
		assert.Equal(t, "environment-key", request.Form.Get("passwd"))
		body := io.NopCloser(strings.NewReader(`{"status":"success","answer":true}`))
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})

	_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
	require.NoError(t, err)
	assert.Zero(t, store.loads)
}

func TestAPIErrorFormatting(t *testing.T) {
	assert.Equal(t, "Beget dns/getData failed", (&APIError{Section: "dns", Method: "getData"}).Error())
	assert.Equal(t, "Beget dns/getData failed: denied", (&APIError{Section: "dns", Method: "getData", Message: "denied"}).Error())
	assert.True(t, (&APIError{Code: float64(1208)}).IsCode(1208))
	assert.True(t, (&APIError{Code: "1208"}).IsCode(1208))
	assert.False(t, (&APIError{Code: 7}).IsCode(1208))
}

func TestClientCallFailuresAndEmptyAnswer(t *testing.T) {
	t.Run("encode input", func(t *testing.T) {
		client, err := NewClient("https://example.com", "login", "key", nil)
		require.NoError(t, err)
		_, err = client.Call(context.Background(), "user", "getAccountInfo", make(chan int))
		assert.ErrorContains(t, err, "encode Beget input")
		var inputError *InputError
		assert.ErrorAs(t, err, &inputError)
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
		var transportError *TransportError
		require.ErrorAs(t, err, &transportError)
		assert.True(t, transportError.OutcomeUnknown)
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
			if name == "HTTP status" {
				var httpError *HTTPError
				assert.ErrorAs(t, err, &httpError)
			} else {
				var transportError *TransportError
				assert.ErrorAs(t, err, &transportError)
			}
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

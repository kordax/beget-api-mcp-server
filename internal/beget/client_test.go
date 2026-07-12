// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

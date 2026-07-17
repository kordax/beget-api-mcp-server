// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kordax/beget-api-mcp-server/internal/config"
	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialValidatorChecksAccountAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/user/getAccountInfo", request.URL.Path)
		assert.Empty(t, request.URL.RawQuery)
		require.NoError(t, request.ParseForm())
		assert.Equal(t, "account", request.Form.Get("login"))
		assert.Equal(t, "test-only-key", request.Form.Get("passwd"))
		_, _ = response.Write([]byte(`{"status":"success","answer":{"plan_name":"Test"}}`))
	}))
	defer server.Close()

	validator := NewCredentialValidator(config.Config{BaseURL: server.URL}, server.Client())
	err := validator.Validate(context.Background(), credentials.Credentials{Login: "account", APIKey: "test-only-key"})
	require.NoError(t, err)
}

func TestCredentialValidatorDoesNotExposeRejectedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"status":"error","error_code":"AUTH_ERROR","error_text":"denied"}`))
	}))
	defer server.Close()

	validator := NewCredentialValidator(config.Config{BaseURL: server.URL}, server.Client())
	err := validator.Validate(context.Background(), credentials.Credentials{Login: "account", APIKey: "must-not-leak"})
	require.Error(t, err)
	assert.EqualError(t, err, "Beget user/getAccountInfo failed: denied")
	assert.NotContains(t, err.Error(), "must-not-leak")
}

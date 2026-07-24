// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const providerFixtureAPIKey = "fixture-api-key-that-must-never-appear"

func TestProviderEnvelopeFixtures(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/provider/success.json")
		require.NoError(t, err)
		client, err := newProviderFixtureClient(raw)
		require.NoError(t, err)

		answer, err := client.Call(context.Background(), "site", "getList", nil)
		require.NoError(t, err)
		assert.JSONEq(t, `{"items":[{"id":"101","name":"example.test"}],"provider_meta":{"revision":3,"cached":false}}`, string(answer))
	})

	t.Run("method error", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/provider/method_error.json")
		require.NoError(t, err)
		client, err := newProviderFixtureClient(raw)
		require.NoError(t, err)

		_, err = client.Call(context.Background(), "ftp", "add", nil)
		require.Error(t, err)
		var methodError *MethodError
		require.True(t, errors.As(err, &methodError))
		require.Len(t, methodError.Errors, 2)
		assert.Equal(t, "INVALID_DATA", methodError.Errors[0].Code)
		assert.NotContains(t, err.Error(), providerFixtureAPIKey)
	})

	t.Run("api error", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/provider/api_error.json")
		require.NoError(t, err)
		client, err := newProviderFixtureClient(raw)
		require.NoError(t, err)

		_, err = client.Call(context.Background(), "user", "getAccountInfo", nil)
		require.Error(t, err)
		var apiError *APIError
		require.True(t, errors.As(err, &apiError))
		assert.Equal(t, float64(7), apiError.Code)
		assert.NotContains(t, err.Error(), providerFixtureAPIKey)
	})
}

func FuzzProviderEnvelopeDecoding(f *testing.F) {
	for _, path := range []string{
		"testdata/provider/success.json",
		"testdata/provider/method_error.json",
		"testdata/provider/api_error.json",
	} {
		raw, err := os.ReadFile(path)
		require.NoError(f, err)
		f.Add(raw)
	}
	f.Add([]byte(`{`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}

		client, err := newProviderFixtureClient(raw)
		require.NoError(t, err)
		answer, callErr := client.Call(context.Background(), "site", "getList", nil)
		if callErr != nil {
			assert.NotContains(t, callErr.Error(), providerFixtureAPIKey)
			return
		}
		assert.True(t, json.Valid(answer))
	})
}

func newProviderFixtureClient(raw []byte) (*Client, error) {
	return NewClient("https://api.example.test", "fixture-login", providerFixtureAPIKey, httpClientFunc(
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(raw)),
			}, nil
		},
	))
}

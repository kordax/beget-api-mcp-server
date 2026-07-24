// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderResultFixtures(t *testing.T) {
	t.Run("sites", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/provider/sites.json")
		require.NoError(t, err)

		sites, err := decodeTypedResult[[]Site](raw)
		require.NoError(t, err)
		require.Len(t, sites, 1)
		assert.Equal(t, APIInt64(101), sites[0].ID)
		assert.Equal(t, "example.test/public_html", sites[0].Path)
		require.Len(t, sites[0].Domains, 1)
		assert.Equal(t, APIBool(true), sites[0].Domains[0].SSL)
		assert.JSONEq(t, `{"region":"test-zone","revision":7}`,
			sites[0].Domains[0].AdditionalPropertiesJSON["provider_nested"])
		assert.JSONEq(t, `true`, sites[0].AdditionalPropertiesJSON["provider_flag"])
	})

	t.Run("dns", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/provider/dns.json")
		require.NoError(t, err)

		result, err := decodeTypedResult[DNSResult](raw)
		require.NoError(t, err)
		assert.Equal(t, APIBool(true), result.IsUnderControl)
		assert.Equal(t, APIInt64(2), result.SetType)
		require.Len(t, result.Records.A, 1)
		assert.Equal(t, APIInt64(10), result.Records.A[0].Priority)
		assert.JSONEq(t, `600`, result.Records.A[0].AdditionalPropertiesJSON["provider_ttl"])
		require.Len(t, result.Records.DNSIP, 1)
		assert.Nil(t, result.Records.DNSIP[0].Value)
		assert.JSONEq(t, `{"automatic":false}`, result.AdditionalPropertiesJSON["provider_mode"])
	})

	t.Run("account", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/provider/account.json")
		require.NoError(t, err)

		result, err := decodeTypedResult[AccountInfoResult](raw)
		require.NoError(t, err)
		assert.Equal(t, APIInt64(2), result.UserSites)
		assert.Equal(t, APIFloat64(0.42), result.ServerLoadAverage)
		assert.JSONEq(t, `{"burst":true,"labels":["sanitized","fixture"]}`,
			result.AdditionalPropertiesJSON["provider_limits"])
	})
}

func FuzzProviderResultDecoding(f *testing.F) {
	for index, path := range []string{
		"testdata/provider/sites.json",
		"testdata/provider/dns.json",
		"testdata/provider/account.json",
	} {
		raw, err := os.ReadFile(path)
		require.NoError(f, err)
		f.Add(uint8(index), raw)
	}
	f.Add(uint8(0), []byte(`{`))

	f.Fuzz(func(t *testing.T, variant uint8, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}

		var value any
		switch variant % 3 {
		case 0:
			decoded, err := decodeTypedResult[[]Site](raw)
			if err != nil {
				return
			}
			value = decoded
		case 1:
			decoded, err := decodeTypedResult[DNSResult](raw)
			if err != nil {
				return
			}
			value = decoded
		default:
			decoded, err := decodeTypedResult[AccountInfoResult](raw)
			if err != nil {
				return
			}
			value = decoded
		}

		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		assert.True(t, json.Valid(encoded))
	})
}

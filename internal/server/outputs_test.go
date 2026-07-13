// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlexibleProviderScalars(t *testing.T) {
	t.Run("integer", func(t *testing.T) {
		var value APIInt64
		require.NoError(t, json.Unmarshal([]byte(`"42"`), &value))
		assert.Equal(t, APIInt64(42), value)
		assert.Error(t, json.Unmarshal([]byte(`"invalid"`), &value))
		assert.Error(t, json.Unmarshal([]byte(`null`), &value))
	})

	t.Run("number", func(t *testing.T) {
		var value APIFloat64
		require.NoError(t, json.Unmarshal([]byte(`"1.25"`), &value))
		assert.Equal(t, APIFloat64(1.25), value)
		assert.Error(t, json.Unmarshal([]byte(`"invalid"`), &value))
		assert.Error(t, json.Unmarshal([]byte(`null`), &value))
	})

	t.Run("boolean", func(t *testing.T) {
		for input, expected := range map[string]APIBool{
			`true`: true, `false`: false, `"1"`: true, `"0"`: false,
		} {
			var value APIBool
			require.NoError(t, json.Unmarshal([]byte(input), &value), input)
			assert.Equal(t, expected, value, input)
		}
		var value APIBool
		assert.Error(t, json.Unmarshal([]byte(`"sometimes"`), &value))
		assert.Error(t, json.Unmarshal([]byte(`null`), &value))
	})

	t.Run("string", func(t *testing.T) {
		var value APIString
		require.NoError(t, json.Unmarshal([]byte(`123`), &value))
		assert.Equal(t, APIString("123"), value)
		require.NoError(t, json.Unmarshal([]byte(`null`), &value))
		assert.Empty(t, value)
		assert.Error(t, value.UnmarshalJSON([]byte(`"unterminated`)))
	})

	_, err := scalarText(nil)
	assert.Error(t, err)
}

func TestDecodeTypedResultNormalizesCollectionsAndPreservesUnknownFields(t *testing.T) {
	sites, err := decodeTypedResult[[]Site](json.RawMessage(`[
		{"id":"7","path":"site/public_html","new":{"enabled":true}}
	]`))
	require.NoError(t, err)
	require.Len(t, sites, 1)
	assert.Equal(t, APIInt64(7), sites[0].ID)
	assert.NotNil(t, sites[0].Domains)
	assert.Empty(t, sites[0].Domains)
	assert.JSONEq(t, `{"enabled":true}`, sites[0].AdditionalPropertiesJSON["new"])

	zones, err := decodeTypedResult[map[string]DomainZone](json.RawMessage(`{
		"test":{"id":"3","zone":"test","price":"12.5","price_renew":13,"is_idn":"0","is_national":1,"min_period":"1","max_period":2,"new_limit":4}
	}`))
	require.NoError(t, err)
	assert.Equal(t, APIFloat64(12.5), zones["test"].Price)
	assert.Equal(t, `4`, zones["test"].AdditionalPropertiesJSON["new_limit"])

	dns, err := decodeTypedResult[DNSResult](json.RawMessage(`{"fqdn":"example.test"}`))
	require.NoError(t, err)
	assert.NotNil(t, dns.Records.A)
	assert.NotNil(t, dns.Records.DNSIP)

	value, err := decodeTypedResult[*string](json.RawMessage(`"admin@example.test"`))
	require.NoError(t, err)
	require.NotNil(t, value)
	assert.Equal(t, "admin@example.test", *value)
	value, err = decodeTypedResult[*string](json.RawMessage(`null`))
	require.NoError(t, err)
	assert.Nil(t, value)

	_, err = decodeTypedResult[[]Site](json.RawMessage(`{}`))
	assert.Error(t, err)
}

func TestCronTaskResultAcceptsDocumentedShapes(t *testing.T) {
	for input, expected := range map[string]APIInt64{
		`42`:                  42,
		`{"row_number":"43"}`: 43,
	} {
		result, err := decodeTypedResult[CronTaskResult](json.RawMessage(input))
		require.NoError(t, err)
		assert.Equal(t, expected, result.RowNumber)
	}
}

func TestLosslessReflectionHelpersHandleEdgeShapes(t *testing.T) {
	type embedded struct {
		Value string `json:"value"`
	}
	type sample struct {
		embedded
		Items []string `json:"items,omitempty"`
		Skip  string   `json:"-"`
	}

	value := sample{}
	require.NoError(t, json.Unmarshal([]byte(`{"value":"ok"}`), &value))
	require.NoError(t, captureStructProperties(json.RawMessage(`{"value":"ok"}`), reflect.ValueOf(&value).Elem()))
	assert.Equal(t, "ok", value.Value)
	assert.NotNil(t, value.Items)

	pointer := &Site{}
	require.NoError(t, captureAdditionalProperties(json.RawMessage(`{"id":1}`), reflect.ValueOf(&pointer).Elem()))
	assert.Equal(t, APIInt64(0), pointer.ID)

	var items []Site
	require.NoError(t, captureAdditionalProperties(json.RawMessage(`invalid`), reflect.ValueOf(&items).Elem()))
	assert.NotNil(t, items)

	nonStringMap := map[int]string{}
	require.NoError(t, captureAdditionalProperties(json.RawMessage(`{}`), reflect.ValueOf(&nonStringMap).Elem()))

	type names struct {
		Default string
		Tagged  string `json:"tagged,omitempty"`
	}
	typeOfNames := reflect.TypeFor[names]()
	assert.Equal(t, "Default", jsonFieldName(typeOfNames.Field(0)))
	assert.Equal(t, "tagged", jsonFieldName(typeOfNames.Field(1)))
}

// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/kordax/beget-api-mcp-server/internal/beget"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCaller struct {
	mu      sync.Mutex
	calls   int
	section string
	method  string
	input   any
	answer  json.RawMessage
	err     error
}

func (f *fakeCaller) Call(_ context.Context, section, method string, input any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.section, f.method, f.input = section, method, input
	if f.answer == nil {
		return json.RawMessage(`{"ok":true}`), f.err
	}
	return f.answer, f.err
}

func (*fakeCaller) AuthenticationStatus() beget.AuthenticationStatus {
	return beget.AuthenticationStatus{Configured: true, Source: "test", Message: "configured for tests"}
}

func TestToolsExposeSafetyAnnotations(t *testing.T) {
	client := &fakeCaller{}
	session, closeSessions := connectTestClient(t, client)
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}
	assert.Len(t, tools, 12)
	require.Contains(t, tools, "beget_auth_status")
	require.Contains(t, tools, "beget_list_sites")
	assert.True(t, tools["beget_list_sites"].Annotations.ReadOnlyHint)
	require.Contains(t, tools, "beget_change_dns_records")
	change := tools["beget_change_dns_records"].Annotations
	assert.False(t, change.ReadOnlyHint)
	require.NotNil(t, change.DestructiveHint)
	assert.True(t, *change.DestructiveHint)
}

func TestAuthenticationStatusDoesNotCallBeget(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Zero(t, caller.calls)
}

func TestMutationRequiresConfirmation(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_unfreeze_site", Arguments: map[string]any{"id": 42, "confirm": false},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Zero(t, caller.calls, "unconfirmed mutation reached the Beget client")
}

func TestReadToolCallsExpectedEndpoint(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_list_domains", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Equal(t, 1, caller.calls)
	assert.Equal(t, "domain", caller.section)
	assert.Equal(t, "getList", caller.method)
}

func TestValidateDNSRecords(t *testing.T) {
	assert.NoError(t, validateDNSRecords(DNSRecords{A: []DNSRecord{{Value: "192.0.2.1"}}}))
	assert.Error(t, validateDNSRecords(DNSRecords{A: []DNSRecord{{Value: "192.0.2.1"}}, CNAME: []DNSRecord{{Value: "example.com"}}}))
	assert.Error(t, validateDNSRecords(DNSRecords{DNSIP: []DNSRecord{{Value: "192.0.2.53"}}}))
	assert.Error(t, validateDNSRecords(DNSRecords{CNAME: []DNSRecord{{Value: "one.example"}, {Value: "two.example"}}}))
	assert.Error(t, validateDNSRecords(DNSRecords{A: makeRecords(11, "192.0.2.1")}))
	assert.Error(t, validateDNSRecords(DNSRecords{DNS: makeRecords(5, "ns.example")}))
	assert.Error(t, validateDNSRecords(DNSRecords{MX: []DNSRecord{{Value: " "}}}))
	assert.NoError(t, validateDNSRecords(DNSRecords{NS: []DNSRecord{{Value: "ns.example"}}}))
	assert.NoError(t, validateDNSRecords(DNSRecords{CNAME: []DNSRecord{{Value: "target.example"}}}))
	assert.NoError(t, validateDNSRecords(DNSRecords{
		DNS:   []DNSRecord{{Value: "ns.example"}},
		DNSIP: []DNSRecord{{Value: "192.0.2.53"}},
	}))
}

func TestSpecializedHandlers(t *testing.T) {
	ctx := context.Background()
	caller := &fakeCaller{}
	service := &service{client: caller}

	_, _, err := service.getDNSRecords(ctx, nil, DNSInput{})
	assert.ErrorContains(t, err, "fqdn is required")
	_, output, err := service.getDNSRecords(ctx, nil, DNSInput{FQDN: "example.com"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"ok": true}, output.Answer)
	assert.Equal(t, "dns", caller.section)
	assert.Equal(t, "getData", caller.method)

	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{})
	assert.ErrorContains(t, err, "confirm must be true")
	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{Confirm: true})
	assert.ErrorContains(t, err, "fqdn is required")
	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{Confirm: true, FQDN: "example.com"})
	assert.ErrorContains(t, err, "exactly one")
	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{
		Confirm: true,
		FQDN:    "example.com",
		Records: DNSRecords{A: []DNSRecord{{Value: "192.0.2.1"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "changeRecords", caller.method)

	_, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{})
	assert.ErrorContains(t, err, "confirm must be true")
	_, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{Confirm: true})
	assert.ErrorContains(t, err, "id must be positive")
	for _, excludedPath := range []string{"", "/absolute", "../escape"} {
		_, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{ID: 42, Confirm: true, ExcludedPaths: []string{excludedPath}})
		assert.ErrorContains(t, err, "safe relative path")
	}
	_, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{ID: 42, Confirm: true, ExcludedPaths: []string{"cache"}})
	require.NoError(t, err)
	assert.Equal(t, "freeze", caller.method)

	_, _, err = service.unfreezeSite(ctx, nil, UnfreezeSiteInput{})
	assert.ErrorContains(t, err, "confirm must be true")
	_, _, err = service.unfreezeSite(ctx, nil, UnfreezeSiteInput{Confirm: true})
	assert.ErrorContains(t, err, "id must be positive")
	_, _, err = service.unfreezeSite(ctx, nil, UnfreezeSiteInput{ID: 42, Confirm: true})
	require.NoError(t, err)
	assert.Equal(t, "unfreeze", caller.method)
}

func TestServiceCallPropagatesAndDecodesErrors(t *testing.T) {
	caller := &fakeCaller{err: errors.New("Beget unavailable")}
	service := &service{client: caller}

	_, _, err := service.call(context.Background(), "user", "getAccountInfo", nil)
	assert.ErrorContains(t, err, "Beget unavailable")

	caller.err = nil
	caller.answer = json.RawMessage(`not-json`)
	_, _, err = service.call(context.Background(), "user", "getAccountInfo", nil)
	assert.ErrorContains(t, err, "decode Beget user/getAccountInfo answer")
}

func makeRecords(count int, value string) []DNSRecord {
	records := make([]DNSRecord, count)
	for index := range records {
		records[index] = DNSRecord{Value: value}
	}
	return records
}

func connectTestClient(t *testing.T, caller beget.Caller) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(caller).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}

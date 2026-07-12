package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeCaller struct {
	mu      sync.Mutex
	calls   int
	section string
	method  string
	input   any
}

func (f *fakeCaller) Call(_ context.Context, section, method string, input any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.section, f.method, f.input = section, method, input
	return json.RawMessage(`{"ok":true}`), nil
}

func TestToolsExposeSafetyAnnotations(t *testing.T) {
	client := &fakeCaller{}
	session, closeSessions := connectTestClient(t, client)
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}
	if len(tools) != 11 {
		t.Fatalf("expected 11 tools, got %d", len(tools))
	}
	if !tools["beget_list_sites"].Annotations.ReadOnlyHint {
		t.Fatal("list tool must be read-only")
	}
	change := tools["beget_change_dns_records"].Annotations
	if change.ReadOnlyHint || change.DestructiveHint == nil || !*change.DestructiveHint {
		t.Fatal("DNS mutation must be marked destructive")
	}
}

func TestMutationRequiresConfirmation(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_unfreeze_site", Arguments: map[string]any{"id": 42, "confirm": false},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("unconfirmed mutation should return a tool error")
	}
	if caller.calls != 0 {
		t.Fatal("unconfirmed mutation reached the Beget client")
	}
}

func TestReadToolCallsExpectedEndpoint(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_list_domains", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("CallTool failed: result=%+v err=%v", result, err)
	}
	if caller.calls != 1 || caller.section != "domain" || caller.method != "getList" {
		t.Fatalf("unexpected endpoint: calls=%d %s/%s", caller.calls, caller.section, caller.method)
	}
}

func TestValidateDNSRecords(t *testing.T) {
	if err := validateDNSRecords(DNSRecords{A: []DNSRecord{{Value: "192.0.2.1"}}}); err != nil {
		t.Fatalf("valid A record rejected: %v", err)
	}
	if err := validateDNSRecords(DNSRecords{A: []DNSRecord{{Value: "192.0.2.1"}}, CNAME: []DNSRecord{{Value: "example.com"}}}); err == nil {
		t.Fatal("mixed record groups should fail")
	}
	if err := validateDNSRecords(DNSRecords{DNSIP: []DNSRecord{{Value: "192.0.2.53"}}}); err == nil {
		t.Fatal("DNS_IP without a DNS server should fail")
	}
}

func connectTestClient(t *testing.T, caller Caller) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(caller).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("connect client: %v", err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}

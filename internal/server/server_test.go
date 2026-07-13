// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kordax/beget-api-mcp-server/internal/beget"
	"github.com/kordax/beget-api-mcp-server/internal/updater"
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

type fakeReleaseChecker struct {
	status      updater.VersionStatus
	err         error
	calls       atomic.Int32
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
}

func (checker *fakeReleaseChecker) Check(ctx context.Context) (updater.VersionStatus, error) {
	checker.calls.Add(1)
	if checker.started != nil {
		checker.startedOnce.Do(func() { close(checker.started) })
	}
	if checker.release != nil {
		select {
		case <-checker.release:
		case <-ctx.Done():
			return updater.VersionStatus{}, ctx.Err()
		}
	}
	return checker.status, checker.err
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
	assert.Len(t, tools, 66)
	require.Contains(t, tools, "beget_auth_status")
	require.Contains(t, tools, "beget_list_sites")
	assert.True(t, tools["beget_list_sites"].Annotations.ReadOnlyHint)
	require.Contains(t, tools, "beget_change_dns_records")
	change := tools["beget_change_dns_records"].Annotations
	assert.False(t, change.ReadOnlyHint)
	require.NotNil(t, change.DestructiveHint)
	assert.True(t, *change.DestructiveHint)
	require.Contains(t, tools, "beget_change_mailbox_password")
	mailPassword := tools["beget_change_mailbox_password"].Annotations
	require.NotNil(t, mailPassword.DestructiveHint)
	assert.True(t, *mailPassword.DestructiveHint)
	assert.True(t, mailPassword.IdempotentHint)
	require.Contains(t, tools, "beget_add_cron_job")
	assert.False(t, tools["beget_add_cron_job"].Annotations.IdempotentHint)
	require.Contains(t, tools, "beget_download_file_backup")
	assert.False(t, tools["beget_download_file_backup"].Annotations.ReadOnlyHint)
	assert.False(t, tools["beget_download_file_backup"].Annotations.IdempotentHint)
}

func TestInitializeProvidesUniversalAgentInstructions(t *testing.T) {
	session, closeSessions := connectTestClient(t, &fakeCaller{})
	defer closeSessions()

	result := session.InitializeResult()
	require.NotNil(t, result)
	assert.Contains(t, result.Instructions, "beget_auth_status")
	assert.Contains(t, result.Instructions, "Never guess identifiers")
	assert.Contains(t, result.Instructions, "confirm=true")
	assert.Contains(t, result.Instructions, "Never retry a mutation automatically")
	assert.NotContains(t, result.Instructions, "BEGET_API_KEY")
}

func TestToolsExposeExactInputContracts(t *testing.T) {
	session, closeSessions := connectTestClient(t, &fakeCaller{})
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}

	assertToolContract(t, tools["beget_account_info"], nil, nil)
	assertToolContract(t, tools["beget_add_cron_job"],
		[]string{"command", "confirm", "days", "hours", "minutes", "months", "weekdays"},
		[]string{"command", "confirm", "days", "hours", "minutes", "months", "weekdays"},
	)
	assertToolContract(t, tools["beget_add_ftp_account"],
		[]string{"confirm", "homedir", "password", "suffix"},
		[]string{"confirm", "homedir", "password", "suffix"},
	)
	ftpProperties := schemaProperties(t, inputSchemaMap(t, tools["beget_add_ftp_account"]))
	passwordSchema, ok := ftpProperties["password"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, passwordSchema["writeOnly"])
	assertToolContract(t, tools["beget_change_mailbox_settings"],
		[]string{"confirm", "domain", "forward_mail_status", "mailbox", "spam_filter", "spam_filter_status"},
		[]string{"confirm", "domain", "forward_mail_status", "mailbox", "spam_filter", "spam_filter_status"},
	)
	assertToolContract(t, tools["beget_add_domain_directives"],
		[]string{"confirm", "directives_list", "full_fqdn"},
		[]string{"confirm", "directives_list", "full_fqdn"},
	)
	assertToolContract(t, tools["beget_list_backup_files"],
		[]string{"backup_id", "path"},
		[]string{"path"},
	)

	for _, tool := range result.Tools {
		schema := inputSchemaMap(t, tool)
		properties := schemaProperties(t, schema)
		for _, forbidden := range []string{"api_key", "passwd", "bearer_token", "http_token", "master_password"} {
			assert.NotContainsf(t, properties, forbidden, "%s must not accept credential field %s", tool.Name, forbidden)
		}
		assert.LessOrEqualf(t, len(properties), 8, "%s exposes too many input properties", tool.Name)
	}
}

func TestToolContractSnapshot(t *testing.T) {
	session, closeSessions := connectTestClient(t, &fakeCaller{})
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	encoded, err := json.Marshal(result.Tools)
	require.NoError(t, err)
	assert.Len(t, result.Tools, 66)
	assert.LessOrEqual(t, len(encoded), 66550, "tools/list contracts must remain at least 60%% smaller than the 166376-byte baseline")
	actual := fmt.Sprintf("%x", sha256.Sum256(encoded))
	assert.Equal(t, "0bbcd270db1350bc6732aa514665f0a9a542f341d97bce1779bc22ef080e7f06", actual, "intentional MCP contract changes require updating this snapshot")
}

func TestInvalidContractsDoNotReachBeget(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	for _, call := range []*mcp.CallToolParams{
		{Name: "beget_account_info", Arguments: map[string]any{"domain": "example.com"}},
		{Name: "beget_toggle_ssh", Arguments: map[string]any{"status": 2, "confirm": true}},
		{Name: "beget_add_cron_job", Arguments: map[string]any{"confirm": true}},
		{Name: "beget_add_cron_job", Arguments: map[string]any{
			"minutes": "99", "hours": "*", "days": "*", "months": "*", "weekdays": "*", "command": "true", "confirm": true,
		}},
	} {
		result, err := session.CallTool(context.Background(), call)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Truef(t, result.IsError, "%s should reject invalid arguments", call.Name)
	}
	assert.Zero(t, caller.calls)
}

func TestPublishedOperationsAreRegisteredWithMatchingSafety(t *testing.T) {
	client := &fakeCaller{}
	session, closeSessions := connectTestClient(t, client)
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}

	for _, spec := range publishedOperations {
		tool, ok := tools[spec.name]
		require.Truef(t, ok, "%s is not registered", spec.name)
		if spec.mutating {
			assert.False(t, tool.Annotations.ReadOnlyHint, spec.name)
			require.NotNilf(t, tool.Annotations.DestructiveHint, "%s must declare whether it is destructive", spec.name)
			assert.Contains(t, schemaProperties(t, inputSchemaMap(t, tool)), "confirm", spec.name)
			continue
		}
		assert.True(t, tool.Annotations.ReadOnlyHint, spec.name)
	}
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

func TestUpdateCheckRunsInBackgroundAndOnlyNotifiesOnce(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	checker := &fakeReleaseChecker{status: updater.VersionStatus{
		Current: "v0.3.3", Latest: "v0.3.4", UpdateAvailable: true,
	}, started: make(chan struct{}), release: make(chan struct{})}
	server := newServer(&fakeCaller{}, checker, func() time.Time { return now })
	session, closeSessions := connectServer(t, server)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.Zero(t, checker.calls.Load())
	assert.NotContains(t, callToolText(result), "newer beget-api-mcp-server")

	now = now.Add(updateCheckIdleInterval)
	callDone := make(chan *mcp.CallToolResult, 1)
	go func() {
		callResult, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}})
		if callErr != nil {
			callDone <- nil
			return
		}
		callDone <- callResult
	}()
	select {
	case <-checker.started:
	case <-time.After(time.Second):
		t.Fatal("background release check did not start")
	}
	select {
	case result = <-callDone:
		require.NotNil(t, result)
		assert.NotContains(t, callToolText(result), "newer beget-api-mcp-server")
	case <-time.After(200 * time.Millisecond):
		close(checker.release)
		t.Fatal("release check blocked the MCP tool call")
	}
	close(checker.release)
	assert.EqualValues(t, 1, checker.calls.Load())

	_, err = session.ListTools(context.Background(), nil)
	require.NoError(t, err, "non-tool requests must not consume a cached release notice")
	require.Eventually(t, func() bool {
		result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}})
		return err == nil && strings.Contains(callToolText(result), "newer beget-api-mcp-server release is available: v0.3.4")
	}, time.Second, 10*time.Millisecond)
	assert.Contains(t, callToolText(result), "did not update itself")

	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.EqualValues(t, 1, checker.calls.Load())
	assert.NotContains(t, callToolText(result), "newer beget-api-mcp-server")
}

func TestBackgroundUpdateCheckErrorDoesNotFailToolCall(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	checker := &fakeReleaseChecker{err: errors.New("release service unavailable")}
	server := newServer(&fakeCaller{}, checker, func() time.Time { return now })
	session, closeSessions := connectServer(t, server)
	defer closeSessions()

	now = now.Add(updateCheckIdleInterval)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}})
	require.NoError(t, err, "release checks must not fail Beget tools")
	assert.NotContains(t, callToolText(result), "newer beget-api-mcp-server")
	require.Eventually(t, func() bool { return checker.calls.Load() == 1 }, time.Second, 10*time.Millisecond)
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

func TestMailboxPasswordChangeUsesMailEndpoint(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_change_mailbox_password", Arguments: map[string]any{
			"domain": "example.com", "mailbox": "admin", "mailbox_password": "new-password", "confirm": true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Equal(t, "mail", caller.section)
	assert.Equal(t, "changeMailboxPassword", caller.method)
	parameters := callerInputMap(t, caller.input)
	assert.Equal(t, map[string]any{
		"domain": "example.com", "mailbox": "admin", "mailbox_password": "new-password",
	}, parameters)
	assert.NotContains(t, parameters, "confirm", "confirm must not reach Beget")
}

func TestPublishedMutationRequiresConfirmation(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_change_mailbox_password", Arguments: map[string]any{"domain": "example.com", "mailbox": "admin"},
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

func TestPublishedOperationsCallExactEndpointsWithFilteredArguments(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()
	arguments := validOperationArguments()

	for _, spec := range publishedOperations {
		params := arguments[spec.name]
		if params == nil {
			params = map[string]any{}
		}
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: spec.name, Arguments: params})
		require.NoError(t, err, spec.name)
		require.NotNil(t, result, spec.name)
		assert.False(t, result.IsError, "%s: %s", spec.name, callToolText(result))
		assert.Equal(t, spec.section, caller.section, spec.name)
		assert.Equal(t, spec.method, caller.method, spec.name)
		if spec.mutating {
			assert.NotContains(t, callerInputMap(t, caller.input), "confirm", spec.name)
		}
	}
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
	assert.Error(t, validateDNSRecords(DNSRecords{A: []DNSRecord{{Value: "not-an-ip"}}}))
	assert.Error(t, validateDNSRecords(DNSRecords{NS: []DNSRecord{{Value: "bad domain"}}}))
	assert.Error(t, validateDNSRecords(DNSRecords{MX: []DNSRecord{{Priority: -1, Value: "mx.example"}}}))
}

func TestSemanticInputValidation(t *testing.T) {
	assert.NoError(t, BackupFileListInput{Path: "/example.com/public_html"}.validate())
	assert.Error(t, BackupFileListInput{Path: "relative"}.validate())
	assert.Error(t, RestoreFilesInput{Paths: []string{"/../escape"}}.validate())
	assert.Error(t, DownloadFilesInput{Paths: []string{""}}.validate())
	assert.NoError(t, CronEmailInput{}.validate())
	assert.Error(t, CronEmailInput{Email: "not-an-email"}.validate())
	assert.Error(t, FTPAddInput{HomeDir: "relative"}.validate())
	assert.NoError(t, SiteAddInput{Name: "nested/example.com"}.validate())
	assert.Error(t, SiteAddInput{Name: "../escape"}.validate())
	assert.Error(t, VirtualDomainInput{Hostname: "bad.name"}.validate())
	assert.Error(t, VirtualSubdomainInput{Subdomain: "bad name"}.validate())
	assert.Error(t, DomainRegistrationInput{Hostname: "-bad"}.validate())
	assert.Error(t, FullFQDNInput{FullFQDN: ""}.validate())
	assert.Error(t, PHPVersionInput{FullFQDN: "bad/name"}.validate())
	assert.Error(t, DirectivesInput{FullFQDN: "example.com", DirectivesList: []Directive{{Value: "value"}}}.validate())
	assert.Error(t, DirectivesInput{FullFQDN: "example.com", DirectivesList: []Directive{{Name: "name"}}}.validate())
	assert.Error(t, MailDomainInput{Domain: "bad domain"}.validate())
	assert.Error(t, MailboxInput{Domain: "example.com", Mailbox: "bad@name"}.validate())
	assert.Error(t, MailForwardingInput{Domain: "example.com", Mailbox: "admin", ForwardMailbox: "bad"}.validate())
	assert.Error(t, DomainMailInput{Domain: "example.com", DomainMailbox: "bad"}.validate())
	assert.Error(t, ClearDomainMailInput{Domain: "bad domain"}.validate())
	assert.NoError(t, MySQLAccessInput{Access: "*"}.validate())
	assert.NoError(t, MySQLAccessInput{Access: "192.0.2.1"}.validate())
	assert.Error(t, MySQLAccessInput{Access: "bad access"}.validate())
	assert.Error(t, validateDNSRecordValue("DNS_IP", "bad-ip"))
	assert.Error(t, validateCronExpression("minutes", "*/0", 0, 59))
	assert.Error(t, validateCronExpression("minutes", "60", 0, 59))
	assert.Error(t, validateCronExpression("minutes", "10-2", 0, 59))
}

func TestSpecializedHandlers(t *testing.T) {
	ctx := context.Background()
	caller := &fakeCaller{}
	service := &service{client: caller}

	_, _, err := service.getDNSRecords(ctx, nil, DNSInput{})
	assert.ErrorContains(t, err, "fqdn must be a valid domain name")
	_, output, err := service.getDNSRecords(ctx, nil, DNSInput{FQDN: "example.com"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"ok": true}, output.Answer)
	assert.Equal(t, "dns", caller.section)
	assert.Equal(t, "getData", caller.method)

	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{})
	assert.ErrorContains(t, err, "confirm must be true")
	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{Confirm: true})
	assert.ErrorContains(t, err, "fqdn must be a valid domain name")
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
	return connectServer(t, New(caller, nil))
}

func connectServer(t *testing.T, server *mcp.Server) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}

func callToolText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if value, ok := content.(*mcp.TextContent); ok {
			text.WriteString(value.Text)
		}
	}
	return text.String()
}

func assertToolContract(t *testing.T, tool *mcp.Tool, properties, required []string) {
	t.Helper()
	require.NotNil(t, tool)
	schema := inputSchemaMap(t, tool)
	actualProperties := make([]string, 0, len(schemaProperties(t, schema)))
	for name := range schemaProperties(t, schema) {
		actualProperties = append(actualProperties, name)
	}
	actualRequired := make([]string, 0)
	if values, ok := schema["required"].([]any); ok {
		for _, value := range values {
			actualRequired = append(actualRequired, value.(string))
		}
	}
	assert.ElementsMatch(t, properties, actualProperties, tool.Name)
	assert.ElementsMatch(t, required, actualRequired, tool.Name)
}

func inputSchemaMap(t *testing.T, tool *mcp.Tool) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(encoded, &schema))
	return schema
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	if schema["properties"] == nil {
		return map[string]any{}
	}
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	return properties
}

func callerInputMap(t *testing.T, input any) map[string]any {
	t.Helper()
	encoded, ok := input.(json.RawMessage)
	if !ok {
		var err error
		encoded, err = json.Marshal(input)
		require.NoError(t, err)
	}
	var parameters map[string]any
	require.NoError(t, json.Unmarshal(encoded, &parameters))
	return parameters
}

func validOperationArguments() map[string]map[string]any {
	confirm := map[string]any{"confirm": true}
	withConfirm := func(values map[string]any) map[string]any {
		result := make(map[string]any, len(values)+1)
		for key, value := range values {
			result[key] = value
		}
		for key, value := range confirm {
			result[key] = value
		}
		return result
	}
	return map[string]map[string]any{
		"beget_toggle_ssh":                   withConfirm(map[string]any{"status": 1, "ftplogin": "user_ftp"}),
		"beget_list_backup_files":            {"backup_id": 1, "path": "/example.com/public_html"},
		"beget_list_backup_databases":        {"backup_id": 1},
		"beget_restore_file_backup":          withConfirm(map[string]any{"backup_id": 1, "paths": []string{"/example.com/public_html"}}),
		"beget_restore_mysql_backup":         withConfirm(map[string]any{"backup_id": 1, "bases": []string{"user_database"}}),
		"beget_download_file_backup":         withConfirm(map[string]any{"backup_id": 1, "paths": []string{"/example.com/public_html"}}),
		"beget_download_mysql_backup":        withConfirm(map[string]any{"backup_id": 1, "bases": []string{"user_database"}}),
		"beget_add_cron_job":                 withConfirm(map[string]any{"minutes": "*/5", "hours": "*", "days": "*", "months": "*", "weekdays": "*", "command": "true"}),
		"beget_edit_cron_job":                withConfirm(map[string]any{"id": 1, "minutes": "0", "hours": "1-5", "days": "1", "months": "1,6", "weekdays": "1-5", "command": "true"}),
		"beget_delete_cron_job":              withConfirm(map[string]any{"row_number": 1}),
		"beget_change_cron_hidden_state":     withConfirm(map[string]any{"row_number": 1, "is_hidden": 0}),
		"beget_set_cron_email":               withConfirm(map[string]any{"email": "admin@example.com"}),
		"beget_add_ftp_account":              withConfirm(map[string]any{"suffix": "ftp", "homedir": "/example.com/public_html", "password": "secret"}),
		"beget_change_ftp_password":          withConfirm(map[string]any{"suffix": "ftp", "password": "secret"}),
		"beget_delete_ftp_account":           withConfirm(map[string]any{"suffix": "ftp"}),
		"beget_add_mysql_database":           withConfirm(map[string]any{"suffix": "database", "password": "secret"}),
		"beget_add_mysql_access":             withConfirm(map[string]any{"suffix": "database", "access": "localhost", "password": "secret"}),
		"beget_delete_mysql_database":        withConfirm(map[string]any{"suffix": "database"}),
		"beget_delete_mysql_access":          withConfirm(map[string]any{"suffix": "database", "access": "192.0.2.1"}),
		"beget_change_mysql_access_password": withConfirm(map[string]any{"suffix": "database", "access": "db.example.com", "password": "secret"}),
		"beget_add_site":                     withConfirm(map[string]any{"name": "example.com"}),
		"beget_delete_site":                  withConfirm(map[string]any{"id": 1}),
		"beget_link_domain_to_site":          withConfirm(map[string]any{"domain_id": 1, "site_id": 2}),
		"beget_unlink_domain_from_site":      withConfirm(map[string]any{"domain_id": 1}),
		"beget_is_site_frozen":               {"site_id": 1},
		"beget_add_virtual_domain":           withConfirm(map[string]any{"hostname": "example", "zone_id": 1}),
		"beget_delete_domain":                withConfirm(map[string]any{"id": 1}),
		"beget_add_virtual_subdomain":        withConfirm(map[string]any{"subdomain": "www", "domain_id": 1}),
		"beget_delete_subdomain":             withConfirm(map[string]any{"id": 1}),
		"beget_check_domain_registration":    {"hostname": "example", "zone_id": 1, "period": 1},
		"beget_get_domain_php_version":       {"full_fqdn": "example.com"},
		"beget_change_domain_php_version":    withConfirm(map[string]any{"full_fqdn": "example.com", "php_version": "8.4", "is_cgi": true}),
		"beget_get_domain_directives":        {"full_fqdn": "example.com"},
		"beget_add_domain_directives":        withConfirm(map[string]any{"full_fqdn": "example.com", "directives_list": []map[string]any{{"name": "php_flag", "value": "log_errors on"}}}),
		"beget_remove_domain_directives":     withConfirm(map[string]any{"full_fqdn": "example.com", "directives_list": []map[string]any{{"name": "php_flag", "value": "log_errors on"}}}),
		"beget_list_mailboxes":               {"domain": "example.com"},
		"beget_change_mailbox_password":      withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin", "mailbox_password": "secret"}),
		"beget_create_mailbox":               withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin", "mailbox_password": "secret"}),
		"beget_delete_mailbox":               withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin"}),
		"beget_change_mailbox_settings":      withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin", "spam_filter_status": 1, "spam_filter": 20, "forward_mail_status": "forward"}),
		"beget_add_mail_forwarding":          withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin", "forward_mailbox": "target@example.net"}),
		"beget_delete_mail_forwarding":       withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin", "forward_mailbox": "target@example.net"}),
		"beget_list_mail_forwarding":         {"domain": "example.com", "mailbox": "admin"},
		"beget_set_domain_mail":              withConfirm(map[string]any{"domain": "example.com", "domain_mailbox": "admin@example.com"}),
		"beget_clear_domain_mail":            withConfirm(map[string]any{"domain": "example.com"}),
		"beget_site_load_details":            {"site_id": 1},
		"beget_database_load_details":        {"db_name": "user_database"},
	}
}

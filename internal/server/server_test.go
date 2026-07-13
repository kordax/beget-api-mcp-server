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
	"github.com/kordax/beget-api-mcp-server/internal/passwordpolicy"
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
		return fakeAnswer(section, method), f.err
	}
	return f.answer, f.err
}

func fakeAnswer(section, method string) json.RawMessage {
	switch section + "/" + method {
	case "user/getAccountInfo", "dns/getData", "domain/checkDomainToRegister", "domain/getPhpVersion", "domain/changePhpVersion", "stat/getSiteLoad", "stat/getDbLoad":
		return json.RawMessage(`{}`)
	case "domain/getZoneList":
		return json.RawMessage(`{}`)
	case "cron/getEmail":
		return json.RawMessage(`null`)
	case "site/isSiteFrozen":
		return json.RawMessage(`false`)
	case "cron/add":
		return json.RawMessage(`{"row_number":1}`)
	case "cron/edit", "cron/changeHiddenState", "domain/addVirtual", "domain/addSubdomainVirtual":
		return json.RawMessage(`1`)
	case "user/toggleSsh":
		return json.RawMessage(`[]`)
	case "backup/getFileBackupList", "backup/getMysqlBackupList", "backup/getFileList", "backup/getMysqlList", "backup/getLog",
		"cron/getList", "ftp/getList", "mysql/getList", "site/getList", "domain/getList", "domain/getSubdomainList", "domain/getDirectives",
		"mail/getMailboxList", "mail/forwardListShow", "stat/getSitesListLoad", "stat/getDbListLoad":
		return json.RawMessage(`[]`)
	default:
		return json.RawMessage(`true`)
	}
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
	assert.Contains(t, result.Instructions, "result.changed")
	assert.Contains(t, result.Instructions, "unknown_outcome")
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
	mailProperties := schemaProperties(t, inputSchemaMap(t, tools["beget_change_mailbox_password"]))
	mailPasswordSchema, ok := mailProperties["mailbox_password"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, passwordpolicy.MailboxMinimumLength, mailPasswordSchema["minLength"])
	assert.EqualValues(t, passwordpolicy.MailboxMaximumLength, mailPasswordSchema["maxLength"])
	assert.Equal(t, passwordpolicy.MailboxAllowedCharacterPattern(), mailPasswordSchema["pattern"])
	assert.Contains(t, mailPasswordSchema["description"], "at least one letter, one digit, and one symbol")
	assert.Equal(t, true, mailPasswordSchema["writeOnly"])
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
	assert.LessOrEqual(t, len(encoded), 150000, "typed input and output contracts must remain smaller than the 166376-byte untyped baseline")
	actual := fmt.Sprintf("%x", sha256.Sum256(encoded))
	assert.Equal(t, "466604c54e0f447098b383c6ea74662b3ab2cd806337cb2ff1bc9302f411ccc2", actual, "intentional MCP contract changes require updating this snapshot")
}

func TestToolsExposeTypedOutputContracts(t *testing.T) {
	session, closeSessions := connectTestClient(t, &fakeCaller{})
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range result.Tools {
		schema := outputSchemaMap(t, tool)
		properties := schemaProperties(t, schema)
		assert.ElementsMatch(t, []string{"success", "result", "errors"}, mapKeys(properties), tool.Name)
		assert.NotContains(t, properties, "answer", tool.Name)
	}

	mutation := findTool(t, result.Tools, "beget_delete_domain")
	mutationResult := schemaProperties(t, outputSchemaMap(t, mutation))["result"].(map[string]any)
	mutationResultProperties := schemaProperties(t, mutationResult)
	assert.Contains(t, mutationResultProperties, "changed")
	assert.Contains(t, mutationResultProperties, "details")

	read := findTool(t, result.Tools, "beget_list_sites")
	readResult := schemaProperties(t, outputSchemaMap(t, read))["result"].(map[string]any)
	assert.Contains(t, readResult["type"], "array")

	errorItems := schemaProperties(t, outputSchemaMap(t, read))["errors"].(map[string]any)["items"].(map[string]any)
	errorType := schemaProperties(t, errorItems)["type"].(map[string]any)
	assert.ElementsMatch(t, []any{
		string(ErrorValidation), string(ErrorAuthorization), string(ErrorProviderRejection),
		string(ErrorTransportFailure), string(ErrorConfirmationFailure), string(ErrorUnknownOutcome),
	}, errorType["enum"])
}

func TestToolResultsNormalizeAndPreserveProviderData(t *testing.T) {
	caller := &fakeCaller{answer: json.RawMessage(`[{"id":"125","path":"site/public_html","domains":[],"new_field":{"enabled":true}}]`)}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_list_sites", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	output := structuredMap(t, result)
	assert.Equal(t, true, output["success"])
	assert.Empty(t, output["errors"])
	sites := output["result"].([]any)
	site := sites[0].(map[string]any)
	assert.EqualValues(t, 125, site["id"])
	assert.Equal(t, []any{}, site["domains"])
	extra := site["additional_properties_json"].(map[string]any)
	assert.JSONEq(t, `{"enabled":true}`, extra["new_field"].(string))

	caller.answer = json.RawMessage(`true`)
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_delete_domain", Arguments: map[string]any{"id": 125, "confirm": true},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	mutation := structuredMap(t, result)["result"].(map[string]any)
	assert.Equal(t, true, mutation["changed"])
	assert.Equal(t, true, mutation["details"])
}

func TestToolErrorsUseStableMachineReadableCategories(t *testing.T) {
	tests := map[string]struct {
		name      string
		arguments map[string]any
		err       error
		expected  ErrorType
	}{
		"authorization": {name: "beget_list_domains", arguments: map[string]any{}, err: &beget.AuthenticationError{}, expected: ErrorAuthorization},
		"provider rejection": {name: "beget_list_domains", arguments: map[string]any{}, err: &beget.MethodError{
			Section: "domain", Method: "getList", Errors: []beget.ProviderError{{Code: "LIMIT_ERROR", Message: "limit reached"}},
		}, expected: ErrorProviderRejection},
		"transport failure": {name: "beget_list_domains", arguments: map[string]any{}, err: &beget.TransportError{
			Stage: "send", OutcomeUnknown: true, Cause: errors.New("connection reset"),
		}, expected: ErrorTransportFailure},
		"unknown mutation outcome": {name: "beget_delete_domain", arguments: map[string]any{"id": 125, "confirm": true}, err: &beget.TransportError{
			Stage: "read", OutcomeUnknown: true, Cause: errors.New("connection reset"),
		}, expected: ErrorUnknownOutcome},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			caller := &fakeCaller{err: testCase.err}
			session, closeSessions := connectTestClient(t, caller)
			defer closeSessions()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: testCase.name, Arguments: testCase.arguments})
			require.NoError(t, err)
			assert.True(t, result.IsError)
			output := structuredMap(t, result)
			errorsList := output["errors"].([]any)
			assert.Equal(t, string(testCase.expected), errorsList[0].(map[string]any)["type"])
			assert.NotEmpty(t, errorsList[0].(map[string]any)["next_step"])
			if testCase.expected == ErrorUnknownOutcome {
				assert.Equal(t, false, output["result"].(map[string]any)["changed"])
			}
		})
	}
}

func TestProviderErrorsNeverCopyInputSecrets(t *testing.T) {
	password := "Secret123!"
	caller := &fakeCaller{err: &beget.MethodError{
		Section: "ftp", Method: "changePassword",
		Errors: []beget.ProviderError{{Code: "REJECTED", Message: "provider rejected " + password}},
	}}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_change_ftp_password", Arguments: map[string]any{
			"suffix": "account", "password": password, "confirm": true,
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.NotContains(t, callToolText(result), password)
	assert.NotContains(t, fmt.Sprint(result.StructuredContent), password)
	assert.Contains(t, callToolText(result), "[REDACTED]")
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
	output := structuredMap(t, result)
	assert.Equal(t, string(ErrorConfirmationFailure), output["errors"].([]any)[0].(map[string]any)["type"])
	assert.Equal(t, false, output["result"].(map[string]any)["changed"])
	assert.Zero(t, caller.calls, "unconfirmed mutation reached the Beget client")
}

func TestMailboxPasswordChangeUsesMailEndpoint(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_change_mailbox_password", Arguments: map[string]any{
			"domain": "example.com", "mailbox": "admin", "mailbox_password": "Newpassword1!", "confirm": true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Equal(t, "mail", caller.section)
	assert.Equal(t, "changeMailboxPassword", caller.method)
	parameters := callerInputMap(t, caller.input)
	assert.Equal(t, map[string]any{
		"domain": "example.com", "mailbox": "admin", "mailbox_password": "Newpassword1!",
	}, parameters)
	assert.NotContains(t, parameters, "confirm", "confirm must not reach Beget")
}

func TestMailboxPasswordPolicyRejectsBeforeBeget(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	for name, testCase := range map[string]struct {
		password string
		expected string
	}{
		"missing class":         {password: "abcdef1", expected: "at least one allowed symbol"},
		"too short":             {password: "A1!", expected: "mailbox_password"},
		"unsupported character": {password: "Abc1! x", expected: "mailbox_password"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "beget_create_mailbox", Arguments: map[string]any{
					"domain": "example.com", "mailbox": "admin", "mailbox_password": testCase.password, "confirm": true,
				},
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, result.IsError)
			assert.Contains(t, callToolText(result), testCase.expected)
			assert.NotContains(t, callToolText(result), testCase.password)
		})
	}
	assert.Zero(t, caller.calls)
}

func TestWeakMailboxPasswordProviderErrorUsesSafeValidationGuidance(t *testing.T) {
	password := "Strong1!"
	caller := &fakeCaller{err: &beget.APIError{
		Section: "mail", Method: "changeMailboxPassword", Code: float64(weakMailboxPasswordErrorCode), Message: "Password too weak",
	}}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_change_mailbox_password", Arguments: map[string]any{
			"domain": "example.com", "mailbox": "admin", "mailbox_password": password, "confirm": true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, callToolText(result), "rejected by Beget as too weak")
	assert.Contains(t, callToolText(result), "6 to 64 characters")
	assert.Contains(t, callToolText(result), "at least one letter, one digit, and one symbol")
	assert.NotContains(t, callToolText(result), password)
}

func TestWeakMailboxPasswordMappingReportsMissingClasses(t *testing.T) {
	password := "abcdef"
	mapped := mapBegetError("mail", "createMailbox", json.RawMessage(`{"mailbox_password":"abcdef"}`), &beget.APIError{
		Section: "mail", Method: "createMailbox", Code: "1208", Message: "Password too weak",
	})

	assert.ErrorContains(t, mapped, "at least one digit")
	assert.ErrorContains(t, mapped, "at least one allowed symbol")
	assert.NotContains(t, mapped.Error(), password)
}

func TestSensitiveSchemaErrorsRedactManagedPasswords(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()
	password := "Q7!"

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_add_mysql_database", Arguments: map[string]any{
			"suffix": "database", "password": password, "confirm": true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, callToolText(result), "[REDACTED]")
	assert.NotContains(t, callToolText(result), password)
	assert.Equal(t, string(ErrorValidation), structuredMap(t, result)["errors"].([]any)[0].(map[string]any)["type"])
	assert.NotContains(t, fmt.Sprint(result.StructuredContent), password)

	mailboxPassword := "abcdef1"
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_change_mailbox_password", Arguments: map[string]any{
			"domain": "example.com", "mailbox": "admin", "mailbox_password": mailboxPassword, "confirm": false,
		},
	})
	require.NoError(t, err)
	assert.Contains(t, callToolText(result), "confirm must be true")
	assert.NotContains(t, callToolText(result), mailboxPassword)
	assert.Zero(t, caller.calls)
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

	result, output, err := service.getDNSRecords(ctx, nil, DNSInput{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorValidation, output.Errors[0].Type)
	caller.answer = json.RawMessage(`{"ok":true}`)
	_, output, err = service.getDNSRecords(ctx, nil, DNSInput{FQDN: "example.com"})
	require.NoError(t, err)
	require.NotNil(t, output.Result)
	assert.Equal(t, `true`, output.Result.AdditionalPropertiesJSON["ok"])
	assert.Equal(t, "dns", caller.section)
	assert.Equal(t, "getData", caller.method)
	caller.answer = nil

	result, changeOutput, err := service.changeDNSRecords(ctx, nil, ChangeDNSInput{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorConfirmationFailure, changeOutput.Errors[0].Type)
	assert.False(t, changeOutput.Result.Changed)
	result, changeOutput, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{Confirm: true})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorValidation, changeOutput.Errors[0].Type)
	result, changeOutput, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{Confirm: true, FQDN: "example.com"})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{
		Confirm: true,
		FQDN:    "example.com",
		Records: DNSRecords{A: []DNSRecord{{Value: "192.0.2.1"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "changeRecords", caller.method)

	result, freezeOutput, err := service.freezeSite(ctx, nil, FreezeSiteInput{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorConfirmationFailure, freezeOutput.Errors[0].Type)
	result, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{Confirm: true})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	for _, excludedPath := range []string{"", "/absolute", "../escape"} {
		result, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{ID: 42, Confirm: true, ExcludedPaths: []string{excludedPath}})
		require.NoError(t, err)
		assert.True(t, result.IsError)
	}
	_, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{ID: 42, Confirm: true, ExcludedPaths: []string{"cache"}})
	require.NoError(t, err)
	assert.Equal(t, "freeze", caller.method)

	result, unfreezeOutput, err := service.unfreezeSite(ctx, nil, UnfreezeSiteInput{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorConfirmationFailure, unfreezeOutput.Errors[0].Type)
	result, _, err = service.unfreezeSite(ctx, nil, UnfreezeSiteInput{Confirm: true})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	_, _, err = service.unfreezeSite(ctx, nil, UnfreezeSiteInput{ID: 42, Confirm: true})
	require.NoError(t, err)
	assert.Equal(t, "unfreeze", caller.method)
}

func TestServiceCallPropagatesAndDecodesErrors(t *testing.T) {
	caller := &fakeCaller{err: errors.New("Beget unavailable")}
	service := &service{client: caller}

	result, output, err := callRead[AccountInfoResult](context.Background(), service, "user", "getAccountInfo", nil)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorTransportFailure, output.Errors[0].Type)

	caller.err = nil
	caller.answer = json.RawMessage(`not-json`)
	result, output, err = callRead[AccountInfoResult](context.Background(), service, "user", "getAccountInfo", nil)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, "invalid_provider_response", output.Errors[0].Code)
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

func outputSchemaMap(t *testing.T, tool *mcp.Tool) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(tool.OutputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(encoded, &schema))
	return schema
}

func structuredMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var output map[string]any
	require.NoError(t, json.Unmarshal(encoded, &output))
	return output
}

func findTool(t *testing.T, tools []*mcp.Tool, name string) *mcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	require.FailNow(t, "tool not found", name)
	return nil
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
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
		"beget_change_mailbox_password":      withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin", "mailbox_password": "Secret1!"}),
		"beget_create_mailbox":               withConfirm(map[string]any{"domain": "example.com", "mailbox": "admin", "mailbox_password": "Secret1!"}),
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

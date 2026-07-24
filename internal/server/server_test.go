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
	"github.com/kordax/beget-api-mcp-server/internal/buildinfo"
	"github.com/kordax/beget-api-mcp-server/internal/passwordpolicy"
	"github.com/kordax/beget-api-mcp-server/internal/transport"
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
	auth    *beget.AuthenticationStatus
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
	case "backup/getFileBackupList", "backup/getMysqlBackupList", "backup/getFileList", "backup/getMysqlList", "backup/getLog",
		"cron/getList", "ftp/getList", "mysql/getList", "site/getList", "domain/getList", "domain/getSubdomainList", "domain/getDirectives",
		"mail/getMailboxList", "mail/forwardListShow", "stat/getSitesListLoad", "stat/getDbListLoad":
		return json.RawMessage(`[]`)
	default:
		return json.RawMessage(`true`)
	}
}

func (f *fakeCaller) AuthenticationStatus() beget.AuthenticationStatus {
	if f.auth != nil {
		return *f.auth
	}
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
	assert.Len(t, tools, 67)
	assert.NotContains(t, tools, "beget_toggle_ssh")
	require.Contains(t, tools, "beget_auth_status")
	require.Contains(t, tools, "beget_server_capabilities")
	require.Contains(t, tools, "beget_validate_mailbox_password")
	validation := tools["beget_validate_mailbox_password"].Annotations
	assert.True(t, validation.ReadOnlyHint)
	assert.True(t, validation.IdempotentHint)
	require.NotNil(t, validation.OpenWorldHint)
	assert.False(t, *validation.OpenWorldHint)
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
	assert.Contains(t, result.Instructions, "dry_run=true")
	assert.Contains(t, result.Instructions, "no Beget request")
	assert.Contains(t, result.Instructions, "never guarantees provider acceptance")
	assert.Contains(t, result.Instructions, "exactly one logical Beget group")
	assert.Contains(t, result.Instructions, "empty-value records")
	assert.Contains(t, result.Instructions, "Never guess identifiers")
	assert.Contains(t, result.Instructions, "confirm=true")
	assert.Contains(t, result.Instructions, "result.changed")
	assert.Contains(t, result.Instructions, "unknown_outcome")
	assert.Contains(t, result.Instructions, "Never retry a mutation automatically")
	assert.Contains(t, result.Instructions, "at most one provider request")
	assert.Contains(t, result.Instructions, "no hidden preflight read")
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
		[]string{"command", "confirm", "days", "dry_run", "hours", "minutes", "months", "weekdays"},
		[]string{"command", "confirm", "days", "hours", "minutes", "months", "weekdays"},
	)
	assertToolContract(t, tools["beget_add_ftp_account"],
		[]string{"confirm", "dry_run", "homedir", "password", "suffix"},
		[]string{"confirm", "homedir", "password", "suffix"},
	)
	dnsChange := tools["beget_change_dns_records"]
	assert.Contains(t, dnsChange.Description, "Replace exactly one logical Beget record group")
	assert.Contains(t, dnsChange.Description, "omit all other groups, empty arrays, and empty-value provider placeholders")
	assert.Contains(t, dnsChange.Description, "dry_run=true and confirm=false is local")
	dnsRecordsSchema := schemaProperties(t, inputSchemaMap(t, dnsChange))["records"].(map[string]any)
	assert.Contains(t, dnsRecordsSchema["description"], "exactly one replacement group")
	assert.Contains(t, dnsRecordsSchema["description"], "omit all other and empty groups")
	dnsRecordGroups := schemaProperties(t, dnsRecordsSchema)
	assert.Contains(t, dnsRecordGroups["DNS_IP"].(map[string]any)["description"], "only with at least one DNS record")
	assert.Contains(t, dnsRecordGroups["DNS_IP"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["value"].(map[string]any)["description"], "omit provider placeholder records")
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
	validator := tools["beget_validate_mailbox_password"]
	assertToolContract(t, validator, []string{"mailbox_password"}, []string{"mailbox_password"})
	validationPasswordSchema := schemaProperties(t, inputSchemaMap(t, validator))["mailbox_password"].(map[string]any)
	assert.Equal(t, true, validationPasswordSchema["writeOnly"])
	assert.Contains(t, validationPasswordSchema["description"], "at least one letter, one digit, and one symbol")
	assert.NotContains(t, validationPasswordSchema, "minLength", "the validator must accept values that violate the length rule")
	assert.NotContains(t, validationPasswordSchema, "maxLength", "the validator must accept values that violate the length rule")
	assert.NotContains(t, validationPasswordSchema, "pattern", "the validator must accept unsupported characters and report them")
	assertToolContract(t, tools["beget_change_mailbox_settings"],
		[]string{"confirm", "domain", "dry_run", "forward_mail_status", "mailbox", "spam_filter", "spam_filter_status"},
		[]string{"confirm", "domain", "forward_mail_status", "mailbox", "spam_filter", "spam_filter_status"},
	)
	assertToolContract(t, tools["beget_add_domain_directives"],
		[]string{"confirm", "directives_list", "dry_run", "full_fqdn"},
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
		assert.LessOrEqualf(t, len(properties), 9, "%s exposes too many input properties", tool.Name)
	}
}

func TestToolContractSnapshot(t *testing.T) {
	session, closeSessions := connectTestClient(t, &fakeCaller{})
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	encoded, err := json.Marshal(result.Tools)
	require.NoError(t, err)
	assert.Len(t, result.Tools, 67)
	assert.LessOrEqual(t, len(encoded), 172000, "typed contracts must remain compact after adding explicit safety guidance")
	actual := fmt.Sprintf("%x", sha256.Sum256(encoded))
	assert.Equal(t, "560805bd65630572627ef30ee95ef01024bc3f571e3dc6fd67d6e765162ad49e", actual, "intentional MCP contract changes require updating this snapshot")
}

func TestCapabilitiesResourceIsCompactAndDerivedFromOperationCatalog(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	listed, err := session.ListResources(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, listed.Resources, 1)
	resource := listed.Resources[0]
	assert.Equal(t, capabilitiesResourceURI, resource.URI)
	assert.Equal(t, capabilitiesResourceMIMEType, resource.MIMEType)
	assert.Contains(t, resource.Description, "only when")

	read, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: capabilitiesResourceURI})
	require.NoError(t, err)
	require.Len(t, read.Contents, 1)
	content := read.Contents[0].Text
	assert.EqualValues(t, len(content), resource.Size)
	assert.LessOrEqual(t, len(content), 6000, "optional routing catalog must stay cheaper than another tools/list response")

	var catalog capabilityCatalog
	require.NoError(t, json.Unmarshal([]byte(content), &catalog))
	assert.Equal(t, 1, catalog.Version)
	assert.Contains(t, catalog.Usage, "Read only when tool selection remains unclear")
	assert.Len(t, catalog.Categories, len(capabilitySections))

	actualTools := make(map[string]bool, len(operationCatalog))
	for _, category := range catalog.Categories {
		for _, name := range category.Inspect {
			require.NotContains(t, actualTools, name)
			actualTools[name] = false
		}
		for _, name := range category.Change {
			require.NotContains(t, actualTools, name)
			actualTools[name] = true
		}
	}
	require.Len(t, actualTools, len(operationCatalog))
	for _, operation := range operationCatalog {
		mutating, exists := actualTools[operation.name]
		assert.True(t, exists, operation.name)
		assert.Equal(t, operation.mutating, mutating, operation.name)
	}
	assert.Zero(t, caller.calls, "reading the local catalog must not call Beget")
}

func TestToolSectionFilterLimitsPublishedSurface(t *testing.T) {
	caller := &fakeCaller{}
	server := newConfiguredServer(caller, nil, transport.Options{ToolSections: []string{"dns"}})
	session, closeSessions := connectServer(t, server)
	defer closeSessions()

	listedTools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	toolNames := make([]string, 0, len(listedTools.Tools))
	for _, tool := range listedTools.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	assert.ElementsMatch(t, []string{
		"beget_auth_status",
		"beget_server_capabilities",
		"beget_validate_mailbox_password",
		"beget_get_dns_records",
		"beget_change_dns_records",
	}, toolNames)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_server_capabilities", Arguments: map[string]any{},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	capabilities := structuredMap(t, result)["result"].(map[string]any)
	assert.Equal(t, true, capabilities["has_mutations"])
	methods := capabilities["supported_beget_methods"].([]any)
	require.Len(t, methods, 2)
	for _, value := range methods {
		assert.Equal(t, "dns", value.(map[string]any)["section"])
	}

	read, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: capabilitiesResourceURI})
	require.NoError(t, err)
	require.Len(t, read.Contents, 1)
	var catalog capabilityCatalog
	require.NoError(t, json.Unmarshal([]byte(read.Contents[0].Text), &catalog))
	require.Len(t, catalog.Categories, 2)
	assert.Equal(t, "local diagnostics", catalog.Categories[0].Name)
	assert.Equal(t, "dns", catalog.Categories[1].Name)
	catalogTools := append([]string{}, catalog.Categories[0].Inspect...)
	catalogTools = append(catalogTools, catalog.Categories[0].Change...)
	catalogTools = append(catalogTools, catalog.Categories[1].Inspect...)
	catalogTools = append(catalogTools, catalog.Categories[1].Change...)
	assert.ElementsMatch(t, toolNames, catalogTools)
	assert.Zero(t, caller.calls, "filtered local metadata must not call Beget")
}

func TestOperationCatalogLinksOfficialDocumentationAndDoesNotHideMutations(t *testing.T) {
	seenNames := make(map[string]struct{}, len(operationCatalog))
	seenEndpoints := make(map[string]string, len(operationCatalog))
	for _, operation := range operationCatalog {
		require.NotEmpty(t, operation.name)
		require.NotEmpty(t, operation.description, operation.name)
		require.NotEmpty(t, operation.method, operation.name)
		require.NotContains(t, seenNames, operation.name)
		seenNames[operation.name] = struct{}{}

		section, exists := capabilitySections[operation.section]
		require.Truef(t, exists, "%s has unknown section %q", operation.name, operation.section)
		if operation.section == "local" {
			assert.Empty(t, section.documentation)
			continue
		}
		assert.Contains(t, section.documentation, "https://beget.com/ru/kb/api/", operation.name)
		endpoint := operation.section + "/" + operation.method
		assert.NotContainsf(t, seenEndpoints, endpoint, "%s duplicates endpoint used by %s", operation.name, seenEndpoints[endpoint])
		seenEndpoints[endpoint] = operation.name
		if operation.mutating {
			assert.NotRegexp(t, `^beget_(get|list|is)_`, operation.name, "a mutation must not look read-only")
		}
	}
	assert.Len(t, seenNames, 67)
}

func TestAccountSectionOnlyReadsAccountInfo(t *testing.T) {
	var accountOperations []operationSpec
	for _, operation := range operationCatalog {
		if operation.section == "user" {
			accountOperations = append(accountOperations, operation)
		}
	}

	require.Len(t, accountOperations, 1)
	assert.Equal(t, "beget_account_info", accountOperations[0].name)
	assert.Equal(t, "getAccountInfo", accountOperations[0].method)
	assert.False(t, accountOperations[0].mutating)
}

func TestOperationCatalogSnapshot(t *testing.T) {
	type contract struct {
		Name, Description, Section, Method string
		Mutating, Destructive, Idempotent  bool
	}
	contracts := make([]contract, 0, len(operationCatalog))
	for _, operation := range operationCatalog {
		contracts = append(contracts, contract{
			Name: operation.name, Description: operation.description,
			Section: operation.section, Method: operation.method,
			Mutating: operation.mutating, Destructive: operation.destructive, Idempotent: operation.idempotent,
		})
	}
	encoded, err := json.Marshal(contracts)
	require.NoError(t, err)
	actual := fmt.Sprintf("%x", sha256.Sum256(encoded))
	assert.Equal(t, "285a8e77d4ccb6baa6fa9bd145e6b48efd803f6f82bea507b1e688d9fb8b1f2d", actual, "intentional operation catalog changes require updating this snapshot")
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
	assert.Contains(t, mutationResultProperties, "dry_run")
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

func TestDNSValidationProvidesSpecificLocalRecoveryWithoutCallingBeget(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	tests := map[string]struct {
		records         map[string]any
		expectedField   string
		expectedMessage string
	}{
		"mixed logical groups": {
			records: map[string]any{
				"A":  []map[string]any{{"priority": 0, "value": "192.0.2.1"}},
				"NS": []map[string]any{{"priority": 0, "value": "ns.example.com"}},
			},
			expectedField: "records",
		},
		"empty group": {
			records: map[string]any{
				"A":  []map[string]any{{"priority": 0, "value": "192.0.2.1"}},
				"NS": []map[string]any{},
			},
			expectedField:   "records.NS",
			expectedMessage: "omit the empty group instead",
		},
		"empty DNS IP placeholder": {
			records: map[string]any{
				"DNS":    []map[string]any{{"priority": 0, "value": "ns.example.com"}},
				"DNS_IP": []map[string]any{{"priority": 0, "value": ""}},
			},
			expectedField:   "records.DNS_IP.value",
			expectedMessage: "omit empty-value provider placeholders",
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "beget_change_dns_records",
				Arguments: map[string]any{
					"confirm": false, "dry_run": true, "fqdn": "example.com", "records": testCase.records,
				},
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, result.IsError)
			output := structuredMap(t, result)
			assert.Equal(t, false, output["success"])
			assert.Equal(t, false, output["result"].(map[string]any)["changed"])
			errorsList := output["errors"].([]any)
			require.Len(t, errorsList, 1)
			toolError := errorsList[0].(map[string]any)
			assert.Equal(t, testCase.expectedField, toolError["field"])
			assert.Contains(t, toolError["next_step"], "Choose exactly one group")
			assert.Contains(t, toolError["next_step"], "omit every other group and every empty array")
			if testCase.expectedMessage != "" {
				assert.Contains(t, toolError["message"], testCase.expectedMessage)
			}
		})
	}
	assert.Zero(t, caller.calls)
}

func TestEveryMutationSupportsLocalDryRunWithoutCallingBeget(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()
	arguments := validOperationArguments()

	for _, operation := range operationCatalog {
		if !operation.mutating {
			continue
		}
		params := make(map[string]any, len(arguments[operation.name])+1)
		for name, value := range arguments[operation.name] {
			params[name] = value
		}
		params["confirm"] = false
		params["dry_run"] = true
		callsBefore := caller.calls
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: operation.name, Arguments: params})
		require.NoError(t, err, operation.name)
		require.NotNil(t, result, operation.name)
		assert.False(t, result.IsError, operation.name)
		assert.Equal(t, callsBefore, caller.calls, "%s dry-run must not call Beget", operation.name)

		mutation := structuredMap(t, result)["result"].(map[string]any)
		assert.Equal(t, false, mutation["changed"], operation.name)
		assessment := mutation["dry_run"].(map[string]any)
		assert.Equal(t, "local", assessment["scope"], operation.name)
		assert.Equal(t, "confirmation_required", assessment["status"], operation.name)
		assert.Equal(t, false, assessment["provider_acceptance_guaranteed"], operation.name)
	}
}

func TestDryRunReportsConfirmationAndCredentialPrerequisitesWithoutSecrets(t *testing.T) {
	credentialStatus := beget.AuthenticationStatus{
		Configured: false, Source: "secret-store-name", Message: "credential-message-that-must-not-leak",
	}
	caller := &fakeCaller{auth: &credentialStatus}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()
	password := "Secret123!"
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_change_ftp_password",
		Arguments: map[string]any{
			"suffix": "ftp", "password": password, "confirm": true, "dry_run": true,
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assessment := structuredMap(t, result)["result"].(map[string]any)["dry_run"].(map[string]any)
	assert.Equal(t, "credentials_required", assessment["status"])
	assert.Zero(t, caller.calls)
	assert.NotContains(t, callToolText(result), password)
	assert.NotContains(t, callToolText(result), credentialStatus.Source)
	assert.NotContains(t, callToolText(result), credentialStatus.Message)
}

func TestServerCapabilitiesAreLocalTypedAndCredentialIndependent(t *testing.T) {
	credentialStatus := beget.AuthenticationStatus{
		Configured: true, Source: "secret-store-name", Message: "credential-message-that-must-not-leak",
	}
	caller := &fakeCaller{auth: &credentialStatus}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_server_capabilities", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	output := structuredMap(t, result)
	capabilities := output["result"].(map[string]any)
	assert.Equal(t, buildinfo.Version, capabilities["server_version"])
	assert.Equal(t, true, capabilities["has_mutations"])
	methods := capabilities["supported_beget_methods"].([]any)
	assert.Len(t, methods, 64)
	methodIndex := 0
	for _, operation := range operationCatalog {
		if operation.section == "local" {
			continue
		}
		method := methods[methodIndex].(map[string]any)
		assert.Equal(t, operation.name, method["tool"])
		assert.Equal(t, operation.section, method["section"])
		assert.Equal(t, operation.method, method["method"])
		assert.Equal(t, operation.mutating, method["mutating"])
		assert.Equal(t, operation.destructive, method["destructive"])
		assert.Equal(t, operation.idempotent, method["idempotent"])
		assert.Equal(t, operation.mutating, method["dry_run_supported"])
		assert.Equal(t, operation.mutating, method["confirmation_required"])
		methodIndex++
	}
	assert.Equal(t, "local", capabilities["dry_run"].(map[string]any)["scope"])
	assert.Equal(t, false, capabilities["dry_run"].(map[string]any)["provider_acceptance_guaranteed"])
	assert.Equal(t, false, capabilities["confirmation_tokens"].(map[string]any)["supported"])
	assert.Equal(t, true, capabilities["idempotency"].(map[string]any)["annotations"])
	assert.Equal(t, false, capabilities["secret_references"].(map[string]any)["supported"])
	assert.Equal(t, false, capabilities["rotation_workflow"].(map[string]any)["supported"])
	assert.Zero(t, caller.calls)
	assert.NotContains(t, callToolText(result), credentialStatus.Source)
	assert.NotContains(t, callToolText(result), credentialStatus.Message)
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

	for _, spec := range operationCatalog {
		tool, ok := tools[spec.name]
		require.Truef(t, ok, "%s is not registered", spec.name)
		assert.Equal(t, spec.description, tool.Description, spec.name)
		if spec.mutating {
			assert.False(t, tool.Annotations.ReadOnlyHint, spec.name)
			require.NotNilf(t, tool.Annotations.DestructiveHint, "%s must declare whether it is destructive", spec.name)
			assert.Equal(t, spec.destructive, *tool.Annotations.DestructiveHint, spec.name)
			assert.Equal(t, spec.idempotent, tool.Annotations.IdempotentHint, spec.name)
			assert.Contains(t, schemaProperties(t, inputSchemaMap(t, tool)), "confirm", spec.name)
			assert.Contains(t, schemaProperties(t, inputSchemaMap(t, tool)), "dry_run", spec.name)
			continue
		}
		assert.True(t, tool.Annotations.ReadOnlyHint, spec.name)
		assert.True(t, tool.Annotations.IdempotentHint, spec.name)
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

func TestMailboxPasswordValidationIsLocalStructuredAndSecretSafe(t *testing.T) {
	credentialStatus := beget.AuthenticationStatus{Configured: false, Source: "test", Message: "not configured"}
	caller := &fakeCaller{auth: &credentialStatus}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_validate_mailbox_password", Arguments: map[string]any{"mailbox_password": "Strong1!"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	validation := structuredMap(t, result)["result"].(map[string]any)
	assert.Equal(t, true, validation["valid"])
	assert.Empty(t, validation["violations"])
	assert.Zero(t, caller.calls)

	password := "abc 12"
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_validate_mailbox_password", Arguments: map[string]any{"mailbox_password": password},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError, "policy violations are the successful result of the validation tool")
	validation = structuredMap(t, result)["result"].(map[string]any)
	assert.Equal(t, false, validation["valid"])
	violations := validation["violations"].([]any)
	codes := make([]string, len(violations))
	for index, value := range violations {
		violation := value.(map[string]any)
		codes[index] = violation["code"].(string)
		assert.NotEmpty(t, violation["message"])
	}
	assert.ElementsMatch(t, []string{
		string(passwordpolicy.ViolationMissingSymbol),
		string(passwordpolicy.ViolationUnsupportedCharacter),
	}, codes)
	assert.NotContains(t, callToolText(result), password)
	assert.NotContains(t, fmt.Sprint(result.StructuredContent), password)
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

	for _, spec := range operationCatalog {
		callsBefore := caller.calls
		params := arguments[spec.name]
		if params == nil {
			params = map[string]any{}
		}
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: spec.name, Arguments: params})
		require.NoError(t, err, spec.name)
		require.NotNil(t, result, spec.name)
		assert.False(t, result.IsError, "%s: %s", spec.name, callToolText(result))
		if spec.section == "local" {
			assert.Equal(t, callsBefore, caller.calls, "%s must remain local", spec.name)
			continue
		}
		assert.Equal(t, callsBefore+1, caller.calls, "%s must make exactly one Beget request", spec.name)
		assert.Equal(t, spec.section, caller.section, spec.name)
		assert.Equal(t, spec.method, caller.method, spec.name)
		if spec.mutating {
			assert.NotContains(t, callerInputMap(t, caller.input), "confirm", spec.name)
			assert.NotContains(t, callerInputMap(t, caller.input), "dry_run", spec.name)
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
	result, changeOutput, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{Confirmation: Confirmation{Confirm: true}})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorValidation, changeOutput.Errors[0].Type)
	result, changeOutput, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{Confirmation: Confirmation{Confirm: true}, FQDN: "example.com"})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	_, _, err = service.changeDNSRecords(ctx, nil, ChangeDNSInput{
		Confirmation: Confirmation{Confirm: true},
		FQDN:         "example.com",
		Records:      DNSRecords{A: []DNSRecord{{Value: "192.0.2.1"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "changeRecords", caller.method)

	result, freezeOutput, err := service.freezeSite(ctx, nil, FreezeSiteInput{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorConfirmationFailure, freezeOutput.Errors[0].Type)
	result, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{Confirmation: Confirmation{Confirm: true}})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	for _, excludedPath := range []string{"", "/absolute", "../escape"} {
		result, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{Confirmation: Confirmation{Confirm: true}, ID: 42, ExcludedPaths: []string{excludedPath}})
		require.NoError(t, err)
		assert.True(t, result.IsError)
	}
	_, _, err = service.freezeSite(ctx, nil, FreezeSiteInput{Confirmation: Confirmation{Confirm: true}, ID: 42, ExcludedPaths: []string{"cache"}})
	require.NoError(t, err)
	assert.Equal(t, "freeze", caller.method)

	result, unfreezeOutput, err := service.unfreezeSite(ctx, nil, UnfreezeSiteInput{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, ErrorConfirmationFailure, unfreezeOutput.Errors[0].Type)
	result, _, err = service.unfreezeSite(ctx, nil, UnfreezeSiteInput{Confirmation: Confirmation{Confirm: true}})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	_, _, err = service.unfreezeSite(ctx, nil, UnfreezeSiteInput{Confirmation: Confirmation{Confirm: true}, ID: 42})
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
		"beget_validate_mailbox_password":    {"mailbox_password": "Strong1!"},
		"beget_get_dns_records":              {"fqdn": "example.com"},
		"beget_change_dns_records":           withConfirm(map[string]any{"fqdn": "example.com", "records": map[string]any{"A": []map[string]any{{"priority": 0, "value": "192.0.2.1"}}}}),
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
		"beget_freeze_site":                  withConfirm(map[string]any{"id": 1, "excluded_paths": []string{"cache"}}),
		"beget_unfreeze_site":                withConfirm(map[string]any{"id": 1}),
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

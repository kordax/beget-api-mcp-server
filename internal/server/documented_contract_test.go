// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type documentedInputContract struct {
	properties []string
	required   []string
}

func TestEveryPublishedToolExposesDocumentedInputFields(t *testing.T) {
	session, closeSessions := connectTestClient(t, &fakeCaller{})
	defer closeSessions()

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	contracts := documentedInputContracts()
	require.Len(t, contracts, 67)
	require.Len(t, result.Tools, len(contracts))

	for _, tool := range result.Tools {
		contract, exists := contracts[tool.Name]
		require.Truef(t, exists, "missing documented input contract for %s", tool.Name)
		t.Run(tool.Name, func(t *testing.T) {
			assertToolContract(t, tool, contract.properties, contract.required)
		})
	}

	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}
	assertDocumentedNestedInputFields(t, tools)
}

func TestEveryLocalToolResultFieldIsCovered(t *testing.T) {
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	authResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.False(t, authResult.IsError)
	auth := structuredMap(t, authResult)["result"].(map[string]any)
	assert.ElementsMatch(t, []string{"configured", "source", "message"}, mapKeys(auth))
	assert.IsType(t, false, auth["configured"])
	assert.IsType(t, "", auth["source"])
	assert.IsType(t, "", auth["message"])

	capabilityResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "beget_server_capabilities", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.False(t, capabilityResult.IsError)
	capabilities := structuredMap(t, capabilityResult)["result"].(map[string]any)
	assert.ElementsMatch(t, []string{
		"server_version", "supported_beget_methods", "has_mutations", "dry_run",
		"confirmation_tokens", "idempotency", "secret_references", "rotation_workflow",
	}, mapKeys(capabilities))
	methods := capabilities["supported_beget_methods"].([]any)
	require.Len(t, methods, 64)
	for _, rawMethod := range methods {
		method := rawMethod.(map[string]any)
		assert.ElementsMatch(t, []string{
			"tool", "section", "method", "mutating", "destructive", "idempotent",
			"dry_run_supported", "confirmation_required",
		}, mapKeys(method))
	}
	assert.ElementsMatch(t, []string{
		"supported", "scope", "checks", "provider_requests", "provider_acceptance_guaranteed",
	}, mapKeys(capabilities["dry_run"].(map[string]any)))
	assert.ElementsMatch(t, []string{"supported", "boolean_confirmation", "field"},
		mapKeys(capabilities["confirmation_tokens"].(map[string]any)))
	assert.ElementsMatch(t, []string{"annotations", "idempotency_keys", "automatic_mutation_retry"},
		mapKeys(capabilities["idempotency"].(map[string]any)))
	assert.ElementsMatch(t, []string{"supported", "inline_managed_passwords", "api_credentials_as_tool_inputs"},
		mapKeys(capabilities["secret_references"].(map[string]any)))
	assert.ElementsMatch(t, []string{"supported", "atomic", "stages"},
		mapKeys(capabilities["rotation_workflow"].(map[string]any)))

	validationResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "beget_validate_mailbox_password", Arguments: map[string]any{"mailbox_password": "abc 12"},
	})
	require.NoError(t, err)
	assert.False(t, validationResult.IsError)
	validation := structuredMap(t, validationResult)["result"].(map[string]any)
	assert.ElementsMatch(t, []string{"valid", "violations"}, mapKeys(validation))
	violations := validation["violations"].([]any)
	require.NotEmpty(t, violations)
	for _, rawViolation := range violations {
		assert.ElementsMatch(t, []string{"code", "message"}, mapKeys(rawViolation.(map[string]any)))
	}
	assert.Zero(t, caller.calls)
}

func TestEveryPublishedBegetOperationMatchesOfficialContract(t *testing.T) {
	answers := loadDocumentedProviderResults(t)
	arguments := validOperationArguments()
	caller := &fakeCaller{}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()

	providerOperations := 0
	seenEndpoints := make(map[string]struct{}, len(answers))
	for _, spec := range operationCatalog {
		if spec.section == "local" {
			continue
		}
		providerOperations++
		endpoint := spec.section + "/" + spec.method
		seenEndpoints[endpoint] = struct{}{}
		require.Containsf(t, answers, endpoint, "%s has no documented response fixture", spec.name)

		t.Run(spec.name, func(t *testing.T) {
			caller.answer = answers[endpoint]
			callsBefore := caller.calls
			params := arguments[spec.name]
			if params == nil {
				params = map[string]any{}
			}

			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: spec.name, Arguments: params})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.IsError, callToolText(result))
			assert.Equal(t, callsBefore+1, caller.calls, "each tool call must make exactly one provider request")
			assert.Equal(t, spec.section, caller.section)
			assert.Equal(t, spec.method, caller.method)
			assert.Equal(t, documentedProviderInput(t, spec, params), callerInputMap(t, caller.input))

			structured := structuredMap(t, result)
			assert.Equal(t, true, structured["success"])
			assert.Empty(t, structured["errors"])
			actualProviderResult := structured["result"]
			if spec.mutating {
				mutationResult, ok := actualProviderResult.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, true, mutationResult["changed"])
				assert.NotContains(t, mutationResult, "dry_run")
				actualProviderResult = mutationResult["details"]
			}

			var expectedProviderResult any
			require.NoError(t, json.Unmarshal(answers[endpoint], &expectedProviderResult))
			assertDocumentedValue(t, endpoint, expectedProviderResult, actualProviderResult)
		})
	}

	assert.Equal(t, 64, providerOperations)
	assert.Len(t, answers, providerOperations)
	for endpoint := range answers {
		assert.Containsf(t, seenEndpoints, endpoint, "fixture references unsupported endpoint %s", endpoint)
	}
}

func TestEveryPublishedBegetOperationRejectsMalformedResponses(t *testing.T) {
	caller := &fakeCaller{answer: json.RawMessage(`{`)}
	session, closeSessions := connectTestClient(t, caller)
	defer closeSessions()
	arguments := validOperationArguments()

	for _, spec := range operationCatalog {
		if spec.section == "local" {
			continue
		}
		t.Run(spec.name, func(t *testing.T) {
			params := arguments[spec.name]
			if params == nil {
				params = map[string]any{}
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: spec.name, Arguments: params})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, result.IsError)

			structured := structuredMap(t, result)
			assert.Equal(t, false, structured["success"])
			if spec.mutating {
				mutationResult, ok := structured["result"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, false, mutationResult["changed"])
				assert.NotContains(t, mutationResult, "details")
			} else {
				assert.Nil(t, structured["result"])
			}
			errors, ok := structured["errors"].([]any)
			require.True(t, ok)
			require.Len(t, errors, 1)
			toolError, ok := errors[0].(map[string]any)
			require.True(t, ok)
			if spec.mutating {
				assert.Equal(t, string(ErrorUnknownOutcome), toolError["type"])
				assert.Equal(t, "mutation_outcome_unknown", toolError["code"])
			} else {
				assert.Equal(t, string(ErrorTransportFailure), toolError["type"])
				assert.Equal(t, "invalid_provider_response", toolError["code"])
			}
		})
	}
}

func TestDocumentedNullableAndPolymorphicResponses(t *testing.T) {
	t.Run("cron email null", func(t *testing.T) {
		result, err := decodeTypedResult[*string](json.RawMessage(`null`))
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("domain dates preserve null and scalar variants", func(t *testing.T) {
		result, err := decodeTypedResult[Domain](json.RawMessage(`{"date_register":null,"date_expire":0}`))
		require.NoError(t, err)
		assert.Nil(t, result.DateRegistered)
		assert.Equal(t, apiStringPointer("0"), result.DateExpires)

		result, err = decodeTypedResult[Domain](json.RawMessage(`{"date_register":false,"date_expire":"2027-07-01"}`))
		require.NoError(t, err)
		assert.Equal(t, apiStringPointer("false"), result.DateRegistered)
		assert.Equal(t, apiStringPointer("2027-07-01"), result.DateExpires)
	})

	t.Run("documented false and zero fields remain present", func(t *testing.T) {
		siteDomain, err := decodeTypedResult[SiteDomain](json.RawMessage(`{"id":"1","fqdn":"example.test","php_version":"8.4","http_version":2,"ssl":false,"ssl_status":"none","nginx_template":"default","redis_session":false}`))
		require.NoError(t, err)
		assert.Equal(t, APIBool(false), siteDomain.SSL)
		assert.Equal(t, APIBool(false), siteDomain.RedisSession)
		encoded, err := json.Marshal(siteDomain)
		require.NoError(t, err)
		var siteDomainOutput map[string]any
		require.NoError(t, json.Unmarshal(encoded, &siteDomainOutput))
		assert.Contains(t, siteDomainOutput, "ssl")
		assert.Contains(t, siteDomainOutput, "redis_session")

		loadPoint, err := decodeTypedResult[DatabaseLoadPoint](json.RawMessage(`{"cpu_time":"0","date":"2026-07-20"}`))
		require.NoError(t, err)
		assert.Equal(t, APIFloat64(0), loadPoint.CPUTime)
		encoded, err = json.Marshal(loadPoint)
		require.NoError(t, err)
		var loadPointOutput map[string]any
		require.NoError(t, json.Unmarshal(encoded, &loadPointOutput))
		assert.Contains(t, loadPointOutput, "cpu_time")

		sizePoint, err := decodeTypedResult[DatabaseSizePoint](json.RawMessage(`{"date":"2026-07-20","size":"0"}`))
		require.NoError(t, err)
		assert.Equal(t, APIInt64(0), sizePoint.Size)
		encoded, err = json.Marshal(sizePoint)
		require.NoError(t, err)
		var sizePointOutput map[string]any
		require.NoError(t, json.Unmarshal(encoded, &sizePointOutput))
		assert.Contains(t, sizePointOutput, "size")

		details, err := decodeTypedResult[DatabaseLoadDetails](json.RawMessage(`{"days":[{"cpu_time":"0","date":"2026-07-20"}],"hours":[{"cpu_time":"0","date":"2026-07-20 18:00:00"}],"size_days":[{"date":"2026-07-20","size":"0"}]}`))
		require.NoError(t, err)
		require.Len(t, details.Days, 1)
		require.Len(t, details.Hours, 1)
		require.Len(t, details.SizeDays, 1)
	})

	for _, test := range []struct {
		name     string
		payType  string
		expected *APIString
	}{
		{name: "false", payType: `false`, expected: apiStringPointer("false")},
		{name: "money", payType: `"money"`, expected: apiStringPointer("money")},
		{name: "bonus", payType: `"bonus_domain"`, expected: apiStringPointer("bonus_domain")},
		{name: "null", payType: `null`, expected: nil},
	} {
		t.Run("domain pay type "+test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"may_be_registered":true,"bonus_domains":0,"balance":100,"pay_type":` + test.payType + `,"price":100,"in_system":false}`)
			result, err := decodeTypedResult[DomainRegistrationResult](raw)
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.PayType)
		})
	}

	for _, test := range []struct {
		name     string
		raw      string
		expected APIInt64
	}{
		{name: "object", raw: `{"row_number":"42"}`, expected: 42},
		{name: "number", raw: `43`, expected: 43},
		{name: "numeric string", raw: `"44"`, expected: 44},
	} {
		t.Run("cron row "+test.name, func(t *testing.T) {
			result, err := decodeTypedResult[CronTaskResult](json.RawMessage(test.raw))
			require.NoError(t, err)
			assert.Equal(t, test.expected, result.RowNumber)
		})
	}
}

func documentedInputContracts() map[string]documentedInputContract {
	contract := func(properties, required string) documentedInputContract {
		return documentedInputContract{properties: strings.Fields(properties), required: strings.Fields(required)}
	}
	return map[string]documentedInputContract{
		"beget_auth_status":                  contract("", ""),
		"beget_server_capabilities":          contract("", ""),
		"beget_validate_mailbox_password":    contract("mailbox_password", "mailbox_password"),
		"beget_account_info":                 contract("", ""),
		"beget_list_file_backups":            contract("", ""),
		"beget_list_mysql_backups":           contract("", ""),
		"beget_list_backup_files":            contract("backup_id path", "path"),
		"beget_list_backup_databases":        contract("backup_id", ""),
		"beget_restore_file_backup":          contract("backup_id confirm dry_run paths", "backup_id confirm paths"),
		"beget_restore_mysql_backup":         contract("backup_id bases confirm dry_run", "backup_id bases confirm"),
		"beget_download_file_backup":         contract("backup_id confirm dry_run paths", "confirm paths"),
		"beget_download_mysql_backup":        contract("backup_id bases confirm dry_run", "bases confirm"),
		"beget_backup_log":                   contract("", ""),
		"beget_list_cron_jobs":               contract("", ""),
		"beget_add_cron_job":                 contract("command confirm days dry_run hours minutes months weekdays", "command confirm days hours minutes months weekdays"),
		"beget_edit_cron_job":                contract("command confirm days dry_run hours id minutes months weekdays", "command confirm days hours id minutes months weekdays"),
		"beget_delete_cron_job":              contract("confirm dry_run row_number", "confirm row_number"),
		"beget_change_cron_hidden_state":     contract("confirm dry_run is_hidden row_number", "confirm is_hidden row_number"),
		"beget_cron_email":                   contract("", ""),
		"beget_set_cron_email":               contract("confirm dry_run email", "confirm email"),
		"beget_get_dns_records":              contract("fqdn", "fqdn"),
		"beget_change_dns_records":           contract("confirm dry_run fqdn records", "confirm fqdn records"),
		"beget_list_ftp_accounts":            contract("", ""),
		"beget_add_ftp_account":              contract("confirm dry_run homedir password suffix", "confirm homedir password suffix"),
		"beget_change_ftp_password":          contract("confirm dry_run password suffix", "confirm password suffix"),
		"beget_delete_ftp_account":           contract("confirm dry_run suffix", "confirm suffix"),
		"beget_list_mysql_databases":         contract("", ""),
		"beget_add_mysql_database":           contract("confirm dry_run password suffix", "confirm password suffix"),
		"beget_add_mysql_access":             contract("access confirm dry_run password suffix", "access confirm password suffix"),
		"beget_delete_mysql_database":        contract("confirm dry_run suffix", "confirm suffix"),
		"beget_delete_mysql_access":          contract("access confirm dry_run suffix", "access confirm suffix"),
		"beget_change_mysql_access_password": contract("access confirm dry_run password suffix", "access confirm password suffix"),
		"beget_list_sites":                   contract("", ""),
		"beget_add_site":                     contract("confirm dry_run name", "confirm name"),
		"beget_delete_site":                  contract("confirm dry_run id", "confirm id"),
		"beget_link_domain_to_site":          contract("confirm domain_id dry_run site_id", "confirm domain_id site_id"),
		"beget_unlink_domain_from_site":      contract("confirm domain_id dry_run", "confirm domain_id"),
		"beget_freeze_site":                  contract("confirm dry_run excluded_paths id", "confirm id"),
		"beget_unfreeze_site":                contract("confirm dry_run id", "confirm id"),
		"beget_is_site_frozen":               contract("site_id", "site_id"),
		"beget_list_domains":                 contract("", ""),
		"beget_list_domain_zones":            contract("", ""),
		"beget_add_virtual_domain":           contract("confirm dry_run hostname zone_id", "confirm hostname zone_id"),
		"beget_delete_domain":                contract("confirm dry_run id", "confirm id"),
		"beget_list_subdomains":              contract("", ""),
		"beget_add_virtual_subdomain":        contract("confirm domain_id dry_run subdomain", "confirm domain_id subdomain"),
		"beget_delete_subdomain":             contract("confirm dry_run id", "confirm id"),
		"beget_check_domain_registration":    contract("hostname period zone_id", "hostname period zone_id"),
		"beget_get_domain_php_version":       contract("full_fqdn", "full_fqdn"),
		"beget_change_domain_php_version":    contract("confirm dry_run full_fqdn is_cgi php_version", "confirm full_fqdn php_version"),
		"beget_get_domain_directives":        contract("full_fqdn", "full_fqdn"),
		"beget_add_domain_directives":        contract("confirm directives_list dry_run full_fqdn", "confirm directives_list full_fqdn"),
		"beget_remove_domain_directives":     contract("confirm directives_list dry_run full_fqdn", "confirm directives_list full_fqdn"),
		"beget_list_mailboxes":               contract("domain", "domain"),
		"beget_change_mailbox_password":      contract("confirm domain dry_run mailbox mailbox_password", "confirm domain mailbox mailbox_password"),
		"beget_create_mailbox":               contract("confirm domain dry_run mailbox mailbox_password", "confirm domain mailbox mailbox_password"),
		"beget_delete_mailbox":               contract("confirm domain dry_run mailbox", "confirm domain mailbox"),
		"beget_change_mailbox_settings":      contract("confirm domain dry_run forward_mail_status mailbox spam_filter spam_filter_status", "confirm domain forward_mail_status mailbox spam_filter spam_filter_status"),
		"beget_add_mail_forwarding":          contract("confirm domain dry_run forward_mailbox mailbox", "confirm domain forward_mailbox mailbox"),
		"beget_delete_mail_forwarding":       contract("confirm domain dry_run forward_mailbox mailbox", "confirm domain forward_mailbox mailbox"),
		"beget_list_mail_forwarding":         contract("domain mailbox", "domain mailbox"),
		"beget_set_domain_mail":              contract("confirm domain domain_mailbox dry_run", "confirm domain domain_mailbox"),
		"beget_clear_domain_mail":            contract("confirm domain dry_run", "confirm domain"),
		"beget_site_load":                    contract("", ""),
		"beget_database_load":                contract("", ""),
		"beget_site_load_details":            contract("site_id", "site_id"),
		"beget_database_load_details":        contract("db_name", "db_name"),
	}
}

func assertDocumentedNestedInputFields(t *testing.T, tools map[string]*mcp.Tool) {
	t.Helper()
	dnsRecords := schemaProperties(t, inputSchemaMap(t, tools["beget_change_dns_records"]))["records"].(map[string]any)
	recordGroups := schemaProperties(t, dnsRecords)
	assert.ElementsMatch(t, []string{"A", "MX", "TXT", "NS", "CNAME", "DNS", "DNS_IP"}, mapKeys(recordGroups))
	for name, rawGroup := range recordGroups {
		group := rawGroup.(map[string]any)
		item := group["items"].(map[string]any)
		assert.ElementsMatch(t, []string{"priority", "value"}, mapKeys(schemaProperties(t, item)), name)
		assert.ElementsMatch(t, []string{"priority", "value"}, stringSlice(item["required"]), name)
	}

	for _, name := range []string{"beget_add_domain_directives", "beget_remove_domain_directives"} {
		directives := schemaProperties(t, inputSchemaMap(t, tools[name]))["directives_list"].(map[string]any)
		item := directives["items"].(map[string]any)
		assert.ElementsMatch(t, []string{"name", "value"}, mapKeys(schemaProperties(t, item)), name)
		assert.ElementsMatch(t, []string{"name", "value"}, stringSlice(item["required"]), name)
	}
}

func documentedProviderInput(t *testing.T, spec operationSpec, arguments map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	var expected map[string]any
	require.NoError(t, json.Unmarshal(encoded, &expected))
	delete(expected, "confirm")
	delete(expected, "dry_run")
	if spec.name == "beget_freeze_site" {
		expected["excludedPaths"] = expected["excluded_paths"]
		delete(expected, "excluded_paths")
	}
	return expected
}

func loadDocumentedProviderResults(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/provider/documented_results.json")
	require.NoError(t, err)
	var results map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &results))
	return results
}

func assertDocumentedValue(t *testing.T, path string, expected, actual any) {
	t.Helper()
	switch expectedValue := expected.(type) {
	case map[string]any:
		actualValue, ok := actual.(map[string]any)
		require.Truef(t, ok, "%s: expected object, got %T", path, actual)
		assert.ElementsMatch(t, mapKeys(expectedValue), mapKeys(actualValue), path)
		for key, child := range expectedValue {
			assertDocumentedValue(t, path+"."+key, child, actualValue[key])
		}
	case []any:
		actualValue, ok := actual.([]any)
		require.Truef(t, ok, "%s: expected array, got %T", path, actual)
		require.Len(t, actualValue, len(expectedValue), path)
		for index, child := range expectedValue {
			assertDocumentedValue(t, fmt.Sprintf("%s[%d]", path, index), child, actualValue[index])
		}
	default:
		if documentedScalarsEqual(expected, actual) {
			return
		}
		assert.Equal(t, expected, actual, path)
	}
}

func documentedScalarsEqual(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	if expected == actual {
		return true
	}
	if expectedText, ok := expected.(string); ok {
		return textMatchesScalar(expectedText, actual)
	}
	if actualText, ok := actual.(string); ok {
		return textMatchesScalar(actualText, expected)
	}
	if expectedNumber, ok := expected.(float64); ok {
		if actualBool, ok := actual.(bool); ok {
			return (expectedNumber == 1 && actualBool) || (expectedNumber == 0 && !actualBool)
		}
	}
	if expectedBool, ok := expected.(bool); ok {
		if actualNumber, ok := actual.(float64); ok {
			return (actualNumber == 1 && expectedBool) || (actualNumber == 0 && !expectedBool)
		}
	}
	return false
}

func textMatchesScalar(text string, scalar any) bool {
	switch value := scalar.(type) {
	case float64:
		parsed, err := strconv.ParseFloat(text, 64)
		return err == nil && parsed == value
	case bool:
		parsed, ok := parseDocumentedBool(text)
		return ok && parsed == value
	default:
		return false
	}
}

func parseDocumentedBool(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "1", "true":
		return true, true
	case "0", "false":
		return false, true
	default:
		return false, false
	}
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if ok {
			result = append(result, text)
		}
	}
	return result
}

func apiStringPointer(value APIString) *APIString {
	return &value
}

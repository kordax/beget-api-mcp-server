// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kordax/beget-api-mcp-server/internal/beget"
	"github.com/kordax/beget-api-mcp-server/internal/passwordpolicy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const weakMailboxPasswordErrorCode = 1208

var sensitiveToolFields = []string{"password", "mailbox_password"}

var (
	validationPropertyPathPattern = regexp.MustCompile(`/properties/[A-Za-z][A-Za-z0-9_]*(?:(?:/properties/[A-Za-z][A-Za-z0-9_]*)|/items)*`)
	validationPropertyNamePattern = regexp.MustCompile(`/properties/([A-Za-z][A-Za-z0-9_]*)`)
	validationLeadingFieldPattern = regexp.MustCompile(`^([a-z][a-z0-9_]*)`)
)

const dnsValidationNextStep = "Choose exactly one group: A/MX/TXT, NS, CNAME, or DNS/DNS_IP. Preserve every record in that selected group, omit every other group and every empty array, and omit provider placeholder records whose value is empty. Then retry local validation without guessing values."

func successfulOutput[Result any](result Result) ToolOutput[Result] {
	return ToolOutput[Result]{Success: true, Result: &result, Errors: []ToolError{}}
}

func failedOutput[Result any](toolErrors ...ToolError) (*mcp.CallToolResult, ToolOutput[Result], error) {
	return &mcp.CallToolResult{IsError: true}, ToolOutput[Result]{Errors: toolErrors}, nil
}

func failedMutationOutput[Details any](toolErrors ...ToolError) (*mcp.CallToolResult, ToolOutput[MutationResult[Details]], error) {
	result := MutationResult[Details]{Changed: false}
	return &mcp.CallToolResult{IsError: true}, ToolOutput[MutationResult[Details]]{Result: &result, Errors: toolErrors}, nil
}

func validationFailure[Result any](err error) (*mcp.CallToolResult, ToolOutput[Result], error) {
	return failedOutput[Result](validationToolError(err.Error()))
}

func mutationValidationFailure[Details any](err error) (*mcp.CallToolResult, ToolOutput[MutationResult[Details]], error) {
	return failedMutationOutput[Details](validationToolError(err.Error()))
}

func dnsRecordsValidationFailure[Details any](err error) (*mcp.CallToolResult, ToolOutput[MutationResult[Details]], error) {
	toolError := dnsValidationToolError(err.Error())
	if !strings.HasPrefix(toolError.Field, "records") {
		toolError.Field = "records"
	}
	return failedMutationOutput[Details](toolError)
}

func mutationConfirmationFailure[Details any](name string) (*mcp.CallToolResult, ToolOutput[MutationResult[Details]], error) {
	return failedMutationOutput[Details](ToolError{
		Type: ErrorConfirmationFailure, Code: "confirmation_required", Field: "confirm",
		Message:  fmt.Sprintf("confirm must be true before calling %s", name),
		NextStep: "Describe the exact change, obtain explicit user approval, then call once with confirm=true.",
	})
}

func validationToolError(message string) ToolError {
	return ToolError{
		Type: ErrorValidation, Code: "invalid_arguments", Field: validationField(message),
		Message: message, NextStep: "Correct the named field using the tool schema, then call again. Do not guess values.",
	}
}

func dnsValidationToolError(message string) ToolError {
	toolError := validationToolError(message)
	if strings.Contains(message, "minLength") && strings.HasSuffix(toolError.Field, ".value") {
		toolError.Message = toolError.Field + " must not be empty; omit empty-value provider placeholders instead of submitting them"
	}
	if strings.Contains(message, "minItems") && strings.HasPrefix(toolError.Field, "records.") {
		toolError.Message = toolError.Field + " must not be an empty array; omit the empty group instead"
	}
	toolError.NextStep = dnsValidationNextStep
	return toolError
}

func validationField(message string) string {
	paths := validationPropertyPathPattern.FindAllString(message, -1)
	if len(paths) > 0 {
		longest := paths[0]
		for _, path := range paths[1:] {
			if len(path) > len(longest) {
				longest = path
			}
		}
		matches := validationPropertyNamePattern.FindAllStringSubmatch(longest, -1)
		fields := make([]string, 0, len(matches))
		for _, match := range matches {
			fields = append(fields, match[1])
		}
		return strings.Join(fields, ".")
	}
	match := validationLeadingFieldPattern.FindStringSubmatch(message)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func toolErrorsForBeget(err error, mutating bool) []ToolError {
	var authenticationError *beget.AuthenticationError
	if errors.As(err, &authenticationError) {
		return []ToolError{{
			Type: ErrorAuthorization, Code: "credentials_required", Message: authenticationError.Error(),
			NextStep: "Configure Beget credentials, then call beget_auth_status before retrying.",
		}}
	}

	var transportError *beget.TransportError
	if errors.As(err, &transportError) {
		if mutating && transportError.OutcomeUnknown {
			return []ToolError{{
				Type: ErrorUnknownOutcome, Code: "mutation_outcome_unknown",
				Message:  "The connection failed after the mutation may have reached Beget.",
				NextStep: "Do not retry the mutation. Read the current resource state first and decide from that result.",
			}}
		}
		return []ToolError{{
			Type: ErrorTransportFailure, Code: "transport_failure", Message: "The Beget response could not be completed safely.",
			NextStep: "Check connectivity and authorization status, then retry this read-only operation.",
		}}
	}

	var apiError *beget.APIError
	if errors.As(err, &apiError) {
		code := fmt.Sprint(apiError.Code)
		if code == "AUTH_ERROR" {
			return []ToolError{{
				Type: ErrorAuthorization, Code: code, Message: "Beget rejected the configured credentials.",
				NextStep: "Update Beget credentials, then call beget_auth_status before retrying.",
			}}
		}
		return []ToolError{{
			Type: ErrorProviderRejection, Code: code, Message: apiError.Error(),
			NextStep: "Read the current resource state and correct the request according to the provider error before retrying.",
		}}
	}

	var methodError *beget.MethodError
	if errors.As(err, &methodError) {
		result := make([]ToolError, 0, len(methodError.Errors))
		for _, providerError := range methodError.Errors {
			if fmt.Sprint(providerError.Code) == "AUTH_ERROR" {
				result = append(result, ToolError{
					Type: ErrorAuthorization, Code: "AUTH_ERROR", Message: "Beget rejected the configured credentials.",
					NextStep: "Update Beget credentials, then call beget_auth_status before retrying.",
				})
				continue
			}
			result = append(result, ToolError{
				Type: ErrorProviderRejection, Code: fmt.Sprint(providerError.Code), Message: providerError.Message,
				NextStep: "Read the current resource state and correct the request according to the provider error before retrying.",
			})
		}
		if len(result) > 0 {
			return result
		}
	}

	var httpError *beget.HTTPError
	if errors.As(err, &httpError) {
		return []ToolError{{
			Type: ErrorProviderRejection, Code: fmt.Sprintf("http_%d", httpError.Status), Message: httpError.Error(),
			NextStep: "Check Beget service status and the current resource state before retrying.",
		}}
	}

	var inputError *beget.InputError
	if errors.As(err, &inputError) {
		return []ToolError{validationToolError(inputError.Error())}
	}

	return []ToolError{{
		Type: ErrorTransportFailure, Code: "transport_failure", Message: "The Beget operation failed before a usable response was received.",
		NextStep: "Check the MCP server logs and connectivity. Retry only if this was a read-only operation.",
	}}
}

func redactSensitiveToolErrors(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, request)
		toolResult, ok := result.(*mcp.CallToolResult)
		if method != "tools/call" || !ok || !toolResult.IsError {
			return result, err
		}
		callRequest, ok := request.(*mcp.CallToolRequest)
		if !ok || callRequest.Params == nil {
			return result, err
		}
		arguments := sensitiveArguments(callRequest.Params.Arguments)
		message := callToolErrorText(toolResult)
		if password := arguments["mailbox_password"]; password != "" {
			if validationMessage := passwordpolicy.ValidationMessage(password); validationMessage != "" && toolErrorReferencesField(toolResult, "mailbox_password") {
				toolResult.Content = []mcp.Content{&mcp.TextContent{Text: validationMessage}}
				message = validationMessage
			}
		}
		for _, secret := range arguments {
			message = redactSecret(message, secret)
		}
		if toolResult.StructuredContent != nil {
			for _, content := range toolResult.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					for _, secret := range arguments {
						textContent.Text = redactSecret(textContent.Text, secret)
					}
				}
			}
			return result, err
		}

		toolError := validationToolError(message)
		if callRequest.Params.Name == "beget_change_dns_records" && strings.HasPrefix(toolError.Field, "records") {
			toolError = dnsValidationToolError(message)
		}
		failure := map[string]any{
			"success": false,
			"result":  nil,
			"errors":  []ToolError{toolError},
		}
		if isMutatingTool(callRequest.Params.Name) {
			failure["result"] = map[string]any{"changed": false}
		}
		encoded, marshalErr := json.Marshal(failure)
		if marshalErr != nil {
			return result, err
		}
		toolResult.StructuredContent = json.RawMessage(encoded)
		toolResult.Content = []mcp.Content{&mcp.TextContent{Text: string(encoded)}}
		return result, err
	}
}

func callToolErrorText(result *mcp.CallToolResult) string {
	var message strings.Builder
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			message.WriteString(textContent.Text)
		}
	}
	if message.Len() == 0 {
		return "tool arguments were rejected"
	}
	return message.String()
}

func isMutatingTool(name string) bool {
	for _, operation := range operationCatalog {
		if operation.name == name {
			return operation.mutating
		}
	}
	return false
}

func toolErrorReferencesField(result *mcp.CallToolResult, field string) bool {
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			message := textContent.Text
			if strings.HasPrefix(message, field+" ") || strings.Contains(message, "/properties/"+field) {
				return true
			}
		}
	}
	return false
}

func sensitiveArguments(raw json.RawMessage) map[string]string {
	var encoded map[string]json.RawMessage
	if json.Unmarshal(raw, &encoded) != nil {
		return nil
	}
	result := make(map[string]string, len(sensitiveToolFields))
	for _, field := range sensitiveToolFields {
		if value := encoded[field]; len(value) > 0 {
			var secret string
			if json.Unmarshal(value, &secret) == nil && secret != "" {
				result[field] = secret
			}
		}
	}
	return result
}

func redactToolErrors(input any, toolErrors []ToolError) []ToolError {
	encoded, err := json.Marshal(input)
	if err != nil {
		return toolErrors
	}
	for _, secret := range sensitiveArguments(encoded) {
		for index := range toolErrors {
			toolErrors[index].Message = redactSecret(toolErrors[index].Message, secret)
			toolErrors[index].NextStep = redactSecret(toolErrors[index].NextStep, secret)
		}
	}
	return toolErrors
}

func redactSecret(message, secret string) string {
	quoted, _ := json.Marshal(secret)
	message = strings.ReplaceAll(message, string(quoted), `"[REDACTED]"`)
	return strings.ReplaceAll(message, secret, "[REDACTED]")
}

func mapBegetError(section, method string, input any, err error) error {
	if section != "mail" || method != "changeMailboxPassword" && method != "createMailbox" {
		return err
	}
	weakPassword := false
	var apiError *beget.APIError
	if errors.As(err, &apiError) && apiError.IsCode(weakMailboxPasswordErrorCode) {
		weakPassword = true
	}
	var methodError *beget.MethodError
	if errors.As(err, &methodError) {
		for _, providerError := range methodError.Errors {
			if fmt.Sprint(providerError.Code) == fmt.Sprint(weakMailboxPasswordErrorCode) {
				weakPassword = true
				break
			}
		}
	}
	if !weakPassword {
		return err
	}

	message := passwordpolicy.MailboxRequirement()
	if password, ok := mailboxPasswordFromInput(input); ok {
		if validationMessage := passwordpolicy.ValidationMessage(password); validationMessage != "" {
			message = validationMessage
		}
	}
	return &beget.MethodError{Section: section, Method: method, Errors: []beget.ProviderError{{
		Code: weakMailboxPasswordErrorCode, Message: "mailbox_password was rejected by Beget as too weak: " + message,
	}}}
}

func mailboxPasswordFromInput(input any) (string, bool) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", false
	}
	var parameters struct {
		MailboxPassword string `json:"mailbox_password"`
	}
	if err := json.Unmarshal(encoded, &parameters); err != nil || parameters.MailboxPassword == "" {
		return "", false
	}
	return parameters.MailboxPassword, true
}

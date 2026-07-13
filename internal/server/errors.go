// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kordax/beget-api-mcp-server/internal/beget"
	"github.com/kordax/beget-api-mcp-server/internal/passwordpolicy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const weakMailboxPasswordErrorCode = 1208

var sensitiveToolFields = []string{"password", "mailbox_password"}

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
		if password := arguments["mailbox_password"]; password != "" {
			if message := passwordpolicy.ValidationMessage(password); message != "" && toolErrorReferencesField(toolResult, "mailbox_password") {
				toolResult.Content = []mcp.Content{&mcp.TextContent{Text: message}}
				toolResult.StructuredContent = nil
				return result, err
			}
		}
		for _, content := range toolResult.Content {
			textContent, ok := content.(*mcp.TextContent)
			if !ok {
				continue
			}
			for _, secret := range arguments {
				textContent.Text = redactSecret(textContent.Text, secret)
			}
		}
		toolResult.StructuredContent = nil
		return result, err
	}
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

func redactSecret(message, secret string) string {
	quoted, _ := json.Marshal(secret)
	message = strings.ReplaceAll(message, string(quoted), `"[REDACTED]"`)
	return strings.ReplaceAll(message, secret, "[REDACTED]")
}

func mapBegetError(section, method string, input any, err error) error {
	if section != "mail" || method != "changeMailboxPassword" && method != "createMailbox" {
		return err
	}
	var apiError *beget.APIError
	if !errors.As(err, &apiError) || !apiError.IsCode(weakMailboxPasswordErrorCode) {
		return err
	}

	message := passwordpolicy.MailboxRequirement()
	if password, ok := mailboxPasswordFromInput(input); ok {
		if validationMessage := passwordpolicy.ValidationMessage(password); validationMessage != "" {
			message = validationMessage
		}
	}
	return fmt.Errorf("mailbox_password was rejected by Beget as too weak: %s", message)
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

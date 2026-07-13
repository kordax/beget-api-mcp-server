// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"

	"github.com/kordax/beget-api-mcp-server/internal/passwordpolicy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (*service) validateMailboxPassword(_ context.Context, _ *mcp.CallToolRequest, input MailboxPasswordValidationInput) (*mcp.CallToolResult, ToolOutput[MailboxPasswordValidationResult], error) {
	policyViolations := passwordpolicy.ValidateMailbox(input.MailboxPassword)
	violations := make([]MailboxPasswordViolation, len(policyViolations))
	for index, violation := range policyViolations {
		violations[index] = MailboxPasswordViolation{Code: string(violation.Code), Message: violation.Message}
	}

	return nil, successfulOutput(MailboxPasswordValidationResult{
		Valid:      len(violations) == 0,
		Violations: violations,
	}), nil
}

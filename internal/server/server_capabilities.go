// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"

	"github.com/kordax/beget-api-mcp-server/internal/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ServerCapabilitiesResult struct {
	ServerVersion         string                      `json:"server_version" jsonschema:"running server version"`
	SupportedBegetMethods []SupportedBegetMethod      `json:"supported_beget_methods" jsonschema:"typed Beget endpoints exposed by this server"`
	HasMutations          bool                        `json:"has_mutations" jsonschema:"whether the server exposes state-changing Beget operations"`
	DryRun                DryRunCapability            `json:"dry_run" jsonschema:"dry-run behavior shared by every mutation"`
	ConfirmationTokens    ConfirmationTokenCapability `json:"confirmation_tokens" jsonschema:"confirmation mechanism supported by mutations"`
	Idempotency           IdempotencyCapability       `json:"idempotency" jsonschema:"idempotency metadata and retry behavior"`
	SecretReferences      SecretReferenceCapability   `json:"secret_references" jsonschema:"support for indirect managed-secret inputs"`
	RotationWorkflow      RotationWorkflowCapability  `json:"rotation_workflow" jsonschema:"support for staged secret rotation"`
}

type SupportedBegetMethod struct {
	Tool                 string `json:"tool"`
	Section              string `json:"section"`
	Method               string `json:"method"`
	Mutating             bool   `json:"mutating"`
	Destructive          bool   `json:"destructive"`
	Idempotent           bool   `json:"idempotent"`
	DryRunSupported      bool   `json:"dry_run_supported"`
	ConfirmationRequired bool   `json:"confirmation_required"`
}

type DryRunCapability struct {
	Supported                    bool     `json:"supported"`
	Scope                        string   `json:"scope"`
	Checks                       []string `json:"checks"`
	ProviderRequests             bool     `json:"provider_requests"`
	ProviderAcceptanceGuaranteed bool     `json:"provider_acceptance_guaranteed"`
}

type ConfirmationTokenCapability struct {
	Supported           bool   `json:"supported"`
	BooleanConfirmation bool   `json:"boolean_confirmation"`
	Field               string `json:"field"`
}

type IdempotencyCapability struct {
	Annotations            bool `json:"annotations"`
	IdempotencyKeys        bool `json:"idempotency_keys"`
	AutomaticMutationRetry bool `json:"automatic_mutation_retry"`
}

type SecretReferenceCapability struct {
	Supported                  bool `json:"supported"`
	InlineManagedPasswords     bool `json:"inline_managed_passwords"`
	APICredentialsAsToolInputs bool `json:"api_credentials_as_tool_inputs"`
}

type RotationWorkflowCapability struct {
	Supported bool     `json:"supported"`
	Atomic    bool     `json:"atomic"`
	Stages    []string `json:"stages"`
}

func (s *service) serverCapabilities(context.Context, *mcp.CallToolRequest, NoArgs) (*mcp.CallToolResult, ToolOutput[ServerCapabilitiesResult], error) {
	methods := make([]SupportedBegetMethod, 0, len(s.operations))
	hasMutations := false
	for _, operation := range s.operations {
		if operation.section == "local" {
			continue
		}
		hasMutations = hasMutations || operation.mutating
		methods = append(methods, SupportedBegetMethod{
			Tool: operation.name, Section: operation.section, Method: operation.method,
			Mutating: operation.mutating, Destructive: operation.destructive, Idempotent: operation.idempotent,
			DryRunSupported: operation.mutating, ConfirmationRequired: operation.mutating,
		})
	}
	return nil, successfulOutput(ServerCapabilitiesResult{
		ServerVersion: buildinfo.Version, SupportedBegetMethods: methods, HasMutations: hasMutations,
		DryRun: DryRunCapability{
			Supported: true, Scope: "local",
			Checks:           []string{"input_schema", "known_local_constraints", "credentials_configured", "confirmation_prerequisite"},
			ProviderRequests: false, ProviderAcceptanceGuaranteed: false,
		},
		ConfirmationTokens: ConfirmationTokenCapability{
			Supported: false, BooleanConfirmation: true, Field: "confirm",
		},
		Idempotency: IdempotencyCapability{
			Annotations: true, IdempotencyKeys: false, AutomaticMutationRetry: false,
		},
		SecretReferences: SecretReferenceCapability{
			Supported: false, InlineManagedPasswords: true, APICredentialsAsToolInputs: false,
		},
		RotationWorkflow: RotationWorkflowCapability{
			Supported: false, Atomic: false, Stages: []string{},
		},
	}), nil
}

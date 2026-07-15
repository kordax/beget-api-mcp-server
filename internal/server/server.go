// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/kordax/beget-api-mcp-server/internal/beget"
	"github.com/kordax/beget-api-mcp-server/internal/buildinfo"
	"github.com/kordax/beget-api-mcp-server/internal/updater"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

type NoArgs struct{}

const (
	updateCheckIdleInterval = 10 * time.Minute
	updateCheckTimeout      = 5 * time.Second
	serverInstructions      = `Use beget_auth_status before the first Beget API operation when authorization state is unknown. Read current state and obtain resource identifiers from the matching list tool before a mutation. Never guess identifiers, domains, enum values, settings, or secrets. Use dry_run=true with confirm=false only when local validation is useful; it sends no Beget request and never guarantees provider acceptance. For beget_change_dns_records, submit exactly one logical Beget group per call: A/MX/TXT, NS, CNAME, or DNS/DNS_IP. Preserve every existing record within the selected group, but omit all other groups, empty arrays, and empty-value records returned as provider placeholders. Describe the exact target and change, obtain explicit user approval, and only then call one mutating tool with confirm=true. Read success, the typed result, every machine-readable error type, and its next_step; for mutations always inspect result.changed. Verify a successful mutation with the matching read-only tool when available. Never retry a mutation automatically after a timeout, disconnect, or unknown_outcome; read the current state first. Each ordinary Beget tool call sends at most one provider request and performs no hidden preflight read. Treat authorization as a credentials setup request, not as an MCP transport failure. Never request or pass a Beget API key as a tool argument.`
)

type DNSInput struct {
	FQDN string `json:"fqdn" jsonschema:"fully qualified domain name managed by Beget"`
}

type DNSRecord struct {
	Priority int    `json:"priority" jsonschema:"record priority"`
	Value    string `json:"value" jsonschema:"non-empty record value; omit provider placeholder records whose value is empty"`
}

type DNSRecords struct {
	A     []DNSRecord `json:"A,omitempty" jsonschema:"A records in the combined A/MX/TXT group"`
	MX    []DNSRecord `json:"MX,omitempty" jsonschema:"MX records in the combined A/MX/TXT group"`
	TXT   []DNSRecord `json:"TXT,omitempty" jsonschema:"TXT records in the combined A/MX/TXT group"`
	NS    []DNSRecord `json:"NS,omitempty" jsonschema:"standalone NS group; do not combine with another group"`
	CNAME []DNSRecord `json:"CNAME,omitempty" jsonschema:"standalone CNAME group containing exactly one record"`
	DNS   []DNSRecord `json:"DNS,omitempty" jsonschema:"DNS records in the combined DNS/DNS_IP group; at least one is required when DNS_IP is present"`
	DNSIP []DNSRecord `json:"DNS_IP,omitempty" jsonschema:"optional non-empty DNS_IP records used only with at least one DNS record"`
}

type ChangeDNSInput struct {
	Confirmation
	FQDN    string     `json:"fqdn" jsonschema:"fully qualified domain name managed by Beget"`
	Records DNSRecords `json:"records" jsonschema:"exactly one replacement group: A/MX/TXT, NS, CNAME, or DNS/DNS_IP; preserve every existing record within that selected group, omit all other and empty groups, and omit empty-value provider placeholders"`
}

type FreezeSiteInput struct {
	Confirmation
	ID            int64    `json:"id" jsonschema:"site identifier returned by beget_list_sites"`
	ExcludedPaths []string `json:"excluded_paths,omitempty" jsonschema:"relative paths that remain writable"`
}

type UnfreezeSiteInput struct {
	Confirmation
	ID int64 `json:"id" jsonschema:"site identifier returned by beget_list_sites"`
}

type AuthenticationOutput struct {
	Configured bool   `json:"configured" jsonschema:"whether Beget credentials are ready for API calls"`
	Source     string `json:"source" jsonschema:"credential source without secret values"`
	Message    string `json:"message" jsonschema:"safe setup guidance"`
}

type service struct {
	client     beget.Caller
	operations []operationSpec
}

type releaseChecker interface {
	Check(context.Context) (updater.VersionStatus, error)
}

type updateMonitor struct {
	mu          sync.Mutex
	checker     releaseChecker
	now         func() time.Time
	lastCommand time.Time
	checking    bool
	notice      string
}

var Module = fx.Module("mcp", fx.Provide(New))

func New(client beget.Caller, checker *updater.Updater) *mcp.Server {
	return newServer(client, checker, time.Now)
}

func newServer(client beget.Caller, checker releaseChecker, now func() time.Time) *mcp.Server {
	service := &service{client: client, operations: operationCatalog}
	server := mcp.NewServer(&mcp.Implementation{Name: "beget-api-mcp-server", Version: buildinfo.Version}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})
	monitor := &updateMonitor{checker: checker, now: now, lastCommand: now()}
	server.AddReceivingMiddleware(redactSensitiveToolErrors, monitor.middleware)
	addCapabilitiesResource(server)
	service.addOperations(server)
	return server
}

func (monitor *updateMonitor) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		monitor.scheduleCheck(method)
		result, err := next(ctx, method, request)
		toolResult, ok := result.(*mcp.CallToolResult)
		if method != "tools/call" || !ok {
			return result, err
		}
		if notice := monitor.takeNotice(); notice != "" {
			toolResult.Content = append(toolResult.Content, &mcp.TextContent{Text: notice})
		}
		return result, err
	}
}

func (monitor *updateMonitor) scheduleCheck(method string) {
	if method != "tools/call" || monitor.checker == nil {
		return
	}
	now := monitor.now()
	monitor.mu.Lock()
	idle := now.Sub(monitor.lastCommand) >= updateCheckIdleInterval
	monitor.lastCommand = now
	if !idle || monitor.checking {
		monitor.mu.Unlock()
		return
	}
	monitor.checking = true
	monitor.mu.Unlock()

	go monitor.check()
}

func (monitor *updateMonitor) check() {
	checkContext, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	status, err := monitor.checker.Check(checkContext)
	notice := ""
	if err != nil {
		log.Printf("check for beget-api-mcp-server update: %v", err)
	} else if status.UpdateAvailable {
		notice = fmt.Sprintf("A newer beget-api-mcp-server release is available: %s (current: %s). Run `beget-api-mcp-server upgrade` to install it; this MCP server did not update itself.", status.Latest, status.Current)
	}

	monitor.mu.Lock()
	monitor.checking = false
	monitor.notice = notice
	monitor.mu.Unlock()
}

func (monitor *updateMonitor) takeNotice() string {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	notice := monitor.notice
	monitor.notice = ""
	return notice
}

func (s *service) authenticationStatus(context.Context, *mcp.CallToolRequest, NoArgs) (*mcp.CallToolResult, ToolOutput[AuthenticationOutput], error) {
	status := s.client.AuthenticationStatus()
	return nil, successfulOutput(AuthenticationOutput{Configured: status.Configured, Source: status.Source, Message: status.Message}), nil
}

func (s *service) getDNSRecords(ctx context.Context, _ *mcp.CallToolRequest, input DNSInput) (*mcp.CallToolResult, ToolOutput[DNSResult], error) {
	if err := validateDomainName("fqdn", input.FQDN); err != nil {
		return validationFailure[DNSResult](err)
	}
	return callRead[DNSResult](ctx, s, "dns", "getData", input)
}

func (s *service) changeDNSRecords(ctx context.Context, _ *mcp.CallToolRequest, input ChangeDNSInput) (*mcp.CallToolResult, ToolOutput[MutationResult[APIBool]], error) {
	if !input.DryRun && !input.Confirm {
		return mutationConfirmationFailure[APIBool]("beget_change_dns_records")
	}
	if err := validateDomainName("fqdn", input.FQDN); err != nil {
		return mutationValidationFailure[APIBool](err)
	}
	if err := validateDNSRecords(input.Records); err != nil {
		return dnsRecordsValidationFailure[APIBool](err)
	}
	if input.DryRun {
		return localMutationDryRun[APIBool](s, input.Confirm)
	}
	return callMutation[APIBool](ctx, s, "dns", "changeRecords", struct {
		FQDN    string     `json:"fqdn"`
		Records DNSRecords `json:"records"`
	}{FQDN: input.FQDN, Records: input.Records})
}

func (s *service) freezeSite(ctx context.Context, _ *mcp.CallToolRequest, input FreezeSiteInput) (*mcp.CallToolResult, ToolOutput[MutationResult[APIBool]], error) {
	if !input.DryRun && !input.Confirm {
		return mutationConfirmationFailure[APIBool]("beget_freeze_site")
	}
	if input.ID <= 0 {
		return mutationValidationFailure[APIBool](errors.New("id must be positive"))
	}
	for _, excludedPath := range input.ExcludedPaths {
		if !isSafeRelativeHostingPath(excludedPath) {
			return mutationValidationFailure[APIBool](fmt.Errorf("excluded_paths contains %q, which is not a safe relative path", excludedPath))
		}
	}
	if input.DryRun {
		return localMutationDryRun[APIBool](s, input.Confirm)
	}
	return callMutation[APIBool](ctx, s, "site", "freeze", struct {
		ID            int64    `json:"id"`
		ExcludedPaths []string `json:"excludedPaths,omitempty"`
	}{ID: input.ID, ExcludedPaths: input.ExcludedPaths})
}

func (s *service) unfreezeSite(ctx context.Context, _ *mcp.CallToolRequest, input UnfreezeSiteInput) (*mcp.CallToolResult, ToolOutput[MutationResult[APIBool]], error) {
	if !input.DryRun && !input.Confirm {
		return mutationConfirmationFailure[APIBool]("beget_unfreeze_site")
	}
	if input.ID <= 0 {
		return mutationValidationFailure[APIBool](errors.New("id must be positive"))
	}
	if input.DryRun {
		return localMutationDryRun[APIBool](s, input.Confirm)
	}
	return callMutation[APIBool](ctx, s, "site", "unfreeze", struct {
		ID int64 `json:"id"`
	}{ID: input.ID})
}

func callRead[Result any](ctx context.Context, service *service, section, method string, input any) (*mcp.CallToolResult, ToolOutput[Result], error) {
	rawAnswer, err := service.client.Call(ctx, section, method, input)
	if err != nil {
		toolErrors := toolErrorsForBeget(mapBegetError(section, method, input, err), false)
		return failedOutput[Result](redactToolErrors(input, toolErrors)...)
	}
	result, err := decodeTypedResult[Result](rawAnswer)
	if err != nil {
		return failedOutput[Result](ToolError{
			Type: ErrorTransportFailure, Code: "invalid_provider_response",
			Message:  "Beget returned a result that does not match the documented operation contract.",
			NextStep: "Do not guess fields. Check the MCP server logs and update the result contract before retrying.",
		})
	}
	return nil, successfulOutput(result), nil
}

func callMutation[Details any](ctx context.Context, service *service, section, method string, input any) (*mcp.CallToolResult, ToolOutput[MutationResult[Details]], error) {
	rawAnswer, err := service.client.Call(ctx, section, method, input)
	if err != nil {
		toolErrors := toolErrorsForBeget(mapBegetError(section, method, input, err), true)
		return failedMutationOutput[Details](redactToolErrors(input, toolErrors)...)
	}
	details, err := decodeTypedResult[Details](rawAnswer)
	if err != nil {
		return failedMutationOutput[Details](ToolError{
			Type: ErrorUnknownOutcome, Code: "mutation_outcome_unknown",
			Message:  "Beget accepted the request but returned an unexpected mutation result.",
			NextStep: "Do not retry the mutation. Read the current resource state first and decide from that result.",
		})
	}
	if accepted, ok := any(details).(APIBool); ok && !bool(accepted) {
		return failedMutationOutput[Details](ToolError{
			Type: ErrorProviderRejection, Code: "provider_returned_false",
			Message:  "Beget reported that the mutation was not applied.",
			NextStep: "Read the current resource state and correct the request before retrying.",
		})
	}
	result := MutationResult[Details]{Changed: true, Details: &details}
	return nil, successfulOutput(result), nil
}

func validateDNSRecords(records DNSRecords) error {
	standard := len(records.A)+len(records.MX)+len(records.TXT) > 0
	ns := len(records.NS) > 0
	cname := len(records.CNAME) > 0
	dns := len(records.DNS)+len(records.DNSIP) > 0
	groups := 0
	for _, present := range []bool{standard, ns, cname, dns} {
		if present {
			groups++
		}
	}
	if groups != 1 {
		return errors.New("records must contain exactly one Beget record group: A/MX/TXT, NS, CNAME, or DNS/DNS_IP")
	}
	if dns && len(records.DNS) == 0 {
		return errors.New("DNS_IP records require at least one DNS record")
	}
	if len(records.CNAME) > 1 {
		return errors.New("CNAME group accepts exactly one record")
	}
	for name, values := range map[string][]DNSRecord{
		"A": records.A, "MX": records.MX, "TXT": records.TXT, "NS": records.NS,
		"CNAME": records.CNAME, "DNS": records.DNS, "DNS_IP": records.DNSIP,
	} {
		limit := 10
		if name == "DNS" || name == "DNS_IP" {
			limit = 4
		}
		if len(values) > limit {
			return fmt.Errorf("%s accepts at most %d records", name, limit)
		}
		for _, record := range values {
			if strings.TrimSpace(record.Value) == "" {
				return fmt.Errorf("%s record value is required", name)
			}
			if record.Priority < 0 {
				return fmt.Errorf("%s record priority must not be negative", name)
			}
			if err := validateDNSRecordValue(name, record.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func readTool(name, description string) *mcp.Tool {
	openWorld := true
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: name, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &openWorld,
	}}
}

func localReadTool(name, description string) *mcp.Tool {
	openWorld := false
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: name, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &openWorld,
	}}
}

func mutatingTool(name, description string, destructive, idempotent bool) *mcp.Tool {
	openWorld := true
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: name, DestructiveHint: &destructive, IdempotentHint: idempotent, OpenWorldHint: &openWorld,
	}}
}

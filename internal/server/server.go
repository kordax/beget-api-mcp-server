// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
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
	serverInstructions      = `Use beget_auth_status before the first Beget API operation when authorization state is unknown. Read current state and obtain resource identifiers from the matching list tool before a mutation. Never guess identifiers, domains, enum values, settings, or secrets. Describe the exact target and change, obtain explicit user approval, and only then call one mutating tool with confirm=true. Verify a successful mutation with the matching read-only tool when available. Never retry a mutation automatically after a timeout or disconnect because its outcome may be unknown; read the current state first. Treat an authorization error as a credentials setup request, not as an MCP transport failure. Never request or pass a Beget API key as a tool argument.`
)

type DNSInput struct {
	FQDN string `json:"fqdn" jsonschema:"fully qualified domain name managed by Beget"`
}

type DNSRecord struct {
	Priority int    `json:"priority" jsonschema:"record priority"`
	Value    string `json:"value" jsonschema:"record value"`
}

type DNSRecords struct {
	A     []DNSRecord `json:"A,omitempty"`
	MX    []DNSRecord `json:"MX,omitempty"`
	TXT   []DNSRecord `json:"TXT,omitempty"`
	NS    []DNSRecord `json:"NS,omitempty"`
	CNAME []DNSRecord `json:"CNAME,omitempty"`
	DNS   []DNSRecord `json:"DNS,omitempty"`
	DNSIP []DNSRecord `json:"DNS_IP,omitempty"`
}

type ChangeDNSInput struct {
	FQDN    string     `json:"fqdn" jsonschema:"fully qualified domain name managed by Beget"`
	Records DNSRecords `json:"records" jsonschema:"complete replacement record group accepted by Beget"`
	Confirm bool       `json:"confirm" jsonschema:"must be true to authorize changing live DNS records"`
}

type FreezeSiteInput struct {
	ID            int64    `json:"id" jsonschema:"site identifier returned by beget_list_sites"`
	ExcludedPaths []string `json:"excluded_paths,omitempty" jsonschema:"relative paths that remain writable"`
	Confirm       bool     `json:"confirm" jsonschema:"must be true to authorize changing the site freeze state"`
}

type UnfreezeSiteInput struct {
	ID      int64 `json:"id" jsonschema:"site identifier returned by beget_list_sites"`
	Confirm bool  `json:"confirm" jsonschema:"must be true to authorize changing the site freeze state"`
}

type APIOutput struct {
	Answer any `json:"answer" jsonschema:"Beget API answer payload"`
}

type AuthenticationOutput struct {
	Configured bool   `json:"configured" jsonschema:"whether Beget credentials are ready for API calls"`
	Source     string `json:"source" jsonschema:"credential source without secret values"`
	Message    string `json:"message" jsonschema:"safe setup guidance"`
}

type service struct {
	client beget.Caller
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
	service := &service{client: client}
	server := mcp.NewServer(&mcp.Implementation{Name: "beget-api-mcp-server", Version: buildinfo.Version}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})
	monitor := &updateMonitor{checker: checker, now: now, lastCommand: now()}
	server.AddReceivingMiddleware(redactSensitiveToolErrors, monitor.middleware)
	addToolWithSchema(server, localReadTool("beget_auth_status", "Check whether Beget credentials are configured. Call this first when authorization state is unknown; an unconfigured result is a setup request, not an MCP transport failure."), service.authenticationStatus)

	service.addOperations(server)

	addToolWithSchema(server, readTool("beget_get_dns_records", "Read active DNS records for fqdn. Use the returned record group as the current state before beget_change_dns_records."), service.getDNSRecords)
	addToolWithSchema(server, mutatingTool("beget_change_dns_records", "Replace the complete live DNS record group for fqdn. Read beget_get_dns_records first and verify with it afterward. Requires explicit confirm=true after user approval.", true, true), service.changeDNSRecords)
	addToolWithSchema(server, mutatingTool("beget_freeze_site", "Make files for the site id from beget_list_sites read-only, except optional safe relative paths. Verify with beget_is_site_frozen. Requires explicit confirm=true after user approval.", true, true), service.freezeSite)
	addToolWithSchema(server, mutatingTool("beget_unfreeze_site", "Restore writes for the site id from beget_list_sites. Verify with beget_is_site_frozen. Requires explicit confirm=true after user approval.", true, true), service.unfreezeSite)
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

func (s *service) authenticationStatus(context.Context, *mcp.CallToolRequest, NoArgs) (*mcp.CallToolResult, AuthenticationOutput, error) {
	status := s.client.AuthenticationStatus()
	return nil, AuthenticationOutput{Configured: status.Configured, Source: status.Source, Message: status.Message}, nil
}

func (s *service) getDNSRecords(ctx context.Context, _ *mcp.CallToolRequest, input DNSInput) (*mcp.CallToolResult, APIOutput, error) {
	if err := validateDomainName("fqdn", input.FQDN); err != nil {
		return nil, APIOutput{}, err
	}
	return s.call(ctx, "dns", "getData", input)
}

func (s *service) changeDNSRecords(ctx context.Context, _ *mcp.CallToolRequest, input ChangeDNSInput) (*mcp.CallToolResult, APIOutput, error) {
	if !input.Confirm {
		return nil, APIOutput{}, errors.New("confirm must be true before changing live DNS records")
	}
	if err := validateDomainName("fqdn", input.FQDN); err != nil {
		return nil, APIOutput{}, err
	}
	if err := validateDNSRecords(input.Records); err != nil {
		return nil, APIOutput{}, err
	}
	return s.call(ctx, "dns", "changeRecords", struct {
		FQDN    string     `json:"fqdn"`
		Records DNSRecords `json:"records"`
	}{FQDN: input.FQDN, Records: input.Records})
}

func (s *service) freezeSite(ctx context.Context, _ *mcp.CallToolRequest, input FreezeSiteInput) (*mcp.CallToolResult, APIOutput, error) {
	if !input.Confirm {
		return nil, APIOutput{}, errors.New("confirm must be true before freezing a site")
	}
	if input.ID <= 0 {
		return nil, APIOutput{}, errors.New("id must be positive")
	}
	for _, excludedPath := range input.ExcludedPaths {
		if !isSafeRelativeHostingPath(excludedPath) {
			return nil, APIOutput{}, fmt.Errorf("excluded path %q must be a safe relative path", excludedPath)
		}
	}
	return s.call(ctx, "site", "freeze", struct {
		ID            int64    `json:"id"`
		ExcludedPaths []string `json:"excludedPaths,omitempty"`
	}{ID: input.ID, ExcludedPaths: input.ExcludedPaths})
}

func (s *service) unfreezeSite(ctx context.Context, _ *mcp.CallToolRequest, input UnfreezeSiteInput) (*mcp.CallToolResult, APIOutput, error) {
	if !input.Confirm {
		return nil, APIOutput{}, errors.New("confirm must be true before unfreezing a site")
	}
	if input.ID <= 0 {
		return nil, APIOutput{}, errors.New("id must be positive")
	}
	return s.call(ctx, "site", "unfreeze", struct {
		ID int64 `json:"id"`
	}{ID: input.ID})
}

func (s *service) call(ctx context.Context, section, method string, input any) (*mcp.CallToolResult, APIOutput, error) {
	rawAnswer, err := s.client.Call(ctx, section, method, input)
	if err != nil {
		return nil, APIOutput{}, mapBegetError(section, method, input, err)
	}
	var answer any
	if err := json.Unmarshal(rawAnswer, &answer); err != nil {
		return nil, APIOutput{}, fmt.Errorf("decode Beget %s/%s answer: %w", section, method, err)
	}
	return nil, APIOutput{Answer: answer}, nil
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
		Title: name, ReadOnlyHint: true, OpenWorldHint: &openWorld,
	}}
}

func localReadTool(name, description string) *mcp.Tool {
	openWorld := false
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: name, ReadOnlyHint: true, OpenWorldHint: &openWorld,
	}}
}

func mutatingTool(name, description string, destructive, idempotent bool) *mcp.Tool {
	openWorld := true
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: name, DestructiveHint: &destructive, IdempotentHint: idempotent, OpenWorldHint: &openWorld,
	}}
}

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

// OperationInput is the typed input shared by published Beget operations. Every
// field maps to a documented Beget API parameter; tools select a fixed endpoint
// and never accept an arbitrary section or method.
type OperationInput struct {
	Confirm           bool     `json:"confirm,omitempty" jsonschema:"must be true to authorize a change to the hosting account"`
	ID                int64    `json:"id,omitempty" jsonschema:"resource identifier returned by Beget"`
	DomainID          int64    `json:"domain_id,omitempty" jsonschema:"domain identifier returned by Beget"`
	Domain            string   `json:"domain,omitempty" jsonschema:"domain name"`
	Subdomain         string   `json:"subdomain,omitempty" jsonschema:"subdomain name"`
	FQDN              string   `json:"fqdn,omitempty" jsonschema:"fully qualified domain name"`
	SiteID            int64    `json:"site_id,omitempty" jsonschema:"site identifier returned by Beget"`
	Path              string   `json:"path,omitempty" jsonschema:"hosting-relative path"`
	Login             string   `json:"login,omitempty" jsonschema:"FTP or MySQL access login"`
	FTPLogin          string   `json:"ftplogin,omitempty" jsonschema:"FTP account login"`
	Password          string   `json:"password,omitempty" jsonschema:"new password for the selected FTP or MySQL access"`
	Mailbox           string   `json:"mailbox,omitempty" jsonschema:"mailbox local part"`
	MailboxPassword   string   `json:"mailbox_password,omitempty" jsonschema:"new mailbox password"`
	DomainMailbox     string   `json:"domain_mailbox,omitempty" jsonschema:"mailbox address for domain mail"`
	ForwardMailbox    string   `json:"forward_mailbox,omitempty" jsonschema:"mailbox address used for forwarding"`
	SpamFilterStatus  int      `json:"spam_filter_status,omitempty" jsonschema:"spam filter state: 0 or 1"`
	ForwardMailStatus string   `json:"forward_mail_status,omitempty" jsonschema:"forwarding mode"`
	Database          string   `json:"db_name,omitempty" jsonschema:"MySQL database name"`
	Email             string   `json:"email,omitempty" jsonschema:"email address"`
	Command           string   `json:"command,omitempty" jsonschema:"Cron command"`
	Minute            string   `json:"minute,omitempty" jsonschema:"Cron minute expression"`
	Hour              string   `json:"hour,omitempty" jsonschema:"Cron hour expression"`
	Day               string   `json:"day,omitempty" jsonschema:"Cron day-of-month expression"`
	Month             string   `json:"month,omitempty" jsonschema:"Cron month expression"`
	Weekday           string   `json:"weekday,omitempty" jsonschema:"Cron weekday expression"`
	Hidden            bool     `json:"hidden,omitempty" jsonschema:"whether a Cron task is hidden"`
	Status            int      `json:"status,omitempty" jsonschema:"requested Beget status value"`
	PHPVersion        string   `json:"php_version,omitempty" jsonschema:"PHP version"`
	Directives        []string `json:"directives,omitempty" jsonschema:"PHP directives"`
	BackupID          int64    `json:"backup_id,omitempty" jsonschema:"backup identifier"`
	Filename          string   `json:"filename,omitempty" jsonschema:"backup filename"`
}

type operationSpec struct {
	name, description, section, method string
	mutating                           bool
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
}

var Module = fx.Module("mcp", fx.Provide(New))

func New(client beget.Caller, checker *updater.Updater) *mcp.Server {
	return newServer(client, checker, time.Now)
}

func newServer(client beget.Caller, checker releaseChecker, now func() time.Time) *mcp.Server {
	service := &service{client: client}
	server := mcp.NewServer(&mcp.Implementation{Name: "beget-api-mcp-server", Version: buildinfo.Version}, nil)
	monitor := &updateMonitor{checker: checker, now: now, lastCommand: now()}
	server.AddReceivingMiddleware(monitor.middleware)
	mcp.AddTool(server, readTool("beget_auth_status", "Check Beget authorization before API calls and return safe setup guidance without revealing secrets."), service.authenticationStatus)

	service.addOperations(server)

	mcp.AddTool(server, readTool("beget_get_dns_records", "Read active DNS data for one domain."), service.getDNSRecords)
	mcp.AddTool(server, mutatingTool("beget_change_dns_records", "Replace a live DNS record group. Requires confirm=true."), service.changeDNSRecords)
	mcp.AddTool(server, mutatingTool("beget_freeze_site", "Make a site's files read-only, optionally excluding relative paths. Requires confirm=true."), service.freezeSite)
	mcp.AddTool(server, mutatingTool("beget_unfreeze_site", "Restore writes to a frozen site. Requires confirm=true."), service.unfreezeSite)
	return server
}

func (monitor *updateMonitor) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		notice := ""
		if monitor.shouldCheck(method) {
			notice = monitor.check(ctx)
		}
		result, err := next(ctx, method, request)
		if notice == "" || result == nil {
			return result, err
		}
		if toolResult, ok := result.(*mcp.CallToolResult); ok {
			toolResult.Content = append(toolResult.Content, &mcp.TextContent{Text: notice})
		}
		return result, err
	}
}

func (monitor *updateMonitor) shouldCheck(method string) bool {
	if method != "tools/call" || monitor.checker == nil {
		return false
	}
	now := monitor.now()
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	idle := now.Sub(monitor.lastCommand) >= updateCheckIdleInterval
	monitor.lastCommand = now
	return idle
}

func (monitor *updateMonitor) check(ctx context.Context) string {
	checkContext, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	status, err := monitor.checker.Check(checkContext)
	if err != nil {
		log.Printf("check for beget-api-mcp-server update: %v", err)
		return ""
	}
	if !status.UpdateAvailable {
		return ""
	}
	return fmt.Sprintf("A newer beget-api-mcp-server release is available: %s (current: %s). Run `beget-api-mcp-server upgrade` to install it; this MCP server did not update itself.", status.Latest, status.Current)
}

func (s *service) authenticationStatus(context.Context, *mcp.CallToolRequest, NoArgs) (*mcp.CallToolResult, AuthenticationOutput, error) {
	status := s.client.AuthenticationStatus()
	return nil, AuthenticationOutput{Configured: status.Configured, Source: status.Source, Message: status.Message}, nil
}

func (s *service) addOperations(server *mcp.Server) {
	for _, spec := range publishedOperations {
		tool := readTool(spec.name, spec.description)
		if spec.mutating {
			tool = mutatingTool(spec.name, spec.description+" Requires confirm=true.")
		}
		mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, input OperationInput) (*mcp.CallToolResult, APIOutput, error) {
			if spec.mutating && !input.Confirm {
				return nil, APIOutput{}, fmt.Errorf("confirm must be true before calling %s", spec.name)
			}
			// confirm is an MCP safety guard, not a Beget API parameter.
			input.Confirm = false
			return s.call(ctx, spec.section, spec.method, input)
		})
	}
}

var publishedOperations = []operationSpec{
	{"beget_account_info", "Read hosting account plan, server, and quota information.", "user", "getAccountInfo", false},
	{"beget_toggle_ssh", "Enable or disable SSH for the account or an FTP account.", "user", "toggleSsh", true},
	{"beget_list_file_backups", "List file backups.", "backup", "getFileBackupList", false},
	{"beget_list_mysql_backups", "List MySQL backups.", "backup", "getMysqlBackupList", false},
	{"beget_list_backup_files", "List files available in a backup.", "backup", "getFileList", false},
	{"beget_list_backup_databases", "List databases available in a backup.", "backup", "getMysqlList", false},
	{"beget_restore_file_backup", "Restore files from a backup.", "backup", "restoreFile", true},
	{"beget_restore_mysql_backup", "Restore a MySQL database from a backup.", "backup", "restoreMysql", true},
	{"beget_download_file_backup", "Get a file-backup download.", "backup", "downloadFile", false},
	{"beget_download_mysql_backup", "Get a MySQL-backup download.", "backup", "downloadMysql", false},
	{"beget_backup_log", "Read a backup operation log.", "backup", "getLog", false},
	{"beget_list_cron_jobs", "List Cron tasks configured on the hosting account.", "cron", "getList", false},
	{"beget_add_cron_job", "Create a Cron task.", "cron", "add", true},
	{"beget_edit_cron_job", "Edit a Cron task.", "cron", "edit", true},
	{"beget_delete_cron_job", "Delete a Cron task.", "cron", "delete", true},
	{"beget_change_cron_hidden_state", "Change whether a Cron task is hidden.", "cron", "changeHiddenState", true},
	{"beget_cron_email", "Read the Cron notification email address.", "cron", "getEmail", false},
	{"beget_set_cron_email", "Set the Cron notification email address.", "cron", "setEmail", true},
	{"beget_list_ftp_accounts", "List FTP accounts.", "ftp", "getList", false},
	{"beget_add_ftp_account", "Create an FTP account.", "ftp", "add", true},
	{"beget_change_ftp_password", "Change an FTP account password.", "ftp", "changePassword", true},
	{"beget_delete_ftp_account", "Delete an FTP account.", "ftp", "delete", true},
	{"beget_list_mysql_databases", "List MySQL databases and access accounts.", "mysql", "getList", false},
	{"beget_add_mysql_database", "Create a MySQL database.", "mysql", "addDb", true},
	{"beget_add_mysql_access", "Create MySQL database access.", "mysql", "addAccess", true},
	{"beget_delete_mysql_database", "Delete a MySQL database.", "mysql", "dropDb", true},
	{"beget_delete_mysql_access", "Delete MySQL database access.", "mysql", "dropAccess", true},
	{"beget_change_mysql_access_password", "Change a MySQL access password.", "mysql", "changeAccessPassword", true},
	{"beget_list_sites", "List sites configured on the hosting account.", "site", "getList", false},
	{"beget_add_site", "Create a site.", "site", "add", true},
	{"beget_delete_site", "Delete a site.", "site", "delete", true},
	{"beget_link_domain_to_site", "Link a domain to a site.", "site", "linkDomain", true},
	{"beget_unlink_domain_from_site", "Unlink a domain from a site.", "site", "unlinkDomain", true},
	{"beget_is_site_frozen", "Check whether a site is frozen.", "site", "isSiteFrozen", false},
	{"beget_list_domains", "List domains configured on the hosting account.", "domain", "getList", false},
	{"beget_list_domain_zones", "List available domain zones.", "domain", "getZoneList", false},
	{"beget_add_virtual_domain", "Add a virtual domain.", "domain", "addVirtual", true},
	{"beget_delete_domain", "Delete a domain.", "domain", "delete", true},
	{"beget_list_subdomains", "List subdomains.", "domain", "getSubdomainList", false},
	{"beget_add_virtual_subdomain", "Add a virtual subdomain.", "domain", "addSubdomainVirtual", true},
	{"beget_delete_subdomain", "Delete a subdomain.", "domain", "deleteSubdomain", true},
	{"beget_check_domain_registration", "Check whether a domain can be registered.", "domain", "checkDomainToRegister", false},
	{"beget_get_domain_php_version", "Read a domain PHP version.", "domain", "getPhpVersion", false},
	{"beget_change_domain_php_version", "Change a domain PHP version.", "domain", "changePhpVersion", true},
	{"beget_get_domain_directives", "Read domain PHP directives.", "domain", "getDirectives", false},
	{"beget_add_domain_directives", "Add domain PHP directives.", "domain", "addDirectives", true},
	{"beget_remove_domain_directives", "Remove domain PHP directives.", "domain", "removeDirectives", true},
	{"beget_list_mailboxes", "List mailboxes for a domain.", "mail", "getMailboxList", false},
	{"beget_change_mailbox_password", "Change a mailbox password.", "mail", "changeMailboxPassword", true},
	{"beget_create_mailbox", "Create a mailbox.", "mail", "createMailbox", true},
	{"beget_delete_mailbox", "Delete a mailbox.", "mail", "dropMailbox", true},
	{"beget_change_mailbox_settings", "Change mailbox spam-filter and forwarding settings.", "mail", "changeMailboxSettings", true},
	{"beget_add_mail_forwarding", "Add a mailbox forwarding destination.", "mail", "forwardListAddMailbox", true},
	{"beget_delete_mail_forwarding", "Delete a mailbox forwarding destination.", "mail", "forwardListDeleteMailbox", true},
	{"beget_list_mail_forwarding", "List mailbox forwarding destinations.", "mail", "forwardListShow", false},
	{"beget_set_domain_mail", "Set the domain mailbox.", "mail", "setDomainMail", true},
	{"beget_clear_domain_mail", "Clear the domain mailbox.", "mail", "clearDomainMail", true},
	{"beget_site_load", "Read average monthly load for hosted sites.", "stat", "getSiteListLoad", false},
	{"beget_database_load", "Read average monthly load for MySQL databases.", "stat", "getDbListLoad", false},
	{"beget_site_load_details", "Read load for one hosted site.", "stat", "getSiteLoad", false},
	{"beget_database_load_details", "Read load for one MySQL database.", "stat", "getDbLoad", false},
}

func (s *service) getDNSRecords(ctx context.Context, _ *mcp.CallToolRequest, input DNSInput) (*mcp.CallToolResult, APIOutput, error) {
	if strings.TrimSpace(input.FQDN) == "" {
		return nil, APIOutput{}, errors.New("fqdn is required")
	}
	return s.call(ctx, "dns", "getData", input)
}

func (s *service) changeDNSRecords(ctx context.Context, _ *mcp.CallToolRequest, input ChangeDNSInput) (*mcp.CallToolResult, APIOutput, error) {
	if !input.Confirm {
		return nil, APIOutput{}, errors.New("confirm must be true before changing live DNS records")
	}
	if strings.TrimSpace(input.FQDN) == "" {
		return nil, APIOutput{}, errors.New("fqdn is required")
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
	for _, path := range input.ExcludedPaths {
		if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return nil, APIOutput{}, fmt.Errorf("excluded path %q must be a safe relative path", path)
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
		return nil, APIOutput{}, err
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

func mutatingTool(name, description string) *mcp.Tool {
	destructive, openWorld := true, true
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: name, DestructiveHint: &destructive, IdempotentHint: true, OpenWorldHint: &openWorld,
	}}
}

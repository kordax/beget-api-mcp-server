// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kordax/beget-api-mcp-server/internal/passwordpolicy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type operationSpec struct {
	name, description, section, method string
	mutating                           bool
	register                           func(*mcp.Server, *service)
}

const (
	authStatusDescription = "Check whether Beget credentials are configured. Call this first when authorization state is unknown; an unconfigured result is a setup request, not an MCP transport failure."
	getDNSDescription     = "Read active DNS records for fqdn. Use the returned record group as the current state before beget_change_dns_records."
	changeDNSDescription  = "Replace the complete live DNS record group for fqdn. Read beget_get_dns_records first and verify with it afterward. Requires explicit confirm=true after user approval."
	freezeSiteDescription = "Make files for the site id from beget_list_sites read-only, except optional safe relative paths. Verify with beget_is_site_frozen. Requires explicit confirm=true after user approval."
	unfreezeDescription   = "Restore writes for the site id from beget_list_sites. Verify with beget_is_site_frozen. Requires explicit confirm=true after user approval."
)

type confirmedInput interface {
	confirmed() bool
}

type inputValidator interface {
	validate() error
}

type schemaRule struct {
	property string
	apply    func(*jsonschema.Schema)
}

func (s *service) addOperations(server *mcp.Server) {
	for _, spec := range operationCatalog {
		spec.register(server, s)
	}
}

func readOperation[Input, Result any](name, description, section, method string, rules ...schemaRule) operationSpec {
	return operationSpec{
		name: name, description: description, section: section, method: method,
		register: func(server *mcp.Server, service *service) {
			addToolWithSchema(server, readTool(name, description), func(ctx context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, ToolOutput[Result], error) {
				if err := validateOperationInput(input); err != nil {
					return validationFailure[Result](err)
				}
				return callRead[Result](ctx, service, section, method, input)
			}, rules...)
		},
	}
}

func mutationOperation[Input confirmedInput, Details any](name, description, section, method string, destructive, idempotent bool, rules ...schemaRule) operationSpec {
	return operationSpec{
		name: name, description: description, section: section, method: method,
		mutating: true,
		register: func(server *mcp.Server, service *service) {
			addToolWithSchema(server, mutatingTool(name, description, destructive, idempotent), func(ctx context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, ToolOutput[MutationResult[Details]], error) {
				if !input.confirmed() {
					return mutationConfirmationFailure[Details](name)
				}
				if err := validateOperationInput(input); err != nil {
					return mutationValidationFailure[Details](err)
				}
				parameters, err := withoutConfirmation(input)
				if err != nil {
					return mutationValidationFailure[Details](fmt.Errorf("prepare %s parameters: %w", name, err))
				}
				return callMutation[Details](ctx, service, section, method, parameters)
			}, rules...)
		},
	}
}

func customOperation(name, description, section, method string, mutating bool, register func(*mcp.Server, *service)) operationSpec {
	return operationSpec{
		name: name, description: description, section: section, method: method,
		mutating: mutating, register: register,
	}
}

func addToolWithSchema[Input, Output any](server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[Input, Output], rules ...schemaRule) {
	tool.InputSchema = mustInputSchema[Input](rules...)
	tool.OutputSchema = mustOutputSchema[Output]()
	mcp.AddTool(server, tool, handler)
}

func mustOutputSchema[Output any]() *jsonschema.Schema {
	schema, err := jsonschema.For[Output](nil)
	if err != nil {
		panic(fmt.Errorf("infer MCP output schema: %w", err))
	}
	if errors := schema.Properties["errors"]; errors != nil && errors.Items != nil {
		if errorType := errors.Items.Properties["type"]; errorType != nil {
			errorType.Enum = []any{
				ErrorValidation, ErrorAuthorization, ErrorProviderRejection,
				ErrorTransportFailure, ErrorConfirmationFailure, ErrorUnknownOutcome,
			}
		}
	}
	return schema
}

func mustInputSchema[Input any](rules ...schemaRule) *jsonschema.Schema {
	schema, err := jsonschema.For[Input](nil)
	if err != nil {
		panic(fmt.Errorf("infer MCP input schema: %w", err))
	}
	applyDefaultSchemaRules(schema)
	for _, rule := range rules {
		property := schema.Properties[rule.property]
		if property == nil {
			panic(fmt.Errorf("MCP input schema has no property %q", rule.property))
		}
		rule.apply(property)
	}
	var zero Input
	if _, ok := any(zero).(confirmedInput); ok {
		if schema.Properties["confirm"] == nil || !slices.Contains(schema.Required, "confirm") {
			panic("mutating MCP input must expose required confirm")
		}
	}
	return schema
}

func applyDefaultSchemaRules(schema *jsonschema.Schema) {
	for name, property := range schema.Properties {
		applyDefaultPropertyRules(name, property)
		applyDefaultSchemaRules(property)
		if property.Items != nil {
			applyDefaultSchemaRules(property.Items)
		}
	}
}

func applyDefaultPropertyRules(name string, property *jsonschema.Schema) {
	if property.Type == "string" && name != "email" {
		minimum := 1
		property.MinLength = &minimum
	}
	if property.Type == "array" || slices.Contains(property.Types, "array") {
		minimum := 1
		property.Type = "array"
		property.Types = nil
		property.MinItems = &minimum
	}
	if slices.Contains([]string{"id", "domain_id", "site_id", "zone_id", "row_number", "backup_id", "period", "priority"}, name) {
		minimum := float64(1)
		if name == "priority" {
			minimum = 0
		}
		property.Minimum = &minimum
	}
	switch name {
	case "status", "is_hidden", "spam_filter_status":
		property.Enum = []any{0, 1}
	case "spam_filter":
		minimum, maximum := float64(0), float64(100)
		property.Minimum, property.Maximum = &minimum, &maximum
	case "forward_mail_status":
		property.Enum = []any{"no_forward", "forward", "forward_and_delete"}
	case "suffix":
		maximum := 17
		property.MaxLength = &maximum
		property.Pattern = `^[A-Za-z0-9_-]+$`
	case "minutes", "hours", "days", "months", "weekdays":
		property.Pattern = `^[0-9*/,\-]+$`
	case "php_version":
		property.Pattern = `^[0-9]+(\.[0-9]+)*$`
	case "password":
		property.WriteOnly = true
	case "mailbox_password":
		minimum, maximum := passwordpolicy.MailboxMinimumLength, passwordpolicy.MailboxMaximumLength
		property.MinLength, property.MaxLength = &minimum, &maximum
		property.Pattern = passwordpolicy.MailboxAllowedCharacterPattern()
		property.WriteOnly = true
		property.Description += ". " + passwordpolicy.MailboxRequirement()
	case "forward_mailbox", "domain_mailbox":
		property.Format = "email"
	case "paths":
		property.Items.Type = "string"
		property.Items.Types = nil
		minimum := 1
		property.Items.MinLength = &minimum
		property.Items.Pattern = `^/`
	case "directives_list":
		if property.Items != nil {
			for _, item := range property.Items.Properties {
				minimum := 1
				item.MinLength = &minimum
			}
		}
	}
}

func minimumLength(property string, value int) schemaRule {
	return schemaRule{property: property, apply: func(schema *jsonschema.Schema) {
		schema.MinLength = &value
	}}
}

func maximumLength(property string, value int) schemaRule {
	return schemaRule{property: property, apply: func(schema *jsonschema.Schema) {
		schema.MaxLength = &value
	}}
}

func validateOperationInput(input any) error {
	validator, ok := input.(inputValidator)
	if !ok {
		return nil
	}
	return validator.validate()
}

func withoutConfirmation(input any) (json.RawMessage, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var parameters map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &parameters); err != nil {
		return nil, err
	}
	delete(parameters, "confirm")
	return json.Marshal(parameters)
}

var operationCatalog = []operationSpec{
	customOperation(
		"beget_auth_status",
		authStatusDescription,
		"local", "authStatus", false,
		func(server *mcp.Server, service *service) {
			addToolWithSchema(server, localReadTool("beget_auth_status", authStatusDescription), service.authenticationStatus)
		},
	),
	readOperation[NoArgs, AccountInfoResult](
		"beget_account_info",
		"Read the hosting plan, server details, quotas, and current account usage. Use this before changes that may exceed account limits.",
		"user", "getAccountInfo",
	),
	mutationOperation[ToggleSSHInput, []struct{}](
		"beget_toggle_ssh",
		"Enable or disable SSH for the main account or an optional FTP login from beget_list_ftp_accounts. Read the current account first and verify access separately. Requires explicit confirm=true after user approval.",
		"user", "toggleSsh", true, true,
	),
	readOperation[NoArgs, []Backup]("beget_list_file_backups", "List file backup identifiers available for file listing, restore, or download operations.", "backup", "getFileBackupList"),
	readOperation[NoArgs, []Backup]("beget_list_mysql_backups", "List MySQL backup identifiers available for database listing, restore, or download operations.", "backup", "getMysqlBackupList"),
	readOperation[BackupFileListInput, []BackupFile]("beget_list_backup_files", "List files and directories at path in a file backup. Get backup_id from beget_list_file_backups or omit it for the current copy.", "backup", "getFileList"),
	readOperation[BackupIDInput, []string]("beget_list_backup_databases", "List database names in a MySQL backup. Get backup_id from beget_list_mysql_backups or omit it for the current copy.", "backup", "getMysqlList"),
	mutationOperation[RestoreFilesInput, APIBool]("beget_restore_file_backup", "Queue restoration of the exact paths from a file backup. Inspect the backup first and check beget_backup_log afterward. Requires explicit confirm=true after user approval.", "backup", "restoreFile", true, false),
	mutationOperation[RestoreDatabasesInput, APIBool]("beget_restore_mysql_backup", "Queue restoration of the exact database names from a MySQL backup. Inspect the backup first and check beget_backup_log afterward. Requires explicit confirm=true after user approval.", "backup", "restoreMysql", true, false),
	mutationOperation[DownloadFilesInput, APIBool]("beget_download_file_backup", "Queue creation of an archive for the exact backup paths and place it in the account root. This writes account files; check beget_backup_log afterward. Requires explicit confirm=true after user approval.", "backup", "downloadFile", false, false),
	mutationOperation[DownloadDatabasesInput, APIBool]("beget_download_mysql_backup", "Queue creation of an archive for the exact backup databases and place it in the account root. This writes account files; check beget_backup_log afterward. Requires explicit confirm=true after user approval.", "backup", "downloadMysql", false, false),
	readOperation[NoArgs, []BackupLogEntry]("beget_backup_log", "Read queued and completed file or MySQL restore and download operations, including their identifiers and statuses.", "backup", "getLog"),
	readOperation[NoArgs, []CronJob]("beget_list_cron_jobs", "List Cron tasks with row_number, schedule, command, and hidden state. Use row_number before editing, hiding, or deleting a task.", "cron", "getList"),
	mutationOperation[CronAddInput, CronTaskResult]("beget_add_cron_job", "Create and activate one Cron task with the exact schedule and command. Verify with beget_list_cron_jobs. Requires explicit confirm=true after user approval.", "cron", "add", false, false),
	mutationOperation[CronEditInput, CronTaskResult]("beget_edit_cron_job", "Replace the schedule and command of the Cron task id from beget_list_cron_jobs. Verify with that list afterward. Requires explicit confirm=true after user approval.", "cron", "edit", true, true),
	mutationOperation[CronRowInput, APIBool]("beget_delete_cron_job", "Delete the Cron task row_number from beget_list_cron_jobs. Verify its absence afterward. Requires explicit confirm=true after user approval.", "cron", "delete", true, true),
	mutationOperation[CronHiddenInput, CronTaskResult]("beget_change_cron_hidden_state", "Set is_hidden for the Cron task row_number from beget_list_cron_jobs. Verify with that list afterward. Requires explicit confirm=true after user approval.", "cron", "changeHiddenState", false, true),
	readOperation[NoArgs, *string]("beget_cron_email", "Read the email address that receives Cron command output, or null when it is not configured.", "cron", "getEmail"),
	mutationOperation[CronEmailInput, APIBool]("beget_set_cron_email", "Set the Cron notification email, or pass an empty string to clear it. Verify with beget_cron_email. Requires explicit confirm=true after user approval.", "cron", "setEmail", false, true),
	customOperation(
		"beget_get_dns_records",
		getDNSDescription,
		"dns", "getData", false,
		func(server *mcp.Server, service *service) {
			addToolWithSchema(server, readTool("beget_get_dns_records", getDNSDescription), service.getDNSRecords)
		},
	),
	customOperation(
		"beget_change_dns_records",
		changeDNSDescription,
		"dns", "changeRecords", true,
		func(server *mcp.Server, service *service) {
			addToolWithSchema(server, mutatingTool("beget_change_dns_records", changeDNSDescription, true, true), service.changeDNSRecords)
		},
	),
	readOperation[NoArgs, []FTPAccount]("beget_list_ftp_accounts", "List additional FTP accounts with their full logins and home directories. Use the suffix portion for FTP mutations.", "ftp", "getList"),
	mutationOperation[FTPAddInput, APIBool]("beget_add_ftp_account", "Create an additional FTP account for homedir using the requested suffix and password. Verify with beget_list_ftp_accounts. Requires explicit confirm=true after user approval.", "ftp", "add", false, false),
	mutationOperation[FTPPasswordInput, APIBool]("beget_change_ftp_password", "Change the password for the FTP account suffix from beget_list_ftp_accounts. Never repeat the password in the result summary. Requires explicit confirm=true after user approval.", "ftp", "changePassword", true, true),
	mutationOperation[FTPSuffixInput, APIBool]("beget_delete_ftp_account", "Delete the additional FTP account suffix from beget_list_ftp_accounts. Verify its absence afterward. Requires explicit confirm=true after user approval.", "ftp", "delete", true, true),
	readOperation[NoArgs, []MySQLDatabase]("beget_list_mysql_databases", "List MySQL databases and their configured access sources. Use database suffix and access values from this result for mutations.", "mysql", "getList"),
	mutationOperation[MySQLDatabaseInput, APIBool]("beget_add_mysql_database", "Queue creation of a MySQL database suffix and localhost access with the requested password. Verify with beget_list_mysql_databases. Requires explicit confirm=true after user approval.", "mysql", "addDb", false, false, minimumLength("password", 6), maximumLength("suffix", 16)),
	mutationOperation[MySQLAccessInput, APIBool]("beget_add_mysql_access", "Add one access source to the MySQL database suffix with the requested password. Verify with beget_list_mysql_databases. Requires explicit confirm=true after user approval.", "mysql", "addAccess", false, false, minimumLength("password", 6), maximumLength("suffix", 16)),
	mutationOperation[MySQLSuffixInput, APIBool]("beget_delete_mysql_database", "Delete the MySQL database suffix and all of its access entries. Verify its absence with beget_list_mysql_databases. Requires explicit confirm=true after user approval.", "mysql", "dropDb", true, true, maximumLength("suffix", 16)),
	mutationOperation[MySQLAccessDeleteInput, APIBool]("beget_delete_mysql_access", "Delete one exact access source from the MySQL database suffix. Verify with beget_list_mysql_databases. Requires explicit confirm=true after user approval.", "mysql", "dropAccess", true, true, maximumLength("suffix", 16)),
	mutationOperation[MySQLAccessInput, APIBool]("beget_change_mysql_access_password", "Change the password for one exact access source on the MySQL database suffix. Never repeat the password in the result summary. Requires explicit confirm=true after user approval.", "mysql", "changeAccessPassword", true, true, minimumLength("password", 6), maximumLength("suffix", 16)),
	readOperation[NoArgs, []Site]("beget_list_sites", "List sites with their site ids, paths, and linked domains. Use these ids for site mutations and load details.", "site", "getList"),
	mutationOperation[SiteAddInput, APIBool]("beget_add_site", "Create a site directory named name; Beget will use name/public_html. Verify with beget_list_sites. Requires explicit confirm=true after user approval.", "site", "add", false, false),
	mutationOperation[SiteDeleteInput, APIBool]("beget_delete_site", "Delete the site id from beget_list_sites and unlink its domains. Verify with beget_list_sites. Requires explicit confirm=true after user approval.", "site", "delete", true, true),
	mutationOperation[SiteLinkInput, APIBool]("beget_link_domain_to_site", "Link domain_id from beget_list_domains to site_id from beget_list_sites. Verify with beget_list_sites. Requires explicit confirm=true after user approval.", "site", "linkDomain", true, true),
	mutationOperation[SiteUnlinkInput, APIBool]("beget_unlink_domain_from_site", "Unlink domain_id from its current site. Read beget_list_domains and beget_list_sites first, then verify afterward. Requires explicit confirm=true after user approval.", "site", "unlinkDomain", true, true),
	customOperation(
		"beget_freeze_site",
		freezeSiteDescription,
		"site", "freeze", true,
		func(server *mcp.Server, service *service) {
			addToolWithSchema(server, mutatingTool("beget_freeze_site", freezeSiteDescription, true, true), service.freezeSite)
		},
	),
	customOperation(
		"beget_unfreeze_site",
		unfreezeDescription,
		"site", "unfreeze", true,
		func(server *mcp.Server, service *service) {
			addToolWithSchema(server, mutatingTool("beget_unfreeze_site", unfreezeDescription, true, true), service.unfreezeSite)
		},
	),
	readOperation[SiteStatusInput, APIBool]("beget_is_site_frozen", "Read whether files for site_id from beget_list_sites are currently frozen against writes.", "site", "isSiteFrozen"),
	readOperation[NoArgs, []Domain]("beget_list_domains", "List domains with their ids, full names, registration state, and Beget-control status. Use these ids for domain and site-link operations.", "domain", "getList"),
	readOperation[NoArgs, map[string]DomainZone]("beget_list_domain_zones", "List domain zones with zone ids, prices, and allowed registration periods. Use zone_id and its limits for registration checks.", "domain", "getZoneList"),
	mutationOperation[VirtualDomainInput, APIInt64]("beget_add_virtual_domain", "Add a virtual domain from hostname and zone_id from beget_list_domain_zones. Verify with beget_list_domains. Requires explicit confirm=true after user approval.", "domain", "addVirtual", false, false),
	mutationOperation[DomainDeleteInput, APIBool]("beget_delete_domain", "Delete domain id from beget_list_domains, unlink it from its site, and delete its subdomains. Verify afterward. Requires explicit confirm=true after user approval.", "domain", "delete", true, true),
	readOperation[NoArgs, []Subdomain]("beget_list_subdomains", "List subdomains with their ids, full names, and parent domain ids. Use these ids for subdomain deletion.", "domain", "getSubdomainList"),
	mutationOperation[VirtualSubdomainInput, APIInt64]("beget_add_virtual_subdomain", "Add subdomain below domain_id from beget_list_domains. Verify with beget_list_subdomains. Requires explicit confirm=true after user approval.", "domain", "addSubdomainVirtual", false, false),
	mutationOperation[SubdomainDeleteInput, APIBool]("beget_delete_subdomain", "Delete subdomain id from beget_list_subdomains. Verify its absence afterward. Requires explicit confirm=true after user approval.", "domain", "deleteSubdomain", true, true),
	readOperation[DomainRegistrationInput, DomainRegistrationResult]("beget_check_domain_registration", "Check whether hostname can be registered in zone_id for period years. Read beget_list_domain_zones first and inspect all returned eligibility fields.", "domain", "checkDomainToRegister"),
	readOperation[FullFQDNInput, PHPVersionResult]("beget_get_domain_php_version", "Read the current PHP version, CGI mode, and allowed_versions for full_fqdn. Use allowed_versions before changing PHP.", "domain", "getPhpVersion"),
	mutationOperation[PHPVersionInput, PHPVersionChangeResult]("beget_change_domain_php_version", "Set php_version and optional CGI mode for full_fqdn. Read beget_get_domain_php_version first and verify with it afterward. Requires explicit confirm=true after user approval.", "domain", "changePhpVersion", true, true),
	readOperation[FullFQDNInput, []DirectiveResult]("beget_get_domain_directives", "Read the exact custom PHP directive name and value pairs for full_fqdn.", "domain", "getDirectives"),
	mutationOperation[DirectivesInput, APIBool]("beget_add_domain_directives", "Add the exact directives_list name and value pairs to full_fqdn. Read and verify with beget_get_domain_directives. Requires explicit confirm=true after user approval.", "domain", "addDirectives", true, false),
	mutationOperation[DirectivesInput, APIBool]("beget_remove_domain_directives", "Remove the exact directives_list name and value pairs from full_fqdn. Read and verify with beget_get_domain_directives. Requires explicit confirm=true after user approval.", "domain", "removeDirectives", true, true),
	readOperation[MailDomainInput, []Mailbox]("beget_list_mailboxes", "List mailboxes, spam-filter state, and forwarding mode for domain. Use mailbox local parts from this result for mail mutations.", "mail", "getMailboxList"),
	mutationOperation[MailboxPasswordInput, APIBool]("beget_change_mailbox_password", "Change mailbox_password for the mailbox on domain. Never repeat the password in the result summary. Requires explicit confirm=true after user approval.", "mail", "changeMailboxPassword", true, true),
	mutationOperation[MailboxPasswordInput, APIBool]("beget_create_mailbox", "Create mailbox on domain with mailbox_password. Verify with beget_list_mailboxes and never repeat the password in summaries. Requires explicit confirm=true after user approval.", "mail", "createMailbox", false, false),
	mutationOperation[MailboxInput, APIBool]("beget_delete_mailbox", "Delete mailbox from domain. Verify its absence with beget_list_mailboxes. Requires explicit confirm=true after user approval.", "mail", "dropMailbox", true, true),
	mutationOperation[MailboxSettingsInput, APIBool]("beget_change_mailbox_settings", "Replace spam filtering and forwarding mode for mailbox on domain. Read and verify with beget_list_mailboxes. Requires explicit confirm=true after user approval.", "mail", "changeMailboxSettings", true, true),
	mutationOperation[MailForwardingInput, APIBool]("beget_add_mail_forwarding", "Add forward_mailbox to the forwarding list for mailbox on domain. Verify with beget_list_mail_forwarding. Requires explicit confirm=true after user approval.", "mail", "forwardListAddMailbox", false, false),
	mutationOperation[MailForwardingInput, APIBool]("beget_delete_mail_forwarding", "Delete exact forward_mailbox from the forwarding list for mailbox on domain. Verify afterward. Requires explicit confirm=true after user approval.", "mail", "forwardListDeleteMailbox", true, true),
	readOperation[MailForwardingListInput, []MailForward]("beget_list_mail_forwarding", "List complete forwarding destination addresses for mailbox on domain.", "mail", "forwardListShow"),
	mutationOperation[DomainMailInput, APIBool]("beget_set_domain_mail", "Set domain_mailbox as the catch-all destination for domain. Verify the domain mail configuration afterward. Requires explicit confirm=true after user approval.", "mail", "setDomainMail", true, true),
	mutationOperation[ClearDomainMailInput, APIBool]("beget_clear_domain_mail", "Clear the catch-all domain mail destination for domain. Verify the domain mail configuration afterward. Requires explicit confirm=true after user approval.", "mail", "clearDomainMail", true, true),
	readOperation[NoArgs, []SiteLoad]("beget_site_load", "Read average monthly load for all hosted sites, including site ids usable with beget_site_load_details.", "stat", "getSitesListLoad"),
	readOperation[NoArgs, []DatabaseLoad]("beget_database_load", "Read average monthly load for all MySQL databases, including names usable with beget_database_load_details.", "stat", "getDbListLoad"),
	readOperation[SiteStatusInput, SiteLoadDetails]("beget_site_load_details", "Read daily and hourly load details for site_id from beget_site_load or beget_list_sites.", "stat", "getSiteLoad"),
	readOperation[DatabaseLoadInput, DatabaseLoadDetails]("beget_database_load_details", "Read daily and hourly load plus size history for db_name from beget_database_load.", "stat", "getDbLoad"),
}

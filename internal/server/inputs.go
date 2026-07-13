// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/kordax/beget-api-mcp-server/internal/passwordpolicy"
)

type Confirmation struct {
	Confirm bool `json:"confirm" jsonschema:"must be true only after the user explicitly approves this exact change; use false with dry_run=true before approval"`
	DryRun  bool `json:"dry_run,omitempty" jsonschema:"true runs local checks without Beget; use with confirm=false"`
}

func (input Confirmation) confirmed() bool {
	return input.Confirm
}

func (input Confirmation) dryRun() bool {
	return input.DryRun
}

type ToggleSSHInput struct {
	Confirmation
	Status   int    `json:"status" jsonschema:"SSH state: 1 enables access and 0 disables access"`
	FTPLogin string `json:"ftplogin,omitempty" jsonschema:"optional full FTP login from beget_list_ftp_accounts; omit for the main account"`
}

type BackupFileListInput struct {
	BackupID int64  `json:"backup_id,omitempty" jsonschema:"optional backup identifier from beget_list_file_backups; omit for the current copy"`
	Path     string `json:"path" jsonschema:"absolute path from the hosting home directory, for example /example.com/public_html"`
}

func (input BackupFileListInput) validate() error {
	return validateAbsoluteHostingPath("path", input.Path)
}

type BackupIDInput struct {
	BackupID int64 `json:"backup_id,omitempty" jsonschema:"optional backup identifier from the matching backup list; omit for the current copy"`
}

type RestoreFilesInput struct {
	Confirmation
	BackupID int64    `json:"backup_id" jsonschema:"file backup identifier from beget_list_file_backups"`
	Paths    []string `json:"paths" jsonschema:"one or more absolute paths from the hosting home directory to restore"`
}

func (input RestoreFilesInput) validate() error {
	return validateAbsoluteHostingPaths("paths", input.Paths)
}

type RestoreDatabasesInput struct {
	Confirmation
	BackupID int64    `json:"backup_id" jsonschema:"MySQL backup identifier from beget_list_mysql_backups"`
	Bases    []string `json:"bases" jsonschema:"one or more full database names returned by beget_list_backup_databases"`
}

type DownloadFilesInput struct {
	Confirmation
	BackupID int64    `json:"backup_id,omitempty" jsonschema:"optional file backup identifier from beget_list_file_backups; omit for the current copy"`
	Paths    []string `json:"paths" jsonschema:"one or more absolute paths to place as a downloadable archive in the account root"`
}

func (input DownloadFilesInput) validate() error {
	return validateAbsoluteHostingPaths("paths", input.Paths)
}

type DownloadDatabasesInput struct {
	Confirmation
	BackupID int64    `json:"backup_id,omitempty" jsonschema:"optional MySQL backup identifier from beget_list_mysql_backups; omit for the current copy"`
	Bases    []string `json:"bases" jsonschema:"one or more full database names to place as a downloadable archive in the account root"`
}

type CronAddInput struct {
	Confirmation
	Minutes  string `json:"minutes" jsonschema:"Cron minute expression using values from 0 to 59"`
	Hours    string `json:"hours" jsonschema:"Cron hour expression using values from 0 to 23"`
	Days     string `json:"days" jsonschema:"Cron day-of-month expression using values from 1 to 31"`
	Months   string `json:"months" jsonschema:"Cron month expression using values from 1 to 12"`
	Weekdays string `json:"weekdays" jsonschema:"Cron weekday expression using values from 0 to 7, where 0 and 7 are Sunday"`
	Command  string `json:"command" jsonschema:"command to execute"`
}

func (input CronAddInput) validate() error {
	return validateCronSchedule(input.Minutes, input.Hours, input.Days, input.Months, input.Weekdays)
}

type CronEditInput struct {
	Confirmation
	ID       int64  `json:"id" jsonschema:"Cron task identifier returned by beget_list_cron_jobs"`
	Minutes  string `json:"minutes" jsonschema:"Cron minute expression using values from 0 to 59"`
	Hours    string `json:"hours" jsonschema:"Cron hour expression using values from 0 to 23"`
	Days     string `json:"days" jsonschema:"Cron day-of-month expression using values from 1 to 31"`
	Months   string `json:"months" jsonschema:"Cron month expression using values from 1 to 12"`
	Weekdays string `json:"weekdays" jsonschema:"Cron weekday expression using values from 0 to 7, where 0 and 7 are Sunday"`
	Command  string `json:"command" jsonschema:"replacement command to execute"`
}

func (input CronEditInput) validate() error {
	return validateCronSchedule(input.Minutes, input.Hours, input.Days, input.Months, input.Weekdays)
}

type CronRowInput struct {
	Confirmation
	RowNumber int64 `json:"row_number" jsonschema:"Cron task identifier returned by beget_list_cron_jobs"`
}

type CronHiddenInput struct {
	Confirmation
	RowNumber int64 `json:"row_number" jsonschema:"Cron task identifier returned by beget_list_cron_jobs"`
	IsHidden  int   `json:"is_hidden" jsonschema:"task state: 1 hides and disables the task; 0 makes it active"`
}

type CronEmailInput struct {
	Confirmation
	Email string `json:"email" jsonschema:"notification email address, or an empty string to clear it"`
}

func (input CronEmailInput) validate() error {
	if input.Email == "" {
		return nil
	}
	return validateEmail("email", input.Email)
}

type FTPAddInput struct {
	Confirmation
	Suffix   string `json:"suffix" jsonschema:"FTP login suffix; the resulting account login_suffix must not exceed 17 characters"`
	HomeDir  string `json:"homedir" jsonschema:"absolute home directory for the FTP account, for example /example.com/public_html"`
	Password string `json:"password" jsonschema:"password for the new FTP account; never repeat it in summaries or logs"`
}

func (input FTPAddInput) validate() error {
	return validateAbsoluteHostingPath("homedir", input.HomeDir)
}

type FTPPasswordInput struct {
	Confirmation
	Suffix   string `json:"suffix" jsonschema:"FTP login suffix from beget_list_ftp_accounts"`
	Password string `json:"password" jsonschema:"new FTP password; never repeat it in summaries or logs"`
}

type FTPSuffixInput struct {
	Confirmation
	Suffix string `json:"suffix" jsonschema:"FTP login suffix from beget_list_ftp_accounts"`
}

type MySQLDatabaseInput struct {
	Confirmation
	Suffix   string `json:"suffix" jsonschema:"database suffix; the resulting login_suffix must not exceed 16 characters"`
	Password string `json:"password" jsonschema:"password with at least 6 characters for localhost access; never repeat it in summaries or logs"`
}

type MySQLAccessInput struct {
	Confirmation
	Suffix   string `json:"suffix" jsonschema:"database suffix from beget_list_mysql_databases"`
	Access   string `json:"access" jsonschema:"access source: a domain, IP address, *, or localhost"`
	Password string `json:"password" jsonschema:"password with at least 6 characters for this access; never repeat it in summaries or logs"`
}

func (input MySQLAccessInput) validate() error {
	return validateMySQLAccess(input.Access)
}

type MySQLSuffixInput struct {
	Confirmation
	Suffix string `json:"suffix" jsonschema:"database suffix from beget_list_mysql_databases"`
}

type MySQLAccessDeleteInput struct {
	Confirmation
	Suffix string `json:"suffix" jsonschema:"database suffix from beget_list_mysql_databases"`
	Access string `json:"access" jsonschema:"access source shown by beget_list_mysql_databases"`
}

func (input MySQLAccessDeleteInput) validate() error {
	return validateMySQLAccess(input.Access)
}

type SiteAddInput struct {
	Confirmation
	Name string `json:"name" jsonschema:"new site directory name, for example example.com"`
}

func (input SiteAddInput) validate() error {
	if !isSafeRelativeHostingPath(input.Name) {
		return fmt.Errorf("name must be a safe relative site path without parent traversal")
	}
	return nil
}

type SiteDeleteInput struct {
	Confirmation
	ID int64 `json:"id" jsonschema:"site identifier returned by beget_list_sites"`
}

type SiteLinkInput struct {
	Confirmation
	DomainID int64 `json:"domain_id" jsonschema:"domain identifier returned by beget_list_domains"`
	SiteID   int64 `json:"site_id" jsonschema:"site identifier returned by beget_list_sites"`
}

type SiteUnlinkInput struct {
	Confirmation
	DomainID int64 `json:"domain_id" jsonschema:"domain identifier returned by beget_list_domains"`
}

type SiteStatusInput struct {
	SiteID int64 `json:"site_id" jsonschema:"site identifier returned by beget_list_sites"`
}

type VirtualDomainInput struct {
	Confirmation
	Hostname string `json:"hostname" jsonschema:"domain name without its zone"`
	ZoneID   int64  `json:"zone_id" jsonschema:"zone identifier returned by beget_list_domain_zones"`
}

func (input VirtualDomainInput) validate() error {
	return validateDomainLabel("hostname", input.Hostname)
}

type DomainDeleteInput struct {
	Confirmation
	ID int64 `json:"id" jsonschema:"domain identifier returned by beget_list_domains"`
}

type VirtualSubdomainInput struct {
	Confirmation
	Subdomain string `json:"subdomain" jsonschema:"subdomain name without the parent domain; multiple labels are allowed"`
	DomainID  int64  `json:"domain_id" jsonschema:"parent domain identifier returned by beget_list_domains"`
}

func (input VirtualSubdomainInput) validate() error {
	return validateDomainName("subdomain", input.Subdomain)
}

type SubdomainDeleteInput struct {
	Confirmation
	ID int64 `json:"id" jsonschema:"subdomain identifier returned by beget_list_subdomains"`
}

type DomainRegistrationInput struct {
	Hostname string `json:"hostname" jsonschema:"domain name without its zone"`
	ZoneID   int64  `json:"zone_id" jsonschema:"zone identifier returned by beget_list_domain_zones"`
	Period   int    `json:"period" jsonschema:"registration period in years within the selected zone limits"`
}

func (input DomainRegistrationInput) validate() error {
	return validateDomainLabel("hostname", input.Hostname)
}

type FullFQDNInput struct {
	FullFQDN string `json:"full_fqdn" jsonschema:"full domain name returned by beget_list_domains or beget_list_subdomains"`
}

func (input FullFQDNInput) validate() error {
	return validateDomainName("full_fqdn", input.FullFQDN)
}

type PHPVersionInput struct {
	Confirmation
	FullFQDN   string `json:"full_fqdn" jsonschema:"full domain name returned by beget_list_domains or beget_list_subdomains"`
	PHPVersion string `json:"php_version" jsonschema:"PHP version returned in allowed_versions by beget_get_domain_php_version"`
	IsCGI      bool   `json:"is_cgi,omitempty" jsonschema:"whether to enable CGI mode; omitted means false"`
}

func (input PHPVersionInput) validate() error {
	return validateDomainName("full_fqdn", input.FullFQDN)
}

type Directive struct {
	Name  string `json:"name" jsonschema:"directive name"`
	Value string `json:"value" jsonschema:"directive value"`
}

type DirectivesInput struct {
	Confirmation
	FullFQDN       string      `json:"full_fqdn" jsonschema:"full domain name returned by beget_list_domains or beget_list_subdomains"`
	DirectivesList []Directive `json:"directives_list" jsonschema:"one or more exact name and value pairs to add or remove"`
}

func (input DirectivesInput) validate() error {
	if err := validateDomainName("full_fqdn", input.FullFQDN); err != nil {
		return err
	}
	for index, directive := range input.DirectivesList {
		if strings.TrimSpace(directive.Name) == "" {
			return fmt.Errorf("directives_list[%d].name must not be empty", index)
		}
		if strings.TrimSpace(directive.Value) == "" {
			return fmt.Errorf("directives_list[%d].value must not be empty", index)
		}
	}
	return nil
}

type MailDomainInput struct {
	Domain string `json:"domain" jsonschema:"domain whose mailboxes should be listed"`
}

func (input MailDomainInput) validate() error {
	return validateDomainName("domain", input.Domain)
}

type MailboxPasswordInput struct {
	Confirmation
	Domain          string `json:"domain" jsonschema:"domain containing the mailbox"`
	Mailbox         string `json:"mailbox" jsonschema:"mailbox local part returned by beget_list_mailboxes"`
	MailboxPassword string `json:"mailbox_password" jsonschema:"new mailbox password governed by the published mailbox policy; never repeat it in summaries or logs"`
}

func (input MailboxPasswordInput) validate() error {
	if err := validateMailbox(input.Domain, input.Mailbox); err != nil {
		return err
	}
	if message := passwordpolicy.ValidationMessage(input.MailboxPassword); message != "" {
		return errors.New(message)
	}
	return nil
}

type MailboxPasswordValidationInput struct {
	MailboxPassword string `json:"mailbox_password" jsonschema:"candidate mailbox password to validate locally; never repeat it in summaries or logs"`
}

type MailboxInput struct {
	Confirmation
	Domain  string `json:"domain" jsonschema:"domain containing the mailbox"`
	Mailbox string `json:"mailbox" jsonschema:"mailbox local part returned by beget_list_mailboxes"`
}

func (input MailboxInput) validate() error {
	return validateMailbox(input.Domain, input.Mailbox)
}

type MailboxSettingsInput struct {
	Confirmation
	Domain            string `json:"domain" jsonschema:"domain containing the mailbox"`
	Mailbox           string `json:"mailbox" jsonschema:"mailbox local part returned by beget_list_mailboxes"`
	SpamFilterStatus  int    `json:"spam_filter_status" jsonschema:"spam filter state: 0 disables and 1 enables it"`
	SpamFilter        int    `json:"spam_filter" jsonschema:"spam filtering level from 0 for strongest filtering to 100 for weakest"`
	ForwardMailStatus string `json:"forward_mail_status" jsonschema:"forwarding mode: no_forward, forward, or forward_and_delete"`
}

func (input MailboxSettingsInput) validate() error {
	return validateMailbox(input.Domain, input.Mailbox)
}

type MailForwardingInput struct {
	Confirmation
	Domain         string `json:"domain" jsonschema:"domain containing the source mailbox"`
	Mailbox        string `json:"mailbox" jsonschema:"source mailbox local part returned by beget_list_mailboxes"`
	ForwardMailbox string `json:"forward_mailbox" jsonschema:"complete destination email address"`
}

func (input MailForwardingInput) validate() error {
	if err := validateMailbox(input.Domain, input.Mailbox); err != nil {
		return err
	}
	return validateEmail("forward_mailbox", input.ForwardMailbox)
}

type MailForwardingListInput struct {
	Domain  string `json:"domain" jsonschema:"domain containing the source mailbox"`
	Mailbox string `json:"mailbox" jsonschema:"source mailbox local part returned by beget_list_mailboxes"`
}

func (input MailForwardingListInput) validate() error {
	return validateMailbox(input.Domain, input.Mailbox)
}

type DomainMailInput struct {
	Confirmation
	Domain        string `json:"domain" jsonschema:"domain whose catch-all mailbox should be set"`
	DomainMailbox string `json:"domain_mailbox" jsonschema:"complete mailbox address to receive domain mail"`
}

func (input DomainMailInput) validate() error {
	if err := validateDomainName("domain", input.Domain); err != nil {
		return err
	}
	return validateEmail("domain_mailbox", input.DomainMailbox)
}

type ClearDomainMailInput struct {
	Confirmation
	Domain string `json:"domain" jsonschema:"domain whose catch-all mailbox should be cleared"`
}

func (input ClearDomainMailInput) validate() error {
	return validateDomainName("domain", input.Domain)
}

type DatabaseLoadInput struct {
	Database string `json:"db_name" jsonschema:"full database name returned by beget_database_load or beget_list_mysql_databases"`
}

func validateAbsoluteHostingPaths(field string, values []string) error {
	for index, value := range values {
		if err := validateAbsoluteHostingPath(fmt.Sprintf("%s[%d]", field, index), value); err != nil {
			return err
		}
	}
	return nil
}

func validateAbsoluteHostingPath(field, value string) error {
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s must be an absolute path from the hosting home directory", field)
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return fmt.Errorf("%s must not contain parent traversal", field)
		}
	}
	return nil
}

func isSafeRelativeHostingPath(value string) bool {
	if value == "" || path.IsAbs(value) || strings.ContainsAny(value, "\\\x00") || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validateDomainName(field, value string) error {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	if value == "" || len(value) > 253 {
		return fmt.Errorf("%s must be a valid domain name", field)
	}
	for _, label := range strings.Split(value, ".") {
		if err := validateDomainLabel(field, label); err != nil {
			return err
		}
	}
	return nil
}

func validateDomainLabel(field, value string) error {
	if value == "" || len(value) > 63 || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
		return fmt.Errorf("%s must contain valid domain labels", field)
	}
	for _, character := range value {
		if character != '-' && !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return fmt.Errorf("%s must contain only domain letters, digits, dots, or hyphens", field)
		}
	}
	return nil
}

func validateMailbox(domain, mailbox string) error {
	if err := validateDomainName("domain", domain); err != nil {
		return err
	}
	if mailbox == "" || strings.ContainsAny(mailbox, "@\t\r\n ") {
		return fmt.Errorf("mailbox must be a non-empty local part without @ or whitespace")
	}
	return nil
}

func validateEmail(field, value string) error {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(value, "@") {
		return fmt.Errorf("%s must be a complete email address", field)
	}
	return nil
}

func validateMySQLAccess(value string) error {
	if value == "*" || value == "localhost" || net.ParseIP(value) != nil {
		return nil
	}
	if err := validateDomainName("access", value); err != nil {
		return fmt.Errorf("access must be a domain, IP address, *, or localhost")
	}
	return nil
}

func validateDNSRecordValue(recordType, value string) error {
	switch recordType {
	case "A":
		if address := net.ParseIP(value); address == nil || address.To4() == nil {
			return fmt.Errorf("a record value must be an IPv4 address")
		}
	case "DNS_IP":
		if net.ParseIP(value) == nil {
			return fmt.Errorf("DNS_IP record value must be an IP address")
		}
	case "MX", "NS", "CNAME", "DNS":
		if err := validateDomainName(strings.ToLower(recordType)+" record value", value); err != nil {
			return err
		}
	}
	return nil
}

func validateCronSchedule(minutes, hours, days, months, weekdays string) error {
	for _, field := range []struct {
		name    string
		value   string
		minimum int
		maximum int
	}{
		{"minutes", minutes, 0, 59},
		{"hours", hours, 0, 23},
		{"days", days, 1, 31},
		{"months", months, 1, 12},
		{"weekdays", weekdays, 0, 7},
	} {
		if err := validateCronExpression(field.name, field.value, field.minimum, field.maximum); err != nil {
			return err
		}
	}
	return nil
}

func validateCronExpression(field, expression string, minimum, maximum int) error {
	if expression == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	for _, item := range strings.Split(expression, ",") {
		base, stepText, hasStep := strings.Cut(item, "/")
		if hasStep {
			step, err := strconv.Atoi(stepText)
			if err != nil || step <= 0 || step > maximum-minimum+1 || strings.Contains(stepText, "/") {
				return fmt.Errorf("%s contains an invalid step in %q", field, item)
			}
		}
		if base == "*" {
			continue
		}
		startText, endText, hasRange := strings.Cut(base, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < minimum || start > maximum {
			return fmt.Errorf("%s contains a value outside %d..%d in %q", field, minimum, maximum, item)
		}
		if !hasRange {
			continue
		}
		end, err := strconv.Atoi(endText)
		if err != nil || end < start || end > maximum || strings.Contains(endText, "-") {
			return fmt.Errorf("%s contains an invalid range in %q", field, item)
		}
	}
	return nil
}

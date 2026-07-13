// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

type ErrorType string

const (
	ErrorValidation          ErrorType = "validation"
	ErrorAuthorization       ErrorType = "authorization"
	ErrorProviderRejection   ErrorType = "provider_rejection"
	ErrorTransportFailure    ErrorType = "transport_failure"
	ErrorConfirmationFailure ErrorType = "confirmation_failure"
	ErrorUnknownOutcome      ErrorType = "unknown_outcome"
)

type ToolError struct {
	Type     ErrorType `json:"type" jsonschema:"stable machine-readable error category"`
	Code     string    `json:"code" jsonschema:"stable server or provider error code"`
	Message  string    `json:"message" jsonschema:"safe human-readable error without credentials"`
	Field    string    `json:"field,omitempty" jsonschema:"invalid input field when known"`
	NextStep string    `json:"next_step" jsonschema:"safe action to take before another call"`
}

type ToolOutput[Result any] struct {
	Success bool        `json:"success" jsonschema:"true only when the operation completed successfully"`
	Result  *Result     `json:"result" jsonschema:"typed operation result, or null on failure"`
	Errors  []ToolError `json:"errors" jsonschema:"empty on success; stable structured errors on failure"`
}

type MutationResult[Details any] struct {
	Changed bool              `json:"changed" jsonschema:"whether Beget accepted and applied or queued the requested change"`
	DryRun  *DryRunAssessment `json:"dry_run,omitempty" jsonschema:"local-only dry-run assessment"`
	Details *Details          `json:"details,omitempty" jsonschema:"typed provider result when Beget returned one"`
}

type DryRunAssessment struct {
	Scope                        string `json:"scope"`
	Status                       string `json:"status"`
	ProviderAcceptanceGuaranteed bool   `json:"provider_acceptance_guaranteed"`
}

type LosslessFields struct {
	AdditionalPropertiesJSON map[string]string `json:"additional_properties_json,omitempty" jsonschema:"undocumented Beget fields preserved as exact JSON values"`
}

type APIInt64 int64
type APIFloat64 float64
type APIBool bool
type APIString string

func (value *APIInt64) UnmarshalJSON(data []byte) error {
	text, err := scalarText(data)
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("decode integer %q: %w", text, err)
	}
	*value = APIInt64(parsed)
	return nil
}

func (value *APIFloat64) UnmarshalJSON(data []byte) error {
	text, err := scalarText(data)
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("decode number %q: %w", text, err)
	}
	*value = APIFloat64(parsed)
	return nil
}

func (value *APIBool) UnmarshalJSON(data []byte) error {
	text, err := scalarText(data)
	if err != nil {
		return err
	}
	switch text {
	case "true", "1":
		*value = true
	case "false", "0":
		*value = false
	default:
		return fmt.Errorf("decode boolean %q", text)
	}
	return nil
}

func (value *APIString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*value = ""
		return nil
	}
	text, err := scalarText(data)
	if err != nil {
		return err
	}
	*value = APIString(text)
	return nil
}

func scalarText(data []byte) (string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return "", fmt.Errorf("expected scalar value")
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	return string(data), nil
}

type AccountInfoResult struct {
	LosslessFields
	PlanName            string     `json:"plan_name"`
	UserSites           APIInt64   `json:"user_sites"`
	PlanSites           APIInt64   `json:"plan_site"`
	UserDomains         APIInt64   `json:"user_domains"`
	PlanDomains         APIInt64   `json:"plan_domain"`
	UserMySQLSize       APIInt64   `json:"user_mysqlsize"`
	PlanMySQL           APIInt64   `json:"plan_mysql"`
	UserQuota           APIInt64   `json:"user_quota"`
	PlanQuota           APIInt64   `json:"plan_quota"`
	UserFTP             APIInt64   `json:"user_ftp"`
	PlanFTP             APIInt64   `json:"plan_ftp"`
	ServerName          string     `json:"server_name"`
	ServerCPUName       string     `json:"server_cpu_name"`
	ServerMemory        APIInt64   `json:"server_memory"`
	ServerMemoryCurrent APIInt64   `json:"server_memorycurrent"`
	ServerLoadAverage   APIFloat64 `json:"server_loadaverage"`
	ServerUptime        APIInt64   `json:"server_uptime"`
}

type Backup struct {
	LosslessFields
	BackupID APIInt64 `json:"backup_id"`
	Date     string   `json:"date"`
}

type BackupFile struct {
	LosslessFields
	Name        string   `json:"name"`
	IsDirectory APIBool  `json:"is_dir"`
	ModifiedAt  string   `json:"mtime"`
	Size        APIInt64 `json:"size"`
}

type BackupLogEntry struct {
	LosslessFields
	ID         APIInt64 `json:"id"`
	Operation  string   `json:"operation"`
	Type       string   `json:"type"`
	CreatedAt  string   `json:"date_create"`
	TargetList []string `json:"target_list"`
	Status     string   `json:"status"`
}

type CronJob struct {
	LosslessFields
	RowNumber APIInt64 `json:"row_number"`
	Minutes   string   `json:"minutes"`
	Hours     string   `json:"hours"`
	Days      string   `json:"days"`
	Months    string   `json:"months"`
	Weekdays  string   `json:"weekdays"`
	Command   string   `json:"command"`
	IsHidden  APIBool  `json:"is_hidden"`
}

type CronTaskResult struct {
	LosslessFields
	RowNumber APIInt64 `json:"row_number"`
}

func (result *CronTaskResult) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		type plain CronTaskResult
		return json.Unmarshal(data, (*plain)(result))
	}
	return json.Unmarshal(data, &result.RowNumber)
}

type CronEmailResult struct {
	Email *string `json:"email"`
}

type FTPAccount struct {
	LosslessFields
	Login   string `json:"login"`
	HomeDir string `json:"homedir"`
}

type MySQLDatabase struct {
	LosslessFields
	Name     string        `json:"name"`
	Size     APIInt64      `json:"size"`
	Accesses []MySQLAccess `json:"accesses"`
}

type MySQLAccess struct {
	LosslessFields
	Name string `json:"name"`
}

type SiteDomain struct {
	LosslessFields
	ID            APIInt64 `json:"id"`
	FQDN          string   `json:"fqdn"`
	PHPVersion    string   `json:"php_version,omitempty"`
	HTTPVersion   APIInt64 `json:"http_version,omitempty"`
	SSL           APIBool  `json:"ssl,omitempty"`
	SSLStatus     string   `json:"ssl_status,omitempty"`
	NginxTemplate string   `json:"nginx_template,omitempty"`
	RedisSession  APIBool  `json:"redis_session,omitempty"`
}

type Site struct {
	LosslessFields
	ID      APIInt64     `json:"id"`
	Path    string       `json:"path"`
	Domains []SiteDomain `json:"domains"`
}

type SiteFrozenResult struct {
	Frozen APIBool `json:"frozen"`
}

type Domain struct {
	LosslessFields
	ID                   APIInt64  `json:"id"`
	FQDN                 string    `json:"fqdn"`
	DateAdded            string    `json:"date_add"`
	AutoRenew            APIBool   `json:"auto_renew"`
	DateRegistered       string    `json:"date_register,omitempty"`
	DateExpires          APIString `json:"date_expire,omitempty"`
	CanRenew             APIBool   `json:"can_renew"`
	Registrar            *string   `json:"registrar"`
	RegistrarStatus      *string   `json:"registrar_status"`
	RegisterOrderStatus  *string   `json:"register_order_status"`
	RegisterOrderComment *string   `json:"register_order_comment"`
	RenewOrderStatus     APIString `json:"renew_order_status"`
	IsUnderControl       APIBool   `json:"is_under_control"`
}

type DomainZone struct {
	LosslessFields
	ID            APIInt64    `json:"id"`
	Zone          string      `json:"zone"`
	Price         APIFloat64  `json:"price"`
	RenewalPrice  APIFloat64  `json:"price_renew"`
	IDNPrice      *APIFloat64 `json:"price_idn"`
	IDNRenewPrice *APIFloat64 `json:"price_idn_renew"`
	IsIDN         APIBool     `json:"is_idn"`
	IsNational    APIBool     `json:"is_national"`
	MinimumPeriod APIInt64    `json:"min_period"`
	MaximumPeriod APIInt64    `json:"max_period"`
}

type Subdomain struct {
	LosslessFields
	ID       APIInt64 `json:"id"`
	FQDN     string   `json:"fqdn"`
	DomainID APIInt64 `json:"domain_id"`
}

type DomainRegistrationResult struct {
	LosslessFields
	MayBeRegistered APIBool    `json:"may_be_registered"`
	BonusDomains    APIInt64   `json:"bonus_domains"`
	Balance         APIFloat64 `json:"balance"`
	PayType         *string    `json:"pay_type"`
	Price           APIFloat64 `json:"price"`
	InSystem        APIBool    `json:"in_system"`
}

type PHPVersionResult struct {
	LosslessFields
	FQDN            string   `json:"full_fqdn"`
	PHPVersion      string   `json:"php_version"`
	CGI             string   `json:"cgi"`
	AllowedVersions []string `json:"allowed_versions"`
}

type PHPVersionChangeResult struct {
	LosslessFields
	FQDN       string `json:"full_fqdn"`
	Message    string `json:"result"`
	PHPVersion string `json:"php_version"`
	CGI        string `json:"cgi"`
}

type DirectiveResult struct {
	LosslessFields
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Mailbox struct {
	LosslessFields
	Mailbox          string  `json:"mailbox"`
	Domain           string  `json:"domain"`
	SpamFilterStatus APIBool `json:"spam_filter_status"`
	ForwardingMode   string  `json:"forward_mail_status"`
}

type MailForward struct {
	LosslessFields
	Mailbox string `json:"forward_mailbox"`
}

type SiteLoad struct {
	LosslessFields
	Name string     `json:"name"`
	ID   APIInt64   `json:"id"`
	Load APIFloat64 `json:"cp"`
}

type DatabaseLoad struct {
	LosslessFields
	Name string     `json:"name"`
	Load APIFloat64 `json:"cp"`
}

type LoadPoint struct {
	LosslessFields
	Value APIFloat64 `json:"value"`
	Date  string     `json:"date"`
}

type SiteLoadDetails struct {
	LosslessFields
	Days  []LoadPoint `json:"days"`
	Hours []LoadPoint `json:"hours"`
}

type DatabaseLoadPoint struct {
	LosslessFields
	Date    string     `json:"date,omitempty"`
	CPUTime APIFloat64 `json:"cpu_time,omitempty"`
	Size    APIInt64   `json:"size,omitempty"`
}

type DatabaseLoadDetails struct {
	LosslessFields
	Days     []DatabaseLoadPoint `json:"days"`
	Hours    []DatabaseLoadPoint `json:"hours"`
	SizeDays []DatabaseLoadPoint `json:"size_days"`
}

type DNSOutputRecord struct {
	LosslessFields
	Priority APIInt64 `json:"priority"`
	Value    *string  `json:"value"`
}

type DNSOutputRecords struct {
	LosslessFields
	A     []DNSOutputRecord `json:"A"`
	MX    []DNSOutputRecord `json:"MX"`
	TXT   []DNSOutputRecord `json:"TXT"`
	NS    []DNSOutputRecord `json:"NS"`
	CNAME []DNSOutputRecord `json:"CNAME"`
	DNS   []DNSOutputRecord `json:"DNS"`
	DNSIP []DNSOutputRecord `json:"DNS_IP"`
}

type DNSResult struct {
	LosslessFields
	IsUnderControl APIBool          `json:"is_under_control"`
	IsBegetDNS     APIBool          `json:"is_beget_dns"`
	IsSubdomain    APIBool          `json:"is_subdomain"`
	FQDN           string           `json:"fqdn"`
	Records        DNSOutputRecords `json:"records"`
	SetType        APIInt64         `json:"set_type"`
}

func decodeTypedResult[Result any](raw json.RawMessage) (Result, error) {
	var result Result
	if err := decodeLossless(raw, reflect.ValueOf(&result).Elem()); err != nil {
		return result, err
	}
	return result, nil
}

func decodeLossless(raw json.RawMessage, target reflect.Value) error {
	if target.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			target.SetZero()
			return nil
		}
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		return decodeLossless(raw, target.Elem())
	}
	if target.Kind() != reflect.Struct && target.Kind() != reflect.Slice && target.Kind() != reflect.Map {
		return json.Unmarshal(raw, target.Addr().Interface())
	}
	if err := json.Unmarshal(raw, target.Addr().Interface()); err != nil {
		return err
	}
	return captureAdditionalProperties(raw, target)
}

func captureAdditionalProperties(raw json.RawMessage, target reflect.Value) error {
	switch target.Kind() {
	case reflect.Pointer:
		if !target.IsNil() {
			return captureAdditionalProperties(raw, target.Elem())
		}
	case reflect.Slice:
		if target.IsNil() {
			target.Set(reflect.MakeSlice(target.Type(), 0, 0))
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil
		}
		for index := range min(len(items), target.Len()) {
			if err := captureAdditionalProperties(items[index], target.Index(index)); err != nil {
				return err
			}
		}
	case reflect.Map:
		if target.IsNil() {
			target.Set(reflect.MakeMap(target.Type()))
		}
		if target.Type().Key().Kind() != reflect.String {
			return nil
		}
		var entries map[string]json.RawMessage
		if json.Unmarshal(raw, &entries) != nil {
			return nil
		}
		for name, entry := range entries {
			key := reflect.ValueOf(name).Convert(target.Type().Key())
			value := reflect.New(target.Type().Elem()).Elem()
			if current := target.MapIndex(key); current.IsValid() {
				value.Set(current)
			}
			if err := captureAdditionalProperties(entry, value); err != nil {
				return err
			}
			target.SetMapIndex(key, value)
		}
	case reflect.Struct:
		return captureStructProperties(raw, target)
	}
	return nil
}

func captureStructProperties(raw json.RawMessage, target reflect.Value) error {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	known := make(map[string]struct{})
	for index := range target.NumField() {
		fieldType := target.Type().Field(index)
		field := target.Field(index)
		if fieldType.Anonymous {
			if fieldType.Type == reflect.TypeFor[LosslessFields]() {
				continue
			}
			if err := captureStructProperties(raw, field); err != nil {
				return err
			}
			continue
		}
		name := jsonFieldName(fieldType)
		if name == "" || name == "-" {
			continue
		}
		known[name] = struct{}{}
		if value, ok := values[name]; ok {
			if err := captureAdditionalProperties(value, field); err != nil {
				return err
			}
		} else {
			normalizeEmptyCollections(field)
		}
	}
	extra := make(map[string]string)
	for name, value := range values {
		if _, ok := known[name]; !ok {
			extra[name] = string(value)
		}
	}
	if field := target.FieldByName("LosslessFields"); field.IsValid() && field.CanSet() {
		field.FieldByName("AdditionalPropertiesJSON").Set(reflect.ValueOf(extra))
	}
	return nil
}

func normalizeEmptyCollections(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			normalizeEmptyCollections(value.Elem())
		}
	case reflect.Slice:
		if value.IsNil() {
			value.Set(reflect.MakeSlice(value.Type(), 0, 0))
		}
	case reflect.Map:
		if value.IsNil() {
			value.Set(reflect.MakeMap(value.Type()))
		}
	case reflect.Struct:
		for index := range value.NumField() {
			if value.Field(index).CanSet() {
				normalizeEmptyCollections(value.Field(index))
			}
		}
	}
}

func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	for index, character := range tag {
		if character == ',' {
			return tag[:index]
		}
	}
	return tag
}

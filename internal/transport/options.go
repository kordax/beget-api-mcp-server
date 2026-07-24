// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package transport

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"go.uber.org/fx"
)

type Mode string

const (
	ModeStdio          Mode = "stdio"
	ModeStreamableHTTP Mode = "streamable-http"
	ModeSSE            Mode = "sse"
)

var toolSectionByName = map[string]string{
	"account":    "user",
	"backup":     "backup",
	"cron":       "cron",
	"dns":        "dns",
	"ftp":        "ftp",
	"mysql":      "mysql",
	"site":       "site",
	"domain":     "domain",
	"mail":       "mail",
	"statistics": "stat",
}

var toolSectionNames = []string{
	"account", "backup", "cron", "dns", "ftp", "mysql", "site", "domain", "mail", "statistics",
}

type Arguments []string

type Options struct {
	Mode                Mode
	ToolSections        []string
	HTTPAddress         string
	HTTPPath            string
	HTTPBearerToken     string
	SessionTimeout      time.Duration
	JSONResponse        bool
	StreamableStateless bool
}

var Module = fx.Module("transport",
	fx.Provide(
		CommandLineArguments,
		ParseOptions,
		NewRuntime,
	),
)

func CommandLineArguments() Arguments {
	return os.Args[1:]
}

func ParseOptions(arguments Arguments) (Options, error) {
	flags := flag.NewFlagSet("beget-api-mcp-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	stdio := flags.Bool("stdio", false, "use the stdio MCP transport")
	streamableHTTP := flags.Bool("streamable-http", false, "use the Streamable HTTP MCP transport")
	sse := flags.Bool("sse", false, "use the legacy SSE MCP transport")
	toolSections := flags.String("tool-sections", "all", "comma-separated Beget tool sections")
	httpAddress := flags.String("http-address", "127.0.0.1:8080", "loopback address for HTTP transports")
	httpPath := flags.String("http-path", "", "HTTP endpoint path; defaults to /mcp or /sse")
	httpAuth := flags.Bool("http-auth", false, "require a bearer token from BEGET_MCP_HTTP_TOKEN")
	sessionTimeout := flags.Duration("streamable-session-timeout", 30*time.Minute, "idle Streamable HTTP session timeout")
	jsonResponse := flags.Bool("streamable-json-response", false, "return JSON instead of SSE for Streamable HTTP responses")
	stateless := flags.Bool("streamable-stateless", false, "disable Streamable HTTP session state")

	if err := flags.Parse(arguments); err != nil {
		return Options{}, err
	}
	if flags.NArg() != 0 {
		return Options{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	selectedToolSections, err := parseToolSections(*toolSections)
	if err != nil {
		return Options{}, err
	}
	visited := make(map[string]bool)
	flags.Visit(func(current *flag.Flag) { visited[current.Name] = true })

	selected := 0
	for _, enabled := range []bool{*stdio, *streamableHTTP, *sse} {
		if enabled {
			selected++
		}
	}
	if selected > 1 {
		return Options{}, errors.New("transport flags --stdio, --streamable-http, and --sse are mutually exclusive")
	}

	mode := ModeStdio
	if *streamableHTTP {
		mode = ModeStreamableHTTP
	}
	if *sse {
		mode = ModeSSE
	}
	if mode != ModeStreamableHTTP && (*jsonResponse || *stateless || visited["streamable-session-timeout"]) {
		return Options{}, errors.New("streamable HTTP options require --streamable-http")
	}
	if *sessionTimeout < 0 {
		return Options{}, errors.New("session timeout cannot be negative")
	}
	if mode == ModeStdio && *httpAuth {
		return Options{}, errors.New("HTTP authentication requires --streamable-http or --sse")
	}
	httpBearerToken := ""
	if *httpAuth {
		httpBearerToken = os.Getenv("BEGET_MCP_HTTP_TOKEN")
		if httpBearerToken == "" {
			return Options{}, errors.New("BEGET_MCP_HTTP_TOKEN is required when --http-auth is enabled")
		}
		if len(httpBearerToken) < 32 {
			return Options{}, errors.New("BEGET_MCP_HTTP_TOKEN must contain at least 32 characters")
		}
	}

	endpointPath := *httpPath
	if endpointPath == "" {
		if mode == ModeSSE {
			endpointPath = "/sse"
		} else {
			endpointPath = "/mcp"
		}
	}
	if mode != ModeStdio {
		if err := validateHTTPAddress(*httpAddress); err != nil {
			return Options{}, err
		}
		if err := validateHTTPPath(endpointPath); err != nil {
			return Options{}, err
		}
	}

	return Options{
		Mode:                mode,
		ToolSections:        selectedToolSections,
		HTTPAddress:         *httpAddress,
		HTTPPath:            endpointPath,
		HTTPBearerToken:     httpBearerToken,
		SessionTimeout:      *sessionTimeout,
		JSONResponse:        *jsonResponse,
		StreamableStateless: *stateless,
	}, nil
}

func parseToolSections(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "all") {
		return nil, nil
	}
	if value == "" {
		return nil, errors.New("tool sections cannot be empty; use all or a comma-separated section list")
	}

	sections := make([]string, 0, len(toolSectionNames))
	seen := make(map[string]struct{}, len(toolSectionNames))
	for _, item := range strings.Split(value, ",") {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			return nil, errors.New("tool sections cannot contain an empty value")
		}
		if name == "all" {
			return nil, errors.New(`tool section "all" cannot be combined with individual sections`)
		}
		section, exists := toolSectionByName[name]
		if !exists {
			return nil, fmt.Errorf(
				"unknown tool section %q; choose all or one of: %s",
				name, strings.Join(toolSectionNames, ", "),
			)
		}
		if _, exists := seen[section]; exists {
			continue
		}
		seen[section] = struct{}{}
		sections = append(sections, section)
	}
	return sections, nil
}

func validateHTTPAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid HTTP address %q: %w", address, err)
	}
	if portNumber, err := strconv.ParseUint(port, 10, 16); err != nil || portNumber > 65535 {
		return fmt.Errorf("invalid HTTP port %q", port)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("HTTP transport must bind to a loopback address, got %q", host)
	}
	return nil
}

func validateHTTPPath(value string) error {
	if !strings.HasPrefix(value, "/") || value == "/" || path.Clean(value) != value {
		return fmt.Errorf("HTTP path must be a clean absolute path other than root, got %q", value)
	}
	if strings.ContainsAny(value, "?#") {
		return fmt.Errorf("HTTP path cannot contain a query or fragment, got %q", value)
	}
	return nil
}

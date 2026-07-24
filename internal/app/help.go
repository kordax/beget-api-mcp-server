// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"fmt"
	"io"
)

const rootHelp = `Beget API MCP Server

Usage:
  beget-api-mcp-server [transport options]
  beget-api-mcp-server credentials <command> [options]
  beget-api-mcp-server upgrade [--check] [version]
  beget-api-mcp-server help [command]

Commands:
  credentials  Validate, save, check, or delete Beget credentials
  upgrade      Check for updates or install a release
  help         Show help for a command

Transport options:
  --stdio                              Use the stdio MCP transport (default)
  --streamable-http                    Use the Streamable HTTP MCP transport
  --sse                                Use the legacy SSE MCP transport
  --tool-sections <sections>           Enable selected Beget tool sections (default all)
                                        account, backup, cron, dns, ftp, mysql,
                                        site, domain, mail, statistics
  --http-address <address>             Loopback address (default 127.0.0.1:8080)
  --http-path <path>                   Endpoint path (default /mcp or /sse)
  --http-auth                          Require BEGET_MCP_HTTP_TOKEN
  --streamable-session-timeout <time>  Idle session timeout (default 30m)
  --streamable-json-response           Return JSON instead of SSE
  --streamable-stateless               Disable session state

Run "beget-api-mcp-server help <command>" for command-specific help.
`

const accountAccessHelp = `Manage Beget credentials in the persistent credential store.

Usage:
  beget-api-mcp-server credentials set --login <login>
  beget-api-mcp-server credentials check
  beget-api-mcp-server credentials delete

Commands:
  set     Check and save the login and an API key read from a hidden prompt
  check   Validate stored credentials with Beget without displaying them
  delete  Remove stored credentials

Run "beget-api-mcp-server help credentials set" for set options.
`

const accountAccessSetHelp = `Save Beget credentials in the persistent credential store.

Usage:
  beget-api-mcp-server credentials set --login <login>

Options:
  --login <login>  Beget hosting account login (required)

The API key is read from a hidden terminal prompt or stdin and is never accepted
as an argument. The server checks it with a read-only account information call.
Enable the account management API permission for a conclusive result.
`

const upgradeHelp = `Check for updates or install a release.

Usage:
  beget-api-mcp-server upgrade
  beget-api-mcp-server upgrade --check
  beget-api-mcp-server upgrade <version>

Options:
  --check  Show the current and latest versions without installing an update

Without a version, the latest release is installed. A version may be written as
v0.3.0 or 0.3.0.
`

func writeRequestedHelp(arguments []string, output, errorOutput io.Writer) (bool, int) {
	topic, requested := requestedHelpTopic(arguments)
	if !requested {
		return false, 0
	}

	var help string
	switch topic {
	case "":
		help = rootHelp
	case "credentials":
		help = accountAccessHelp
	case "credentials set":
		help = accountAccessSetHelp
	case "upgrade":
		help = upgradeHelp
	default:
		_, _ = fmt.Fprintf(errorOutput, "unknown help topic %q\nRun \"beget-api-mcp-server help\" to list commands.\n", topic)
		return true, 2
	}

	_, _ = io.WriteString(output, help)
	return true, 0
}

func requestedHelpTopic(arguments []string) (string, bool) {
	if len(arguments) == 0 {
		return "", false
	}

	if arguments[0] == "help" {
		switch len(arguments) {
		case 1:
			return "", true
		case 2:
			return arguments[1], true
		default:
			return arguments[1] + " " + arguments[2], true
		}
	}

	for index, argument := range arguments {
		if argument != "-h" && argument != "--help" {
			continue
		}
		if len(arguments) > 0 && arguments[0] == "credentials" {
			if index > 1 && arguments[1] == "set" {
				return "credentials set", true
			}
			return "credentials", true
		}
		if len(arguments) > 0 && arguments[0] == "upgrade" {
			return "upgrade", true
		}
		return "", true
	}

	if len(arguments) >= 2 && arguments[1] == "help" {
		return arguments[0], true
	}
	return "", false
}

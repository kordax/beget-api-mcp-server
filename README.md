# Beget API MCP Server

[![Tests](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Tests.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Tests.yml)
[![Lint](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Lint.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Lint.yml)
[![Security](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Security.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Security.yml)
[![Gitleaks](https://github.com/kordax/beget-api-mcp-server/actions/workflows/gitleaks.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/gitleaks.yml)
[![Coverage](https://raw.githubusercontent.com/kordax/beget-api-mcp-server/badges/.badges/main/coverage.svg)](https://github.com/kordax/beget-api-mcp-server/tree/badges)

[Документация на русском](README.ru.md)

The Russian documentation was translated with AI assistance.

I built this MCP server to manage a Beget hosting account from different MCP clients. Codex is used in some examples, but the server has no dependency on Codex. It works the same way with JetBrains AI Assistant, Claude Desktop, Cursor, VS Code, and other compatible clients.

I intentionally expose a small set of typed tools instead of a universal API proxy. Read operations are marked read-only. Every operation that changes hosting state requires `confirm: true` before the server sends an HTTP request.

## Available tools

Read-only tools:

- `beget_account_info`
- `beget_list_sites`
- `beget_list_domains`
- `beget_get_dns_records`
- `beget_list_cron_jobs`
- `beget_list_backups`
- `beget_site_load`
- `beget_database_load`

Tools that change hosting state:

- `beget_change_dns_records`
- `beget_freeze_site`
- `beget_unfreeze_site`

DNS changes accept the record groups supported by Beget: `A/MX/TXT`, `NS`, `CNAME`, or `DNS/DNS_IP`.

## Architecture

The entry point delegates to `app.Run`, which selects either the MCP server or the credentials command. Both paths resolve their dependencies through Uber Fx. Configuration, the system keyring, HTTP client, Beget adapter, MCP server, and process lifecycle remain separate modules.

The project uses:

- Go 1.26 with the `go1.26.5` toolchain
- Uber Fx 1.24.0
- Testify 1.11.1
- `github.com/kordax/basic-utils/v3` 3.4.0
- `github.com/zalando/go-keyring` 0.2.8
- the official MCP Go SDK

## Transports

The server supports three mutually exclusive transports:

- stdio is the default and can also be selected with `--stdio`
- Streamable HTTP is selected with `--streamable-http`
- legacy SSE is selected with `--sse`

HTTP transports listen on `127.0.0.1:8080` by default and cannot bind to a non-loopback address. The endpoint, session behavior, response mode, and optional bearer authentication have separate flags.

See [the transport guide](docs/transports.md) for every flag and client configuration example.

## Command-line help

Run the binary with `help`, `-h`, or `--help` to see its commands and transport options:

```bash
beget-api-mcp-server help
```

Command-specific help is available without starting the MCP server or accessing the system keyring:

```bash
beget-api-mcp-server help credentials
beget-api-mcp-server help credentials set
beget-api-mcp-server help upgrade
```

## Build and test

```bash
go build -o bin/beget-api-mcp-server ./cmd/beget-api-mcp-server
go vet ./...
go test -race ./...
```

The repository also provides `task verify` for the complete test, coverage, lint, vulnerability, static security, and secret-scanning suite. Run `task tools` once to install its pinned tool versions. The coverage gate requires at least 90%; the current suite covers 91.4% and publishes a badge from the `badges` branch. GitHub Actions runs the same categories of checks and Dependabot monitors Go modules and workflow actions.

Run `task mcp-inspector` to start the pinned official MCP Inspector for interactive protocol and tool testing. This command requires Node.js and npm with `npx`.

## Install on the system

Install the latest release globally for the current Linux or macOS user:

```bash
curl -fsSL https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.ps1 | iex
```

The installer detects the operating system and architecture, verifies the release checksum, and adds the command to the user `PATH`. The complete MCP client setup, manual installation, updating, and removal are in [the installation guide](docs/installation.md).

Update an installed binary in place:

```bash
beget-api-mcp-server upgrade
```

Use `upgrade --check` to compare versions without changing files, or pass a release such as `upgrade v0.3.0`. In an interactive terminal, the command shows a spinner while checking and installing. Self-upgrade verifies the published SHA-256 checksum before atomically replacing the executable.

The running MCP server also performs a lightweight release check on the first tool call after ten minutes without tool calls. If a newer release exists, the tool response includes a notice with the installed and available versions. The server never installs that release automatically, and a failed check does not fail the requested Beget tool.

## Credentials

Enable API access in the Beget control panel and create a dedicated API password. Save it once in the operating system keyring:

```bash
beget-api-mcp-server credentials set --login your-login
```

The command reads the API key from a hidden terminal prompt. A pipe may be used in non-interactive automation. The key is stored by Secret Service on Linux, Keychain on macOS, or Credential Manager on Windows. Verify or remove it without displaying either value:

```bash
beget-api-mcp-server credentials check
beget-api-mcp-server credentials delete
```

MCP clients then need only the executable command:

```toml
[mcp_servers.beget]
command = "beget-api-mcp-server"
```

Stdio is the default transport, so no transport argument is required.

The MCP server starts even when credentials are not configured. Agents can call `beget_auth_status` to detect that state and receive safe setup guidance. Actual Beget tools validate authorization only when called. If the system keyring was temporarily unavailable during startup, the server retries it while credentials are missing. After a successful load, credentials stay in process memory until the server stops, so a temporary later keyring failure does not drop authorization. The installer also provides a `beget-api` Codex skill that teaches this workflow and keeps API keys out of MCP arguments.

`BEGET_API_LOGIN` and `BEGET_API_KEY` remain supported and take precedence over stored values. They are useful in containers, CI, headless Linux sessions without Secret Service, and external password-manager launchers.

The API password is sent to Beget in an HTTPS POST form body. It is not placed in URLs, logs, tool arguments, or tool results. Tests use fake credentials and local HTTP servers, so they never contact a real Beget account.

## API scope

This project intentionally targets the classic Beget Hosting API at `https://api.beget.com/api`. Expanding or changing that upstream API is outside this repository's scope; the server stays a small typed adapter around the supported operations.

Official references:

- [Beget API principles](https://beget.com/ru/kb/api/obshhij-princzip-raboty-s-api)
- [Beget DNS API](https://beget.com/ru/kb/api/funkczii-upravleniya-dns)
- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)

## Author and license

Created by Dmitry Morozov (kordax).

The project is distributed under the MIT License. See [LICENSE](LICENSE).

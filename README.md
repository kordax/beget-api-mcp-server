# Beget API MCP Server

[![Tests](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Tests.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Tests.yml)
[![Lint](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Lint.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Lint.yml)
[![Security](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Security.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Security.yml)
[![Gitleaks](https://github.com/kordax/beget-api-mcp-server/actions/workflows/gitleaks.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/gitleaks.yml)
[![Coverage](https://raw.githubusercontent.com/kordax/beget-api-mcp-server/badges/.badges/main/coverage.svg)](https://github.com/kordax/beget-api-mcp-server/tree/badges)

[Документация на русском](README.ru.md)

The Russian documentation was translated with AI assistance.

I built this MCP server to manage a [Beget hosting](https://beget.com/ru) account from different MCP clients. Codex is used in some examples, but the server has no dependency on Codex. It works the same way with JetBrains AI Assistant, Claude Desktop, Cursor, VS Code, and other compatible clients.

I intentionally expose a small set of typed tools instead of a universal API proxy. Read operations are marked read-only. Every operation that changes hosting state requires `confirm: true` before the server sends an HTTP request.

## Available tools

Read-only tools:

- Account, DNS, domain, FTP, MySQL, site, Cron, backup, mail, and load-statistics queries.
- For example: `beget_list_mailboxes`, `beget_list_ftp_accounts`, `beget_list_mysql_databases`, `beget_list_file_backups`, and `beget_list_cron_jobs`.
- Local authorization, server-capability, and mailbox-password policy checks that do not contact Beget.

Tools that change hosting state:

- All documented mutations in those sections, including `beget_change_mailbox_password`, mailbox creation and forwarding, FTP and MySQL password changes, site and domain management, backup restores, and Cron changes.

The server exposes 68 typed tools in total: 65 fixed Beget endpoints plus the local `beget_auth_status`, `beget_server_capabilities`, and `beget_validate_mailbox_password` tools. It is not a universal API proxy. Every state-changing tool declares accurate destructive and idempotent hints and requires `confirm: true` for a real provider call.

Each input schema contains only the parameters accepted by its Beget method. Required fields, identifiers, enums, ranges, paths, Cron expressions, and incompatible values are checked before an HTTP request. Password fields for managed FTP, MySQL, and mail resources are marked write-only and are never part of result summaries.

Every tool returns the same typed envelope with `success`, `result`, and `errors`. Read operations expose documented Beget fields through operation-specific result models. Mutations return `changed` and typed provider details, including `changed: false` when a request was rejected or its outcome is unknown. Errors use stable categories for validation, authorization, provider rejection, transport failure, confirmation failure, and unknown mutation outcomes, and include a safe next step. Undocumented provider fields are retained in `additional_properties_json` as exact JSON values, while empty lists are always returned as `[]`.

Mailbox passwords contain 6 to 64 characters and use only English letters, digits, and ``.,/<>?;:"'`!@#$%^&*()[]{}_+-=|~``. At least one letter, one digit, and one symbol are required. `beget_validate_mailbox_password` checks a candidate locally without Beget credentials or a network request and returns only `valid` plus stable violation codes and safe messages. The server also applies the same policy before mailbox mutations and returns safe guidance for Beget error 1208 without repeating the password.

Every MCP client receives a short safe-operation workflow during initialization: check authorization when unknown, read current state and real identifiers before a mutation, obtain explicit approval, make one confirmed change, and verify it. The installed Codex skill adds more guidance for tool selection and error handling without duplicating the schemas.

Clients that still cannot choose a category from `tools/list` may read the optional `beget://capabilities` resource. It is a compact local index that groups inspect and change tools by scenario and links each category to the official Beget documentation. Reading it never calls Beget and is not a required step before normal operations. Critical safety rules remain in initialization instructions and tool schemas for clients that do not expose MCP resources.

The operation catalog is the single source for tool registration, endpoint metadata, contract tests, and the capability resource. It was checked against the ten current sections of the official Beget hosting API documentation. The server currently has no compound tools. A compound operation should be added only after a frequent multi-call scenario is demonstrated; it must remain typed, expose its steps, and retain mutation annotations and confirmation instead of hiding a change behind a read-only name.

`beget_server_capabilities` returns the running version, all supported Beget section/method pairs, mutation and per-method idempotency metadata, and explicit support states for dry-run, confirmation tokens, secret references, and staged rotation. It is local and never reports whether credentials are configured or where they are stored.

Every mutation accepts optional `dry_run`. With `dry_run: true` and `confirm: false`, the server validates the schema and known local constraints, checks whether server credentials and confirmation prerequisites are ready, returns `changed: false`, and sends no request to Beget. The result always identifies its scope as `local`; it does not test remote permissions or undocumented provider constraints and never guarantees provider acceptance. A real mutation still requires explicit approval and `confirm: true`.

An ordinary MCP tool call makes at most one Beget HTTP request. The server does not add hidden preflight reads: any read-before-write or verification step remains an explicit agent action. The shared HTTP client reuses connections, propagates MCP cancellation through `context`, and rejects responses larger than 4 MiB without buffering the remainder.

DNS changes accept the record groups supported by Beget: `A/MX/TXT`, `NS`, `CNAME`, or `DNS/DNS_IP`.

## Architecture

The entry point delegates to `app.Run`, which selects either the MCP server or the credentials command. Both paths resolve their dependencies through Uber Fx. Configuration, the persistent credential store, HTTP client, Beget adapter, MCP server, and process lifecycle remain separate modules.

The project uses:

- Go 1.26 with the `go1.26.5` toolchain
- Uber Fx 1.24.0
- Testify 1.11.1
- `github.com/kordax/basic-utils/v3` 3.4.0
- `github.com/zalando/go-keyring` 0.2.8 migrates credentials saved by earlier releases
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

Command-specific help is available without starting the MCP server or accessing the credential store:

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

The repository also provides `task verify` for the complete test, coverage, lint, vulnerability, static security, and secret-scanning suite. Run `task tools` once to install its pinned tool versions. The coverage gate requires at least 90%; the current suite covers 90.7% and publishes a badge from the `badges` branch. GitHub Actions runs the same categories of checks and Dependabot monitors Go modules and workflow actions.

Run `task benchmark` to measure server startup, MCP initialization, `tools/list`, the capability resource, schema generation, tool calls, and local fake-server HTTP round trips. The command reports time, bytes, and allocations without contacting Beget. See the [performance baseline](docs/performance.md) for the current reference run.

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

The running MCP server also starts a lightweight background release check on the first tool call after ten minutes without tool calls. The check never delays the requested Beget operation. If a newer release exists, the first tool response completed after the check includes a notice with the installed and available versions. The server never installs that release automatically, and a failed check does not fail a Beget tool.

## Credentials

Enable API access in the Beget control panel and create a dedicated API password. Save it once in the server's persistent credential store:

```bash
beget-api-mcp-server credentials set --login your-login
```

The command reads the API key from a hidden terminal prompt. A pipe may be used in non-interactive automation. Credentials are written to the standard per-user configuration directory. On Unix systems the server enforces `0700` on its directory and rejects credential files accessible by group or other users; the file itself is created with `0600`. On Windows it uses the current user's AppData directory and inherited user ACL. Verify or remove the values without displaying either one:

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

Every server process reads the same persistent file, so credentials survive restarts and independent MCP processes. Existing Secret Service, Keychain, or Credential Manager entries created by earlier releases are migrated automatically on first use. The MCP server also starts when credentials are not configured: agents can call `beget_auth_status` to receive safe setup guidance, and the server retries the persistent store while credentials are missing. After a successful load, credentials stay cached in process memory until shutdown. Universal safety instructions are sent through MCP initialization. The installer also provides a `beget-api` Codex skill with additional tool-selection and error-handling guidance.

`BEGET_API_LOGIN` and `BEGET_API_KEY` remain supported and take precedence over stored values. They are useful in containers, CI, and external password-manager launchers.

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

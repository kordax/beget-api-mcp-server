# Beget API MCP Server

[Документация на русском](README.ru.md)

I built this local MCP server to manage a Beget hosting account from tools such as Codex without exposing the hosting password to the model. The server is written in Go, composed entirely with Uber Fx, and communicates over stdio.

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

The entry point only starts `app.Module`. Configuration, the HTTP client, the Beget adapter, the MCP server, and process lifecycle are separate Fx modules. This keeps wiring visible and lets `fx.ValidateApp` check the dependency graph in tests.

The project uses:

- Go 1.26 with the `go1.26.5` toolchain
- Uber Fx 1.24.0
- Testify 1.11.1
- `github.com/kordax/basic-utils/v3` 3.4.0
- the official MCP Go SDK

## Build and test

```bash
go build -o bin/beget-api-mcp-server ./cmd/beget-api-mcp-server
go vet ./...
go test -race ./...
```

## Install on the system

For a permanent user-wide installation and global Codex registration, follow [the installation guide](docs/installation.md).

The short version is:

```bash
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Because the repository is private, the first installation also needs GitHub authentication and a `GOPRIVATE` entry. The full guide includes those steps, the keyring check, the global MCP configuration, updating, and removal.

## Credentials

Enable API access in the Beget control panel and create a dedicated API password. The process expects two environment variables:

- `BEGET_API_LOGIN` contains the hosting account login
- `BEGET_API_KEY` contains the dedicated API password

I do not recommend storing the API password in an MCP config. Launch the process through the configured keyring wrapper:

```bash
BEGET_API_LOGIN=your-login codex-keyring run beget-api-key -- /absolute/path/to/bin/beget-api-mcp-server
```

The separator in this command is required by `codex-keyring`.

Example Codex configuration:

```toml
[mcp_servers.beget]
command = "codex-keyring"
args = ["run", "beget-api-key", "--", "/absolute/path/to/bin/beget-api-mcp-server"]
env = { BEGET_API_LOGIN = "your-login" }
```

The API password is sent to Beget in an HTTPS POST form body. It is not placed in URLs, logs, tool arguments, or tool results. Tests use fake credentials and local HTTP servers, so they never contact a real Beget account.

## API scope

This version targets the classic Beget Hosting API at `https://api.beget.com/api`. Beget Cloud uses a different JWT API. I plan to keep that integration in a separate adapter so that credentials and tool contracts do not get mixed together.

Official references:

- [Beget API principles](https://beget.com/ru/kb/api/obshhij-princzip-raboty-s-api)
- [Beget DNS API](https://beget.com/ru/kb/api/funkczii-upravleniya-dns)
- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)

## Author and license

Created by Dmitry Morozov, also known as kordax. Contact: `kordaxmint@gmail.com`.

The project is distributed under the MIT License. See [LICENSE](LICENSE).

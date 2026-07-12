# Beget API MCP Server

A local stdio MCP server for the Beget Hosting API, written in Go and built on the official MCP Go SDK.

The server deliberately exposes typed tools instead of a generic raw API proxy. Read operations are annotated read-only. Every mutating tool is annotated destructive and requires `confirm: true` before any HTTP request is sent.

## Tools

Read-only:

- `beget_account_info`
- `beget_list_sites`
- `beget_list_domains`
- `beget_get_dns_records`
- `beget_list_cron_jobs`
- `beget_list_backups`
- `beget_site_load`
- `beget_database_load`

Mutating:

- `beget_change_dns_records`
- `beget_freeze_site`
- `beget_unfreeze_site`

The mutating tools require `confirm: true`. DNS updates accept the record groups documented by Beget: `A/MX/TXT`, `NS`, `CNAME`, or `DNS/DNS_IP`.

## Build

Requires Go 1.25 or newer.

```bash
go build -o bin/beget-api-mcp-server ./cmd/beget-api-mcp-server
go test -race ./...
```

## Credentials

Beget Hosting API access must first be enabled in the Beget control panel with a dedicated API password.

The process expects:

- `BEGET_API_LOGIN`: hosting account login;
- `BEGET_API_KEY`: dedicated Beget Hosting API password.

Do not store the API password in a config file. Launch the server through the configured keyring wrapper:

```bash
BEGET_API_LOGIN=your-login codex-keyring run beget-api-key -- /absolute/path/to/bin/beget-api-mcp-server
```

Example MCP configuration:

```toml
[mcp_servers.beget]
command = "codex-keyring"
args = ["run", "beget-api-key", "--", "/absolute/path/to/bin/beget-api-mcp-server"]
env = { BEGET_API_LOGIN = "your-login" }
```

The key is sent to Beget in an HTTPS POST form body. It is never put in the request URL or tool output. The server logs only startup/configuration failures without credential values.

## Development

```bash
go fmt ./...
go vet ./...
go test -race ./...
```

Tests use fake credentials and local `httptest` servers; they never call Beget.

## API scope

This project currently targets the classic Beget Hosting API at `https://api.beget.com/api`. Beget Cloud uses a separate JWT API and should be implemented as a separate adapter rather than mixed into these tools.

Official references:

- [Beget API principles](https://beget.com/ru/kb/api/obshhij-princzip-raboty-s-api)
- [Beget DNS API](https://beget.com/ru/kb/api/funkczii-upravleniya-dns)
- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)

# MCP transports

The server supports stdio, Streamable HTTP, and the legacy HTTP with SSE transport. Only one transport can be enabled in a process.

## Transport flags

- No transport flag: use stdio.
- `--stdio`: explicitly use stdio.
- `--streamable-http`: use the current Streamable HTTP transport.
- `--sse`: use the legacy SSE transport from the 2024 MCP specification.

Passing more than one transport flag is an error.

## Stdio

Stdio is the default and the simplest choice for a local MCP client. The client starts the process and exchanges protocol messages through stdin and stdout.

```json
{
  "mcpServers": {
    "beget": {
      "command": "beget-api-mcp-server"
    }
  }
}
```

No arguments are required because stdio is the default.

## Streamable HTTP

Streamable HTTP is the recommended HTTP transport for current MCP clients. Start the server separately:

```bash
beget-api-mcp-server \
  --streamable-http \
  --http-address 127.0.0.1:8080 \
  --http-path /mcp
```

Then point the MCP client to the endpoint:

```json
{
  "mcpServers": {
    "beget": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

Streamable HTTP keeps MCP sessions by default. These optional flags change that behavior:

- `--streamable-session-timeout` defaults to `30m` and closes an idle session after this duration. Use `0` to disable the timeout.
- `--streamable-json-response` returns `application/json` instead of an SSE response stream for requests.
- `--streamable-stateless` creates a temporary session for every request.

The three flags above are valid only together with `--streamable-http`.

## Legacy SSE

The separate SSE transport exists for clients that still implement the 2024 MCP transport. New integrations should use Streamable HTTP.

```bash
beget-api-mcp-server \
  --sse \
  --http-address 127.0.0.1:8080 \
  --http-path /sse
```

Clients with legacy SSE support connect to:

```json
{
  "mcpServers": {
    "beget": {
      "url": "http://127.0.0.1:8080/sse"
    }
  }
}
```

## Shared HTTP flags

- `--http-address` defaults to `127.0.0.1:8080` and controls the TCP address used by Streamable HTTP or SSE.
- `--http-path` defaults to `/mcp` or `/sse` and controls the endpoint path for the selected HTTP transport.
- `--http-auth` requires `Authorization: Bearer ...` on every request. The token is read from `BEGET_MCP_HTTP_TOKEN` and must contain at least 32 characters.

The address must use `localhost`, `127.0.0.1`, or `::1`. A wildcard or external address is rejected before the HTTP server starts.

## HTTP bearer authentication

Enable built-in authentication without placing the token in process arguments:

```bash
export BEGET_MCP_HTTP_TOKEN="replace-with-at-least-32-random-characters"
beget-api-mcp-server --streamable-http --http-auth
```

The client must send the matching header. Clients commonly represent it like this:

```json
{
  "mcpServers": {
    "beget": {
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {
        "Authorization": "Bearer replace-with-at-least-32-random-characters"
      }
    }
  }
}
```

Use the client's protected-secret feature when it supports one. The server compares tokens without early-exit string comparison and never includes the configured token in errors or logs.

## JetBrains and GoLand

GoLand supports stdio, Streamable HTTP, and legacy SSE. For stdio, use the JSON command configuration from the installation guide. For Streamable HTTP, choose the HTTP connection option and enter `http://127.0.0.1:8080/mcp`. Select the `Global` server level if the connection should be shared by every project.

## Security boundary

The MCP tools can change DNS and site state. The built-in HTTP listener is therefore loopback-only. Cross-origin and DNS-rebinding protections remain enabled, and bearer authentication can be enabled independently for Streamable HTTP or SSE.

To access the server from another machine, keep it on loopback and use an authenticated reverse proxy, VPN, or SSH tunnel. Built-in bearer authentication is an additional layer and does not replace transport encryption or the loopback restriction.

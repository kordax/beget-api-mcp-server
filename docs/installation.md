# System installation

This guide installs the server once for the current user and connects it to an MCP client. Codex is one example. The default setup uses stdio. Streamable HTTP and legacy SSE are documented in [the transport guide](transports.md).

## Requirements

Prebuilt release archives do not require Go. Choose the archive for Linux, macOS, or Windows and the `amd64` or `arm64` architecture from [GitHub Releases](https://github.com/kordax/beget-api-mcp-server/releases). Every release includes `checksums.txt` for SHA-256 verification.

Installing from source requires Go 1.26.5:

```bash
go version
```

## First installation

Extract the release archive and move `beget-api-mcp-server` or `beget-api-mcp-server.exe` to a directory listed in `PATH`.

Alternatively, install the current tagged version with Go:

```bash
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Verify the file without starting the MCP server:

```bash
test -x "$HOME/.local/bin/beget-api-mcp-server"
```

The binary can also live outside `PATH` because MCP configurations accept its absolute path.

## Save Beget credentials

Enable Hosting API access in the Beget control panel and create a dedicated API password. Save the login and password in the operating system keyring:

```bash
beget-api-mcp-server credentials set --login your-beget-login
```

The API key is read from a hidden prompt and is never accepted as a command-line argument. The command uses Secret Service on Linux, Keychain on macOS, and Credential Manager on Windows.

Verify or remove the stored credentials without displaying them:

```bash
beget-api-mcp-server credentials check
beget-api-mcp-server credentials delete
```

Linux desktop sessions normally provide Secret Service automatically. For a headless server without a Secret Service daemon, use the environment fallback described below.

## Universal MCP contract

The server uses stdio by default and loads credentials from the system keyring. It does not call Codex APIs and does not require a client-specific launcher. A typical JSON-based MCP client can start it like this:

```json
{
  "mcpServers": {
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"]
    }
  }
}
```

Replace `/home/your-user` with the real path. This format works for clients that use the common `mcpServers` JSON structure.

## Codex example

Codex uses TOML instead of the JSON structure above. Add this to `~/.codex/config.toml`:

```toml
[mcp_servers.beget]
command = "/home/your-user/.local/bin/beget-api-mcp-server"
```

Restart Codex after changing the configuration. This is only a Codex-specific representation of the same universal command and environment contract.

## JetBrains and GoLand example

GoLand and the other current JetBrains IDEs can start local MCP servers over stdio. Open `Settings | Tools | AI Assistant | Model Context Protocol (MCP)`, click `Add`, choose the JSON configuration option for STDIO, and set the server level to `Global`.

Add the Beget server using this JSON configuration:

```json
{
  "mcpServers": {
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"]
    }
  }
}
```

Click `OK`, then `Apply`. The status column should show a successful connection. Its tools button should list the Beget tools. If automatic startup is disabled, enable the new server manually or use `Reconnect`.

To expose custom MCP servers to Junie, open `Settings | Tools | AI Assistant | Agents` and enable `Pass custom MCP servers`.

If the process does not start, open `Help | Show Log in Explorer`, enter the `mcp` directory, and inspect the Beget server log. A wrong executable path is the most common problem when an IDE is started from the desktop.

## Secret storage

The built-in system keyring is the recommended local setup. Environment variables take precedence when both are present:

```bash
BEGET_API_LOGIN=your-beget-login \
BEGET_API_KEY=your-api-password \
beget-api-mcp-server --stdio
```

This fallback is intended for containers, CI, headless systems, and external password managers. Do not place the API key in command-line arguments or commit it to an MCP configuration.

## Install from a local clone

This path is useful while developing the server and requires Git:

```bash
git clone https://github.com/kordax/beget-api-mcp-server.git
cd beget-api-mcp-server
GOBIN="$HOME/.local/bin" go install ./cmd/beget-api-mcp-server
```

The client configuration does not need to change because the installed binary path stays the same.

## Update

Download and replace the binary with a newer release archive, or repeat the Go installation command:

```bash
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Restart or reconnect the MCP server in the client so it starts the new binary.

## Remove

Run `beget-api-mcp-server credentials delete`, remove the Beget server entry from the MCP client, then delete the installed binary.

## Security notes

The API key is sent to Beget only in an HTTPS POST form body. The server does not place it in URLs, logs, MCP arguments, or MCP results. The way the key reaches the process is controlled by the MCP client or the chosen secret manager.

# System installation

This guide installs the server once for the current user and connects it to an MCP client. Codex is one example. The default setup uses stdio. Streamable HTTP and legacy SSE are documented in [the transport guide](transports.md).

## Requirements

Prebuilt release archives do not require Go. Choose the archive for Linux, macOS, or Windows and the `amd64` or `arm64` architecture from [GitHub Releases](https://github.com/kordax/beget-api-mcp-server/releases). Every release includes `checksums.txt` for SHA-256 verification.

Installing from source requires Go 1.26.5:

```bash
go version
```

## First installation

Install the latest release globally for the current Linux or macOS user:

```bash
curl -fsSL https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.ps1 | iex
```

The installer detects the operating system and architecture, downloads the matching release, verifies its SHA-256 checksum, and adds `beget-api-mcp-server` to the user `PATH`. It never invokes `sudo`. Restart the terminal after the first installation, then verify the command:

```bash
command -v beget-api-mcp-server
```

Set `BEGET_MCP_VERSION` to install a specific version or `BEGET_MCP_INSTALL_DIR` to choose another directory. Prebuilt archives and checksums remain available on the release page for manual installation.

The release also installs the `beget-api` skill into Codex. Other MCP clients receive the same authorization guidance through the `beget_auth_status` tool.

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

Credentials are optional during MCP startup. Without them, the server still connects to the client and exposes `beget_auth_status`; an actual Beget operation returns a concise authorization error until credentials are supplied and the server is reconnected.

## Universal MCP contract

The server uses stdio by default and loads credentials from the system keyring. It does not call Codex APIs and does not require a client-specific launcher. A typical JSON-based MCP client can start it like this:

```json
{
  "mcpServers": {
    "beget": {
      "command": "beget-api-mcp-server"
    }
  }
}
```

Stdio is the default, so the configuration needs neither an absolute path nor transport arguments. This format works for clients that use the common `mcpServers` JSON structure.

## Codex example

Codex uses TOML instead of the JSON structure above. Add this to `~/.codex/config.toml`:

```toml
[mcp_servers.beget]
command = "beget-api-mcp-server"
```

Restart Codex after changing the configuration. This is only a Codex-specific representation of the same universal command and environment contract.

## JetBrains and GoLand example

GoLand and the other current JetBrains IDEs can start local MCP servers over stdio. Open `Settings | Tools | AI Assistant | Model Context Protocol (MCP)`, click `Add`, choose the JSON configuration option for STDIO, and set the server level to `Global`.

Add the Beget server using this JSON configuration:

```json
{
  "mcpServers": {
    "beget": {
      "command": "beget-api-mcp-server"
    }
  }
}
```

Click `OK`, then `Apply`. The status column should show a successful connection. Its tools button should list the Beget tools. If automatic startup is disabled, enable the new server manually or use `Reconnect`.

To expose custom MCP servers to Junie, open `Settings | Tools | AI Assistant | Agents` and enable `Pass custom MCP servers`.

If the process does not start, restart the IDE so it receives the updated user `PATH`. Then open `Help | Show Log in Explorer`, enter the `mcp` directory, and inspect the Beget server log.

## Secret storage

The built-in system keyring is the recommended local setup. Environment variables take precedence when both are present:

```bash
BEGET_API_LOGIN=your-beget-login \
BEGET_API_KEY=your-api-password \
beget-api-mcp-server --stdio
```

This fallback is intended for containers, CI, headless systems, and external password managers. Do not place the API key in command-line arguments or commit it to an MCP configuration.

If the keyring is temporarily unavailable when the MCP process starts, the server retries it on later tool calls while credentials are still missing. Once loaded, credentials remain in process memory until shutdown. Running `credentials set` can therefore repair an already running unconfigured server on its next request without exposing the API key through MCP.

## Install from a local clone

This path is useful while developing the server and requires Git:

```bash
git clone https://github.com/kordax/beget-api-mcp-server.git
cd beget-api-mcp-server
go install ./cmd/beget-api-mcp-server
```

The client configuration does not need to change because the installed binary path stays the same.

## Update

Use the built-in self-updater:

```bash
beget-api-mcp-server upgrade
```

Check without installing, or select a specific release:

```bash
beget-api-mcp-server upgrade --check
beget-api-mcp-server upgrade v0.3.0
```

The updater detects the current platform, downloads the matching release, verifies its SHA-256 entry, and replaces the executable atomically. Interactive terminals show a spinner while the command checks for a release and while it installs an update. Redirected output and CI logs remain plain text without terminal control sequences. The updater preserves the previous Windows executable until replacement succeeds. Set `GH_TOKEN` or `GITHUB_TOKEN` only when downloading from a private GitHub repository; public releases need no token. Restart or reconnect the MCP server after updating.

While running, the MCP server checks the latest release on the first tool call after ten minutes without tool calls. An available release is reported in that tool response, but is never installed automatically. A timeout or release-service error is logged and does not change the Beget tool result.

Running the one-line installer again remains a supported recovery path.

## Remove

Run `beget-api-mcp-server credentials delete`, remove the Beget server entry from the MCP client, then delete the command shown by `command -v beget-api-mcp-server`. The installer prints the same location during installation.

## Security notes

The API key is sent to Beget only in an HTTPS POST form body. The server does not place it in URLs, logs, MCP arguments, or MCP results. The way the key reaches the process is controlled by the MCP client or the chosen secret manager.

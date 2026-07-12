# System installation

This guide installs the server once for the current user and connects it to an MCP client. Codex is one example. The default setup uses stdio. Streamable HTTP and legacy SSE are documented in [the transport guide](transports.md).

## Requirements

Install these programs first:

- Go 1.26.5
- Git
- GitHub CLI

Check the versions:

```bash
go version
git --version
gh --version
```

## First installation

The GitHub repository is private. Configure Git to use the existing GitHub CLI session and tell Go not to use the public module proxy for kordax repositories:

```bash
gh auth status
gh auth setup-git
go env -w 'GOPRIVATE=github.com/kordax/*'
```

Install the binary into the current user's executable directory:

```bash
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Verify the file without starting the MCP server:

```bash
test -x "$HOME/.local/bin/beget-api-mcp-server"
```

The binary can live outside `PATH` because MCP configurations can use its absolute path.

## Universal MCP contract

The server uses stdio and reads two environment variables:

- `BEGET_API_LOGIN` contains the hosting account login
- `BEGET_API_KEY` contains the dedicated Hosting API password

It does not call Codex APIs and does not require a Codex-specific launcher. A typical JSON-based MCP client can start it like this:

```json
{
  "mcpServers": {
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"],
      "env": {
        "BEGET_API_LOGIN": "your-beget-login",
        "BEGET_API_KEY": "your-api-password"
      }
    }
  }
}
```

Replace `/home/your-user`, the login, and the API password. This format works for clients that use the common `mcpServers` JSON structure.

## Codex example

Codex uses TOML instead of the JSON structure above. Add this to `~/.codex/config.toml`:

```toml
[mcp_servers.beget]
command = "/home/your-user/.local/bin/beget-api-mcp-server"
env = { BEGET_API_LOGIN = "your-beget-login", BEGET_API_KEY = "your-api-password" }
```

Restart Codex after changing the configuration. This is only a Codex-specific representation of the same universal command and environment contract.

## JetBrains and GoLand example

GoLand and the other current JetBrains IDEs can start local MCP servers over stdio. Open `Settings | Tools | AI Assistant | Model Context Protocol (MCP)`, click `Add`, choose the JSON configuration option for STDIO, and set the server level to `Global`.

This example keeps an existing Gortex setup and adds Beget next to it:

```json
{
  "mcpServers": {
    "gortex": {
      "command": "gortex",
      "args": [
        "mcp",
        "--proxy"
      ]
    },
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"],
      "env": {
        "BEGET_API_LOGIN": "your-beget-login",
        "BEGET_API_KEY": "your-api-password"
      }
    }
  }
}
```

Click `OK`, then `Apply`. The status column should show a successful connection. Its tools button should list the Beget tools. If automatic startup is disabled, enable the new server manually or use `Reconnect`.

To expose custom MCP servers to Junie, open `Settings | Tools | AI Assistant | Agents` and enable `Pass custom MCP servers`.

If the process does not start, open `Help | Show Log in Explorer`, enter the `mcp` directory, and inspect the Beget server log. A wrong executable path is the most common problem when an IDE is started from the desktop.

## Secret storage

Putting `BEGET_API_KEY` directly in a client configuration is the most compatible option, but it stores the password as plain text. Prefer a protected secret feature provided by the MCP client when one is available. A password-manager launcher that injects the environment variable only into the child process is another good option.

`codex-keyring` is a custom utility from my local environment. It is not distributed with this project and is not required. My local Codex configuration uses it like this:

```toml
[mcp_servers.beget]
command = "codex-keyring"
args = ["run", "beget-api-key", "--", "/home/your-user/.local/bin/beget-api-mcp-server"]
env = { BEGET_API_LOGIN = "your-beget-login" }
```

Other users should replace it with their own password manager or use the direct environment configuration.

## Install from a local clone

This path is useful while developing the server:

```bash
git clone git@github.com:kordax/beget-api-mcp-server.git
cd beget-api-mcp-server
GOBIN="$HOME/.local/bin" go install ./cmd/beget-api-mcp-server
```

The client configuration does not need to change because the installed binary path stays the same.

## Update

Run the installation command again:

```bash
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Restart or reconnect the MCP server in the client so it starts the new binary.

## Remove

Remove the Beget server entry from the MCP client, then delete `~/.local/bin/beget-api-mcp-server`.

## Security notes

The API key is sent to Beget only in an HTTPS POST form body. The server does not place it in URLs, logs, MCP arguments, or MCP results. The way the key reaches the process is controlled by the MCP client or the chosen secret manager.

# System installation

This guide installs the server once for the current Linux user and makes it available to every Codex project. It does not require root access.

## Requirements

Install these programs first:

- Go 1.26.5
- Git
- GitHub CLI
- `codex-keyring`

Check the versions:

```bash
go version
git --version
gh --version
codex-keyring check beget-api-key
```

The last command only verifies that the alias exists. It does not print the API password.

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

The binary can live outside `PATH` because the MCP configuration below uses its absolute path. If you also want to run it by name, add `~/.local/bin` to your shell `PATH`.

## Global Codex configuration

Open `~/.codex/config.toml` and add:

```toml
[mcp_servers.beget]
command = "codex-keyring"
args = ["run", "beget-api-key", "--", "/home/your-user/.local/bin/beget-api-mcp-server"]
env = { BEGET_API_LOGIN = "your-beget-login" }
```

Replace `/home/your-user` and `your-beget-login` with real values. Keep the API password out of this file. The separator in `args` is required by `codex-keyring`.

Restart Codex after changing the configuration. The Beget tools should then be available from every project.

## Install from a local clone

This path is useful while developing the server:

```bash
git clone git@github.com:kordax/beget-api-mcp-server.git
cd beget-api-mcp-server
GOBIN="$HOME/.local/bin" go install ./cmd/beget-api-mcp-server
```

The global Codex configuration does not need to change because the installed binary path stays the same.

## Update

Run the installation command again:

```bash
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Restart Codex so that it starts the new binary.

## Remove

Remove the `mcp_servers.beget` section from `~/.codex/config.toml`, then delete `~/.local/bin/beget-api-mcp-server`. The keyring entry can remain if it is used elsewhere.

## Security notes

The server reads the API password only from `BEGET_API_KEY`. The global Codex configuration starts it through `codex-keyring`, which injects the key only into that child process. Do not replace this with a plain `env` entry containing the password.

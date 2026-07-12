# Agent Notes

Go MCP server for the Beget Hosting API. Keep the public surface typed: do not add a raw or arbitrary API-call tool.

## Security

- Never print, log, persist, or return `BEGET_API_KEY`.
- Read credentials only from `BEGET_API_LOGIN` and `BEGET_API_KEY`.
- Use `POST` form bodies for Beget authentication; never place credentials in URLs.
- Mutating tools must carry accurate MCP destructive annotations and require an explicit `confirm: true` argument.
- Tests use fake credentials and local HTTP servers only. Never call the live Beget API from tests.

## Workflow

- Run `go fmt ./...`, `go vet ./...`, and `go test -race ./...` before committing.
- Commit completed changes automatically using lowercase subjects in the form `<module> <what was done>` and push after verification.

# Agent Notes

Go MCP server for the Beget Hosting API. Keep the public surface typed: do not add a raw or arbitrary API-call tool.

## Go Baseline

- Use Go 1.26 with the exact `go1.26.5` toolchain.
- Compose the complete application through named Uber Fx modules. Keep `main` limited to starting the root module.
- Write tests with Testify. Use `require` for prerequisites and `assert` for independent checks.
- Prefer `github.com/kordax/basic-utils/v3` version 3.4.0 when one of its packages directly replaces local utility code. Do not force it into unrelated logic.
- Keep the Dmitry Morozov copyright header and SPDX MIT identifier in Go source files.

## Security

- Never print, log, persist, or return `BEGET_API_KEY`.
- Read credentials only from `BEGET_API_LOGIN` and `BEGET_API_KEY`.
- Use `POST` form bodies for Beget authentication; never place credentials in URLs.
- Mutating tools must carry accurate MCP destructive annotations and require an explicit `confirm: true` argument.
- Tests use fake credentials and local HTTP servers only. Never call the live Beget API from tests.

## Workflow

- Run `go fmt ./...`, `go vet ./...`, and `go test -race ./...` before committing.
- Commit completed changes automatically using lowercase subjects in the form `<module> <what was done>` and push after verification.

## Documentation

- Write in a direct, human voice that sounds like the project author.
- Avoid two consecutive hyphens as punctuation. They are allowed only where command-line syntax requires them.
- Keep English and Russian documentation aligned when behavior, setup, or tool lists change.

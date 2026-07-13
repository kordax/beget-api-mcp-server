---
name: beget-api
description: Safely inspect and manage Beget hosting through the beget MCP server. Use for Beget accounts, sites, domains, DNS, cron jobs, backups, load, or authorization diagnostics.
---

# Beget API

Use the `beget` MCP server for Beget hosting operations.

## Authorization

Call `beget_auth_status` before the first Beget operation when authorization state is unknown.

If credentials are not configured:

1. Tell the user that the MCP server itself is healthy and only Beget authorization is missing.
2. Recommend setting `BEGET_API_LOGIN` and `BEGET_API_KEY` in the MCP server environment, or running `beget-api-mcp-server credentials set --login <login>`. The running server retries stored credentials automatically; reconnect only if environment variables changed.
3. Never request, echo, log, or persist an API key yourself, and never pass it as an MCP tool argument. Only the server's interactive `credentials set` command may write it to the protected persistent store.
4. Never invent credentials or retry an unauthorized operation repeatedly.

The server may start without credentials. Treat an authorization error from a Beget tool as a configuration request, not as an MCP transport failure. Once `credentials set` succeeds, the same user's independent server processes load the protected persistent credential file automatically.

Use `beget-api-mcp-server upgrade --check` to inspect release availability and `beget-api-mcp-server upgrade` only when the user asks to update the local server. Reconnect the MCP client afterward.

The server may append a newer-version notice to a tool response after an idle period. Treat it as informational and never run the upgrade unless the user asks.

## Choosing tools

Use the MCP tool schema as the authority for argument names, required fields, enums, and limits. Never guess an argument or add a field from a different tool.

Choose the narrowest matching category:

- account and authorization
- sites, domains, subdomains, PHP, and DNS
- backups and load statistics
- FTP and MySQL
- mail and Cron

Use list tools to discover current resources and identifiers. Similar identifiers are not interchangeable: a site ID, domain ID, subdomain ID, Cron row number, backup ID, database suffix, and FTP suffix belong to different operations.

## Read before write

For every hosting change:

1. Read the current state with the matching read-only tool.
2. Obtain exact resource identifiers and allowed values from that result. Never invent them.
3. Describe the target, current state, requested state, and important side effects to the user.
4. Obtain explicit approval for that exact change in the current conversation.
5. Call one mutating tool with `confirm: true`.
6. Verify the result with the matching read-only tool when one exists.

Do not reuse confirmation after the target or requested state changes. Do not make an unrelated change while verifying another one.

## Errors and unknown outcomes

Treat a schema or validation error as a request to inspect the tool schema and correct the named field. Do not retry with guessed fields or values.

Treat an authorization error as a configuration request. Stop after explaining the safe setup path; do not retry repeatedly.

Return an upstream Beget rejection with its safe message and let the user decide whether to change the request.

Never retry a mutating call automatically after a timeout, disconnect, or cancelled request. Its outcome may be unknown. Read the current state first, then ask the user before any further mutation.

## Secrets

The Beget API key is only a server credential. Never request, echo, log, persist, generate, or pass it as an MCP tool argument.

Some explicit account operations accept a password for an FTP account, MySQL access, or mailbox. Use such a value only for the exact password operation requested by the user. Never confuse it with the Beget API key, reuse it elsewhere, or repeat it in summaries and error reports.

Do not broaden the requested operation. Return Beget errors without exposing secret values.

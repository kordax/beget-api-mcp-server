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
2. Recommend setting `BEGET_API_LOGIN` and `BEGET_API_KEY` in the MCP server environment, or running `beget-api-mcp-server credentials set --login <login>` followed by reconnecting the MCP server.
3. Never request, echo, log, persist, or pass an API key as an MCP tool argument.
4. Never invent credentials or retry an unauthorized operation repeatedly.

The server may start without credentials. Treat an authorization error from a Beget tool as a configuration request, not as an MCP transport failure.

Use `beget-api-mcp-server upgrade --check` to inspect release availability and `beget-api-mcp-server upgrade` only when the user asks to update the local server. Reconnect the MCP client afterward.

## Operations

Prefer read-only tools for inspection. Before a mutating tool, describe the intended change and obtain explicit user confirmation. Set `confirm` only after that confirmation.

Do not broaden the requested operation. Return Beget errors without exposing secret values.

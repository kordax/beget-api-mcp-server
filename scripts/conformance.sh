#!/bin/sh

# Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
# SPDX-License-Identifier: MIT

set -eu

conformance_version="${1:?conformance runner version is required}"
project_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/beget-api-mcp-conformance.XXXXXX")"
server_pid=""

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi

  case "$temporary" in
    "${TMPDIR:-/tmp}"/beget-api-mcp-conformance.*) rm -rf -- "$temporary" ;;
  esac
}
trap cleanup EXIT HUP INT TERM

need() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'conformance: required command not found: %s\n' "$1" >&2
    exit 1
  }
}

need go
need npx
need awk
need mktemp

cd "$project_root"
go build -o "$temporary/beget-api-mcp-server" ./cmd/beget-api-mcp-server
"$temporary/beget-api-mcp-server" \
  --streamable-http \
  --http-address 127.0.0.1:0 \
  >"$temporary/server.stdout" \
  2>"$temporary/server.stderr" &
server_pid=$!

endpoint=""
attempt=0
while [ "$attempt" -lt 50 ]; do
  endpoint="$(awk '/MCP streamable-http transport listening on / { print $NF; exit }' "$temporary/server.stderr")"
  [ -n "$endpoint" ] && break

  if ! kill -0 "$server_pid" 2>/dev/null; then
    printf 'conformance: server stopped before becoming ready\n' >&2
    sed -n '1,120p' "$temporary/server.stderr" >&2
    exit 1
  fi

  attempt=$((attempt + 1))
  sleep 0.1
done

if [ -z "$endpoint" ]; then
  printf 'conformance: server did not become ready\n' >&2
  sed -n '1,120p' "$temporary/server.stderr" >&2
  exit 1
fi

for scenario in \
  server-initialize \
  ping \
  tools-list \
  resources-list \
  dns-rebinding-protection
do
  if ! npx -y "@modelcontextprotocol/conformance@$conformance_version" server \
    --url "$endpoint" \
    --scenario "$scenario" \
    --spec-version 2025-11-25 \
    --output-dir "$temporary/results/$scenario"
  then
    printf 'conformance: scenario failed: %s\n' "$scenario" >&2
    sed -n '1,120p' "$temporary/server.stderr" >&2
    exit 1
  fi
done

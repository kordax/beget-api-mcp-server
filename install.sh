#!/bin/sh

# Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
# SPDX-License-Identifier: MIT

set -eu

repository="kordax/beget-api-mcp-server"
binary="beget-api-mcp-server"
install_dir="${BEGET_MCP_INSTALL_DIR:-$HOME/.local/bin}"
version="${BEGET_MCP_VERSION:-latest}"
release_endpoint="${BEGET_MCP_RELEASE_ENDPOINT:-}"

fail() {
  printf 'beget-api-mcp-server installer: %s\n' "$1" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

detect_target() {
  case "$(uname -s)" in
    Linux) os="linux" ;;
    Darwin) os="darwin" ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac

  case "$(uname -m)" in
    x86_64 | amd64) arch="amd64" ;;
    arm64 | aarch64) arch="arm64" ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

resolve_version() {
  if [ "$version" = "latest" ]; then
    release_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repository/releases/latest")"
    version="${release_url##*/}"
  fi

  case "$version" in
    v[0-9]*) ;;
    [0-9]*) version="v$version" ;;
    *) fail "invalid release version: $version" ;;
  esac
}

verify_checksum() {
  expected="$(awk -v name="$archive" '$2 == name || $2 == "./" name { print $1; exit }' "$temporary/checksums.txt")"
  [ -n "$expected" ] || fail "checksum is missing for $archive"

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$temporary/$archive" | awk '{ print $1 }')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$temporary/$archive" | awk '{ print $1 }')"
  else
    fail "sha256sum or shasum is required"
  fi

  [ "$actual" = "$expected" ] || fail "checksum verification failed for $archive"
}

add_to_path() {
  case ":$PATH:" in
    *":$install_dir:"*) return ;;
  esac

  case "${SHELL:-}" in
    */zsh) shell_rc="$HOME/.zshrc" ;;
    */bash) shell_rc="$HOME/.bashrc" ;;
    *) shell_rc="$HOME/.profile" ;;
  esac

  path_line='export PATH="$HOME/.local/bin:$PATH"'
  if [ "$install_dir" != "$HOME/.local/bin" ]; then
    path_line="export PATH=\"$install_dir:\$PATH\""
  fi

  if [ ! -f "$shell_rc" ] || ! grep -F "$path_line" "$shell_rc" >/dev/null 2>&1; then
    printf '\n%s\n' "$path_line" >>"$shell_rc"
  fi

  printf 'Added %s to PATH in %s. Restart the terminal before first use.\n' "$install_dir" "$shell_rc"
}

main() {
  need curl
  need tar
  need awk
  need mktemp
  need install
  detect_target
  resolve_version

  temporary="$(mktemp -d "${TMPDIR:-/tmp}/beget-api-mcp-server.XXXXXX")"
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM

  archive="${binary}_${version}_${os}_${arch}.tar.gz"
  release_base="${release_endpoint:-https://github.com/$repository/releases/download/$version}"
  curl -fsSL "$release_base/$archive" -o "$temporary/$archive"
  curl -fsSL "$release_base/checksums.txt" -o "$temporary/checksums.txt"
  verify_checksum

  tar -xzf "$temporary/$archive" -C "$temporary"
  source_binary="$temporary/${binary}_${version}_${os}_${arch}/$binary"
  [ -f "$source_binary" ] || fail "release archive does not contain $binary"

  mkdir -p "$install_dir"
  install -m 0755 "$source_binary" "$install_dir/$binary"
  add_to_path

  printf 'Installed %s %s to %s\n' "$binary" "$version" "$install_dir/$binary"
  printf 'Configure MCP clients with command: %s\n' "$binary"
}

main "$@"

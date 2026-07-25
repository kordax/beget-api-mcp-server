#!/bin/sh

# Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
# SPDX-License-Identifier: MIT

set -eu

project_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
installer="$project_root/install.sh"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/beget-api-installer-test.XXXXXX")"
original_path="$PATH"

cleanup() {
  case "$test_root" in
    "${TMPDIR:-/tmp}"/beget-api-installer-test.*) rm -rf -- "$test_root" ;;
  esac
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'installer test: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  expected="$1"
  file="$2"
  grep -F "$expected" "$file" >/dev/null 2>&1 || fail "$file does not contain: $expected"
}

fake_bin="$test_root/fake-bin"
mkdir -p "$fake_bin"

cat >"$fake_bin/uname" <<'EOF'
#!/bin/sh
set -eu
case "${1:-}" in
  -s) printf '%s\n' "${TEST_UNAME_S:?}" ;;
  -m) printf '%s\n' "${TEST_UNAME_M:?}" ;;
  *) exit 2 ;;
esac
EOF

cat >"$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    -w)
      shift 2
      ;;
    -*) shift ;;
    *)
      url="$1"
      shift
      ;;
  esac
done

case "$url" in
  https://github.com/*/releases/latest)
    printf 'https://github.com/kordax/beget-api-mcp-server/releases/tag/%s' "${TEST_LATEST_VERSION:?}"
    ;;
  file://*)
    [ -n "$output" ] || exit 2
    cp "${url#file://}" "$output"
    ;;
  *)
    printf 'unexpected curl URL: %s\n' "$url" >&2
    exit 2
    ;;
esac
EOF

chmod 0755 "$fake_bin/uname" "$fake_bin/curl"

create_release() {
  release_dir="$1"
  version="$2"
  os="$3"
  arch="$4"
  bundle="beget-api-mcp-server_${version}_${os}_${arch}"
  stage="$test_root/stage/$bundle"
  archive="$bundle.tar.gz"

  mkdir -p "$release_dir" "$stage/skills/beget-api"
  printf '#!/bin/sh\nprintf "fixture %s %s\\n"\n' "$os" "$arch" >"$stage/beget-api-mcp-server"
  chmod 0755 "$stage/beget-api-mcp-server"
  printf '# Fixture skill for %s %s\n' "$os" "$arch" >"$stage/skills/beget-api/SKILL.md"
  tar -czf "$release_dir/$archive" -C "$test_root/stage" "$bundle"

  if command -v sha256sum >/dev/null 2>&1; then
    checksum="$(sha256sum "$release_dir/$archive" | awk '{ print $1 }')"
  elif command -v shasum >/dev/null 2>&1; then
    checksum="$(shasum -a 256 "$release_dir/$archive" | awk '{ print $1 }')"
  else
    fail "sha256sum or shasum is required"
  fi
  printf '%s  %s\n' "$checksum" "$archive" >"$release_dir/checksums.txt"
}

success_root="$test_root/success"
success_release="$success_root/release"
success_home="$success_root/home"
success_install="$success_root/custom-bin"
success_codex="$success_root/codex"
mkdir -p "$success_home"
create_release "$success_release" v9.8.7 darwin arm64

run_success_installer() {
  PATH="$fake_bin:$original_path" \
  HOME="$success_home" \
  SHELL=/bin/bash \
  CODEX_HOME="$success_codex" \
  BEGET_MCP_INSTALL_DIR="$success_install" \
  BEGET_MCP_VERSION=latest \
  BEGET_MCP_RELEASE_ENDPOINT="file://$success_release" \
  TEST_LATEST_VERSION=v9.8.7 \
  TEST_UNAME_S=Darwin \
  TEST_UNAME_M=arm64 \
  sh "$installer" >"$success_root/stdout" 2>"$success_root/stderr"
}

run_success_installer
test -x "$success_install/beget-api-mcp-server" || fail "installed binary is missing or not executable"
assert_contains 'fixture darwin arm64' "$success_install/beget-api-mcp-server"
test -f "$success_codex/skills/beget-api/SKILL.md" || fail "Codex skill was not installed"
assert_contains 'Fixture skill for darwin arm64' "$success_codex/skills/beget-api/SKILL.md"
expected_path_line="export PATH=\"$success_install:\$PATH\""
assert_contains "$expected_path_line" "$success_home/.bashrc"
assert_contains "Installed beget-api-mcp-server v9.8.7" "$success_root/stdout"

run_success_installer
path_line_count="$(grep -Fxc "$expected_path_line" "$success_home/.bashrc")"
[ "$path_line_count" -eq 1 ] || fail "installer added the PATH entry more than once"

failure_root="$test_root/checksum-failure"
failure_release="$failure_root/release"
failure_home="$failure_root/home"
failure_install="$failure_root/bin"
failure_codex="$failure_root/codex"
mkdir -p "$failure_home"
create_release "$failure_release" v0.7.0 linux amd64
failure_archive="beget-api-mcp-server_v0.7.0_linux_amd64.tar.gz"
printf '%064d  %s\n' 0 "$failure_archive" >"$failure_release/checksums.txt"

if PATH="$fake_bin:$original_path" \
  HOME="$failure_home" \
  SHELL=/bin/bash \
  CODEX_HOME="$failure_codex" \
  BEGET_MCP_INSTALL_DIR="$failure_install" \
  BEGET_MCP_VERSION=0.7.0 \
  BEGET_MCP_RELEASE_ENDPOINT="file://$failure_release" \
  TEST_UNAME_S=Linux \
  TEST_UNAME_M=x86_64 \
  sh "$installer" >"$failure_root/stdout" 2>"$failure_root/stderr"
then
  fail "installer accepted an invalid checksum"
fi

assert_contains "checksum verification failed for $failure_archive" "$failure_root/stderr"
test ! -e "$failure_install/beget-api-mcp-server" || fail "binary was installed after checksum failure"
test ! -e "$failure_codex/skills/beget-api/SKILL.md" || fail "skill was installed after checksum failure"

printf 'POSIX installer behavioral tests passed\n'

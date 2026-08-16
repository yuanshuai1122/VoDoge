#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export VODOGE_INSTALL_SOURCE_ONLY=1
# shellcheck source=install.sh
source "${ROOT}/scripts/install.sh"
DOCKER_BIN="$(type -P docker || true)"

fail() {
	printf 'install_test: %s\n' "$*" >&2
	exit 1
}

assert_eq() {
	local expected="$1" actual="$2" label="$3"
	if [[ "$actual" != "$expected" ]]; then
		printf 'install_test: %s\nexpected: %q\nactual:   %q\n' "$label" "$expected" "$actual" >&2
		exit 1
	fi
}

file_mode() {
	if stat -c '%a' "$1" >/dev/null 2>&1; then
		stat -c '%a' "$1"
	else
		stat -f '%Lp' "$1"
	fi
}

TEST_DIR="$(mktemp -d "${TMPDIR:-/tmp}/vodoge-install-test.XXXXXX")"
trap 'rm -rf -- "$TEST_DIR"' EXIT

asset='vodoge_v1.2.3_linux_amd64'
printf 'verified payload\n' >"${TEST_DIR}/${asset}"
hash="$(sha256_file "${TEST_DIR}/${asset}")"
printf '%s  %s\n' "$hash" "$asset" >"${TEST_DIR}/SHA256SUMS"
verify_release_asset "${TEST_DIR}/SHA256SUMS" "${TEST_DIR}/${asset}" "$asset" >/dev/null

unrelated_binary="${TEST_DIR}/not-an-install-target"
unrelated_download="${TEST_DIR}/not-a-download-dir"
printf 'keep\n' >"$unrelated_binary"
mkdir -p "$unrelated_download"
BINARY_TEMP="$unrelated_binary"
DOWNLOAD_DIR="$unrelated_download"
cleanup_install_artifacts
[[ -f "$unrelated_binary" ]] || fail 'cleanup removed an unrelated binary path'
[[ -d "$unrelated_download" ]] || fail 'cleanup removed an unrelated download directory'

DOWNLOAD_DIR="${TEST_DIR}/vodoge-install.partial"
mkdir -p "$DOWNLOAD_DIR"
cleanup_install_artifacts
[[ ! -e "${TEST_DIR}/vodoge-install.partial" ]] || fail 'cleanup left a download directory behind'

PORT=7575
WEB_USER=admin
WEB_PASS=$'bad\nserver: injected'
if write_config "${TEST_DIR}/injected-config.yaml" >/dev/null 2>&1; then
	fail 'multiline web password was accepted'
fi
[[ ! -e "${TEST_DIR}/injected-config.yaml" ]] || fail 'invalid web config was written'
WEB_PASS=admin123

printf '%064d  %s\n' 0 "$asset" >"${TEST_DIR}/SHA256SUMS.bad"
if verify_release_asset "${TEST_DIR}/SHA256SUMS.bad" "${TEST_DIR}/${asset}" "$asset" >/dev/null 2>&1; then
	fail 'checksum mismatch was accepted'
fi
{
	printf '%s  %s\n' "$hash" "$asset"
	printf '%s  %s\n' "$hash" "$asset"
} >"${TEST_DIR}/SHA256SUMS.duplicate"
if verify_release_asset "${TEST_DIR}/SHA256SUMS.duplicate" "${TEST_DIR}/${asset}" "$asset" >/dev/null 2>&1; then
	fail 'duplicate checksum entry was accepted'
fi

INSTALL_DIR="${TEST_DIR}/fresh"
mkdir -p "$INSTALL_DIR"
PG_USER='first-user'
PG_PASS='first-password'
PG_DB='first-db'
prepare_postgres_credentials >/dev/null
assert_eq 600 "$(file_mode "${INSTALL_DIR}/.postgres-credentials")" 'credential file mode'

PG_USER='replacement-user'
PG_PASS='replacement-password'
PG_DB='replacement-db'
prepare_postgres_credentials >/dev/null
assert_eq first-user "$PG_USER" 'rerun preserves database user'
assert_eq first-password "$PG_PASS" 'rerun preserves database password'
assert_eq first-db "$PG_DB" 'rerun preserves database name'

INSTALL_DIR="${TEST_DIR}/legacy"
mkdir -p "$INSTALL_DIR"
cat >"${INSTALL_DIR}/docker-compose.yml" <<'EOF'
services:
  postgres:
    environment:
      POSTGRES_USER: legacy-user
      POSTGRES_PASSWORD: legacy-password
      POSTGRES_DB: legacy-db
EOF
PG_USER='new-user'
PG_PASS='new-password'
PG_DB='new-db'
prepare_postgres_credentials >/dev/null
assert_eq legacy-user "$PG_USER" 'legacy database user import'
assert_eq legacy-password "$PG_PASS" 'legacy database password import'
assert_eq legacy-db "$PG_DB" 'legacy database name import'

INSTALL_DIR="${TEST_DIR}/ambiguous-legacy"
mkdir -p "$INSTALL_DIR"
cat >"${INSTALL_DIR}/docker-compose.yml" <<'EOF'
services:
  postgres:
    environment:
      POSTGRES_USER: first-user
      POSTGRES_USER: second-user
      POSTGRES_PASSWORD: unchanged-password
      POSTGRES_DB: unchanged-db
EOF
if prepare_postgres_credentials >/dev/null 2>&1; then
	fail 'ambiguous legacy credentials were accepted'
fi
[[ ! -e "${INSTALL_DIR}/.postgres-credentials" ]] || fail 'ambiguous legacy deployment was modified'

docker() {
	return 0
}
INSTALL_DIR="${TEST_DIR}/rendered"
PG_USER='render-user'
PG_PASS='render pass$word"quote\slash'
PG_DB='render-db'
IMAGE='ghcr.io/example/vodoge:test'
install_with_compose >/dev/null
assert_eq 600 "$(file_mode "${INSTALL_DIR}/docker-compose.yml")" 'compose file mode'
assert_eq 600 "$(file_mode "${INSTALL_DIR}/config/config.yaml")" 'application config file mode'
if [[ -n "$DOCKER_BIN" ]] && "$DOCKER_BIN" compose version >/dev/null 2>&1; then
	"$DOCKER_BIN" compose -f "${INSTALL_DIR}/docker-compose.yml" config --quiet
fi

original_dsn='host=127.0.0.1 user=vodoge password=a b$"`\c dbname=vodoge'
VODOGE_DB_DSN="$original_dsn"
write_systemd_environment "${TEST_DIR}/vodoge.env"
assert_eq 600 "$(file_mode "${TEST_DIR}/vodoge.env")" 'systemd environment file mode'
unset VODOGE_DB_DSN CONFIG_PATH
# EnvironmentFile uses the same shell-like double-quote escapes exercised here.
source "${TEST_DIR}/vodoge.env"
assert_eq "$original_dsn" "$VODOGE_DB_DSN" 'systemd DSN round trip'
assert_eq /etc/vodoge/config.yaml "$CONFIG_PATH" 'systemd config path round trip'

printf 'install_test: ok\n'

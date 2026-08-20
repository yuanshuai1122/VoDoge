#!/usr/bin/env bash
# VoDoge 一键安装。优先 Docker Compose + GHCR；没有 Docker 时下载发行二进制并写 systemd。
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/yuanshuai1122/VoDoge/main/scripts/install.sh | bash
#   VODOGE_DIR=/opt/vodoge bash scripts/install.sh
set -euo pipefail

REPO="${VODOGE_REPO:-yuanshuai1122/VoDoge}"
IMAGE="${VODOGE_IMAGE:-ghcr.io/yuanshuai1122/vodoge:latest}"
INSTALL_DIR="${VODOGE_DIR:-/opt/vodoge}"
PG_USER="${VODOGE_POSTGRES_USER:-vodoge}"
PG_PASS="${VODOGE_POSTGRES_PASSWORD:-}"
PG_DB="${VODOGE_POSTGRES_DB:-vodoge}"
WEB_USER="${VODOGE_WEB_USER:-admin}"
WEB_PASS="${VODOGE_WEB_PASSWORD:-admin123}"
PORT="${VODOGE_PORT:-7575}"
QMI_PROXY_EXECUTABLE="/usr/libexec/qmi-proxy"

need_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf '需要命令: %s\n' "$1" >&2
		return 1
	fi
}

require_single_line() {
	local name="$1" value="$2"
	if [[ -z "$value" || "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
		printf '%s 必须是非空单行值\n' "$name" >&2
		return 1
	fi
}

generate_postgres_password() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -hex 24
		return
	fi
	need_cmd od
	need_cmd tr
	LC_ALL=C od -An -N24 -tx1 /dev/urandom | tr -d ' \n'
}

save_postgres_credentials() {
	local dest="$1" temp
	require_single_line VODOGE_POSTGRES_USER "$PG_USER"
	require_single_line VODOGE_POSTGRES_PASSWORD "$PG_PASS"
	require_single_line VODOGE_POSTGRES_DB "$PG_DB"
	temp="$(mktemp "${dest}.tmp.XXXXXX")"
	chmod 0600 "$temp"
	{
		printf '%s\n' "$PG_USER"
		printf '%s\n' "$PG_PASS"
		printf '%s\n' "$PG_DB"
	} >"$temp"
	mv -f "$temp" "$dest"
	chmod 0600 "$dest"
}

load_postgres_credentials() {
	local src="$1" user password database extra
	{
		IFS= read -r user || return 1
		IFS= read -r password || return 1
		IFS= read -r database || return 1
		if IFS= read -r extra; then
			return 1
		fi
	} <"$src"
	require_single_line VODOGE_POSTGRES_USER "$user" || return 1
	require_single_line VODOGE_POSTGRES_PASSWORD "$password" || return 1
	require_single_line VODOGE_POSTGRES_DB "$database" || return 1
	PG_USER="$user"
	PG_PASS="$password"
	PG_DB="$database"
	chmod 0600 "$src"
}

legacy_compose_value() {
	local key="$1" src="$2" value
	if ! value="$(awk -v key="$key" '
		{
			line = $0
			sub(/^[[:space:]]*/, "", line)
			prefix = key ":"
			if (substr(line, 1, length(prefix)) == prefix) {
				count++
				line = substr(line, length(prefix) + 1)
				sub(/^[[:space:]]*/, "", line)
				value = line
			}
		}
		END {
			if (count != 1) {
				exit 1
			}
			print value
		}
	' "$src")"; then
		return 1
	fi
	case "$value" in
	\"*\")
		value="${value#\"}"
		value="${value%\"}"
		[[ "$value" != *\\* ]] || return 1
		;;
	\'*\')
		value="${value#\'}"
		value="${value%\'}"
		;;
	*)
		value="$(printf '%s' "$value" | sed 's/[[:space:]]#.*$//; s/[[:space:]]*$//')"
		;;
	esac
	if [[ "$value" == *'$'* ]]; then
		return 1
	fi
	require_single_line "$key" "$value" >/dev/null || return 1
	printf '%s' "$value"
}

prepare_postgres_credentials() {
	local credentials="${INSTALL_DIR}/.postgres-credentials" legacy="${INSTALL_DIR}/docker-compose.yml"
	if [[ -f "$credentials" ]]; then
		if ! load_postgres_credentials "$credentials"; then
			printf 'PostgreSQL 凭据文件格式无效，拒绝改写已有部署: %s\n' "$credentials" >&2
			return 1
		fi
		printf '保留已有 PostgreSQL 凭据 %s（VODOGE_POSTGRES_* 仅在首次安装时生效）\n' "$credentials"
		return
	fi

	if [[ -f "$legacy" ]]; then
		if ! PG_USER="$(legacy_compose_value POSTGRES_USER "$legacy")" ||
			! PG_PASS="$(legacy_compose_value POSTGRES_PASSWORD "$legacy")" ||
			! PG_DB="$(legacy_compose_value POSTGRES_DB "$legacy")"; then
			printf '检测到旧部署，但无法安全恢复 PostgreSQL 凭据；已保留 %s，未改动现有数据库。\n' "$legacy" >&2
			return 1
		fi
		printf '从已有 docker-compose.yml 导入 PostgreSQL 凭据。\n'
	elif [[ -z "$PG_PASS" ]]; then
		PG_PASS="$(generate_postgres_password)"
		printf '已为首次安装生成随机 PostgreSQL 密码。\n'
	fi

	save_postgres_credentials "$credentials"
}

yaml_double_quote() {
	local value="$1"
	require_single_line YAML_VALUE "$value" >/dev/null || return 1
	value="${value//\\/\\\\}"
	value="${value//\"/\\\"}"
	value="${value//\$/\$\$}"
	printf '"%s"' "$value"
}

config_yaml_double_quote() {
	local value="$1"
	require_single_line CONFIG_VALUE "$value" >/dev/null || return 1
	value="${value//\\/\\\\}"
	value="${value//\"/\\\"}"
	printf '"%s"' "$value"
}

libpq_quote() {
	local value="$1"
	require_single_line LIBPQ_VALUE "$value" >/dev/null || return 1
	value="${value//\\/\\\\}"
	value="${value//\'/\\\'}"
	printf "'%s'" "$value"
}

write_config() {
	local dest="$1" port_yaml web_user_yaml web_pass_yaml
	port_yaml="$(config_yaml_double_quote "$PORT")" || return 1
	web_user_yaml="$(config_yaml_double_quote "$WEB_USER")" || return 1
	web_pass_yaml="$(config_yaml_double_quote "$WEB_PASS")" || return 1
	mkdir -p "$(dirname "$dest")"
	if [[ -f "$dest" ]]; then
		chmod 0600 "$dest"
		printf '保留已有配置 %s\n' "$dest"
		return 0
	fi
	cat >"$dest" <<EOF
server:
  port: ${port_yaml}
  debug: false
web:
  username: ${web_user_yaml}
  password: ${web_pass_yaml}
EOF
	chmod 0600 "$dest"
	printf '已写默认配置 %s（登录后立刻改密码）\n' "$dest"
}

install_with_compose() {
	need_cmd docker
	if ! docker compose version >/dev/null 2>&1; then
		printf '需要 docker compose 插件\n' >&2
		return 1
	fi
	mkdir -p "${INSTALL_DIR}/config" "${INSTALL_DIR}/data" "${INSTALL_DIR}/logs"
	write_config "${INSTALL_DIR}/config/config.yaml"
	prepare_postgres_credentials
	local pg_user_yaml pg_pass_yaml pg_db_yaml image_yaml dsn dsn_yaml compose_path compose_temp
	pg_user_yaml="$(yaml_double_quote "$PG_USER")"
	pg_pass_yaml="$(yaml_double_quote "$PG_PASS")"
	pg_db_yaml="$(yaml_double_quote "$PG_DB")"
	image_yaml="$(yaml_double_quote "$IMAGE")"
	dsn="host='127.0.0.1' user=$(libpq_quote "$PG_USER") password=$(libpq_quote "$PG_PASS") dbname=$(libpq_quote "$PG_DB") port='5432' sslmode='disable' TimeZone='UTC'"
	dsn_yaml="$(yaml_double_quote "$dsn")"
	compose_path="${INSTALL_DIR}/docker-compose.yml"
	compose_temp="$(mktemp "${compose_path}.tmp.XXXXXX")"
	chmod 0600 "$compose_temp"
	cat >"$compose_temp" <<EOF
services:
  postgres:
    image: postgres:16-alpine
    container_name: vodoge-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${pg_user_yaml}
      POSTGRES_PASSWORD: ${pg_pass_yaml}
      POSTGRES_DB: ${pg_db_yaml}
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "127.0.0.1:5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U \"\$\${POSTGRES_USER}\" -d \"\$\${POSTGRES_DB}\""]
      interval: 5s
      timeout: 5s
      retries: 10

  vodoge:
    image: ${image_yaml}
    container_name: vodoge
    restart: unless-stopped
    network_mode: host
    privileged: true
    volumes:
      - ./config:/app/config
      - ./data:/app/data
      - ./logs:/app/logs
      - /dev:/dev
      - /run/pcscd:/run/pcscd
    environment:
      TZ: "Asia/Shanghai"
      CONFIG_PATH: "/app/config/config.yaml"
      VODOGE_DB_DSN: ${dsn_yaml}
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
EOF
	mv -f "$compose_temp" "$compose_path"
	chmod 0600 "$compose_path"
	(
		cd "${INSTALL_DIR}"
		docker compose pull
		docker compose up -d
	)
	printf '\nVoDoge 已启动。管理面: http://127.0.0.1:%s\n默认账密见 %s/config/config.yaml\n需要 PostgreSQL，没有 SQLite。\n' "$PORT" "$INSTALL_DIR"
}

detect_arch() {
	local arch
	arch="$(uname -m)"
	case "$arch" in
	x86_64 | amd64) printf 'amd64' ;;
	aarch64 | arm64) printf 'arm64' ;;
	armv7l | armv7) printf 'armv7' ;;
	*)
		printf '不支持的架构: %s\n' "$arch" >&2
		return 1
		;;
	esac
}

latest_tag() {
	curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --show-error --location --retry 3 \
		"https://api.github.com/repos/${REPO}/releases/latest" |
		sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1
}

download_url() {
	local url="$1" dest="$2"
	curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --show-error --location --retry 3 \
		"$url" --output "$dest"
}

sha256_file() {
	local file="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum -- "$file" | awk '{ print tolower($1) }'
		return
	fi
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 -- "$file" | awk '{ print tolower($1) }'
		return
	fi
	printf '需要命令: sha256sum 或 shasum\n' >&2
	return 1
}

release_checksum() {
	local checksum_file="$1" asset="$2"
	awk -v asset="$asset" '
		{
			name = $2
			sub(/^\*/, "", name)
			if (name == asset) {
				count++
				hash = tolower($1)
			}
		}
		END {
			if (count != 1 || length(hash) != 64 || hash !~ /^[0-9a-f]+$/) {
				exit 1
			}
			print hash
		}
	' "$checksum_file"
}

verify_release_asset() {
	local checksum_file="$1" asset_path="$2" asset="$3" expected actual
	if ! expected="$(release_checksum "$checksum_file" "$asset")"; then
		printf 'SHA256SUMS 中缺少唯一且有效的 %s 校验值\n' "$asset" >&2
		return 1
	fi
	actual="$(sha256_file "$asset_path")" || return 1
	if [[ "$actual" != "$expected" ]]; then
		printf 'SHA-256 校验失败: %s\n' "$asset" >&2
		printf '期望: %s\n实际: %s\n' "$expected" "$actual" >&2
		return 1
	fi
	printf 'SHA-256 校验通过: %s\n' "$asset"
}

systemd_env_quote() {
	local value="$1"
	require_single_line SYSTEMD_ENV_VALUE "$value" >/dev/null || return 1
	value="${value//\\/\\\\}"
	value="${value//\"/\\\"}"
	value="${value//\$/\\\$}"
	value="${value//\`/\\\`}"
	printf '"%s"' "$value"
}

write_systemd_environment() {
	local dest="$1" dsn config_path temp
	dsn="$(systemd_env_quote "$VODOGE_DB_DSN")" || return 1
	config_path="$(systemd_env_quote /etc/vodoge/config.yaml)" || return 1
	temp="$(mktemp "${dest}.tmp.XXXXXX")"
	chmod 0600 "$temp"
	{
		printf 'VODOGE_DB_DSN=%s\n' "$dsn"
		printf 'CONFIG_PATH=%s\n' "$config_path"
	} >"$temp"
	mv -f "$temp" "$dest"
	chmod 0600 "$dest"
}

write_qmi_proxy_systemd_unit() {
	local dest="$1" temp
	temp="$(mktemp "${dest}.tmp.XXXXXX")"
	cat >"$temp" <<'EOF'
[Unit]
Description=VoDoge shared QMI proxy
Before=vodoge.service

[Service]
Type=simple
ExecStart=/usr/libexec/qmi-proxy --no-exit
# Type=simple only confirms exec(), so wait for the abstract socket before
# allowing vodoge.service to open its QMI clients.
ExecStartPost=/bin/sh -ec 'for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50; do grep -aq "@qmi-proxy" /proc/net/unix && exit 0; sleep 0.1; done; exit 1'
Restart=always
RestartSec=1
TimeoutStartSec=10

[Install]
WantedBy=multi-user.target
EOF
	chmod 0644 "$temp"
	mv -f "$temp" "$dest"
	chmod 0644 "$dest"
}

write_vodoge_systemd_unit() {
	local dest="$1" bin="$2" qmi_proxy_enabled="$3" temp
	if [[ "$qmi_proxy_enabled" != "0" && "$qmi_proxy_enabled" != "1" ]]; then
		printf 'qmi_proxy_enabled 必须是 0 或 1\n' >&2
		return 1
	fi
	temp="$(mktemp "${dest}.tmp.XXXXXX")"
	{
		cat <<EOF
[Unit]
Description=VoDoge SMS hub
After=network-online.target postgresql.service
Wants=network-online.target
EOF
		if [[ "$qmi_proxy_enabled" -eq 1 ]]; then
			printf 'Requires=vodoge-qmi-proxy.service\n'
			printf 'After=vodoge-qmi-proxy.service\n'
		fi
		cat <<EOF

[Service]
Type=simple
EnvironmentFile=/etc/vodoge/vodoge.env
ExecStart=${bin} -c /etc/vodoge/config.yaml
WorkingDirectory=/var/lib/vodoge
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
	} >"$temp"
	chmod 0644 "$temp"
	mv -f "$temp" "$dest"
	chmod 0644 "$dest"
}

warn_missing_optional_runtime_dependencies() {
	if ! command -v arecord >/dev/null 2>&1 || ! command -v aplay >/dev/null 2>&1; then
		printf '警告：未找到 arecord/aplay；如需 CS 语音，请先安装 alsa-utils。\n' >&2
	fi
	if [[ ! -S /run/pcscd/pcscd.comm ]]; then
		printf '警告：未找到 /run/pcscd/pcscd.comm；如需 CCID/eSIM，请先安装并启动 pcscd。\n' >&2
	fi
	if [[ ! -x "$QMI_PROXY_EXECUTABLE" ]]; then
		printf '警告：未找到 %s；QMI 多客户端共享控制口需要安装提供 qmi-proxy 的 libqmi 软件包。\n' "$QMI_PROXY_EXECUTABLE" >&2
	fi
}

cleanup_download_dir() {
	if [[ -n "${DOWNLOAD_DIR:-}" && "$DOWNLOAD_DIR" == */vodoge-install.* && -d "$DOWNLOAD_DIR" ]]; then
		rm -rf -- "$DOWNLOAD_DIR"
	fi
	DOWNLOAD_DIR=""
}

cleanup_install_artifacts() {
	cleanup_download_dir
	if [[ -n "${BINARY_TEMP:-}" && "$BINARY_TEMP" == /usr/local/bin/vodoge.tmp.* && -f "$BINARY_TEMP" ]]; then
		rm -f -- "$BINARY_TEMP"
	fi
	BINARY_TEMP=""
}

install_binary() {
	if [[ "$(id -u)" -ne 0 ]]; then
		printf '发行二进制需要写入 /usr/local/bin、/etc 和 systemd，请用 root 运行。\n' >&2
		return 1
	fi
	need_cmd curl
	if [[ ! "$REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
		printf '无效的 GitHub 仓库名: %s\n' "$REPO" >&2
		return 1
	fi
	if [[ -z "${VODOGE_DB_DSN:-}" ]]; then
		printf '二进制安装需要已有 PostgreSQL，并设置 VODOGE_DB_DSN。\n例如：export VODOGE_DB_DSN="host=127.0.0.1 user=vodoge password=vodoge dbname=vodoge port=5432 sslmode=disable"\n' >&2
		return 1
	fi
	require_single_line VODOGE_DB_DSN "$VODOGE_DB_DSN"
	local tag arch asset url checksum_url bin qmi_proxy_enabled
	tag="${VODOGE_VERSION:-$(latest_tag)}"
	if [[ -z "$tag" ]]; then
		printf '读不到 GitHub Release 标签\n' >&2
		return 1
	fi
	if [[ ! "$tag" =~ ^[A-Za-z0-9._+-]+$ ]]; then
		printf '无效的发行标签: %s\n' "$tag" >&2
		return 1
	fi
	arch="$(detect_arch)"
	asset="vodoge_${tag}_linux_${arch}"
	url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
	checksum_url="https://github.com/${REPO}/releases/download/${tag}/SHA256SUMS"
	bin="/usr/local/bin/vodoge"
	DOWNLOAD_DIR="$(mktemp -d /tmp/vodoge-install.XXXXXX)"
	BINARY_TEMP=""
	trap cleanup_install_artifacts EXIT
	trap 'exit 1' HUP INT TERM
	printf '下载 %s\n' "$url"
	download_url "$url" "${DOWNLOAD_DIR}/${asset}"
	download_url "$checksum_url" "${DOWNLOAD_DIR}/SHA256SUMS"
	verify_release_asset "${DOWNLOAD_DIR}/SHA256SUMS" "${DOWNLOAD_DIR}/${asset}" "$asset"
	BINARY_TEMP="$(mktemp "${bin}.tmp.XXXXXX")"
	install -m 0755 "${DOWNLOAD_DIR}/${asset}" "$BINARY_TEMP"
	mv -f "$BINARY_TEMP" "$bin"
	BINARY_TEMP=""
	cleanup_install_artifacts
	trap - EXIT HUP INT TERM
	mkdir -p /etc/vodoge /var/lib/vodoge /var/log/vodoge
	write_config /etc/vodoge/config.yaml
	warn_missing_optional_runtime_dependencies
	if command -v systemctl >/dev/null 2>&1; then
		write_systemd_environment /etc/vodoge/vodoge.env
		qmi_proxy_enabled=0
		if [[ -x "$QMI_PROXY_EXECUTABLE" ]]; then
			write_qmi_proxy_systemd_unit /etc/systemd/system/vodoge-qmi-proxy.service
			qmi_proxy_enabled=1
		fi
		write_vodoge_systemd_unit /etc/systemd/system/vodoge.service "$bin" "$qmi_proxy_enabled"
		systemctl daemon-reload
		if [[ "$qmi_proxy_enabled" -eq 1 ]]; then
			# Stop the old app-managed qmi-proxy before claiming its shared socket.
			systemctl stop vodoge
			systemctl enable vodoge-qmi-proxy
			systemctl start vodoge-qmi-proxy
		fi
		systemctl enable vodoge
		systemctl restart vodoge
		printf '\nVoDoge systemd 单元已启动。管理面: http://127.0.0.1:%s\n' "$PORT"
	else
		printf '没有 systemd。请自行用 VODOGE_DB_DSN 运行：%s -c /etc/vodoge/config.yaml\n' "$bin"
	fi
}

warn_if_install_dir_unwritable() {
	if [[ "$(id -u)" -eq 0 ]]; then
		return
	fi
	local probe="$INSTALL_DIR"
	while [[ ! -e "$probe" && "$probe" != "/" && "$probe" != "." ]]; do
		probe="$(dirname "$probe")"
	done
	if [[ ! -w "$probe" ]]; then
		printf '安装目录 %s 对当前用户不可写；请用 root，或把 VODOGE_DIR 设为可写目录。\n' "$INSTALL_DIR" >&2
	fi
}

main() {
	warn_if_install_dir_unwritable
	if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
		install_with_compose
		return 0
	fi
	printf '未检测到 Docker Compose，改走发行二进制。\n'
	install_binary
}

if [[ "${VODOGE_INSTALL_SOURCE_ONLY:-0}" != "1" ]]; then
	main "$@"
fi

#!/usr/bin/env bash
# VoDoge 一键安装。优先 Docker Compose + GHCR；没有 Docker 时下载发行二进制并写 systemd。
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/yuanshuai1122/VoDoge/main/scripts/install.sh | bash
#   VODOGE_DIR=/opt/vodoge bash scripts/install.sh
set -euo pipefail

REPO="${VODOGE_REPO:-${VODOG_REPO:-yuanshuai1122/VoDoge}}"
IMAGE="${VODOGE_IMAGE:-${VODOG_IMAGE:-ghcr.io/yuanshuai1122/vodoge:latest}}"
INSTALL_DIR="${VODOGE_DIR:-${VODOG_DIR:-/opt/vodoge}}"
PG_USER="${VODOGE_POSTGRES_USER:-${VODOG_POSTGRES_USER:-vodoge}}"
PG_PASS="${VODOGE_POSTGRES_PASSWORD:-${VODOG_POSTGRES_PASSWORD:-vodoge}}"
PG_DB="${VODOGE_POSTGRES_DB:-${VODOG_POSTGRES_DB:-vodoge}}"
WEB_USER="${VODOGE_WEB_USER:-${VODOG_WEB_USER:-admin}}"
WEB_PASS="${VODOGE_WEB_PASSWORD:-${VODOG_WEB_PASSWORD:-admin123}}"
PORT="${VODOGE_PORT:-${VODOG_PORT:-7575}}"

need_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf '需要命令: %s\n' "$1" >&2
		return 1
	fi
}

write_config() {
	local dest="$1"
	mkdir -p "$(dirname "$dest")"
	if [[ -f "$dest" ]]; then
		printf '保留已有配置 %s\n' "$dest"
		return 0
	fi
	cat >"$dest" <<EOF
server:
  port: ${PORT}
  debug: false
web:
  username: ${WEB_USER}
  password: ${WEB_PASS}
EOF
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
	cat >"${INSTALL_DIR}/docker-compose.yml" <<EOF
services:
  postgres:
    image: postgres:16-alpine
    container_name: vodoge-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${PG_USER}
      POSTGRES_PASSWORD: ${PG_PASS}
      POSTGRES_DB: ${PG_DB}
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "127.0.0.1:5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${PG_USER} -d ${PG_DB}"]
      interval: 5s
      timeout: 5s
      retries: 10

  vodoge:
    image: ${IMAGE}
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
      - TZ=Asia/Shanghai
      - CONFIG_PATH=/app/config/config.yaml
      - VODOGE_DB_DSN=host=127.0.0.1 user=${PG_USER} password=${PG_PASS} dbname=${PG_DB} port=5432 sslmode=disable TimeZone=UTC
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
EOF
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
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
	else
		wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
	fi
}

install_binary() {
	need_cmd curl
	if [[ -z "${VODOGE_DB_DSN:-${VODOG_DB_DSN:-}}" ]]; then
		printf '二进制安装需要已有 PostgreSQL，并设置 VODOGE_DB_DSN。\n例如：export VODOGE_DB_DSN="host=127.0.0.1 user=vodoge password=vodoge dbname=vodoge port=5432 sslmode=disable"\n' >&2
		return 1
	fi
	VODOGE_DB_DSN="${VODOGE_DB_DSN:-${VODOG_DB_DSN}}"
	local tag arch asset url bin
	tag="${VODOGE_VERSION:-$(latest_tag)}"
	if [[ -z "$tag" ]]; then
		printf '读不到 GitHub Release 标签\n' >&2
		return 1
	fi
	arch="$(detect_arch)"
	asset="vodoge_${tag}_linux_${arch}"
	url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
	bin="/usr/local/bin/vodoge"
	printf '下载 %s\n' "$url"
	curl -fL "$url" -o /tmp/vodoge.new
	chmod +x /tmp/vodoge.new
	install -m 0755 /tmp/vodoge.new "$bin"
	rm -f /tmp/vodoge.new
	mkdir -p /etc/vodoge /var/lib/vodoge /var/log/vodoge
	write_config /etc/vodoge/config.yaml
	if command -v systemctl >/dev/null 2>&1; then
		cat >/etc/systemd/system/vodoge.service <<EOF
[Unit]
Description=VoDoge SMS hub
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
Environment=VODOGE_DB_DSN=${VODOGE_DB_DSN}
Environment=CONFIG_PATH=/etc/vodoge/config.yaml
ExecStart=${bin} -c /etc/vodoge/config.yaml
WorkingDirectory=/var/lib/vodoge
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
		systemctl daemon-reload
		systemctl enable --now vodoge
		printf '\nVoDoge systemd 单元已启动。管理面: http://127.0.0.1:%s\n' "$PORT"
	else
		printf '没有 systemd。请自行用 VODOGE_DB_DSN 运行：%s -c /etc/vodoge/config.yaml\n' "$bin"
	fi
}

main() {
	if [[ "$(id -u)" -ne 0 && "${VODOGE_DIR:-}" = "/opt/vodoge" ]]; then
		printf '默认装到 /opt/vodoge，建议用 root，或设置 VODOGE_DIR=$HOME/vodoge\n' >&2
	fi
	if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
		install_with_compose
		return 0
	fi
	printf '未检测到 Docker Compose，改走发行二进制。\n'
	install_binary
}

main "$@"

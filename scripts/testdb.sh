#!/usr/bin/env bash
# 管理一个**一次性**的 PostgreSQL 测试实例。
#
# 存在的理由见 docs/known-issues.md KI-002：db.OpenTestDB 会 TRUNCATE 目标库
# 当前 schema 下的所有表。把 TEST_DATABASE_URL 指向正在服务的实例会清空业务数据，
# 因此测试必须有自己的库。
#
# 用法：
#   scripts/testdb.sh ensure   起容器并等待就绪（已在跑则复用）
#   scripts/testdb.sh stop     停止并删除
#   scripts/testdb.sh dsn      打印可用的 DSN
set -euo pipefail

NAME="${TEST_DB_CONTAINER:-vodoge-testdb}"
NETWORK="${TEST_DB_NETWORK:-vodoge-test-net}"
IMAGE="${TEST_DB_IMAGE:-postgres:16-alpine}"
DB="${TEST_DB_NAME:-vodoge_test}"
USER="${TEST_DB_USER:-vodoge}"
PASS="${TEST_DB_PASSWORD:-vodoge}"
# 宿主机端口，便于本机直接连；容器间通信走 NETWORK 上的容器名
HOST_PORT="${TEST_DB_PORT:-5433}"

ensure_network() {
	if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
		docker network create "$NETWORK" >/dev/null
	fi
}

attached_to_network() {
	docker inspect "$NAME" \
		--format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' 2>/dev/null |
		tr ' ' '\n' | grep -qx "$NETWORK"
}

ensure() {
	ensure_network

	if [[ -n "$(docker ps -q -f "name=^${NAME}$")" ]]; then
		printf 'test database already running: %s\n' "$NAME"
		# 复用已有容器时必须确认它挂在目标网络上——早先手工起的实例可能只在
		# bridge 上，测试容器按名字解析不到它，报 "no such host"。
		if ! attached_to_network; then
			docker network connect "$NETWORK" "$NAME"
			printf 'attached %s to network %s\n' "$NAME" "$NETWORK"
		fi
	else
		# 可能存在同名的已停止容器
		docker rm -f "$NAME" >/dev/null 2>&1 || true
		docker run -d --name "$NAME" --network "$NETWORK" \
			-e POSTGRES_USER="$USER" -e POSTGRES_PASSWORD="$PASS" -e POSTGRES_DB="$DB" \
			-p "${HOST_PORT}:5432" \
			--health-cmd "pg_isready -U $USER -d $DB" \
			--health-interval 2s --health-timeout 3s --health-retries 20 \
			"$IMAGE" >/dev/null
		printf 'started test database: %s\n' "$NAME"
	fi

	printf 'waiting for readiness'
	for _ in $(seq 1 40); do
		if docker exec "$NAME" pg_isready -U "$USER" -d "$DB" >/dev/null 2>&1; then
			printf ' — ready\n'
			return 0
		fi
		printf '.'
		sleep 1
	done
	printf '\ntest database did not become ready\n' >&2
	return 1
}

case "${1:-ensure}" in
	ensure)
		ensure
		;;
	stop)
		docker rm -f "$NAME" >/dev/null 2>&1 || true
		printf 'removed %s\n' "$NAME"
		;;
	dsn)
		printf 'host=127.0.0.1 port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC\n' \
			"$HOST_PORT" "$USER" "$PASS" "$DB"
		;;
	*)
		printf 'usage: %s [ensure|stop|dsn]\n' "$0" >&2
		exit 2
		;;
esac

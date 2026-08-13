#!/usr/bin/env bash
# 在 .dockerignore 临时放开 **/*_test.go 的前提下执行给定命令，结束后恢复。
#
# 生产镜像刻意不含测试代码，但编译校验必须看得到它们——UTF-8 损坏、未使用
# import 这类问题只在加载测试文件时才暴露，普通镜像构建永远覆盖不到。
#
# 用法：scripts/with-test-sources.sh docker build -f Dockerfile.vet -t vohive-vet .
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MARKER='**/*_test.go'
BACKUP=".dockerignore.with-test-sources.bak"

if [[ ! -f .dockerignore ]]; then
	printf '.dockerignore not found\n' >&2
	exit 1
fi

restore() {
	if [[ -f "$BACKUP" ]]; then
		mv -f "$BACKUP" .dockerignore
	fi
}
# 任何退出路径都要还原，否则会把「排除测试代码」这条规则永久丢掉
trap restore EXIT INT TERM

cp .dockerignore "$BACKUP"

node -e '
const fs = require("fs");
const marker = process.argv[1];
const s = fs.readFileSync(".dockerignore", "utf8");
if (!s.includes(marker)) {
  console.error("marker not found in .dockerignore: " + marker);
  process.exit(1);
}
fs.writeFileSync(".dockerignore", s.replace(marker, "# (temporarily allowed by with-test-sources.sh)"));
' "$MARKER"

"$@"

#!/usr/bin/env bash
# VoDoge 本地流水线。
#
# 本机验证流水线。发版镜像与 GitHub Release 走 .github/workflows/release.yml。
# 需要 Docker 的环节（vet-all / test）会自动起容器，见各任务说明。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export GOWORK="${GOWORK:-off}"

# 需要 Docker 的任务共用的镜像名
VET_IMAGE="${VET_IMAGE:-vodoge-vet}"

find_go() {
	if [[ -n "${GO_BIN:-}" ]]; then
		printf '%s\n' "$GO_BIN"
		return
	fi
	if command -v go >/dev/null 2>&1; then
		command -v go
		return
	fi
	if [[ -x /usr/local/go/bin/go ]]; then
		printf '%s\n' /usr/local/go/bin/go
		return
	fi
	# 没有本机 Go 也能跑 encoding / web / vet-all / test（后者走容器）
	printf ''
	return 0
}

run() {
	printf '\n==> %s\n' "$*"
	"$@"
}

need_go() {
	if [[ -z "$GO_BIN" ]]; then
		printf 'this task needs Go on the host; set GO_BIN=/path/to/go\n' >&2
		return 127
	fi
}

need_docker() {
	if ! command -v docker >/dev/null 2>&1; then
		printf 'this task needs Docker\n' >&2
		return 127
	fi
}

dependency_hygiene() {
	local forbidden_refs forbidden_modules modules paths
	paths=(go.mod go.sum Dockerfile Dockerfile.runtime Dockerfile.vet
		docker-compose.yml docker-compose.windows.yml DEPLOY.md
		Makefile scripts internal cmd pkg web/app)

	if [[ -f go.work ]]; then
		printf 'go.work must not be committed or required for builds\n' >&2
		return 1
	fi

	forbidden_refs="$(
		{
			git grep -nE 'github[.]com/iniwex5|github[.]com/boa-z/qqbot|iniwex[/]vohive' -- \
				"${paths[@]}" ':!internal/web/dist/**' ':!web/dist/**' || true
			git grep -nE 'replace[[:space:]].*=>[[:space:]]*(\.{1,2}/|/|~)' -- \
				"${paths[@]}" ':!internal/web/dist/**' ':!web/dist/**' || true
		} | sed '/^$/d'
	)"
	if [[ -n "$forbidden_refs" ]]; then
		printf 'forbidden dependency or local-path references found:\n%s\n' "$forbidden_refs" >&2
		return 1
	fi

	if [[ -n "$GO_BIN" ]]; then
		if ! modules="$(env GOWORK=off "$GO_BIN" list -m all)"; then
			printf 'go list -m all failed; dependency hygiene could not be verified\n' >&2
			return 1
		fi
		forbidden_modules="$(printf '%s\n' "$modules" | grep -E 'github[.]com/iniwex5|github[.]com/boa-z/qqbot' || true)"
		if [[ -n "$forbidden_modules" ]]; then
			printf 'forbidden modules resolved by go list -m all:\n%s\n' "$forbidden_modules" >&2
			return 1
		fi
	fi

	printf '\n==> dependency hygiene ok\n'
}

# Go 要求源文件是合法 UTF-8，任一文件损坏都会让整个包无法编译，
# 即使损坏只出现在注释里。仓库曾因此连续多次构建失败（见 docs/known-issues.md KI-001）。
# 注意：iconv -f UTF-8 -t UTF-8 会误报数百个文件，不能用来做这项检查。
encoding_check() {
	if ! command -v node >/dev/null 2>&1; then
		printf '\n==> encoding check skipped (node not found)\n'
		return 0
	fi

	local bad
	bad="$(node scripts/check-encoding.mjs --list)"

	if [[ -n "$bad" ]]; then
		printf 'Go 源文件存在非法 UTF-8，会导致整个包无法编译:\n%s\n' "$bad" >&2
		return 1
	fi

	printf '\n==> encoding check ok\n'
}

# gofmt 校验。
#
# 在容器里跑，因为本机不一定有 Go。这项检查此前根本无法使用：仓库在 Windows 上
# 检出为 CRLF，gofmt 会把每一个文件都当成"未格式化"（548 个里报 450 个）。
# `.gitattributes` 把源码钉成 LF 之后，它才开始有意义。
fmt_check() {
	need_docker
	local unformatted
	# MSYS_NO_PATHCONV：Git Bash 会把 `-w /src` 这类参数当成路径改写成
	# `C:/Program Files/Git/src`，docker 随即报"工作目录不是绝对路径"。
	unformatted="$(MSYS_NO_PATHCONV=1 docker run --rm -v "$ROOT":/src -w /src -e GOWORK=off \
		"$VET_IMAGE" sh -c 'gofmt -l internal/ pkg/ cmd/')"

	if [[ -n "$unformatted" ]]; then
		printf '以下文件未经 gofmt 格式化：\n%s\n\n运行 gofmt -w 修复。\n' "$unformatted" >&2
		return 1
	fi
	printf '\n==> gofmt ok\n'
}

# 校验 openapi.vodoge.yaml 与 server.go 实际注册的路由一致。
route_check() {
	if ! command -v node >/dev/null 2>&1; then
		printf '\n==> route check skipped (node not found)\n'
		return 0
	fi
	run node scripts/check-routes.mjs
}

web_build() {
	run npm ci --prefix web
	run npm run typecheck --prefix web
	run npm run lint --prefix web
	# tsc 查不出契约问题：后端响应体在类型系统里是 unknown，
	# 归一化层（unwrap/errors/client）解析错了只会在运行时才显形。
	run npm run test --prefix web
	run npm run build --prefix web -- --webpack
	rm -rf internal/web/dist
	mkdir -p internal/web
	cp -R web/dist internal/web/dist
	touch internal/web/dist/.gitkeep
}

tidy_check() {
	need_go
	run "$GO_BIN" mod tidy -diff
}

# 在容器里编译**包含测试文件**的全部源码。
# 生产镜像排除了 _test.go，普通构建覆盖不到这条路径，而 UTF-8 损坏、
# 未使用 import 这类问题恰恰只在加载测试文件时才暴露。
vet_all() {
	need_docker
	run bash scripts/with-test-sources.sh docker build -f Dockerfile.vet -t "$VET_IMAGE" .
}

# 跑全部测试。需要一个 PostgreSQL；默认用 scripts/testdb.sh 起的独立实例。
#
# 两点必须注意：
#  1. 切勿把 TEST_DATABASE_URL 指向生产库——OpenTestDB 会清空目标库所有表（KI-002）。
#  2. **必须 -p 1**：go test 默认并行跑不同的包，而这些包共用同一个测试库；
#     一个包的 OpenTestDB 全表 truncate 会把另一个包正在用的数据清掉，
#     表现为随机的、与被测逻辑无关的失败。串行是这里的正确性前提。
go_tests() {
	need_docker
	local pkgs
	pkgs="${CI_GO_TEST_PACKAGES:-./internal/... ./pkg/... ./cmd/...}"
	run bash scripts/testdb.sh ensure
	run docker run --rm --network "${TEST_DB_NETWORK:-vodoge-test-net}" \
		-e TEST_DATABASE_URL="${TEST_DATABASE_URL:-host=vodoge-testdb user=vodoge password=vodoge dbname=vodoge_test port=5432 sslmode=disable TimeZone=UTC}" \
		"$VET_IMAGE" sh -c "go test -p 1 $pkgs"
}

go_build() {
	need_go
	(
		export CGO_ENABLED="${CGO_ENABLED:-0}"
		export GOOS="${GOOS:-linux}"
		run "$GO_BIN" build -trimpath -buildvcs=false -tags "${GO_TAGS:-with_utls nomsgpack}" -o "${CI_BUILD_OUTPUT:-/tmp/vodoge}" ./cmd/vodoge
	)
}

# 版本号。
#
# 必须由构建方传入：`.git` 在 .dockerignore 里（镜像不该带版本库），
# 容器内跑 git describe 只会恒定得到 "unknown"——界面上的版本号一直是假的。
build_version() {
	git describe --tags --always --dirty 2>/dev/null || echo unknown
}

image_build() {
	need_docker
	run docker build --build-arg VERSION="$(build_version)" -t "${VODOGE_IMAGE:-vodoge:latest}" .
}

# 交叉构建 arm64 与 armv7，验证目标平台产物真的能出来。
#
# 只构建、不推送。发版推 GHCR 由 tag v* 的 GitHub Actions 负责。
# 这一步回答「换个架构还编得过吗」。
#
# 前端与 Go 编译都固定在 BUILDPLATFORM 上跑（见 Dockerfile），走 Go 自己的
# 交叉编译而不是 QEMU 模拟——后者会把每个架构的构建时间从一分半拖到十几分钟。
multiarch_build() {
	need_docker
	if ! docker buildx version >/dev/null 2>&1; then
		printf 'this task needs docker buildx\n' >&2
		return 127
	fi

	local version
	version="$(build_version)"
	for platform in ${MULTIARCH_PLATFORMS:-linux/arm64 linux/arm/v7}; do
		# 多平台镜像无法 --load 进本地镜像库，这里逐个平台构建并加载，
		# 顺便让产物可以被检查（见 docs 里的 ELF 头验证）。
		run docker buildx build --platform "$platform" \
			--build-arg VERSION="$version" \
			-t "vodoge:${platform//\//-}" --load .
	done
}

usage() {
	cat <<'USAGE'
Usage: scripts/ci.sh [all|hygiene|encoding|routes|web|tidy|vet-all|fmt|test|build|image|multiarch ...]

Default `all` runs: hygiene, encoding, routes, web, vet-all, fmt, test, image.

Tasks:
  hygiene   forbidden dependency / local replace directives
  encoding  every tracked .go file must be valid UTF-8
  routes    openapi.vodoge.yaml must match the routes server.go registers
  fmt       gofmt -l must be empty (needs the vet image; run vet-all first)
  web       npm ci + typecheck + lint + test + build, then embed into internal/web/dist
  tidy      go mod tidy -diff            (needs Go on the host)
  vet-all   compile everything incl. _test.go, in a container
  test      run the test suite against a throwaway PostgreSQL
  build     build the binary             (needs Go on the host)
  image     build the production image
  multiarch cross-build arm64 + armv7 (needs buildx; not part of `all`)

Environment:
  GO_BIN               path to go binary (only needed by tidy/build)
  VET_IMAGE            image name for the vet/test container (default vodoge-vet)
  TEST_DATABASE_URL    override the throwaway test database
  CI_GO_TEST_PACKAGES  package list for tests
  VODOGE_IMAGE         production image tag (default vodoge:latest)
  MULTIARCH_PLATFORMS  platforms for multiarch (default "linux/arm64 linux/arm/v7")
USAGE
}

GO_BIN="$(find_go)"

if [[ $# -eq 0 || "${1:-}" == "all" ]]; then
	# fmt 排在 vet-all 之后：它用的就是 vet-all 构建出来的镜像
	tasks=(hygiene encoding routes web vet-all fmt test image)
else
	tasks=("$@")
fi

printf 'Go: %s\n' "${GO_BIN:-<not found; host-Go tasks unavailable>}"
printf 'GOWORK: %s\n' "$GOWORK"

for task in "${tasks[@]}"; do
	case "$task" in
		hygiene | dependency-hygiene) dependency_hygiene ;;
		encoding | encoding-check) encoding_check ;;
		routes | route-check) route_check ;;
		fmt | gofmt) fmt_check ;;
		web | frontend) web_build ;;
		tidy | tidy-check) tidy_check ;;
		vet-all | vet) vet_all ;;
		test | go-test) go_tests ;;
		build | go-build) go_build ;;
		image | docker) image_build ;;
		multiarch | arm) multiarch_build ;;
		-h | --help | help)
			usage
			exit 0
			;;
		*)
			printf 'unknown task: %s\n' "$task" >&2
			usage >&2
			exit 2
			;;
	esac
done

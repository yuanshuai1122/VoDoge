# 构建阶段 1: 前端构建 (Frontend)
#
# 固定在 BUILDPLATFORM（构建机的架构）上跑：前端产物是与架构无关的静态文件，
# 让 npm 在 QEMU 模拟的 arm 里跑一遍纯属浪费——那会把构建时间拖到十几分钟。
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# 构建阶段 2: 后端构建 (Backend)
#
# 同样固定在 BUILDPLATFORM，用 Go 自己的交叉编译产出目标架构的二进制，
# 而不是在模拟器里跑一个 arm 版的 Go 工具链。CGO 已禁用，交叉编译没有额外代价。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder
WORKDIR /app

# buildx 注入的目标平台信息
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

# 版本号由构建方传入。
# 不能在容器里跑 git describe——.git 在 .dockerignore 里（镜像不该带版本库），
# 于是它恒定回落到 "unknown"，界面上的版本号一直是假的。
ARG VERSION=unknown

# 启用 Go 工具链自动下载
ENV GOTOOLCHAIN=auto
ENV GOWORK=off
ENV GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.google.cn

# 安装构建依赖（不再需要 git：版本号改由 --build-arg 传入）
RUN apk add --no-cache ca-certificates

# 复制 go mod 文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码 (不包含 internal/web/dist，这将在下一步从前端构建阶段复制)
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg

# 复制构建好的前端资源到 internal/web/dist 以便嵌入
# 必须在 go build 之前完成
COPY --from=frontend-builder /app/web/dist ./internal/web/dist/

# 验证前端资源已复制
RUN ls -la internal/web/dist/ && echo "Frontend assets copied successfully"

# 验证依赖并编译二进制；依赖整理必须在提交前由 CI 的 tidy gate 完成。
RUN go mod verify
RUN BUILD_TIME=$(date "+%Y-%m-%d %H:%M:%S") && \
    # GOARM 只在 GOARCH=arm 时有意义；armv7 的 TARGETVARIANT 是 "v7"，去掉前缀即可
    if [ "$TARGETARCH" = "arm" ]; then export GOARM="${TARGETVARIANT#v}"; fi && \
    CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -buildvcs=false -tags "with_utls nomsgpack" -ldflags "-s -w -X 'github.com/yuanshuai1122/vodog/internal/global.Version=${VERSION}' -X 'github.com/yuanshuai1122/vodog/internal/global.BuildTime=${BUILD_TIME}'" -o /app/vodog ./cmd/vodog

# 运行阶段 (Runtime)
FROM alpine:latest
WORKDIR /app

# 安装运行时依赖
# - ca-certificates / tzdata: 基础 HTTPS 与时区支持
RUN apk add --no-cache ca-certificates tzdata

# 复制二进制文件
COPY --from=backend-builder /app/vodog .

# 创建配置和数据目录
RUN mkdir -p config data logs

# 暴露端口 (API)
EXPOSE 7575

# 默认配置路径环境变量
ENV CONFIG_PATH=/app/config/config.yaml

# 入口点
ENTRYPOINT ["./vodog"]
CMD ["-c", "/app/config/config.yaml"]

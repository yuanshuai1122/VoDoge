# VoDoge 前端

Next.js 16（App Router）+ React 19 + TypeScript + Tailwind 4 + shadcn/ui。

接口依据是 [`docs/frontend-api-matrix.md`](../docs/frontend-api-matrix.md)，
**不是** `openapi.vodoge.yaml`——后者缺 17 个真实端点且声明了 3 个不存在的端点。

## 开发

```bash
npm install
npm run dev
```

前端在 `:3000`，`/api/*` 由 `next.config.ts` 的 rewrites 反代到后端 `:7575`。
**必须走反代**：后端没有全局 CORS 中间件，跨域直连只有 `/api/logs/stream` 可用。

后端需要单独起（仓库根目录）：

```bash
docker compose up -d postgres
go run ./cmd/vodoge -c config/config.yaml
```

## 构建

```bash
npm run build
```

生产模式为**静态导出**：产物落在 `web/dist/`，由 `make frontend-dist` 或 Dockerfile
拷入 `internal/web/dist` 供 Go 嵌入托管，最终仍是单镜像交付（ADR-005）。

因此以下 Next 能力**不可用**：Server Actions、Route Handlers、middleware、ISR、
`next/image` 优化、以及没有 `generateStaticParams()` 的动态路由。
设备详情用 `/devices/detail?id=` 而非 `/devices/[id]` 正是这个原因。

## 检查

```bash
npm run typecheck
npm run lint
```

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `NEXT_PUBLIC_API_BASE` | 空（同源） | API 前缀。留空即走同源 `/api`，推荐保持默认；指向其它 origin 需自行解决 CORS |

## 目录

```
app/            路由与页面（薄，只做编排）
components/
  ui/           shadcn 组件
  layout/ devices/ sms/ settings/ traffic/ common/ auth/
lib/
  api/          client / unwrap（6 种成功形状）/ errors（3 种错误形状）/ endpoints
  sse/          EventSource 封装
  auth/         token 存储
  device-status.ts  生命周期 + 布尔位 → 展示状态
hooks/  stores/  types/
```

## 几个必须知道的后端行为

- **改密后所有 token 立即失效**：token 的 HMAC 密钥就是登录密码，改密成功后前端强制登出。
- **SSE 用 query 传 token**：`EventSource` 无法设请求头，后端仅对 4 个流式端点开放 `?token=`。
  token 会进浏览器历史，**不要把流地址渲染成可见链接**。
- **开发期 SSE 必须直连后端**：Next dev 的 rewrite 代理会缓冲 SSE，经代理订阅收不到任何数据。
  因此 `useEventSource` 在 development 下直接指向 `http://127.0.0.1:7575`（可用
  `NEXT_PUBLIC_SSE_BASE` 覆盖）。这需要后端配置 `server.debug: true`——CORS 仅在
  Debug 模式下放行 localhost。生产同源，不经代理，无此问题。
- **eSIM 操作互斥**：所有 eSIM 调用经 APDU 仲裁器串行化，可能返回 409 `ESIM_BUSY`，
  由 `stores/esim-lock` 统一调度，不要自行重试。
- **短信按 ICCID 归属**：换卡后历史记录跟卡走，不跟设备走。
- **分页是游标式**：没有总数也没有 `has_more`，靠"返回条数 == limit"判断还有更多。

## 冒烟

针对运行中的后端跑一遍主路径：

```bash
node ../scripts/smoke-api.mjs
```

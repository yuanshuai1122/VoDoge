#!/usr/bin/env node
/**
 * 校验 openapi.vohive.yaml 与后端实际注册的路由一致。
 *
 * 这份 spec 曾严重滞后：缺 17 条真实端点、声明了 3 条并不存在的端点，
 * 以致完全不能作为前端依据（docs/frontend-api-matrix.md §7）。
 * 有了这项检查，二者再次分叉时会在本地流水线里立刻暴露。
 *
 * 判据是 internal/api/routes.go 里的路由表——那张表本身就是注册用的数据，
 * 不存在"清单说一套、代码做一套"的可能。表项形如：
 *
 *   {"GET", "/devices", s.handleDeviceMgmtList, authRequired, false, "设备列表"},
 *
 * 挂在根而非 /api 组下的路由（/ping、/debug/embed）不在表里，spec 也不描述。
 *
 * 用法：node scripts/check-routes.mjs
 */

import { readFileSync } from "node:fs";
import path from "node:path";

const API_DIR = "internal/api";
const SPEC = path.join(API_DIR, "openapi.vohive.yaml");
const ROUTES_FILE = path.join(API_DIR, "routes.go");

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"];

/**
 * 不纳入公开契约的路由。
 *
 * websheet 的承载页与代理通道是把运营商的 E911 设置页反向代理进本服务的管道：
 * 路径含 `*target` 通配、请求体与响应完全由对端决定，描述它没有意义。
 * 但 `/websheets/:id/status` **不在此列**——前端靠轮询它判断流程是否结束，
 * 那是实打实的契约，必须写进 spec。
 * /docs/assets 是 Swagger UI 自身的静态资源。
 */
const INTERNAL_ROUTE_PATTERNS = [
  /^\/websheets\/:id$/,
  /^\/websheets\/:id\/proxy/,
  /^\/websheets\/:id\/callback$/,
  /^\/websheets\/:id\/done$/,
  /^\/docs\/assets\//,
];

function isInternal(p) {
  return INTERNAL_ROUTE_PATTERNS.some((re) => re.test(p));
}

/** gin 的 :param 与 OpenAPI 的 {param} 归一为 :param */
function normalize(p) {
  return p.replace(/\{([^}]+)\}/g, ":$1");
}

// ---- 1) 从源码收集实际注册的路由 ----

function collectRegisteredRoutes() {
  const routes = new Set();
  const src = readFileSync(ROUTES_FILE, "utf8");

  for (const line of src.split("\n")) {
    // 跳过注释掉的表项
    if (/^\s*\/\//.test(line)) continue;

    // {"GET", "/devices", s.handleX, authRequired, false, "..."},
    const m = line.match(
      new RegExp(`^\\s*\\{"(${METHODS.join("|")})",\\s*"([^"]+)"`),
    );
    if (m) {
      const p = normalize(m[2]);
      if (!isInternal(p)) routes.add(`${m[1]} ${p}`);
    }
  }

  // websheet 的代理通道由两层循环展开（方法 × 路径），不是逐条字面量。
  // 那些路径全部属于内部通道，本就不纳入契约，这里无需还原。
  if (routes.size === 0) {
    console.error(
      `没有从 ${ROUTES_FILE} 解析出任何路由——表的写法可能变了，请同步本脚本`,
    );
    process.exit(2);
  }
  return routes;
}

// ---- 2) 从 spec 收集声明的路径 ----
// 只需要 paths 段的键与其下的方法，用不上完整 YAML 解析器。

function collectSpecRoutes() {
  const routes = new Set();
  const lines = readFileSync(SPEC, "utf8").split("\n");

  let inPaths = false;
  let currentPath = null;

  for (const raw of lines) {
    const line = raw.replace(/\r$/, "");
    if (/^paths:\s*$/.test(line)) {
      inPaths = true;
      continue;
    }
    if (!inPaths) continue;
    // 顶层的下一个键结束 paths 段
    if (/^[a-zA-Z]/.test(line)) break;

    const pathMatch = line.match(/^ {2}(\/[^\s:]*):\s*$/);
    if (pathMatch) {
      currentPath = normalize(pathMatch[1]);
      continue;
    }
    const methodMatch = line.match(
      /^ {4}(get|post|put|patch|delete|options):\s*$/,
    );
    if (methodMatch && currentPath) {
      routes.add(`${methodMatch[1].toUpperCase()} ${currentPath}`);
    }
  }
  return routes;
}

// ---- 3) 比对 ----

const registered = collectRegisteredRoutes();
const declared = collectSpecRoutes();

const missing = [...registered].filter((r) => !declared.has(r)).sort();
const phantom = [...declared].filter((r) => !registered.has(r)).sort();

if (missing.length === 0 && phantom.length === 0) {
  console.log(
    `routes ok — ${registered.size} registered endpoints all declared in the spec`,
  );
  process.exit(0);
}

if (missing.length > 0) {
  console.error(`\n${missing.length} route(s) exist but are NOT in the spec:`);
  for (const r of missing) console.error(`  ${r}`);
}
if (phantom.length > 0) {
  console.error(
    `\n${phantom.length} route(s) declared in the spec but NOT registered:`,
  );
  for (const r of phantom) console.error(`  ${r}`);
  console.error(
    "\n(实现这些端点，或从 spec 删除——照着不存在的端点写客户端只会拿到 404)",
  );
}

console.error(
  `\nspec: ${SPEC}\nregistered: ${registered.size}, declared: ${declared.size}`,
);
process.exitCode = 1;

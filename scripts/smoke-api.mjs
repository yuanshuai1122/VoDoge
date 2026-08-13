#!/usr/bin/env node
/**
 * VoHive API 冒烟脚本。
 *
 * 覆盖前端 MVP 依赖的主路径：登录 → 系统信息 → 设备列表 → 短信会话 →
 * 卡策略 → 代理概览 → SSE 日志流。可选发送一条短信。
 *
 * 不引入依赖，使用 Node 内置 fetch（Node 18+）。
 *
 * 用法：
 *   node scripts/smoke-api.mjs
 *   VOHIVE_URL=http://192.168.1.10:7575 VOHIVE_PASS=xxx node scripts/smoke-api.mjs
 *   SMOKE_SEND_TO=+8613800138000 SMOKE_DEVICE=dev1 node scripts/smoke-api.mjs
 *
 * 环境变量：
 *   VOHIVE_URL   默认 http://127.0.0.1:7575
 *   VOHIVE_USER  默认 admin
 *   VOHIVE_PASS  默认 admin123
 *   SMOKE_SEND_TO / SMOKE_DEVICE  同时提供时才会真的发送短信
 */

const BASE = (process.env.VOHIVE_URL ?? "http://127.0.0.1:7575").replace(/\/$/, "");
const USER = process.env.VOHIVE_USER ?? "admin";
const PASS = process.env.VOHIVE_PASS ?? "admin123";

let token = "";
let failures = 0;

function log(status, name, detail = "") {
  const mark = status === "ok" ? "PASS" : status === "skip" ? "SKIP" : "FAIL";
  if (status === "fail") failures += 1;
  console.log(`[${mark}] ${name}${detail ? ` — ${detail}` : ""}`);
}

async function call(method, path, { json, raw = false } = {}) {
  const headers = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  if (json !== undefined) headers["Content-Type"] = "application/json";

  const res = await fetch(`${BASE}/api${path}`, {
    method,
    headers,
    body: json === undefined ? undefined : JSON.stringify(json),
    signal: AbortSignal.timeout(30_000),
  });

  const text = await res.text();
  let body = text;
  try {
    body = JSON.parse(text);
  } catch {
    /* 非 JSON 原样返回 */
  }

  if (!res.ok) {
    const msg =
      (body && (body.message || body.error)) || `HTTP ${res.status}`;
    throw new Error(msg);
  }
  return raw ? text : body;
}

async function step(name, fn) {
  try {
    const detail = await fn();
    log("ok", name, detail ?? "");
  } catch (e) {
    log("fail", name, e.message);
  }
}

async function main() {
  console.log(`VoHive 冒烟测试 → ${BASE}\n`);

  await step("登录", async () => {
    const r = await call("POST", "/auth/login", {
      json: { username: USER, password: PASS },
    });
    if (!r?.token) throw new Error("响应中没有 token");
    token = r.token;
    return `token 有效期至 ${r.expires_at ?? "未知"}`;
  });

  if (!token) {
    console.log("\n登录失败，后续检查无法进行。");
    process.exit(1);
  }

  // 鉴权必须真的生效：无 token 的请求应当被拒绝
  await step("未授权请求被拒绝", async () => {
    const saved = token;
    token = "";
    try {
      await call("GET", "/devices");
      throw new Error("无 token 竟然成功了");
    } catch (e) {
      if (e.message === "无 token 竟然成功了") throw e;
      return "按预期返回 401";
    } finally {
      token = saved;
    }
  });

  await step("系统信息", async () => {
    const r = await call("GET", "/system/info");
    return typeof r === "object" ? "已返回" : "响应异常";
  });

  let devices = [];
  await step("设备列表", async () => {
    const r = await call("GET", "/devices");
    devices = r?.devices ?? [];
    return `${devices.length} 台设备`;
  });

  if (devices.length > 0) {
    const id = devices[0].id;

    await step("设备概览", async () => {
      const r = await call("GET", `/devices/${encodeURIComponent(id)}/overview`);
      const item = r?.devices?.[0];
      if (!item) throw new Error("overview 未返回 devices[0]");
      return `${item.name || item.id} · ${item.lifecycle_phase}`;
    });

    const iccid = devices[0]?.modem?.iccid;
    if (iccid) {
      await step("卡策略", async () => {
        const r = await call("GET", `/cards/${encodeURIComponent(iccid)}/policy`);
        return `source=${r?.source ?? "?"}`;
      });
    } else {
      log("skip", "卡策略", "设备未读取到 ICCID");
    }
  } else {
    log("skip", "设备概览", "无设备");
    log("skip", "卡策略", "无设备");
  }

  await step("短信会话列表", async () => {
    const r = await call("GET", "/sms/contacts?limit=5");
    if (!Array.isArray(r)) throw new Error("期望裸数组");
    return `${r.length} 个会话`;
  });

  await step("代理概览", async () => {
    const r = await call("GET", "/proxy-instances/overview");
    return `${r?.instances?.length ?? 0} 个实例`;
  });

  await step("通知设置", async () => {
    const r = await call("GET", "/settings/notifications");
    return typeof r === "object" ? "已返回" : "响应异常";
  });

  // SSE 走 query token（后端仅对流式端点白名单开放）
  await step("日志流 SSE", async () => {
    // 用 AbortController 而非读完 body：这是个不会结束的流。
    // 必须 abort 而不是 body.cancel()——后者在 Windows 上会让 libuv 在
    // 进程退出时命中 UV_HANDLE_CLOSING 断言。
    const ac = new AbortController();
    const timer = setTimeout(() => ac.abort(), 8000);
    try {
      const res = await fetch(
        `${BASE}/api/logs/stream?level=info&token=${encodeURIComponent(token)}`,
        { signal: ac.signal },
      );
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const ct = res.headers.get("content-type") ?? "";
      if (!ct.includes("text/event-stream")) {
        throw new Error(`content-type 非 SSE：${ct}`);
      }
      return "已建立事件流";
    } finally {
      clearTimeout(timer);
      ac.abort();
    }
  });

  const to = process.env.SMOKE_SEND_TO;
  const dev = process.env.SMOKE_DEVICE;
  if (to && dev) {
    await step("发送短信", async () => {
      await call("POST", "/sms/send", {
        json: { phone: to, message: "VoHive smoke test", device_id: dev },
      });
      return `已发往 ${to}`;
    });
  } else {
    log("skip", "发送短信", "需同时设置 SMOKE_SEND_TO 与 SMOKE_DEVICE");
  }

  console.log(`\n${failures === 0 ? "全部通过" : `${failures} 项失败`}`);
  // 设置退出码而非强制 exit，让 Node 自行清理已中止的连接
  process.exitCode = failures === 0 ? 0 : 1;
}

main().catch((e) => {
  console.error("冒烟脚本异常:", e.message);
  process.exitCode = 1;
});

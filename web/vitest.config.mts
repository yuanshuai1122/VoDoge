import { defineConfig } from "vitest/config";
import path from "node:path";

/**
 * 前端测试配置。
 *
 * 优先覆盖 API 归一化层（unwrap/errors/endpoints）——那层是整个前端与后端契约的
 * 唯一接缝，写错了会在**所有**页面上以奇怪的方式表现出来，而 tsc 查不出来：
 * 后端返回的形状在 TypeScript 眼里全是 unknown。
 *
 * 不跑 Next 的构建管线：这些是纯逻辑与组件，用 jsdom 足够，秒级反馈。
 */
export default defineConfig({
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.mts"],
    include: ["**/*.test.{ts,tsx}"],
    exclude: ["node_modules/**", "dist/**", ".next/**"],
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "."),
    },
  },
});

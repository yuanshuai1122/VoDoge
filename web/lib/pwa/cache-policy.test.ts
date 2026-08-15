import { describe, expect, it } from "vitest";
import { isStaticShell, shouldBypassCache } from "./cache-policy";

describe("shouldBypassCache", () => {
  it("/api 与 /ping 以及 SSE 不进缓存", () => {
    expect(shouldBypassCache(new URL("http://x/api/sms/contacts"))).toBe(true);
    expect(shouldBypassCache(new URL("http://x/api"))).toBe(true);
    expect(shouldBypassCache(new URL("http://x/ping"))).toBe(true);
    expect(
      shouldBypassCache(new URL("http://x/anything"), "text/event-stream"),
    ).toBe(true);
    expect(shouldBypassCache(new URL("http://x/sms"))).toBe(false);
  });
});

describe("isStaticShell", () => {
  it("只把壳和静态资源当缓存对象", () => {
    expect(isStaticShell(new URL("http://x/_next/static/a.js"))).toBe(true);
    expect(isStaticShell(new URL("http://x/icons/icon.svg"))).toBe(true);
    expect(isStaticShell(new URL("http://x/manifest.webmanifest"))).toBe(true);
    expect(isStaticShell(new URL("http://x/api/sms/contacts"))).toBe(false);
  });
});

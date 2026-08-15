import { describe, expect, it } from "vitest";
import { en, zh, type MessageKey } from "./messages";
import { interpolate, t } from "./index";

describe("i18n catalogs", () => {
  it("every Chinese key has an English string", () => {
    const missing = (Object.keys(zh) as MessageKey[]).filter((k) => !en[k]);
    expect(missing).toEqual([]);
  });

  it("interpolates placeholders", () => {
    expect(interpolate("已接入 {n} / {limit} 台设备", { n: 2, limit: 5 })).toBe(
      "已接入 2 / 5 台设备",
    );
    expect(t("devices.count", { n: 1, limit: 10 }, "en")).toBe("1 / 10 devices");
  });

  it("defaults to Chinese", () => {
    expect(t("nav.sms")).toBe("短信中心");
  });
});

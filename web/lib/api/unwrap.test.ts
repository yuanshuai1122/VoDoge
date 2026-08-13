import { describe, expect, it } from "vitest";
import { ApiError } from "./errors";
import { ok, pick, pickFirstDevice, pickOr, raw, rawArray } from "./unwrap";

/**
 * 成功响应的形状有六种，前端的原则是**每个 endpoint 显式声明自己期望哪一种**，
 * 不做运行时猜测——猜测会在字段名撞车时静默返回错的数据，比抛错难查得多。
 * 这些测试守的就是"该抛的时候要抛"。
 */

describe("pick", () => {
  it("取包装键的值", () => {
    expect(pick<number[]>({ items: [1, 2] }, "items")).toEqual([1, 2]);
  });

  it("兼容有无 {status:'ok'} 外壳", () => {
    expect(pick<string[]>({ status: "ok", devices: ["a"] }, "devices")).toEqual([
      "a",
    ]);
  });

  // 字段缺失必须抛错而不是返回 undefined：后者会一路飘到渲染层才炸，
  // 那时已经看不出是契约不符还是组件写错。
  it("字段缺失时抛 ApiError", () => {
    expect(() => pick({ other: 1 }, "items")).toThrow(ApiError);
    expect(() => pick(null, "items")).toThrow(/items/);
    expect(() => pick([1, 2], "items")).toThrow(ApiError);
  });

  it("字段存在但为 null 时按存在处理", () => {
    expect(pick<null>({ items: null }, "items")).toBeNull();
  });
});

describe("pickOr", () => {
  it("缺失或为 null 时返回兜底值", () => {
    expect(pickOr({ a: 1 }, "items", [])).toEqual([]);
    expect(pickOr({ items: null }, "items", [])).toEqual([]);
    expect(pickOr(null, "items", [])).toEqual([]);
  });

  it("有值时返回原值", () => {
    expect(pickOr({ items: [1] }, "items", [])).toEqual([1]);
  });
});

describe("rawArray", () => {
  // 后端多处返回裸数组，但无数据时可能是 null；渲染层会直接 .map。
  it("null / 非数组一律归一为空数组", () => {
    expect(rawArray(null)).toEqual([]);
    expect(rawArray(undefined)).toEqual([]);
    expect(rawArray({ a: 1 })).toEqual([]);
  });

  it("数组原样返回", () => {
    expect(rawArray([1, 2])).toEqual([1, 2]);
  });
});

describe("raw", () => {
  it("原样透传", () => {
    const v = { a: 1 };
    expect(raw(v)).toBe(v);
  });
});

describe("ok", () => {
  it("正常响应不抛", () => {
    expect(() => ok({ status: "ok", message: "done" })).not.toThrow();
    expect(() => ok(null)).not.toThrow();
  });

  // 后端有过 200 + {status:"error"} 的自相矛盾响应，这里必须当失败处理。
  it("200 里带 status:error 时抛出", () => {
    expect(() => ok({ status: "error", message: "配置无效" })).toThrow(
      /配置无效/,
    );
  });
});

describe("pickFirstDevice", () => {
  // GET /devices/:id/overview 返回的是 {devices:[单元素]} 而不是单对象，
  // 忘了取 [0] 是很容易犯的错，固化在这里。
  it("从 {devices:[...]} 取第一个", () => {
    expect(pickFirstDevice<{ id: string }>({ devices: [{ id: "a" }] })).toEqual({
      id: "a",
    });
  });

  it("空列表按 404 抛出", () => {
    try {
      pickFirstDevice({ devices: [] });
      expect.unreachable("应当抛出");
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError);
      expect((e as ApiError).httpStatus).toBe(404);
    }
  });

  it("缺 devices 字段时抛出", () => {
    expect(() => pickFirstDevice({})).toThrow(ApiError);
  });
});

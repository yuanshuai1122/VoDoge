import { describe, expect, it } from "vitest";
import { laneLabel, normalizeLane } from "./lane";

describe("normalizeLane", () => {
  it("只认 cn / intl", () => {
    expect(normalizeLane("cn")).toBe("cn");
    expect(normalizeLane(" INTL ")).toBe("intl");
    expect(normalizeLane("")).toBe("");
    expect(normalizeLane("eu")).toBe("");
    expect(normalizeLane(undefined)).toBe("");
  });
});

describe("laneLabel", () => {
  it("空值不显示徽章文字", () => {
    expect(laneLabel("")).toBe("");
    expect(laneLabel("cn")).toBe("国内");
    expect(laneLabel("intl")).toBe("国外");
  });
});

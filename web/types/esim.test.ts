import { describe, expect, it } from "vitest";
import { currentProfile, type EUICCProfiles } from "./esim";

const groups: EUICCProfiles[] = [
  {
    eid: "eid-a",
    aid_hex: "A000",
    profiles: [
      {
        iccid: "8986001",
        name: "国内",
        service_provider_name: "CMCC",
        state: 0,
        state_text: "已禁用",
      },
      {
        iccid: "8986002",
        name: "国外号",
        service_provider_name: "Airalo",
        state: 1,
        state_text: "已启用",
      },
    ],
  },
];

describe("currentProfile", () => {
  it("找出当前启用的那一张", () => {
    const got = currentProfile(groups);
    expect(got?.profile.iccid).toBe("8986002");
    expect(got?.aid_hex).toBe("A000");
  });

  it("没有启用时返回 null", () => {
    const empty: EUICCProfiles[] = [
      { eid: "e", aid_hex: "A", profiles: [{ ...groups[0].profiles[0] }] },
    ];
    expect(currentProfile(empty)).toBeNull();
    expect(currentProfile([])).toBeNull();
  });
});

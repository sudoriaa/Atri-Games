import { describe, expect, it } from "vitest";
import {
  adminActivityLabel,
  gameDeleteConfirmations,
  healthEndpoint,
  matchesUserQuery,
  parseTagInput,
} from "./admin-utils";

describe("parseTagInput", () => {
  it("trims, de-duplicates and accepts Chinese commas", () => {
    expect(parseTagInput(" Arcade, 独立，arcade,  双人  ,")).toEqual(["Arcade", "独立", "双人"]);
  });
});

describe("matchesUserQuery", () => {
  const user = { displayName: "Atri Admin", email: "ADMIN@ATRI.GAMES" };

  it("matches case-insensitively after trimming", () => {
    expect(matchesUserQuery(user, "  atri.games ")).toBe(true);
    expect(matchesUserQuery(user, "visitor")).toBe(false);
  });
});

describe("healthEndpoint", () => {
  it("normalizes trailing slashes and falls back to the default API base", () => {
    expect(healthEndpoint("https://example.test/api/v1/")).toBe("https://example.test/api/v1/health");
    expect(healthEndpoint("")).toBe("/api/v1/health");
  });
});

describe("game lifecycle copy", () => {
  it("uses explicit audit labels for unpublishing and permanent deletion", () => {
    expect(adminActivityLabel("game.unpublished")).toBe("下架游戏");
    expect(adminActivityLabel("game.deleted")).toBe("彻底删除游戏");
    expect(adminActivityLabel("future.action")).toBe("future.action");
  });

  it("warns twice about deleting database data and local files", () => {
    const prompts = gameDeleteConfirmations("测试游戏");

    expect(prompts).toHaveLength(2);
    expect(prompts[0]).toContain("全部数据库关联数据");
    expect(prompts[0]).toContain("对应本地文件");
    expect(prompts[1]).toContain("不可撤销");
  });
});

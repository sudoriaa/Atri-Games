import { describe, expect, it } from "vitest";
import {
  buildAiGameIntegrationPrompt,
  defaultAiGameIntegrationPromptConfig,
  normalizeAtriGameId,
} from "./ai-game-integration-prompt";

function manifestFrom(prompt: string) {
  const match = prompt.match(/~~~json\n([\s\S]+?)\n~~~/);
  const json = match?.[1];
  if (!json) throw new Error("generated prompt is missing its manifest JSON block");
  return JSON.parse(json) as Record<string, any>;
}

describe("Atri AI game integration prompt", () => {
  it("keeps the detailed platform delivery contract in the default prompt", () => {
    const prompt = buildAiGameIntegrationPrompt(defaultAiGameIntegrationPromptConfig);
    const manifest = manifestFrom(prompt);

    expect(prompt.length).toBeGreaterThan(12_000);
    expect(prompt).toContain("atri-game.json");
    expect(prompt).toContain("/sdk/atri-game-sdk.js");
    expect(prompt).toContain("atri.storage.get(key)");
    expect(prompt).toContain("atri.matchmaking.join");
    expect(prompt).toContain("pnpm game-kit validate");
    expect(prompt).toContain("pnpm game-kit pack");
    expect(prompt).toContain("ATRIENC1");
    expect(prompt).toContain("--public-key /path/to/atri-package-public.pem");
    expect(prompt).toContain("最终回复格式");
    expect(prompt).toContain("可直接导入 Atri Games");
		expect(prompt).toContain("提交内容规范（强制自检）");
		expect(prompt).toContain("赌博博彩");
		expect(prompt).toContain("违规引流");
		expect(prompt).toContain("内容合规自检");
    expect(manifest.schemaVersion).toBe(2);
    expect(manifest.runtime.kind).toBe("static");
    expect(manifest.services).not.toHaveProperty("storage");
  });

  it("generates a login, SQLite and matchmaking profile for a static game", () => {
    const prompt = buildAiGameIntegrationPrompt({
      ...defaultAiGameIntegrationPromptConfig,
      projectName: "星轨竞技场",
      gameId: "Star Rail Arena",
      runtime: "static",
      identity: "required",
      storage: "sqlite",
      matchmaking: true,
    });
    const manifest = manifestFrom(prompt);

    expect(prompt).toContain("星轨竞技场");
    expect(prompt).toContain('"id": "star-rail-arena"');
    expect(prompt).toContain('"kind": "static"');
    expect(prompt).toContain('"mode": "required"');
    expect(prompt).toContain('"provider": "sqlite"');
    expect(prompt).toContain('"enabled": true');
    expect(prompt).toContain('目录是否应标记“需登录”：是');
    expect(manifest.services).toMatchObject({
      networkRequired: true,
      identity: { mode: "required" },
      storage: { provider: "sqlite", scope: "player-game" },
      matchmaking: { enabled: true, protocol: "http" },
    });
  });

  it("turns off built-in platform services for an external game", () => {
    const prompt = buildAiGameIntegrationPrompt({
      ...defaultAiGameIntegrationPromptConfig,
      runtime: "external",
      externalUrl: "https://games.example.test/arena/",
      identity: "required",
      storage: "sqlite",
      matchmaking: true,
    });
    const manifest = manifestFrom(prompt);

    expect(prompt).toContain('"kind": "external"');
    expect(prompt).toContain('"url": "https://games.example.test/arena/"');
    expect(prompt).toContain('"mode": "none"');
    expect(prompt).toContain('"provider": "none"');
    expect(prompt).toContain('"enabled": false');
    expect(prompt).toContain("external 继续使用自己的身份、数据库、匹配和实时后端");
    expect(prompt).not.toContain("显式使用平台 SQLite：Manifest");
    expect(prompt).not.toContain("开启 Atri 内置匹配：Manifest");
    expect(prompt).not.toContain('const sdkUrl = "/sdk/atri-game-sdk.js"');
    expect(prompt).not.toContain("平台从门户启动 static 游戏时，通过 URL fragment");
    expect(manifest.services).toMatchObject({
      identity: { mode: "none" },
      storage: { provider: "none", scope: "game" },
      matchmaking: { enabled: false, protocol: "http" },
    });
  });

  it("explains both outcomes without applying static capabilities to the external auto branch", () => {
    const prompt = buildAiGameIntegrationPrompt({
      ...defaultAiGameIntegrationPromptConfig,
      runtime: "auto",
      storage: "sqlite",
      matchmaking: true,
    });

    expect(prompt).toContain("若审计后选择 `static`");
    expect(prompt).toContain("若审计后选择 `external`");
    expect(prompt).toContain("只有审计结果为 `static` 时才执行本小节");
    expect(prompt).toContain("external 游戏不使用 Atri SQLite");
    expect(prompt).toContain("external 游戏不使用 Atri 内置匹配");
  });

  it("treats explicit player SQLite as a login and network gate even without identity scope", () => {
    const prompt = buildAiGameIntegrationPrompt({
      ...defaultAiGameIntegrationPromptConfig,
      runtime: "static",
      identity: "none",
      storage: "sqlite",
      matchmaking: false,
    });
    const manifest = manifestFrom(prompt);

    expect(manifest.services.identity.mode).toBe("none");
    expect(manifest.services.networkRequired).toBe(true);
    expect(prompt).toContain('目录是否应标记“需登录”：是');
    expect(prompt).toContain("显式 SQLite player scope 仍会让目录显示“需登录”并在启动前拦截匿名玩家");
  });

  it("normalizes a proposed permanent id to manifest-safe kebab-case", () => {
    expect(normalizeAtriGameId("  我的 GAME__2026 !! ")).toBe("game-2026");
    expect(normalizeAtriGameId("x")).toBe("my-ai-game");
    expect(normalizeAtriGameId(`${"a".repeat(63)}-b`)).toBe("a".repeat(63));
  });
});

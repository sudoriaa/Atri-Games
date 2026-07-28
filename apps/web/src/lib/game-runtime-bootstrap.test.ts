import { describe, expect, it } from "vitest";
import runtimeBootstrap from "../../public/sdk/atri-game-runtime-bootstrap.js?raw";

type BootstrapWindow = {
  name: string;
  location: { hash: string; pathname: string; search: string };
  history: { state: unknown; replaceState: (...args: unknown[]) => void };
  __ATRI_GAME_CONTEXT__?: Record<string, unknown>;
};

function runBootstrap(input: { name?: string; hash?: string; context?: Record<string, unknown> }) {
  const replacements: unknown[][] = [];
  const window: BootstrapWindow = {
    name: input.name ?? "",
    location: {
      hash: input.hash ?? "",
      pathname: "/playables/find-mzk/index.html",
      search: "",
    },
    history: {
      state: null,
      replaceState: (...args) => replacements.push(args),
    },
    ...(input.context ? { __ATRI_GAME_CONTEXT__: input.context } : {}),
  };
  new Function("window", runtimeBootstrap)(window);
  return { window, replacements };
}

describe("Atri runtime bootstrap", () => {
  it("consumes the trusted window.name handoff before a game router sees a legacy hash", () => {
    const handoff = JSON.stringify({
      source: "atri-game-launch",
      version: 1,
      ticket: "fresh-ticket",
      gameSlug: "find-mzk",
      apiBaseUrl: "/api/v1",
    });
    const { window, replacements } = runBootstrap({
      name: handoff,
      hash: "#/level/2?difficulty=hard&atri_ticket=old-ticket&atri_game=old-game",
      context: { integration: "keep-me" },
    });

    expect(window.name).toBe("");
    expect(window.__ATRI_GAME_CONTEXT__).toMatchObject({
      integration: "keep-me",
      ticket: "fresh-ticket",
      gameSlug: "find-mzk",
      apiBaseUrl: "/api/v1",
    });
    expect(replacements).toHaveLength(1);
    expect(replacements[0]?.[2]).toBe("/playables/find-mzk/index.html#/level/2?difficulty=hard");
  });

  it("repairs the legacy #/atri_ticket form without consuming an unrelated window.name", () => {
    const { window, replacements } = runBootstrap({
      name: "a game-owned window name",
      hash: "#/atri_ticket=legacy-ticket&atri_game=find-mzk&atri_api=%2Fapi%2Fv1",
    });

    expect(window.name).toBe("a game-owned window name");
    expect(window.__ATRI_GAME_CONTEXT__).toMatchObject({
      ticket: "legacy-ticket",
      gameSlug: "find-mzk",
      apiBaseUrl: "/api/v1",
    });
    expect(replacements[0]?.[2]).toBe("/playables/find-mzk/index.html#/");
  });
});

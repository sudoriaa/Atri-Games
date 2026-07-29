import test from "node:test";
import assert from "node:assert/strict";
import { createAtriGame, version } from "../src/index.js";

function fixtureTicket(payload) {
  return `eyJhbGciOiJIUzI1NiJ9.${Buffer.from(JSON.stringify(payload)).toString("base64url")}.signature`;
}

test("SDK works as a no-op in a normal top-level runtime", async () => {
  const game = createAtriGame();
  assert.equal(game.gameId, undefined);
  assert.equal(version, "0.1.0");
  assert.doesNotThrow(() => game.ready({ phase: "boot" }).progress(2).score(42));
  assert.equal(await game.fullscreen(), false);
  game.dispose();
});

test("SDK emits local lifecycle events without a host", () => {
  const game = createAtriGame();
  const events = [];
  const off = game.on("ready", (details) => events.push(details));
  game.ready({ ok: true });
  off();
  game.ready({ ok: false });
  assert.deepEqual(events, [{ ok: true }]);
  game.dispose();
});

test("SDK maps the platform menu state to pause and resume lifecycle events", () => {
  const previousAdd = Object.getOwnPropertyDescriptor(globalThis, "addEventListener");
  const previousRemove = Object.getOwnPropertyDescriptor(globalThis, "removeEventListener");
  const listeners = new Map();
  Object.defineProperty(globalThis, "addEventListener", {
    configurable: true,
    value: (type, listener) => listeners.set(type, listener),
  });
  Object.defineProperty(globalThis, "removeEventListener", {
    configurable: true,
    value: (type) => listeners.delete(type),
  });
  try {
    const game = createAtriGame();
    const events = [];
    game.on("pause", () => events.push("pause"));
    game.on("resume", () => events.push("resume"));
    listeners.get("atri-platform-menu")?.({ detail: { open: true } });
    listeners.get("atri-platform-menu")?.({ detail: { open: false } });
    assert.deepEqual(events, ["pause", "resume"]);
    game.dispose();
    assert.equal(listeners.has("atri-platform-menu"), false);
  } finally {
    if (previousAdd) Object.defineProperty(globalThis, "addEventListener", previousAdd);
    else delete globalThis.addEventListener;
    if (previousRemove) Object.defineProperty(globalThis, "removeEventListener", previousRemove);
    else delete globalThis.removeEventListener;
  }
});

test("SDK exposes game-scoped storage and matchmaking helpers", async () => {
  const calls = [];
  const previousFetch = globalThis.fetch;
  const previousContext = globalThis.__ATRI_GAME_CONTEXT__;
  const gameTicket = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c3ItdGVzdCIsImdhbWVJZCI6ImdhbWVfMSJ9.signature";
  globalThis.__ATRI_GAME_CONTEXT__ = {
    gameSlug: "demo-game",
    ticket: gameTicket,
    apiBaseUrl: "https://atri.example/api/v1",
  };
  globalThis.fetch = async (url, init) => {
    calls.push({ url, init });
    return {
      ok: true,
      status: 200,
      async json() {
        if (String(url).includes("/data/")) return { key: "progress", value: { level: 2 }, updatedAt: "2026-01-01T00:00:00Z" };
        return { ticketId: "queue-1", status: "queued", gameId: "demo-game", mode: "ranked", region: "auto", createdAt: "2026-01-01T00:00:00Z", expiresAt: "2026-01-01T00:01:00Z" };
      },
    };
  };
  try {
    const game = createAtriGame();
    assert.equal(game.authenticated, true);
    assert.equal(game.userId, "usr-test");
    assert.equal(game.gameId, "game_1");
    assert.equal(await game.identity.getTicket(), gameTicket);
    await game.storage.set("progress", { level: 2 });
    const saved = await game.storage.get("progress");
    const queue = await game.matchmaking.join({ mode: "ranked" });
    assert.equal(saved.value.level, 2);
    assert.equal(queue.ticketId, "queue-1");
    assert.equal(calls[0].init.headers.get("Authorization"), `Bearer ${gameTicket}`);
    assert.match(calls[0].url, /\/games\/demo-game\/data\/progress$/);
    assert.match(calls[2].url, /\/matchmaking\/tickets$/);
    game.dispose();
  } finally {
    globalThis.fetch = previousFetch;
    if (previousContext === undefined) delete globalThis.__ATRI_GAME_CONTEXT__;
    else globalThis.__ATRI_GAME_CONTEXT__ = previousContext;
  }
});

test("SDK consumes and clears the portal window.name handoff", () => {
  const previousName = Object.getOwnPropertyDescriptor(globalThis, "name");
  const previousContext = globalThis.__ATRI_GAME_CONTEXT__;
  Object.defineProperty(globalThis, "name", {
    configurable: true,
    writable: true,
    value: JSON.stringify({
      source: "atri-game-launch",
      version: 1,
      ticket: "handoff-ticket",
      gameSlug: "handoff-game",
      apiBaseUrl: "/api/v1",
    }),
  });
  delete globalThis.__ATRI_GAME_CONTEXT__;
  try {
    const game = createAtriGame();
    assert.equal(game.ticket, "handoff-ticket");
    assert.equal(game.gameSlug, "handoff-game");
    assert.equal(game.apiBaseUrl, "/api/v1");
    assert.equal(globalThis.name, "");
    game.dispose();
  } finally {
    if (previousName) Object.defineProperty(globalThis, "name", previousName);
    else delete globalThis.name;
    if (previousContext === undefined) delete globalThis.__ATRI_GAME_CONTEXT__;
    else globalThis.__ATRI_GAME_CONTEXT__ = previousContext;
  }
});

test("SDK consumes platform fragment values without rewriting a game's hash route", () => {
  const previousLocation = Object.getOwnPropertyDescriptor(globalThis, "location");
  const previousHistory = Object.getOwnPropertyDescriptor(globalThis, "history");
  const replaced = [];
  Object.defineProperty(globalThis, "location", {
    configurable: true,
    value: { hash: "#/level/2?difficulty=hard&atri_ticket=secret&atri_game=demo-game", pathname: "/playables/demo-game/index.html", search: "" },
  });
  Object.defineProperty(globalThis, "history", {
    configurable: true,
    value: { replaceState: (...args) => replaced.push(args) },
  });
  try {
    const game = createAtriGame();
    assert.equal(game.ticket, "secret");
    assert.equal(game.gameSlug, "demo-game");
    assert.equal(replaced[0][2], "/playables/demo-game/index.html#/level/2?difficulty=hard");
    game.dispose();
  } finally {
    if (previousLocation) Object.defineProperty(globalThis, "location", previousLocation);
    else delete globalThis.location;
    if (previousHistory) Object.defineProperty(globalThis, "history", previousHistory);
    else delete globalThis.history;
  }
});

test("SDK repairs the legacy bare platform hash before a hash router starts", () => {
  const previousLocation = Object.getOwnPropertyDescriptor(globalThis, "location");
  const previousHistory = Object.getOwnPropertyDescriptor(globalThis, "history");
  const replaced = [];
  Object.defineProperty(globalThis, "location", {
    configurable: true,
    value: { hash: "#/atri_ticket=secret&atri_game=demo-game&atri_api=%2Fapi%2Fv1", pathname: "/playables/demo-game/index.html", search: "" },
  });
  Object.defineProperty(globalThis, "history", {
    configurable: true,
    value: { replaceState: (...args) => replaced.push(args) },
  });
  try {
    const game = createAtriGame();
    assert.equal(game.ticket, "secret");
    assert.equal(game.gameSlug, "demo-game");
    assert.equal(game.apiBaseUrl, "/api/v1");
    assert.equal(replaced[0][2], "/playables/demo-game/index.html");
    game.dispose();
  } finally {
    if (previousLocation) Object.defineProperty(globalThis, "location", previousLocation);
    else delete globalThis.location;
    if (previousHistory) Object.defineProperty(globalThis, "history", previousHistory);
    else delete globalThis.history;
  }
});

test("SDK refreshes a near-expiry game ticket before a platform request", async () => {
  const previousContext = globalThis.__ATRI_GAME_CONTEXT__;
  const calls = [];
  const current = fixtureTicket({ sub: "usr-refresh", gameId: "game-refresh", exp: Math.floor(Date.now() / 1000) + 30 });
  const refreshed = fixtureTicket({ sub: "usr-refresh", gameId: "game-refresh", exp: Math.floor(Date.now() / 1000) + 900 });
  globalThis.__ATRI_GAME_CONTEXT__ = {
    gameSlug: "refresh-game",
    ticket: current,
    apiBaseUrl: "https://atri.example/api/v1",
  };
  try {
    const game = createAtriGame({
      fetch: async (url) => {
        calls.push(String(url));
        if (String(url).endsWith("/ticket/refresh")) {
          return { ok: true, status: 200, json: async () => ({ ticket: refreshed }) };
        }
        return { ok: true, status: 200, json: async () => ({ key: "progress", value: { level: 4 }, updatedAt: "2026-01-01T00:00:00Z" }) };
      },
    });
    const record = await game.storage.get("progress");
    assert.equal(record.value.level, 4);
    assert.equal(game.ticket, refreshed);
    assert.equal(calls.filter((url) => url.endsWith("/ticket/refresh")).length, 1);
    game.dispose();
  } finally {
    if (previousContext === undefined) delete globalThis.__ATRI_GAME_CONTEXT__;
    else globalThis.__ATRI_GAME_CONTEXT__ = previousContext;
  }
});

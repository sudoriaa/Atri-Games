import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import assert from "node:assert/strict";
import { requiresPlayerLogin, resolvePlatformServices, starterManifest, validateManifest } from "../src/manifest.mjs";

test("accepts a static package with local files", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "atri-game-kit-"));
  await mkdir(path.join(root, "game"), { recursive: true });
  await writeFile(path.join(root, "cover.webp"), "fixture");
  await writeFile(path.join(root, "game", "index.html"), "<!doctype html><script src='./main.js'></script>");
  const result = await validateManifest(starterManifest("static-fixture"), { rootDir: root, requirePackageFiles: true });
  assert.deepEqual(result.errors, []);
});

test("rejects an external URL that is not HTTPS", async () => {
  const manifest = starterManifest("external-fixture");
  manifest.runtime = { kind: "external", url: "http://localhost:3000", openIn: "same-tab" };
  const result = await validateManifest(manifest);
  assert.ok(result.errors.some((item) => item.includes("runtime.url")));
});

test("rejects unsafe package paths", async () => {
  const manifest = starterManifest("unsafe-fixture");
  manifest.media.cover = "../secret.webp";
  const result = await validateManifest(manifest);
  assert.ok(result.errors.some((item) => item.includes("media.cover")));
});

test("accepts optional schema and AI disclosure metadata", async () => {
  const manifest = starterManifest("metadata-fixture");
  manifest.ai = { tools: ["image-generation"], disclosure: "Generated cover art is disclosed in the credits." };
  const result = await validateManifest(manifest);
  assert.deepEqual(result.errors, []);
});

test("accepts opt-in identity, SQLite storage, and built-in matchmaking", async () => {
  const manifest = starterManifest("platform-fixture");
  manifest.services = {
    ...manifest.services,
    identity: { mode: "required" },
    storage: { provider: "sqlite", scope: "player-game" },
    matchmaking: { enabled: true, protocol: "websocket" },
  };
  const result = await validateManifest(manifest);
  assert.deepEqual(result.errors, []);
  assert.equal(requiresPlayerLogin(manifest), true);
  assert.equal(resolvePlatformServices(manifest).storage.provider, "sqlite");
});

test("keeps the generated starter package anonymous", async () => {
  const manifest = starterManifest("anonymous-fixture");
  const result = await validateManifest(manifest);
  assert.deepEqual(result.errors, []);
  assert.equal(requiresPlayerLogin(manifest), false);
  assert.deepEqual(resolvePlatformServices(manifest).storage, { provider: "sqlite", scope: "player-game" });
});

test("rejects built-in services for external games", async () => {
  const manifest = starterManifest("external-platform-fixture");
  manifest.runtime = { kind: "external", url: "https://example.com/game", openIn: "same-tab" };
  manifest.services = {
    ...manifest.services,
    storage: { provider: "sqlite", scope: "player-game" },
  };
  const result = await validateManifest(manifest);
  assert.ok(result.errors.some((item) => item.includes("external games cannot use built-in storage")));
});

test("rejects a player scope without a storage provider", async () => {
  const manifest = starterManifest("invalid-storage-fixture");
  manifest.services.storage = { provider: "none", scope: "player" };
  const result = await validateManifest(manifest);
  assert.ok(result.errors.some((item) => item.includes("storage.scope must be game")));
});

test("rejects a globally shared writable SQLite scope", async () => {
  const manifest = starterManifest("shared-storage-fixture");
  manifest.services.storage = { provider: "sqlite", scope: "game" };
  const result = await validateManifest(manifest);
  assert.ok(result.errors.some((item) => item.includes("scope must be player-game or player")));
});

test("rejects fields outside the versioned contract", async () => {
  const manifest = starterManifest("unknown-field-fixture");
  manifest.runtime.command = "node server.js";
  const result = await validateManifest(manifest);
  assert.ok(result.errors.some((item) => item.includes("runtime.command")));
});

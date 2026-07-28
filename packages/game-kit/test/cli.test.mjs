import { execFile } from "node:child_process";
import { constants, generateKeyPairSync, privateDecrypt } from "node:crypto";
import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import test from "node:test";
import assert from "node:assert/strict";
import { starterManifest } from "../src/manifest.mjs";

const run = promisify(execFile);
const cli = fileURLToPath(new URL("../src/cli.mjs", import.meta.url));

async function packageFixture() {
  const root = await mkdtemp(path.join(os.tmpdir(), "atri-game-cli-"));
  await mkdir(path.join(root, "game"), { recursive: true });
  await writeFile(path.join(root, "atri-game.json"), `${JSON.stringify(starterManifest("cli-fixture"), null, 2)}\n`);
  await writeFile(path.join(root, "cover.webp"), "fixture");
  await writeFile(path.join(root, "game", "index.html"), "<!doctype html><title>CLI fixture</title>");
  return root;
}

test("pack encrypts by default and supports --out without an explicit manifest argument", async () => {
  const root = await packageFixture();
  const output = path.join(root, "cli-fixture.atri");

  const result = await run(process.execPath, [cli, "pack", "--out", output], { cwd: root });
  assert.match(result.stdout, /encrypted ATRIENC1/);
  const signature = await readFile(output);
  assert.equal(signature.subarray(0, 8).toString("ascii"), "ATRIENC1");
  assert.notDeepEqual([...signature.subarray(0, 4)], [0x50, 0x4b, 0x03, 0x04]);
});

test("pack --plain retains explicit legacy ZIP compatibility", async () => {
  const root = await packageFixture();
  const output = path.join(root, "cli-fixture-plain.atri");

  const result = await run(process.execPath, [cli, "pack", "--out", output, "--plain"], { cwd: root });
  assert.match(result.stdout, /plaintext compatibility/);
  const signature = await readFile(output);
  assert.deepEqual([...signature.subarray(0, 4)], [0x50, 0x4b, 0x03, 0x04]);
});

test("pack accepts an explicit platform public key", async () => {
  const root = await packageFixture();
  const output = path.join(root, "cli-fixture-custom-key.atri");
  const publicKeyPath = path.join(root, "custom-platform-public.pem");
  const { publicKey, privateKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" },
  });
  await writeFile(publicKeyPath, publicKey);

  await run(process.execPath, [cli, "pack", "--out", output, "--public-key", publicKeyPath], { cwd: root });
  const signature = await readFile(output);
  assert.equal(signature.subarray(0, 8).toString("ascii"), "ATRIENC1");
  const wrappedKeyLength = signature.readUInt32BE(9);
  const wrappedKey = signature.subarray(19, 19 + wrappedKeyLength);
  const contentKey = privateDecrypt(
    { key: privateKey, padding: constants.RSA_PKCS1_OAEP_PADDING, oaepHash: "sha256" },
    wrappedKey,
  );
  assert.equal(contentKey.length, 32);
});

#!/usr/bin/env node

import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { encryptAtriArchive } from "./encryption.mjs";
import { readManifest, requiresPlayerLogin, resolvePlatformServices, starterManifest } from "./manifest.mjs";
import { collectFiles, createZip } from "./zip.mjs";

const defaultPublicKeyPath = fileURLToPath(new URL("../keys/atri-platform-public.pem", import.meta.url));

function usage() {
  console.log(`Atri Game Kit — prepare a technology-neutral game package

Commands:
  atri-game init [directory]                 Create atri-game.json
  atri-game validate [manifest]              Validate metadata and packaged files
  atri-game pack [manifest] [--out FILE]     Build an encrypted .atri package

Static packages use:
  atri-game.json
  cover.webp (or the path in media.cover)
  game/                     built browser output
  game/<runtime.entry>      entry page

External packages omit game/ and set runtime.kind=external with an HTTPS URL.

Optional built-in services are declared in services:
  identity.mode                  none | optional | required
  storage.provider/scope         none|sqlite + game|player|player-game
  matchmaking.enabled/protocol  false/true + websocket|sse|http

Static packages retain a compatibility SQLite metadata fallback when storage
is omitted, but only an explicit services.storage declaration requests a game
ticket and makes the storage API available. Set storage.provider to "none" to
fully disable it. External games cannot use Atri's built-in identity, storage,
or matchmaking.

Package encryption:
  pack encrypts the internal ZIP by default as an ATRIENC1 container. Its
  AES-256-GCM content key is wrapped with the platform RSA-OAEP-SHA256 public
  key. The default public key is bundled with this tool; use --public-key FILE
  for a compatible platform key, or --plain only for legacy ZIP importers.

  --public-key FILE     PEM public key used to wrap the package content key
  --plain                Explicitly emit a legacy plaintext ZIP .atri package
`);
}

function option(args, name, fallback = null) {
  const index = args.indexOf(name);
  if (index < 0) return fallback;
  const value = args[index + 1];
  if (!value || value.startsWith("--")) throw new Error(`${name} requires a value`);
  return value;
}

async function init(directory) {
  const root = path.resolve(directory);
  await mkdir(root, { recursive: true });
  const target = path.join(root, "atri-game.json");
  const id = path.basename(root).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 48) || "my-game";
  const safeId = id.length >= 3 ? id : "my-game";
  try {
    await readFile(target);
    throw new Error(`${target} already exists`);
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }
  await writeFile(target, `${JSON.stringify(starterManifest(safeId), null, 2)}\n`, "utf8");
  console.log(`created ${target}`);
}

async function load(manifestArg) {
  const manifestPath = path.resolve(manifestArg || "atri-game.json");
  const result = await readManifest(manifestPath, { rootDir: path.dirname(manifestPath), requirePackageFiles: true });
  for (const warning of result.warnings) console.warn(`warning: ${warning}`);
  const platform = resolvePlatformServices(result.manifest);
  const login = requiresPlayerLogin(result.manifest) ? "login required" : "anonymous play";
  console.log(
    `platform: ${login}; storage=${platform.storage.provider}/${platform.storage.scope}; matchmaking=${platform.matchmaking.enabled ? platform.matchmaking.protocol : "off"}`,
  );
  return { ...result, manifestPath, rootDir: path.dirname(manifestPath) };
}

async function pack(manifestArg, args) {
  const { manifest, manifestPath, rootDir } = await load(manifestArg);
  const entries = [{ source: manifestPath, name: "atri-game.json" }];
  const add = async (relative, name = relative) => {
    entries.push({ source: path.join(rootDir, relative), name });
  };
  await add(manifest.media.cover);
  for (const screenshot of manifest.media.screenshots ?? []) await add(screenshot);
  if (manifest.runtime.kind === "static") {
    for (const file of await collectFiles(path.join(rootDir, "game"))) entries.push({ source: file.source, name: `game/${file.name}` });
  }
  const output = path.resolve(option(args, "--out", `${manifest.id}-${manifest.version}.atri`));
  const plaintext = args.includes("--plain") || args.includes("--plaintext");
  const publicKeyOption = option(args, "--public-key");
  if (plaintext && publicKeyOption) throw new Error("--public-key cannot be used with --plain");
  if (plaintext) {
    await createZip(output, entries);
    console.log(`created plaintext compatibility package ${output}`);
    return;
  }

  const publicKeyPath = path.resolve(publicKeyOption ?? defaultPublicKeyPath);
  const publicKey = await readFile(publicKeyPath, "utf8");
  const temporaryDirectory = await mkdtemp(path.join(path.dirname(output), `.${path.basename(output)}-zip-`));
  try {
    const zipPath = path.join(temporaryDirectory, "package.zip");
    await createZip(zipPath, entries);
    await encryptAtriArchive(zipPath, output, publicKey);
  } finally {
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
  console.log(`created encrypted ATRIENC1 package ${output}`);
}

async function main() {
  const [command, firstArg, ...remaining] = process.argv.slice(2);
  const hasManifest = firstArg && !firstArg.startsWith("-");
  const manifestArg = hasManifest ? firstArg : undefined;
  const args = hasManifest ? remaining : [firstArg, ...remaining].filter(Boolean);
  if (!command || command === "--help" || command === "-h") return usage();
  if (command === "init") return init(manifestArg || ".");
  if (command === "validate") {
    const result = await load(manifestArg);
    console.log(`valid: ${result.manifest.id}@${result.manifest.version} (${result.manifest.runtime.kind})`);
    return;
  }
  if (command === "pack") return pack(manifestArg, args);
  throw new Error(`unknown command: ${command}`);
}

main().catch((error) => {
  console.error(`error: ${error instanceof Error ? error.message : String(error)}`);
  process.exitCode = 1;
});

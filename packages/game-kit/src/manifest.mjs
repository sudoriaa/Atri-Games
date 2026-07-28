import { access, readFile, stat } from "node:fs/promises";
import path from "node:path";

const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const semverPattern =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;
const imagePattern = /\.(?:avif|jpe?g|png|webp)$/i;
const identityModes = ["none", "optional", "required"];
const storageProviders = ["none", "sqlite"];
const storageScopes = ["player-game", "player", "game"];
const matchmakingProtocols = ["websocket", "sse", "http"];

export function isSafePackagePath(value) {
  if (typeof value !== "string" || value.length === 0 || value.length > 240) return false;
  if (value.startsWith("/") || value.includes("\\") || value.split("/").some((part) => part === "" || part === "." || part === "..")) {
    return false;
  }
  return value.split("/").every((part) => /^[A-Za-z0-9._-]+$/.test(part));
}

function add(errors, condition, message) {
  if (!condition) errors.push(message);
}

function allowedKeys(value, allowed, field, errors) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return;
  for (const key of Object.keys(value)) {
    if (!allowed.includes(key)) errors.push(`${field}.${key} is not supported`);
  }
}

function enumList(value, allowed, { required = false, max = allowed.length } = {}) {
  return (
    Array.isArray(value) &&
    (!required || value.length > 0) &&
    value.length <= max &&
    new Set(value).size === value.length &&
    value.every((item) => typeof item === "string" && allowed.includes(item))
  );
}

/**
 * Resolve the platform capabilities a package receives at runtime.
 *
 * Keeping defaults here means a developer can start with the generated
 * manifest and only add fields when the game needs a platform feature:
 * static packages get a private SQLite namespace, while external games never
 * receive built-in services.
 */
export function resolvePlatformServices(manifest) {
  const isStatic = manifest?.runtime?.kind === "static";
  const services = manifest?.services ?? {};
  return {
    identity: {
      mode: services.identity?.mode ?? "none",
    },
    storage: {
      provider: services.storage?.provider ?? (isStatic ? "sqlite" : "none"),
      scope: services.storage?.scope ?? (isStatic ? "player-game" : "game"),
    },
    matchmaking: {
      enabled: services.matchmaking?.enabled ?? false,
      protocol: services.matchmaking?.protocol ?? "http",
    },
  };
}

export function requiresPlayerLogin(manifest) {
  const platform = resolvePlatformServices(manifest);
  const declaredStorage = manifest?.services?.storage;
  return (
    platform.identity.mode === "required" ||
    (declaredStorage?.provider === "sqlite" && ["player", "player-game"].includes(declaredStorage.scope)) ||
    platform.matchmaking.enabled
  );
}

function httpsUrl(value, field, errors) {
  try {
    const parsed = new URL(value);
    add(errors, parsed.protocol === "https:" && !parsed.username && !parsed.password, `${field} must be an HTTPS URL without credentials`);
  } catch {
    errors.push(`${field} must be an HTTPS URL`);
  }
}

async function fileExists(rootDir, relative) {
  try {
    const info = await stat(path.join(rootDir, relative));
    return info.isFile();
  } catch {
    return false;
  }
}

export async function validateManifest(manifest, { rootDir = null, requirePackageFiles = false } = {}) {
  const errors = [];
  const warnings = [];
  add(errors, manifest && typeof manifest === "object" && !Array.isArray(manifest), "manifest must be an object");
  if (errors.length) return { errors, warnings };
  allowedKeys(
    manifest,
    [
      "$schema",
      "schemaVersion",
      "id",
      "version",
      "title",
      "summary",
      "description",
      "authors",
      "license",
      "repository",
      "homepage",
      "engine",
      "runtime",
      "services",
      "privacy",
      "media",
      "compatibility",
      "tags",
      "ai",
    ],
    "manifest",
    errors,
  );

  if (manifest.$schema !== undefined) add(errors, typeof manifest.$schema === "string", "$schema must be a string");
  add(errors, manifest.schemaVersion === 2, "schemaVersion must be 2");
  add(errors, typeof manifest.id === "string" && slugPattern.test(manifest.id) && manifest.id.length >= 3 && manifest.id.length <= 64, "id must be 3-64 lowercase kebab-case characters");
  add(errors, typeof manifest.version === "string" && semverPattern.test(manifest.version), "version must be semantic versioning");
  for (const field of ["title", "summary", "description", "license"]) {
    add(errors, typeof manifest[field] === "string" && manifest[field].trim().length > 0, `${field} is required`);
  }
  add(errors, typeof manifest.title === "string" && manifest.title.length <= 80, "title is too long");
  add(errors, typeof manifest.summary === "string" && manifest.summary.length >= 10 && manifest.summary.length <= 240, "summary must be 10-240 characters");
  add(errors, typeof manifest.description === "string" && manifest.description.length <= 4000, "description is too long");
  add(errors, Array.isArray(manifest.authors) && manifest.authors.length > 0 && manifest.authors.length <= 20, "authors must contain 1-20 authors");
  if (Array.isArray(manifest.authors)) {
    manifest.authors.forEach((author, index) => {
      allowedKeys(author, ["name", "url"], `authors[${index}]`, errors);
      add(errors, author && typeof author.name === "string" && author.name.trim().length > 0 && author.name.length <= 80, `authors[${index}].name is required`);
      if (author?.url !== undefined) httpsUrl(author.url, `authors[${index}].url`, errors);
    });
  }
  if (manifest.license !== undefined) add(errors, typeof manifest.license === "string" && manifest.license.length <= 80, "license is too long");
  for (const field of ["repository", "homepage"]) {
    if (manifest[field] !== undefined) httpsUrl(manifest[field], field, errors);
  }
  add(errors, manifest.engine && typeof manifest.engine === "object" && typeof manifest.engine.name === "string" && manifest.engine.name.trim().length > 0, "engine.name is required");
  allowedKeys(manifest.engine, ["name", "version", "framework"], "engine", errors);
  if (manifest.engine?.name !== undefined) add(errors, manifest.engine.name.length <= 80, "engine.name is too long");
  if (manifest.engine?.version !== undefined) add(errors, typeof manifest.engine.version === "string" && manifest.engine.version.length <= 40, "engine.version is invalid");
  if (manifest.engine?.framework !== undefined) add(errors, typeof manifest.engine.framework === "string" && manifest.engine.framework.length <= 80, "engine.framework is invalid");

  const runtime = manifest.runtime;
  add(errors, runtime && typeof runtime === "object", "runtime is required");
  if (runtime?.kind === "external") {
    allowedKeys(runtime, ["kind", "url", "openIn"], "runtime", errors);
    httpsUrl(runtime.url, "runtime.url", errors);
    add(errors, runtime.openIn === "same-tab" || runtime.openIn === "new-tab", "runtime.openIn is invalid");
  } else if (runtime?.kind === "static") {
    allowedKeys(runtime, ["kind", "entry", "openIn", "bridge"], "runtime", errors);
    add(errors, isSafePackagePath(runtime.entry) && /\.html$/i.test(runtime.entry), "runtime.entry must be a safe HTML path");
    add(errors, runtime.openIn === "same-tab" || runtime.openIn === "new-tab", "runtime.openIn is invalid");
    if (runtime.bridge !== undefined) add(errors, ["none", "optional", "required"].includes(runtime.bridge), "runtime.bridge is invalid");
  } else {
    errors.push("runtime.kind must be external or static");
  }

  add(errors, manifest.services && typeof manifest.services === "object", "services is required");
  allowedKeys(
    manifest.services,
    ["networkRequired", "ownBackend", "realtime", "platformIntegrations", "identity", "storage", "matchmaking"],
    "services",
    errors,
  );
  add(errors, typeof manifest.services?.networkRequired === "boolean", "services.networkRequired is required");
  add(errors, typeof manifest.services?.ownBackend === "boolean", "services.ownBackend is required");
  if (manifest.services?.realtime !== undefined) {
    add(errors, enumList(manifest.services.realtime, ["websocket", "server-sent-events", "webrtc", "other"]), "services.realtime is invalid");
  }
  if (manifest.services?.platformIntegrations !== undefined) {
    add(
      errors,
      enumList(manifest.services.platformIntegrations, ["lifecycle", "identity", "cloud-save", "achievements", "leaderboards", "webhooks"]),
      "services.platformIntegrations is invalid",
    );
  }
  if (manifest.services?.identity !== undefined) {
    allowedKeys(manifest.services.identity, ["mode"], "services.identity", errors);
    add(errors, manifest.services.identity && identityModes.includes(manifest.services.identity.mode), "services.identity.mode is invalid");
  }
  if (manifest.services?.storage !== undefined) {
    allowedKeys(manifest.services.storage, ["provider", "scope"], "services.storage", errors);
    add(errors, manifest.services.storage && storageProviders.includes(manifest.services.storage.provider), "services.storage.provider is invalid");
    add(errors, manifest.services.storage && storageScopes.includes(manifest.services.storage.scope), "services.storage.scope is invalid");
    if (manifest.services.storage?.provider === "none") {
      add(errors, manifest.services.storage.scope === "game", "services.storage.scope must be game when provider is none");
    } else if (manifest.services.storage?.provider === "sqlite") {
      add(
        errors,
        ["player-game", "player"].includes(manifest.services.storage.scope),
        "services.storage.scope must be player-game or player when provider is sqlite",
      );
    }
  }
  if (manifest.services?.matchmaking !== undefined) {
    allowedKeys(manifest.services.matchmaking, ["enabled", "protocol"], "services.matchmaking", errors);
    add(errors, typeof manifest.services.matchmaking?.enabled === "boolean", "services.matchmaking.enabled is required");
    add(errors, matchmakingProtocols.includes(manifest.services.matchmaking?.protocol), "services.matchmaking.protocol is invalid");
  }
  if (runtime?.kind === "external") {
    const platform = resolvePlatformServices(manifest);
    add(errors, platform.identity.mode === "none", "external games cannot use built-in identity");
    add(errors, platform.storage.provider === "none", "external games cannot use built-in storage");
    add(errors, !platform.matchmaking.enabled, "external games cannot use built-in matchmaking");
  }

  const privacy = manifest.privacy;
  add(errors, privacy && typeof privacy === "object", "privacy is required");
  allowedKeys(privacy, ["collectsPersonalData", "policyUrl", "dataSummary"], "privacy", errors);
  add(errors, typeof privacy?.collectsPersonalData === "boolean", "privacy.collectsPersonalData is required");
  add(errors, typeof privacy?.dataSummary === "string" && privacy.dataSummary.trim().length >= 10 && privacy.dataSummary.length <= 800, "privacy.dataSummary must be 10-800 characters");
  if (privacy?.collectsPersonalData) {
    add(errors, typeof privacy.policyUrl === "string", "privacy.policyUrl is required when personal data is collected");
    if (privacy.policyUrl) httpsUrl(privacy.policyUrl, "privacy.policyUrl", errors);
  } else if (privacy?.policyUrl !== undefined) {
    httpsUrl(privacy.policyUrl, "privacy.policyUrl", errors);
  }

  const media = manifest.media;
  allowedKeys(media, ["cover", "screenshots"], "media", errors);
  add(errors, media && typeof media === "object" && isSafePackagePath(media.cover) && imagePattern.test(media.cover), "media.cover must be a packaged image path");
  if (media?.screenshots !== undefined) {
    add(errors, Array.isArray(media.screenshots) && media.screenshots.length <= 8, "media.screenshots must contain at most 8 images");
    if (Array.isArray(media.screenshots)) {
      media.screenshots.forEach((item, index) => add(errors, isSafePackagePath(item) && imagePattern.test(item), `media.screenshots[${index}] is invalid`));
    }
  }

  const compatibility = manifest.compatibility;
  add(errors, compatibility && typeof compatibility === "object", "compatibility is required");
  allowedKeys(compatibility, ["devices", "inputs", "orientation", "minimumViewport"], "compatibility", errors);
  add(errors, enumList(compatibility?.devices, ["desktop", "mobile", "tablet"], { required: true }), "compatibility.devices is invalid");
  add(errors, enumList(compatibility?.inputs, ["keyboard", "mouse", "touch", "gamepad"], { required: true }), "compatibility.inputs is invalid");
  add(errors, ["any", "landscape", "portrait"].includes(compatibility?.orientation), "compatibility.orientation is invalid");
  if (compatibility?.minimumViewport !== undefined) {
    allowedKeys(compatibility.minimumViewport, ["width", "height"], "compatibility.minimumViewport", errors);
    add(
      errors,
      Number.isInteger(compatibility.minimumViewport?.width) &&
        compatibility.minimumViewport.width >= 240 &&
        compatibility.minimumViewport.width <= 7680,
      "compatibility.minimumViewport.width is invalid",
    );
    add(
      errors,
      Number.isInteger(compatibility.minimumViewport?.height) &&
        compatibility.minimumViewport.height >= 240 &&
        compatibility.minimumViewport.height <= 4320,
      "compatibility.minimumViewport.height is invalid",
    );
  }
  add(errors, Array.isArray(manifest.tags) && manifest.tags.length > 0 && manifest.tags.length <= 10, "tags must contain 1-10 values");
  if (Array.isArray(manifest.tags)) manifest.tags.forEach((tag, index) => add(errors, typeof tag === "string" && tag.trim().length > 0 && tag.length <= 40, `tags[${index}] is invalid`));
  if (Array.isArray(manifest.tags)) add(errors, new Set(manifest.tags).size === manifest.tags.length, "tags must be unique");

  if (manifest.ai !== undefined) {
    allowedKeys(manifest.ai, ["tools", "disclosure"], "ai", errors);
    add(
      errors,
      Array.isArray(manifest.ai?.tools) &&
        manifest.ai.tools.length <= 20 &&
        new Set(manifest.ai.tools).size === manifest.ai.tools.length &&
        manifest.ai.tools.every((tool) => typeof tool === "string" && tool.trim().length > 0 && tool.length <= 80),
      "ai.tools is invalid",
    );
    add(errors, typeof manifest.ai?.disclosure === "string" && manifest.ai.disclosure.trim().length > 0 && manifest.ai.disclosure.length <= 1000, "ai.disclosure is invalid");
  }

  if (requirePackageFiles && rootDir && errors.length === 0) {
    add(errors, await fileExists(rootDir, manifest.media.cover), `missing packaged cover: ${manifest.media.cover}`);
    if (Array.isArray(manifest.media.screenshots)) {
      for (const screenshot of manifest.media.screenshots) add(errors, await fileExists(rootDir, screenshot), `missing packaged screenshot: ${screenshot}`);
    }
    if (runtime.kind === "static") {
      add(errors, await fileExists(path.join(rootDir, "game"), runtime.entry), `missing static entry: game/${runtime.entry}`);
      const entryPath = path.join(rootDir, "game", runtime.entry);
      try {
        const html = await readFile(entryPath, "utf8");
        if (/(?:src|href)\s*=\s*["']\/(?!\/)/i.test(html)) {
          warnings.push("static entry contains root-relative asset URLs; configure the build with a relative base or /playables/<id>/");
        }
      } catch {
        // The missing-entry error above is the useful diagnostic.
      }
    }
  }
  return { errors, warnings };
}

export async function readManifest(filePath, options = {}) {
  let parsed;
  try {
    parsed = JSON.parse(await readFile(filePath, "utf8"));
  } catch (error) {
    throw new Error(`could not read JSON manifest: ${error instanceof Error ? error.message : String(error)}`);
  }
  const result = await validateManifest(parsed, { ...options, rootDir: options.rootDir ?? path.dirname(filePath) });
  if (result.errors.length) {
    throw new Error(result.errors.join("\n"));
  }
  return { manifest: parsed, warnings: result.warnings };
}

export function starterManifest(id = "my-game") {
  return {
    $schema: "https://atri.games/schemas/game-manifest.schema.json",
    schemaVersion: 2,
    id,
    version: "0.1.0",
    title: "My Game",
    summary: "A short description of the game and what makes one round interesting.",
    description: "Explain the objective, controls, supported devices, and any online behavior.",
    authors: [{ name: "Your Name" }],
    license: "MIT",
    engine: { name: "Your engine or framework" },
    runtime: { kind: "static", entry: "index.html", openIn: "same-tab", bridge: "optional" },
    services: {
      networkRequired: false,
      ownBackend: false,
      realtime: [],
      platformIntegrations: [],
    },
    privacy: { collectsPersonalData: false, dataSummary: "No personal data is collected by this game." },
    media: { cover: "cover.webp", screenshots: [] },
    compatibility: {
      devices: ["desktop"],
      inputs: ["keyboard", "mouse"],
      orientation: "any",
      minimumViewport: { width: 320, height: 480 },
    },
    tags: ["indie"],
  };
}

const SDK_VERSION = "0.1.0";
const CONTEXT_KEY = "__ATRI_GAME_CONTEXT__";
const HANDOFF_SOURCE = "atri-game-launch";
const HANDOFF_VERSION = 1;
const FRAGMENT_CONTEXT_KEYS = ["atri_ticket", "atri_game", "atri_api"];

function rawFragment() {
  return globalThis.location?.hash?.replace(/^#/, "") ?? "";
}

function fragmentParts(raw = rawFragment()) {
  const queryIndex = raw.indexOf("?");
  if (queryIndex >= 0) {
    return { prefix: raw.slice(0, queryIndex), query: raw.slice(queryIndex + 1), hasQuery: true };
  }
  // Legacy portal launches used `#/atri_ticket=...`. Treat that leading slash
  // as part of the platform parameter form rather than as a game route.
  const query = raw.startsWith("/") ? raw.slice(1) : raw;
  const params = new URLSearchParams(query);
  if (FRAGMENT_CONTEXT_KEYS.some((name) => params.has(name))) {
    return { prefix: "", query, hasQuery: false };
  }
  return { prefix: raw, query: "", hasQuery: false };
}

function readFragmentValue(name) {
  try {
    const { query } = fragmentParts();
    return query ? new URLSearchParams(query).get(name) : null;
  } catch {
    return null;
  }
}

function removeFragmentValues(raw, names) {
  const { prefix, query, hasQuery } = fragmentParts(raw);
  const params = new URLSearchParams(query);
  if (!names.some((name) => params.has(name))) return raw;
  for (const name of names) params.delete(name);
  const rest = params.toString();
  if (hasQuery) return `${prefix}${rest ? `?${rest}` : ""}`;
  return rest;
}

function clearFragmentTicket() {
  try {
    const location = globalThis.location;
    if (!location?.hash || !globalThis.history?.replaceState) return;
    const raw = location.hash.slice(1);
    const names = FRAGMENT_CONTEXT_KEYS;
    const hadPlatformParams = names.some((key) => new URLSearchParams(fragmentParts(raw).query).has(key));
    if (!hadPlatformParams) return;
    const rest = removeFragmentValues(raw, names);
    const next = `${location.pathname}${location.search}${rest ? `#${rest}` : ""}`;
    globalThis.history.replaceState(null, "", next);
  } catch {
    // URL cleanup is best-effort and must never stop a game from loading.
  }
}

function consumeWindowNameContext() {
  try {
    const raw = globalThis.name;
    if (typeof raw !== "string" || raw.length === 0 || raw.length > 16_384) return {};
    const value = JSON.parse(raw);
    if (!value || typeof value !== "object" || value.source !== HANDOFF_SOURCE || value.version !== HANDOFF_VERSION) return {};
    // Do this before returning so a navigation, an exception, or another game
    // script cannot leave the bearer ticket in the browsing-context name.
    globalThis.name = "";
    return value;
  } catch {
    return {};
  }
}

function readURLContext() {
  try {
    const encoded = new URLSearchParams(globalThis.location?.search ?? "").get("atri_context");
    return encoded ? JSON.parse(atob(encoded)) : {};
  } catch {
    return {};
  }
}

function inferApiBaseUrl() {
  try {
    const referrer = globalThis.document?.referrer;
    if (referrer) return `${new URL(referrer).origin}/api/v1`;
  } catch {
    // Fall through to the relative API path for a same-origin host.
  }
  return "/api/v1";
}

function readContext() {
  const globalContext = globalThis[CONTEXT_KEY];
  const context = {
    ...readURLContext(),
    ...consumeWindowNameContext(),
    ...(globalContext && typeof globalContext === "object" ? globalContext : {}),
  };
  const ticket = context.ticket ?? context.gameTicket ?? readFragmentValue("atri_ticket");
  const gameSlug = context.gameSlug ?? context.slug ?? readFragmentValue("atri_game");
  const apiBaseUrl = context.apiBaseUrl ?? readFragmentValue("atri_api");
  return { ...context, ...(ticket ? { ticket } : {}), ...(gameSlug ? { gameSlug } : {}), ...(apiBaseUrl ? { apiBaseUrl } : {}) };
}

function safePost(message, context) {
  const target = globalThis.parent && globalThis.parent !== globalThis ? globalThis.parent : globalThis.opener;
  if (!target || typeof target.postMessage !== "function") return;
  // Never broadcast lifecycle data to an unknown origin. A host can opt in by
  // passing the exact origin in the launch context.
  const origin = typeof context.parentOrigin === "string" && context.parentOrigin ? context.parentOrigin : null;
  if (!origin) return;
  target.postMessage({ source: "atri-game-sdk", version: SDK_VERSION, ...message }, origin);
}

function readTicketPayload(ticket) {
  try {
    const segment = String(ticket ?? "").split(".")[1];
    if (!segment) return null;
    const encoded = segment.replace(/-/g, "+").replace(/_/g, "/");
    const padded = encoded.padEnd(Math.ceil(encoded.length / 4) * 4, "=");
    return JSON.parse(atob(padded));
  } catch {
    return null;
  }
}

function readTicketClaim(ticket, name) {
  const value = readTicketPayload(ticket)?.[name];
  return typeof value === "string" ? value : undefined;
}

function readTicketExpiry(ticket) {
  const value = readTicketPayload(ticket)?.exp;
  return typeof value === "number" && Number.isFinite(value) ? value * 1000 : undefined;
}

/**
 * Optional browser bridge. A game remains fully functional when this package
 * is absent; every method degrades to a local browser behavior or a no-op.
 */
export class AtriGame {
  constructor(options = {}) {
    this.context = { ...readContext(), ...options.context };
    this.context.ticket = options.ticket ?? this.context.ticket ?? this.context.gameTicket;
    this.apiBaseUrl = String(options.apiBaseUrl ?? this.context.apiBaseUrl ?? inferApiBaseUrl()).replace(/\/+$/, "");
    this.fetchImpl = options.fetch ?? globalThis.fetch?.bind(globalThis);
    this.listeners = new Map();
    this.disposed = false;
    this._refreshPromise = null;
    this._refreshTimer = null;
    this.storage = {
      get: (key) => this.getData(key),
      set: (key, value) => this.setData(key, value),
      remove: (key) => this.removeData(key),
    };
    this.matchmaking = {
      join: (input) => this.joinMatchmaking(input),
      status: (ticketId) => this.matchmakingStatus(ticketId),
      cancel: (ticketId) => this.cancelMatchmaking(ticketId),
    };
    this.identity = {
      getTicket: () => this.requestTicket(),
      getUser: () => this.context.user ?? (this.userId ? { id: this.userId } : null),
    };
    if (this.context.ticket) {
      clearFragmentTicket();
      this._scheduleTicketRefresh();
    }
    this._visibility = () => {
      if (typeof document !== "undefined") this._emit(document.visibilityState === "hidden" ? "pause" : "resume");
    };
    this._pagehide = () => this._emit("pause");
    if (typeof document !== "undefined") document.addEventListener("visibilitychange", this._visibility);
    if (typeof globalThis.addEventListener === "function") globalThis.addEventListener("pagehide", this._pagehide);
  }

  get gameId() {
    if (typeof this.context.gameId === "string" && this.context.gameId) return this.context.gameId;
    return this.ticket ? readTicketClaim(this.ticket, "gameId") : undefined;
  }

  get userId() {
    if (typeof this.context.userId === "string" && this.context.userId) return this.context.userId;
    return this.ticket ? readTicketClaim(this.ticket, "sub") : undefined;
  }

  get ticket() {
    return typeof this.context.ticket === "string" && this.context.ticket ? this.context.ticket : undefined;
  }

  get gameSlug() {
    const value = this.context.gameSlug ?? this.context.slug ?? this.context.gameId;
    if (typeof value === "string" && value) return value;
    try {
      const match = globalThis.location?.pathname?.match(/\/playables\/([^/]+)/);
      return match ? decodeURIComponent(match[1]) : undefined;
    } catch {
      return undefined;
    }
  }

  get authenticated() {
    return Boolean(this.ticket);
  }

  async _request(path, { method = "GET", body, token = this.ticket } = {}) {
    if (typeof this.fetchImpl !== "function") throw new Error("Atri platform API is not available in this runtime");
    const headers = new Headers();
    if (body !== undefined) headers.set("Content-Type", "application/json");
    if (token) headers.set("Authorization", `Bearer ${token}`);
    const response = await this.fetchImpl(`${this.apiBaseUrl}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    let payload = null;
    try {
      payload = await response.json();
    } catch {
      // Empty 204 responses are valid for delete operations.
    }
    if (!response.ok) {
      const message = payload?.error?.message ?? payload?.message ?? `Atri platform request failed (${response.status})`;
      const error = new Error(message);
      error.name = "AtriPlatformError";
      error.status = response.status;
      error.code = payload?.error?.code ?? "platform_request_failed";
      throw error;
    }
    return payload;
  }

  _requireGameSlug() {
    if (!this.gameSlug) throw new Error("Atri game context is missing a game slug");
    return encodeURIComponent(this.gameSlug);
  }

  _requireTicket() {
    if (!this.ticket) throw new Error("登录后才能使用 Atri 游戏服务");
    return this.ticket;
  }

  _scheduleTicketRefresh() {
    if (this.disposed) return;
    if (this._refreshTimer !== null) {
      globalThis.clearTimeout?.(this._refreshTimer);
      this._refreshTimer = null;
    }
    const expiresAt = readTicketExpiry(this.ticket);
    if (!expiresAt || expiresAt <= Date.now() || typeof globalThis.setTimeout !== "function") return;
    const delay = Math.max(0, Math.min(expiresAt - Date.now() - 60_000, 2_147_000_000));
    this._refreshTimer = globalThis.setTimeout(() => {
      this._refreshTimer = null;
      void this._refreshGameTicket().catch(() => {
        // A foreground API call will surface the error or retry while the
        // current ticket remains valid.
      });
    }, delay);
    this._refreshTimer?.unref?.();
  }

  async _refreshGameTicket() {
    if (this._refreshPromise) return this._refreshPromise;
    const currentTicket = this._requireTicket();
    const slug = this._requireGameSlug();
    this._refreshPromise = this._request(`/games/${slug}/ticket/refresh`, {
      method: "POST",
      token: currentTicket,
    })
      .then((result) => {
        if (typeof result?.ticket !== "string" || !result.ticket) {
          throw new Error("Atri ticket refresh returned no ticket");
        }
        this.context.ticket = result.ticket;
        this._scheduleTicketRefresh();
        return result.ticket;
      })
      .finally(() => {
        this._refreshPromise = null;
      });
    return this._refreshPromise;
  }

  async _ensureFreshTicket() {
    const ticket = this._requireTicket();
    const expiresAt = readTicketExpiry(ticket);
    if (!expiresAt || expiresAt - Date.now() > 60_000) return ticket;
    return this._refreshGameTicket();
  }

  /**
   * Mint a game-scoped ticket when the host supplied a platform user token.
   * Normally launch() supplies the short-lived ticket automatically.
   */
  async requestTicket() {
    const slug = this._requireGameSlug();
    const platformToken = this.context.userToken ?? this.context.platformToken;
    if (!platformToken) {
      const expiresAt = readTicketExpiry(this.ticket);
      if (this.ticket && expiresAt && expiresAt - Date.now() <= 60_000) return this._refreshGameTicket();
      return this.ticket;
    }
    const result = await this._request(`/games/${slug}/ticket`, { method: "POST", token: platformToken });
    this.context.ticket = result?.ticket;
    this._scheduleTicketRefresh();
    clearFragmentTicket();
    return this.context.ticket;
  }

  async getData(key) {
    const slug = this._requireGameSlug();
    return this._request(`/games/${slug}/data/${encodeURIComponent(key)}`, { token: await this._ensureFreshTicket() });
  }

  async setData(key, value) {
    const slug = this._requireGameSlug();
    return this._request(`/games/${slug}/data/${encodeURIComponent(key)}`, {
      method: "PUT",
      body: { value },
      token: await this._ensureFreshTicket(),
    });
  }

  async removeData(key) {
    const slug = this._requireGameSlug();
    return this._request(`/games/${slug}/data/${encodeURIComponent(key)}`, {
      method: "DELETE",
      token: await this._ensureFreshTicket(),
    });
  }

  async joinMatchmaking(input = {}) {
    const slug = this._requireGameSlug();
    return this._request(`/games/${slug}/matchmaking/tickets`, {
      method: "POST",
      body: input,
      token: await this._ensureFreshTicket(),
    });
  }

  async matchmakingStatus(ticketId) {
    const slug = this._requireGameSlug();
    return this._request(`/games/${slug}/matchmaking/tickets/${encodeURIComponent(ticketId)}`, {
      token: await this._ensureFreshTicket(),
    });
  }

  async cancelMatchmaking(ticketId) {
    const slug = this._requireGameSlug();
    return this._request(`/games/${slug}/matchmaking/tickets/${encodeURIComponent(ticketId)}`, {
      method: "DELETE",
      token: await this._ensureFreshTicket(),
    });
  }

  ready(details = {}) {
    safePost({ type: "ready", details }, this.context);
    this._emit("ready", details);
    return this;
  }

  progress(value, details = {}) {
    const normalized = Math.max(0, Math.min(1, Number(value) || 0));
    safePost({ type: "progress", value: normalized, details }, this.context);
    return this;
  }

  score(value, details = {}) {
    safePost({ type: "score", value: Number(value) || 0, details }, this.context);
    return this;
  }

  exit() {
    safePost({ type: "exit" }, this.context);
    if (typeof this.context.returnUrl === "string" && this.context.returnUrl) globalThis.location?.assign(this.context.returnUrl);
  }

  async fullscreen(element) {
    const target = element ?? (typeof document !== "undefined" ? document.documentElement : undefined);
    if (target && typeof target.requestFullscreen === "function") {
      await target.requestFullscreen();
      return true;
    }
    return false;
  }

  async exitFullscreen() {
    if (typeof document !== "undefined" && typeof document.exitFullscreen === "function" && document.fullscreenElement) {
      await document.exitFullscreen();
      return true;
    }
    return false;
  }

  on(event, listener) {
    if (!this.listeners.has(event)) this.listeners.set(event, new Set());
    this.listeners.get(event).add(listener);
    return () => this.off(event, listener);
  }

  off(event, listener) {
    this.listeners.get(event)?.delete(listener);
  }

  _emit(event, value) {
    for (const listener of this.listeners.get(event) ?? []) {
      try {
        listener(value);
      } catch {
        // A telemetry/listener failure must never break the game loop.
      }
    }
  }

  dispose() {
    if (this.disposed) return;
    this.disposed = true;
    if (typeof document !== "undefined") document.removeEventListener("visibilitychange", this._visibility);
    if (typeof globalThis.removeEventListener === "function") globalThis.removeEventListener("pagehide", this._pagehide);
    if (this._refreshTimer !== null) globalThis.clearTimeout?.(this._refreshTimer);
    this._refreshTimer = null;
    this.listeners.clear();
  }
}

export function createAtriGame(options) {
  return new AtriGame(options);
}

export const version = SDK_VERSION;

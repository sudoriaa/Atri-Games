import { describe, expect, it } from "vitest";
import runtimeBootstrap from "../../public/sdk/atri-game-runtime-bootstrap.js?raw";

type MemoryStorage = {
  readonly length: number;
  key: (index: number) => string | null;
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
  clear: () => void;
};

type BootstrapWindow = {
  name: string;
  innerWidth?: number;
  innerHeight?: number;
  location: {
    hash: string;
    pathname: string;
    search: string;
    href?: string;
    origin?: string;
    assign?: (url: string) => void;
    reload?: () => void;
  };
  history: { state: unknown; replaceState: (...args: unknown[]) => void };
  localStorage?: MemoryStorage;
  sessionStorage?: MemoryStorage;
  __ATRI_GAME_CONTEXT__?: Record<string, unknown>;
};

type FakeEvent = {
  button?: number;
  clientX?: number;
  clientY?: number;
  isPrimary?: boolean;
  key?: string;
  pointerId?: number;
  shiftKey?: boolean;
  target?: FakeElement;
  preventDefault: () => void;
};

type FakeListener = (event: FakeEvent) => void;

class FakeElement {
  readonly children: FakeElement[] = [];
  readonly attributes = new Map<string, string>();
  readonly listeners = new Map<string, FakeListener[]>();
  readonly style: Record<string, string> = {};
  shadowRoot?: FakeElement;
  className = "";
  capturedPointerId?: number;
  textContent = "";
  hidden = false;
  id = "";
  title = "";
  type = "";
  private rect = { left: 764, top: 12, width: 248, height: 52 };

  constructor(readonly tagName: string, private readonly document: FakeDocument) {}

  appendChild(child: FakeElement) {
    this.children.push(child);
    return child;
  }

  attachShadow() {
    this.shadowRoot = new FakeElement("shadow-root", this.document);
    return this.shadowRoot;
  }

  setAttribute(name: string, value: string) {
    this.attributes.set(name, value);
  }

  removeAttribute(name: string) {
    this.attributes.delete(name);
  }

  getAttribute(name: string) {
    return this.attributes.get(name) ?? null;
  }

  addEventListener(type: string, listener: FakeListener) {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  focus() {
    this.document.activeElement = this;
  }

  setPointerCapture(pointerId: number) {
    this.capturedPointerId = pointerId;
  }

  releasePointerCapture(pointerId: number) {
    if (this.capturedPointerId === pointerId) this.capturedPointerId = undefined;
  }

  setBoundingClientRect(rect: Partial<{ left: number; top: number; width: number; height: number }>) {
    this.rect = { ...this.rect, ...rect };
  }

  getBoundingClientRect() {
    const left = this.style.left ? Number.parseFloat(this.style.left) : this.rect.left;
    const top = this.style.top ? Number.parseFloat(this.style.top) : this.rect.top;
    return {
      left,
      top,
      width: this.rect.width,
      height: this.rect.height,
      right: left + this.rect.width,
      bottom: top + this.rect.height,
    };
  }

  dispatch(type: string, event: Omit<FakeEvent, "target" | "preventDefault"> = {}) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener({ ...event, target: this, preventDefault: () => {} });
    }
  }

  click() {
    this.dispatch("click");
  }
}

class FakeDocument {
  readonly body = new FakeElement("body", this);
  readonly documentElement = new FakeElement("html", this);
  activeElement?: FakeElement;
  fullscreenElement: FakeElement | null = null;

  createElement(tagName: string) {
    return new FakeElement(tagName, this);
  }

  querySelector(selector: string) {
    if (selector === "[data-atri-platform-menu]") {
      return findElement(this.body, (element) => element.attributes.has("data-atri-platform-menu")) ?? null;
    }
    return null;
  }
}

function findElement(root: FakeElement, predicate: (element: FakeElement) => boolean): FakeElement | undefined {
  if (predicate(root)) return root;
  for (const child of root.children) {
    const found = findElement(child, predicate);
    if (found) return found;
    if (child.shadowRoot) {
      const inShadow = findElement(child.shadowRoot, predicate);
      if (inShadow) return inShadow;
    }
  }
  return undefined;
}

function runBootstrap(input: {
  name?: string;
  hash?: string;
  context?: Record<string, unknown>;
  denyStorage?: boolean;
}) {
  const replacements: unknown[][] = [];
  const window: BootstrapWindow = {
    name: input.name ?? "",
    location: {
      hash: input.hash ?? "",
      pathname: "/games/find-mzk/play/",
      search: "",
    },
    history: {
      state: null,
      replaceState: (...args) => replacements.push(args),
    },
    ...(input.context ? { __ATRI_GAME_CONTEXT__: input.context } : {}),
  };
  if (input.denyStorage) {
    for (const name of ["localStorage", "sessionStorage"] as const) {
      Object.defineProperty(window, name, {
        configurable: true,
        get: () => {
          throw new DOMException("Storage is disabled for sandboxed documents", "SecurityError");
        },
      });
    }
  }
  new Function("window", runtimeBootstrap)(window);
  return { window, replacements };
}

function runBootstrapWithMenu() {
  const replacements: unknown[][] = [];
  const navigations: string[] = [];
  const document = new FakeDocument();
  const window: BootstrapWindow & {
    document: FakeDocument;
    addEventListener: (type: string, listener: FakeListener) => void;
    dispatchEvent: () => void;
    CustomEvent: new (type: string, init: { detail: unknown }) => unknown;
  } = {
    name: "",
    innerWidth: 1024,
    innerHeight: 768,
    location: {
      hash: "",
      pathname: "/games/find-mzk/play/",
      search: "",
      href: "https://atri.test/games/find-mzk/play/",
      origin: "https://atri.test",
      assign: (url) => navigations.push(url),
    },
    history: {
      state: null,
      replaceState: (...args) => replacements.push(args),
    },
    document,
    addEventListener: () => {},
    dispatchEvent: () => {},
    CustomEvent: class {
      constructor(_type: string, _init: { detail: unknown }) {}
    },
  };
  new Function("window", runtimeBootstrap)(window);
  return { document, navigations, replacements, window };
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
    expect(replacements[0]?.[2]).toBe("/games/find-mzk/play/#/level/2?difficulty=hard");
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
    expect(replacements[0]?.[2]).toBe("/games/find-mzk/play/#/");
  });

  it("installs independent in-memory Storage shims for an opaque sandbox origin", () => {
    const { window } = runBootstrap({ denyStorage: true });
    const local = window.localStorage;
    const session = window.sessionStorage;
    expect(local).toBeDefined();
    expect(session).toBeDefined();
    expect(local).not.toBe(session);

    local?.setItem("progress", "3");
    session?.setItem("progress", "session");
    expect(local?.getItem("progress")).toBe("3");
    expect(session?.getItem("progress")).toBe("session");
    expect(local?.length).toBe(1);
    expect(local?.key(0)).toBe("progress");

    local?.removeItem("progress");
    expect(local?.getItem("progress")).toBeNull();
    session?.clear();
    expect(session?.length).toBe(0);
  });

  it("mounts one platform menu and exits back to the current game detail page", () => {
    const { document, navigations, window } = runBootstrapWithMenu();
    const host = findElement(document.body, (element) => element.attributes.has("data-atri-platform-menu"));
    expect(host).toBeDefined();
    expect(host?.shadowRoot).toBeDefined();
    if (!host?.shadowRoot) throw new Error("platform menu shadow root was not created");

    const trigger = findElement(host.shadowRoot, (element) => element.className === "atri-platform-menu__trigger");
    const panel = findElement(host.shadowRoot, (element) => element.id === "atri-platform-menu-panel");
    const exit = findElement(host.shadowRoot, (element) => element.getAttribute("data-atri-action") === "exit");
    expect(trigger).toBeDefined();
    expect(panel?.hidden).toBe(true);
    trigger?.click();
    expect(trigger?.getAttribute("aria-expanded")).toBe("true");
    expect(panel?.hidden).toBe(false);

    exit?.click();
    expect(navigations).toEqual(["/games/find-mzk"]);

    new Function("window", runtimeBootstrap)(window);
    expect(document.body.children.filter((element) => element.attributes.has("data-atri-platform-menu"))).toHaveLength(1);
  });

  it("keeps the menu subtle while idle and supports drag without toggling it", () => {
    const { document } = runBootstrapWithMenu();
    const host = findElement(document.body, (element) => element.attributes.has("data-atri-platform-menu"));
    if (!host?.shadowRoot) throw new Error("platform menu shadow root was not created");

    const trigger = findElement(host.shadowRoot, (element) => element.className === "atri-platform-menu__trigger");
    const panel = findElement(host.shadowRoot, (element) => element.id === "atri-platform-menu-panel");
    const head = findElement(host.shadowRoot, (element) => element.className === "atri-platform-menu__head");
    if (!trigger || !panel || !head) throw new Error("platform menu controls were not created");

    expect(runtimeBootstrap).toContain("opacity: .58");
    expect(runtimeBootstrap).toContain(":host(:hover)");
    expect(runtimeBootstrap).toContain("data-atri-dragging");

    trigger.setBoundingClientRect({ left: 964, top: 12, width: 48, height: 48 });
    trigger.dispatch("pointerdown", { button: 0, clientX: 988, clientY: 36, pointerId: 21 });
    expect(trigger.capturedPointerId).toBe(21);
    trigger.dispatch("pointermove", { clientX: 50, clientY: 36, pointerId: 21 });
    expect(host.getAttribute("data-atri-dragging")).toBe("");
    expect(host.getAttribute("data-atri-align")).toBe("start");
    expect(host.style.left).toBe("26px");
    expect(host.style.top).toBe("12px");
    trigger.dispatch("pointerup", { clientX: 50, clientY: 36, pointerId: 21 });
    expect(trigger.capturedPointerId).toBeUndefined();
    expect(host.getAttribute("data-atri-dragging")).toBeNull();

    trigger.click();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    trigger.click();
    expect(trigger.getAttribute("aria-expanded")).toBe("true");

    head.setBoundingClientRect({ left: 26, top: 68, width: 248, height: 52 });
    panel.setBoundingClientRect({ left: 26, top: 68, width: 248, height: 250 });
    head.dispatch("pointerdown", { button: 0, clientX: 150, clientY: 90, pointerId: 22 });
    head.dispatch("pointermove", { clientX: 2000, clientY: 2000, pointerId: 22 });
    expect(host.getAttribute("data-atri-dragging")).toBe("");
    expect(host.getAttribute("data-atri-align")).toBe("end");
    expect(host.style.left).toBe("768px");
    expect(host.style.top).toBe("454px");
    head.dispatch("pointerup", { clientX: 2000, clientY: 2000, pointerId: 22 });
    expect(host.getAttribute("data-atri-dragging")).toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
  });
});

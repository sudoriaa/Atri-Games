(function () {
  "use strict";

  // This is deliberately a classic, parser-blocking script. The importer
  // places it before a package's first script tag so a hash-router never sees
  // legacy Atri launch parameters as one of its routes.
  var CONTEXT_KEY = "__ATRI_GAME_CONTEXT__";
  var HANDOFF_SOURCE = "atri-game-launch";
  var HANDOFF_VERSION = 1;
  var LEGACY_KEYS = ["atri_ticket", "atri_game", "atri_api"];
  var PLATFORM_MENU_ATTRIBUTE = "data-atri-platform-menu";
  var PLATFORM_MENU_EVENT = "atri-platform-menu";
  var PLATFORM_MENU_CSS = [
    ":host { position: fixed; inset-block-start: max(12px, env(safe-area-inset-top)); inset-inline-end: max(12px, env(safe-area-inset-right)); z-index: 2147483000; display: block; width: 248px; height: 52px; pointer-events: none; color: #171614; font: 500 13px/1.3 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; }",
    ".atri-platform-menu-host { position: fixed; top: 12px; right: 12px; z-index: 2147483000; display: block; width: 248px; height: 52px; pointer-events: none; color: #171614; font: 500 13px/1.3 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; }",
    ".atri-platform-menu { position: relative; width: 100%; height: 100%; pointer-events: none; }",
    ".atri-platform-menu button { box-sizing: border-box; margin: 0; font: inherit; }",
    ".atri-platform-menu__trigger { position: absolute; inset-block-start: 0; inset-inline-end: 0; z-index: 3; width: 48px; height: 48px; display: grid; place-items: center; padding: 0; border: 1px solid #171614; border-radius: 7px; color: #171614; background: #fbf9f3; box-shadow: 5px 5px 0 #171614; cursor: pointer; pointer-events: auto; transition: transform .16s ease, box-shadow .16s ease, background-color .16s ease; }",
    ".atri-platform-menu__trigger:hover { background: #ccff33; transform: translate(-1px, -1px); box-shadow: 6px 6px 0 #171614; }",
    ".atri-platform-menu__trigger[aria-expanded='true'] { background: #ccff33; transform: translate(-1px, -1px); box-shadow: 3px 3px 0 #171614; }",
    ".atri-platform-menu__trigger:focus-visible, .atri-platform-menu__item:focus-visible, .atri-platform-menu__close:focus-visible { outline: 3px solid #7d66ff; outline-offset: 3px; }",
    ".atri-platform-menu__mark { display: grid; gap: 3px; width: 18px; }",
    ".atri-platform-menu__mark i { display: block; height: 2px; width: 18px; background: currentColor; }",
    ".atri-platform-menu__mark i:nth-child(2) { width: 13px; margin-inline-start: 5px; }",
    ".atri-platform-menu__backdrop { position: fixed; inset: 0; z-index: 1; background: rgba(23, 22, 20, .2); opacity: 0; pointer-events: none; transition: opacity .16s ease; }",
    ".atri-platform-menu.is-open .atri-platform-menu__backdrop { opacity: 1; pointer-events: auto; }",
    ".atri-platform-menu__panel { position: absolute; inset-block-start: 56px; inset-inline-end: 0; z-index: 2; width: 248px; overflow: hidden; border: 1px solid #171614; border-radius: 7px; background: #fbf9f3; box-shadow: 8px 8px 0 #171614; opacity: 0; pointer-events: none; transform: translateY(-7px) scale(.98); transform-origin: top right; transition: opacity .16s ease, transform .16s ease; }",
    ".atri-platform-menu.is-open .atri-platform-menu__panel { opacity: 1; pointer-events: auto; transform: none; }",
    ".atri-platform-menu [hidden] { display: none !important; }",
    ".atri-platform-menu__head { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 52px; padding: 10px 12px 10px 15px; border-bottom: 1px solid rgba(23, 22, 20, .17); }",
    ".atri-platform-menu__kicker { color: #716e67; font: 850 10px/1 ui-monospace, SFMono-Regular, Consolas, monospace; letter-spacing: .12em; }",
    ".atri-platform-menu__close { width: 30px; height: 30px; display: grid; place-items: center; padding: 0; border: 0; border-radius: 50%; color: #171614; background: transparent; font-size: 22px; line-height: 1; cursor: pointer; }",
    ".atri-platform-menu__close:hover { background: rgba(23, 22, 20, .08); }",
    ".atri-platform-menu__items { display: grid; gap: 4px; padding: 8px; }",
    ".atri-platform-menu__item { min-height: 46px; display: grid; grid-template-columns: 28px 1fr; align-items: center; gap: 9px; width: 100%; padding: 0 11px; border: 1px solid transparent; border-radius: 5px; color: #171614; background: transparent; text-align: start; cursor: pointer; }",
    ".atri-platform-menu__item:hover { border-color: rgba(23, 22, 20, .2); background: #ccff33; }",
    ".atri-platform-menu__item[data-atri-action='exit'] { margin-top: 4px; border-top: 1px solid rgba(23, 22, 20, .17); border-radius: 0 0 5px 5px; color: #b64022; }",
    ".atri-platform-menu__item[data-atri-action='exit']:hover { border-color: #b64022; background: #fff0e6; }",
    ".atri-platform-menu__icon { width: 25px; height: 25px; display: grid; place-items: center; color: currentColor; font: 700 17px/1 Georgia, serif; }",
    ".atri-platform-menu__label { font-weight: 760; }",
    ".atri-platform-menu__status { min-height: 0; margin: 0; padding: 0 15px 11px; color: #716e67; font-size: 10px; line-height: 1.5; }",
    "@media (max-width: 560px) { :host, .atri-platform-menu-host { inset-block-start: max(8px, env(safe-area-inset-top)); inset-inline-end: max(8px, env(safe-area-inset-right)); top: 8px; right: 8px; width: min(280px, calc(100vw - 16px)); } .atri-platform-menu__panel { width: min(280px, calc(100vw - 16px)); } .atri-platform-menu__trigger { width: 46px; height: 46px; } }",
    "@media (prefers-reduced-motion: reduce) { .atri-platform-menu__trigger, .atri-platform-menu__backdrop, .atri-platform-menu__panel { transition: none; } }",
  ].join("");

  function memoryStorage() {
    var values = Object.create(null);
    return {
      get length() {
        return Object.keys(values).length;
      },
      key: function (index) {
        var key = Object.keys(values)[index];
        return key === undefined ? null : key;
      },
      getItem: function (key) {
        key = String(key);
        return Object.prototype.hasOwnProperty.call(values, key) ? values[key] : null;
      },
      setItem: function (key, value) {
        values[String(key)] = String(value);
      },
      removeItem: function (key) {
        delete values[String(key)];
      },
      clear: function () {
        values = Object.create(null);
      },
    };
  }

  // A game served with CSP sandbox and no allow-same-origin receives an opaque
  // origin. Browsers intentionally throw SecurityError from the Storage
  // getters in that context. Install separate in-memory implementations before
  // the package's first script runs so ordinary games degrade to session-only
  // persistence instead of crashing during application bootstrap.
  ["localStorage", "sessionStorage"].forEach(function (name) {
    try {
      window[name].length;
    } catch (_error) {
      try {
        Object.defineProperty(window, name, {
          configurable: true,
          value: memoryStorage(),
        });
      } catch (_defineError) {
        // Some engines expose a non-configurable Window property. Games that
        // access it will still receive the browser's original SecurityError,
        // but the runtime handoff below must continue.
      }
    }
  });

  function isRecord(value) {
    return value !== null && typeof value === "object" && !Array.isArray(value);
  }

  function copyContext(value) {
    if (!isRecord(value)) return {};
    var copy = {};
    Object.keys(value).forEach(function (key) {
      if (key === "__proto__" || key === "constructor" || key === "prototype") return;
      copy[key] = value[key];
    });
    return copy;
  }

  function readHandoff() {
    var raw;
    try {
      raw = window.name;
    } catch (_error) {
      return {};
    }
    if (typeof raw !== "string" || !raw) return {};

    try {
      var handoff = JSON.parse(raw);
      if (!isRecord(handoff) || handoff.source !== HANDOFF_SOURCE || handoff.version !== HANDOFF_VERSION) {
        return {};
      }
      // Do not leave a short-lived game ticket available to later navigations
      // in this browsing context.
      try {
        window.name = "";
      } catch (_error) {
        // Publishing the context still lets the game start if a browser
        // disallows assigning window.name in this situation.
      }
      return copyContext(handoff);
    } catch (_error) {
      return {};
    }
  }

  function decodePart(value) {
    try {
      return decodeURIComponent(value.replace(/\+/g, " "));
    } catch (_error) {
      return value;
    }
  }

  function readAndRemoveParams(raw) {
    var values = {};
    var queryStart = raw.indexOf("?");
    var route = queryStart >= 0 ? raw.slice(0, queryStart) : "";
    var query = queryStart >= 0 ? raw.slice(queryStart + 1) : raw;
    var isParameterOnlyHash = queryStart < 0;

    // Older launches used `#/atri_ticket=...`; that leading slash was not a
    // game route, it was merely the browser's normalized fragment form. Treat
    // it as the root route after stripping the Atri values.
    var hasLeadingRoot = false;
    if (isParameterOnlyHash && query.charAt(0) === "/") {
      hasLeadingRoot = true;
      query = query.slice(1);
    }

    var parts = query ? query.split("&") : [];
    var firstKey = "";
    for (var index = 0; index < parts.length; index += 1) {
      if (!parts[index]) continue;
      firstKey = decodePart(parts[index].split("=", 1)[0]);
      break;
    }

    // Without a `?`, only a fragment whose first key is an Atri parameter is
    // a legacy parameter-only hash. This avoids changing normal hash routes
    // that happen to contain an ampersand in their path.
    var mayContainLegacyParams = queryStart >= 0 || LEGACY_KEYS.indexOf(firstKey) >= 0;
    if (!mayContainLegacyParams) return { context: values, hash: raw, changed: false };

    var retained = [];
    var changed = false;
    for (var partIndex = 0; partIndex < parts.length; partIndex += 1) {
      var part = parts[partIndex];
      if (!part) {
        retained.push(part);
        continue;
      }
      var equalsAt = part.indexOf("=");
      var key = decodePart(equalsAt >= 0 ? part.slice(0, equalsAt) : part);
      var value = equalsAt >= 0 ? decodePart(part.slice(equalsAt + 1)) : "";
      if (LEGACY_KEYS.indexOf(key) < 0) {
        retained.push(part);
        continue;
      }
      changed = true;
      if (values[key] === undefined) values[key] = value;
    }
    if (!changed) return { context: values, hash: raw, changed: false };

    var nextQuery = retained.join("&");
    if (queryStart >= 0) {
      return {
        context: values,
        hash: route + (nextQuery ? "?" + nextQuery : ""),
        changed: true,
      };
    }
    if (hasLeadingRoot) {
      return {
        context: values,
        hash: "/" + (nextQuery ? "?" + nextQuery : ""),
        changed: true,
      };
    }
    return { context: values, hash: nextQuery, changed: true };
  }

  function consumeLegacyHash() {
    try {
      var location = window.location;
      if (!location || !location.hash) return {};
      var raw = location.hash.slice(1);
      var result = readAndRemoveParams(raw);
      if (result.changed && window.history && typeof window.history.replaceState === "function") {
        var next = location.pathname + location.search + (result.hash ? "#" + result.hash : "");
        window.history.replaceState(window.history.state, "", next);
      }
      return {
        ticket: result.context.atri_ticket,
        gameSlug: result.context.atri_game,
        apiBaseUrl: result.context.atri_api,
      };
    } catch (_error) {
      // Legacy URL cleanup is best-effort and must never prevent a package
      // from starting.
      return {};
    }
  }

  function safeSlug(value) {
    if (typeof value !== "string") return "";
    var decoded = value;
    try {
      decoded = decodeURIComponent(value);
    } catch (_error) {
      // Keep the original value when a malformed path segment is supplied.
    }
    return /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(decoded) ? decoded : "";
  }

  function inferGameSlug(context) {
    var fromContext = context && (context.gameSlug || context.slug || context.gameId);
    var slug = safeSlug(fromContext);
    if (slug) return slug;

    var location = window.location;
    var pathname = location && typeof location.pathname === "string" ? location.pathname : "";
    var pathMatch = pathname.match(/\/games\/([^/]+)\/play(?:\/|$)/);
    if (pathMatch) {
      slug = safeSlug(pathMatch[1]);
      if (slug) return slug;
    }

    var search = location && typeof location.search === "string" ? location.search : "";
    if (search) {
      try {
        slug = safeSlug(new URLSearchParams(search).get("game") || new URLSearchParams(search).get("slug"));
      } catch (_error) {
        slug = "";
      }
    }
    return slug;
  }

  function returnPath(context, slug) {
    var fallback = slug ? "/games/" + encodeURIComponent(slug) : "/discover";
    var candidate = context && context.returnUrl;
    if (typeof candidate !== "string" || !candidate) return fallback;
    try {
      var parsed = new URL(candidate, window.location && window.location.href ? window.location.href : fallback);
      var currentOrigin = window.location && window.location.origin;
      if (currentOrigin && currentOrigin !== "null" && parsed.origin !== currentOrigin) return fallback;
      if (slug && parsed.pathname !== fallback) return fallback;
      if (parsed.pathname === "/discover" || (slug && parsed.pathname === fallback)) {
        return parsed.href || fallback;
      }
    } catch (_error) {
      // A malformed handoff must never become an open redirect.
    }
    return fallback;
  }

  function createMenuElement(document, parent, tagName, className, text) {
    var element = document.createElement(tagName);
    if (className) element.className = className;
    if (text !== undefined) element.textContent = text;
    if (parent) parent.appendChild(element);
    return element;
  }

  function publishMenuState(isOpen) {
    if (typeof window.dispatchEvent !== "function") return;
    var EventConstructor = window.CustomEvent;
    if (typeof EventConstructor !== "function") return;
    try {
      window.dispatchEvent(new EventConstructor(PLATFORM_MENU_EVENT, { detail: { open: isOpen } }));
    } catch (_error) {
      // Lifecycle notifications are optional and must never block the menu.
    }
  }

  function installPlatformMenu(context) {
    var document = window.document;
    if (!document || typeof document.createElement !== "function") return;

    var mount = function () {
      if (!document.body) return;
      try {
        if (document.querySelector && document.querySelector("[" + PLATFORM_MENU_ATTRIBUTE + "]")) return;
      } catch (_error) {
        // Continue when a restricted document does not expose querySelector.
      }

      var slug = inferGameSlug(context);
      var host = document.createElement("div");
      host.className = "atri-platform-menu-host";
      host.setAttribute(PLATFORM_MENU_ATTRIBUTE, "");
      host.setAttribute("aria-label", "Atri 平台菜单");

      var shadow = typeof host.attachShadow === "function" ? host.attachShadow({ mode: "open" }) : host;
      var style = createMenuElement(document, shadow, "style", "", PLATFORM_MENU_CSS);
      style.setAttribute("aria-hidden", "true");
      var root = createMenuElement(document, shadow, "div", "atri-platform-menu");
      var backdrop = createMenuElement(document, root, "div", "atri-platform-menu__backdrop");
      backdrop.setAttribute("aria-hidden", "true");
      var trigger = createMenuElement(document, root, "button", "atri-platform-menu__trigger");
      trigger.type = "button";
      trigger.setAttribute("aria-label", "打开 Atri 平台菜单");
      trigger.setAttribute("aria-expanded", "false");
      trigger.setAttribute("aria-controls", "atri-platform-menu-panel");
      trigger.title = "打开平台菜单";
      var mark = createMenuElement(document, trigger, "span", "atri-platform-menu__mark");
      mark.setAttribute("aria-hidden", "true");
      createMenuElement(document, mark, "i");
      createMenuElement(document, mark, "i");
      createMenuElement(document, mark, "i");

      var panel = createMenuElement(document, root, "section", "atri-platform-menu__panel");
      panel.id = "atri-platform-menu-panel";
      panel.setAttribute("role", "menu");
      panel.setAttribute("aria-label", "Atri 平台菜单");
      panel.hidden = true;
      var head = createMenuElement(document, panel, "header", "atri-platform-menu__head");
      createMenuElement(document, head, "span", "atri-platform-menu__kicker", "ATRI / PLATFORM");
      var close = createMenuElement(document, head, "button", "atri-platform-menu__close", "×");
      close.type = "button";
      close.setAttribute("aria-label", "关闭平台菜单");
      close.title = "关闭平台菜单";

      var items = createMenuElement(document, panel, "div", "atri-platform-menu__items");
      var continueItem = createMenuElement(document, items, "button", "atri-platform-menu__item");
      continueItem.type = "button";
      continueItem.setAttribute("role", "menuitem");
      continueItem.setAttribute("data-atri-action", "continue");
      createMenuElement(document, continueItem, "span", "atri-platform-menu__icon", "×").setAttribute("aria-hidden", "true");
      createMenuElement(document, continueItem, "span", "atri-platform-menu__label", "继续游戏");

      var fullscreenItem = createMenuElement(document, items, "button", "atri-platform-menu__item");
      fullscreenItem.type = "button";
      fullscreenItem.setAttribute("role", "menuitem");
      fullscreenItem.setAttribute("data-atri-action", "fullscreen");
      createMenuElement(document, fullscreenItem, "span", "atri-platform-menu__icon", "↗").setAttribute("aria-hidden", "true");
      var fullscreenLabel = createMenuElement(document, fullscreenItem, "span", "atri-platform-menu__label", "全屏显示");

      var restartItem = createMenuElement(document, items, "button", "atri-platform-menu__item");
      restartItem.type = "button";
      restartItem.setAttribute("role", "menuitem");
      restartItem.setAttribute("data-atri-action", "restart");
      createMenuElement(document, restartItem, "span", "atri-platform-menu__icon", "↻").setAttribute("aria-hidden", "true");
      createMenuElement(document, restartItem, "span", "atri-platform-menu__label", "重新开始");

      var exitItem = createMenuElement(document, items, "button", "atri-platform-menu__item");
      exitItem.type = "button";
      exitItem.setAttribute("role", "menuitem");
      exitItem.setAttribute("data-atri-action", "exit");
      createMenuElement(document, exitItem, "span", "atri-platform-menu__icon", "←").setAttribute("aria-hidden", "true");
      createMenuElement(document, exitItem, "span", "atri-platform-menu__label", "退出游戏");
      var status = createMenuElement(document, panel, "p", "atri-platform-menu__status");
      status.setAttribute("role", "status");
      status.setAttribute("aria-live", "polite");
      status.hidden = true;

      var open = false;
      var statusTimer = null;
      var focusables = [close, continueItem, fullscreenItem, restartItem, exitItem];

      var syncFullscreen = function () {
        var isFullscreen = false;
        try {
          isFullscreen = Boolean(document.fullscreenElement);
        } catch (_error) {
          isFullscreen = false;
        }
        fullscreenLabel.textContent = isFullscreen ? "退出全屏" : "全屏显示";
        fullscreenItem.setAttribute("aria-label", isFullscreen ? "退出全屏" : "进入全屏");
      };

      var showStatus = function (message) {
        status.textContent = message;
        status.hidden = !message;
        if (statusTimer !== null && typeof window.clearTimeout === "function") window.clearTimeout(statusTimer);
        if (message && typeof window.setTimeout === "function") {
          statusTimer = window.setTimeout(function () {
            status.hidden = true;
            status.textContent = "";
            statusTimer = null;
          }, 3200);
        }
      };

      var setOpen = function (next, restoreFocus) {
        if (open === next) return;
        open = next;
        root.className = next ? "atri-platform-menu is-open" : "atri-platform-menu";
        panel.hidden = !next;
        backdrop.hidden = !next;
        trigger.setAttribute("aria-expanded", String(next));
        trigger.setAttribute("aria-label", next ? "关闭 Atri 平台菜单" : "打开 Atri 平台菜单");
        publishMenuState(next);
        if (next) {
          syncFullscreen();
          if (typeof continueItem.focus === "function") continueItem.focus();
        } else if (restoreFocus && typeof trigger.focus === "function") {
          trigger.focus();
        }
      };

      var goBack = function () {
        setOpen(false, false);
        var target = returnPath(context, slug);
        try {
          if (window.location && typeof window.location.assign === "function") {
            window.location.assign(target);
          } else if (window.location) {
            window.location.href = target;
          }
        } catch (_error) {
          // Top-navigation can be denied outside a user gesture; the menu
          // remains harmless and the game's own back link stays available.
        }
      };

      var restart = function () {
        setOpen(false, false);
        try {
          if (window.location && typeof window.location.reload === "function") window.location.reload();
          else if (window.location && typeof window.location.assign === "function") window.location.assign(window.location.href);
        } catch (_error) {
          // Reload is a convenience action and may be unavailable in a test
          // harness or a restricted embedded document.
        }
      };

      var toggleFullscreen = function () {
        var operation;
        try {
          if (document.fullscreenElement && typeof document.exitFullscreen === "function") {
            operation = document.exitFullscreen();
          } else if (document.documentElement && typeof document.documentElement.requestFullscreen === "function") {
            operation = document.documentElement.requestFullscreen();
          } else {
            showStatus("当前浏览器不支持全屏");
            return;
          }
        } catch (_error) {
          showStatus("全屏暂时不可用");
          return;
        }
        if (operation && typeof operation.then === "function") {
          operation.then(function () {
            syncFullscreen();
            setOpen(false, true);
          }).catch(function () {
            showStatus("全屏暂时不可用");
          });
        } else {
          syncFullscreen();
          setOpen(false, true);
        }
      };

      trigger.addEventListener("click", function () { setOpen(!open, true); });
      close.addEventListener("click", function () { setOpen(false, true); });
      continueItem.addEventListener("click", function () { setOpen(false, true); });
      fullscreenItem.addEventListener("click", toggleFullscreen);
      restartItem.addEventListener("click", restart);
      exitItem.addEventListener("click", goBack);
      backdrop.addEventListener("click", function () { setOpen(false, true); });
      panel.addEventListener("keydown", function (event) {
        if (event.key === "Escape") {
          event.preventDefault();
          setOpen(false, true);
          return;
        }
        if (event.key !== "Tab" && event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
        var currentIndex = focusables.indexOf(event.target);
        if (currentIndex < 0) currentIndex = 0;
        if (event.key === "Tab") {
          event.preventDefault();
          var nextIndex = (currentIndex + (event.shiftKey ? -1 : 1) + focusables.length) % focusables.length;
          focusables[nextIndex].focus();
        } else {
          event.preventDefault();
          var direction = event.key === "ArrowUp" ? -1 : 1;
          focusables[(currentIndex + direction + focusables.length) % focusables.length].focus();
        }
      });
      if (typeof window.addEventListener === "function") {
        window.addEventListener("keydown", function (event) {
          if (open && event.key === "Escape") {
            event.preventDefault();
            setOpen(false, true);
          }
        });
        window.addEventListener("fullscreenchange", syncFullscreen);
      }
      syncFullscreen();
      document.body.appendChild(host);
    };

    if (document.body) mount();
    else if (typeof window.addEventListener === "function") window.addEventListener("DOMContentLoaded", mount, { once: true });
  }

  var existing = copyContext(window[CONTEXT_KEY]);
  var legacy = consumeLegacyHash();
  var handoff = readHandoff();
  // A freshly issued handoff is authoritative over a stale copied context or
  // an old bookmarked fragment. Keep arbitrary pre-existing integration
  // fields intact for game authors that set them before the bootstrap runs.
  window[CONTEXT_KEY] = Object.assign(existing, legacy, handoff);
  var inferredSlug = inferGameSlug(window[CONTEXT_KEY]);
  if (inferredSlug && !window[CONTEXT_KEY].gameSlug) window[CONTEXT_KEY].gameSlug = inferredSlug;
  installPlatformMenu(window[CONTEXT_KEY]);
})();

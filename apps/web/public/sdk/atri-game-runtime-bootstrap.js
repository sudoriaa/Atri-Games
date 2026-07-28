(function () {
  "use strict";

  // This is deliberately a classic, parser-blocking script. The importer
  // places it before a package's first script tag so a hash-router never sees
  // legacy Atri launch parameters as one of its routes.
  var CONTEXT_KEY = "__ATRI_GAME_CONTEXT__";
  var HANDOFF_SOURCE = "atri-game-launch";
  var HANDOFF_VERSION = 1;
  var LEGACY_KEYS = ["atri_ticket", "atri_game", "atri_api"];

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

  var existing = copyContext(window[CONTEXT_KEY]);
  var legacy = consumeLegacyHash();
  var handoff = readHandoff();
  // A freshly issued handoff is authoritative over a stale copied context or
  // an old bookmarked fragment. Keep arbitrary pre-existing integration
  // fields intact for game authors that set them before the bootstrap runs.
  window[CONTEXT_KEY] = Object.assign(existing, legacy, handoff);
})();

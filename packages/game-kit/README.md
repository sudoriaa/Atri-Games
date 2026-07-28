# @atri/game-kit

`@atri/game-kit` is the developer-side, zero-runtime-dependency tool for Atri Games. It does not care whether a game was built with Unity Web, Godot, Phaser, React, Vue, Svelte, Canvas, WebAssembly, or a custom stack.

```bash
npx @atri/game-kit init my-game
cd my-game
# put the browser build in game/ and a cover.webp beside atri-game.json
npx @atri/game-kit validate
npx @atri/game-kit pack --out my-game.atri
```

Inside this repository, the same commands are available before the first registry release as `pnpm game-kit init|validate|pack`.

For a complete Chinese walkthrough covering React/Vite, Unity Web, Godot Web, external backends, asset paths, admin import, version replacement, and troubleshooting, see [`docs/GAME_INTEGRATION.md`](../../docs/GAME_INTEGRATION.md).

The resulting package's internal ZIP contains `atri-game.json`, the cover, optional screenshots, and (for `runtime.kind: "static"`) the built `game/` directory. The platform only serves inert browser files; it never executes uploaded server code. A project with its own backend uses `runtime.kind: "external"` and points to the already deployed HTTPS entry URL.

## Encrypted package format

`pack` now emits an `ATRIENC1` container by default rather than a raw ZIP. The
tool first builds the normal internal ZIP, encrypts it with a fresh AES-256-GCM
content key, and wraps that 32-byte key with the bundled platform RSA public
key using RSA-OAEP-SHA256. The RSA-OAEP label is empty/default (`null` on the
server side). The complete unencrypted prefix through the GCM base nonce is passed
as AES-GCM additional authenticated data, so the platform detects altered key
metadata before it accepts the ZIP.

The binary layout is fixed for compatibility:

```text
ATRIENC1 (8 ASCII bytes)
version (uint8, 1)
wrappedKeyLength (uint32 big-endian)
nonceLength (uint8, 12)
tagLength (uint8, 16)
chunkSize (uint32 big-endian, 1048576)
wrapped AES content key
AES-GCM base nonce
repeated: frameLength (uint32 big-endian), AES-GCM ciphertext + tag
final authenticated empty frame (frameLength = 16)
```

Every non-terminal content frame is exactly 1 MiB; only the final content frame
may be shorter, and it is immediately followed by the authenticated empty
terminal frame. Frame `i` uses the nonce `baseNonce[0:4] || uint64BE(i)` and
additional authenticated data `header-through-baseNonce || uint64BE(i)`, which
lets the importer decrypt and verify a package as a stream rather than holding
its entire ZIP in memory.

Use the platform's matching public PEM by default, or select another
compatible environment key explicitly:

```bash
atri-game pack --out my-game.atri --public-key ./atri-platform-public.pem
```

For a legacy importer that only accepts raw ZIP archives, make the downgrade
explicit:

```bash
atri-game pack --out my-game.atri --plain
```

`--plain` intentionally disables package confidentiality and integrity at the
container layer; do not use it for normal platform imports. Only a public PEM
is distributed with this package. Private keys belong exclusively in the
platform's protected server configuration.

Static builds should use relative asset URLs or a base path of `/playables/<id>/`. Root `index.html` packages also receive an SPA history fallback. The Game Origin recognizes precompressed Unity-style `.wasm.br/.gz`, `.js.br/.gz`, and `.data.br/.gz` assets. Projects that require a backend, custom response headers, server-side rendering, or a different deployment model should use the external runtime; no framework-specific adapter is required.

## Optional platform services

The generated manifest launches anonymously. It retains a compatibility
SQLite metadata fallback when `storage` is omitted, but does not receive a
game ticket or expose the storage API until the game explicitly declares a
platform service. To use player storage, require login up front, or enable
matchmaking, edit the `services` block:

```json
{
  "services": {
    "networkRequired": true,
    "ownBackend": false,
    "identity": { "mode": "required" },
    "storage": { "provider": "sqlite", "scope": "player-game" },
    "matchmaking": { "enabled": true, "protocol": "http" }
  }
}
```

`player-game` keeps a separate SQLite JSON namespace for each player and game;
`player` marks player-owned data but remains isolated to the declaring game.
Browser tickets never receive a globally shared writable namespace. If a
static package omits `storage`, it stays an ordinary anonymous game. Declare
`{ "provider": "sqlite", "scope": "player-game" }` before calling the SDK
storage helpers; use `{ "provider": "none", "scope": "game" }` when the
game should remain fully independent of platform storage.

When `identity.mode` is `required`, or player storage/matchmaking is enabled,
the catalog displays “需登录” and the launch flow supplies a short-lived
game ticket. No OAuth library or platform secret is needed in the game:

```js
// On Atri's Game Origin, this module is served by the platform itself.
import { createAtriGame } from "/sdk/atri-game-sdk.js";

const atri = createAtriGame();
await atri.storage.set("progress", { level: 3 });
const progress = await atri.storage.get("progress");
const queueTicket = await atri.matchmaking.join({ mode: "ranked", region: "asia" });
```

`queueTicket.status` starts as `waiting` and becomes `matched` when another
player joins the same game/mode/region. Use `atri.matchmaking.status(id)` for
polling and `atri.matchmaking.cancel(id)` when the player leaves the queue.

Games that do not call these helpers remain ordinary browser games. External
games cannot use built-in services; they keep their own identity, database,
and matchmaking backend.

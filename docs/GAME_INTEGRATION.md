# 第三方游戏接入教程

本文从一个已经可以运行的游戏项目开始，说明如何把它接入 Atri Games。游戏可以使用任意语言、引擎、前端框架和后端技术；平台只要求玩家能够通过浏览器打开一个稳定的游戏入口。

## 先选择接入方式

| 方式 | 适用项目 | 需要上传什么 | 游戏代码运行在哪里 |
| --- | --- | --- | --- |
| `external` | 已经部署的 SSR、Node、Python、Go、Java、WebSocket、WebRTC 或带数据库的完整服务 | `atri-game.json` + 封面 | 你的 HTTPS 服务器 |
| `static` | Unity Web、Godot Web、Wasm、Canvas、React、Vue、Phaser、纯 HTML 等浏览器构建 | `.atri` 包 | Atri Games 的隔离 Game Origin |

选择规则很简单：

- 游戏有自己的服务端、数据库、SSR 或长连接：使用 `external`。
- 游戏构建后是一组可以直接由浏览器加载的文件：使用 `static`。
- 不需要为了接入而重写引擎，也不要求安装 SDK。

## 方案 A：打包成 `.atri`

### 1. 准备目录

最终目录应类似这样：

```text
my-game/
├── atri-game.json       # 必需：游戏元数据
├── cover.webp           # 必需：封面，支持 avif/jpg/jpeg/png/webp
├── screenshots/         # 可选：最多 8 张截图
│   └── gameplay.webp
└── game/                # static 必需：浏览器构建产物
    ├── index.html       # runtime.entry 指向的入口
    ├── assets/
    ├── game.js
    └── game.wasm
```

`game/` 内可以放任意静态文件。平台只托管这些浏览器文件，不会执行压缩包里的服务端代码。

### 2. 生成清单

在 Atri Games 仓库根目录开发时：

```bash
pnpm game-kit init ../my-game
```

如果 `@atri/game-kit` 已经安装或发布到 npm，也可以在游戏项目目录使用：

```bash
npx @atri/game-kit init .
```

打开 `atri-game.json`，至少确认以下字段：

```json
{
  "$schema": "https://atri.games/schemas/game-manifest.schema.json",
  "schemaVersion": 2,
  "id": "my-arcade-game",
  "version": "1.0.0",
  "title": "My Arcade Game",
  "summary": "A short description of the game and its main challenge.",
  "description": "Explain the objective, controls, supported devices, and online behavior.",
  "authors": [
    { "name": "Your Studio" }
  ],
  "license": "MIT",
  "engine": {
    "name": "HTML5",
    "framework": "Your framework"
  },
  "runtime": {
    "kind": "static",
    "entry": "index.html",
    "openIn": "same-tab",
    "bridge": "optional"
  },
  "services": {
    "networkRequired": false,
    "ownBackend": false,
    "realtime": [],
    "platformIntegrations": []
  },
  "privacy": {
    "collectsPersonalData": false,
    "dataSummary": "This game stores no personal data."
  },
  "media": {
    "cover": "cover.webp",
    "screenshots": ["screenshots/gameplay.webp"]
  },
  "compatibility": {
    "devices": ["desktop", "mobile"],
    "inputs": ["keyboard", "mouse", "touch"],
    "orientation": "landscape",
    "minimumViewport": {
      "width": 320,
      "height": 240
    }
  },
  "tags": ["arcade"]
}
```

字段规则：

- `id` 是永久标识，只能使用小写字母、数字和连字符，例如 `my-arcade-game`。
- `version` 使用语义化版本，例如 `1.0.0`。
- `runtime.entry` 是相对于 `game/` 的 HTML 文件，所以 `index.html` 实际路径是 `game/index.html`。
- `media.cover` 和截图路径是相对于 `atri-game.json` 的路径。
- `collectsPersonalData` 为 `true` 时，还必须填写 HTTPS `policyUrl`。
- `services.networkRequired` 和 `ownBackend` 使用 JSON 布尔值 `false`，不带引号。
- `services.identity.mode` 可选 `none`、`optional`、`required`。
- `services.storage` 声明 `sqlite` 时，平台为每个游戏提供隔离的 SQLite 命名空间；`scope` 可选 `player-game` 或 `player`，两者都按玩家隔离且始终带当前 `gameId`。浏览器票据不提供“所有玩家共享可写”的全局空间，避免任意玩家覆盖公共状态。显式 `provider: "none"` 时 `scope` 使用 `game` 作为关闭状态占位值。
- `services.matchmaking.enabled` 开启内置匹配，`protocol` 目前使用 `http` 轮询，也接受 `sse`/`websocket` 作为版本化能力声明。
- starter 默认省略三个可选平台服务，游戏可以匿名启动。省略 `storage` 时只保留兼容性 SQLite 元数据兜底，不会签发票据或开放 SDK 存档；要调用存档接口，必须显式声明玩家存储。显式声明玩家存储、`identity:required` 或开启匹配时，目录才标记“需登录”并在启动前强制登录。完全不使用内置数据时可声明 `storage: {"provider":"none","scope":"game"}`。
- `runtime.kind: "external"` 只允许这些服务保持关闭状态；外部游戏继续使用自己的登录、数据库和后端。

### 3. 构建游戏并放入 `game/`

以 Vite/React 为例，构建时使用相对资源路径：

```ts
// vite.config.ts
import { defineConfig } from "vite";

export default defineConfig({
  base: "./"
});
```

然后构建并复制产物：

```bash
npm run build

# macOS/Linux
rm -rf game
mkdir game
cp -R dist/. game/

# Windows PowerShell
Remove-Item game -Recurse -Force -ErrorAction SilentlyContinue
New-Item game -ItemType Directory | Out-Null
Copy-Item dist\* game\ -Recurse
```

其他常见项目的处理方式：

- Unity Web：将 WebGL Build 输出目录的全部文件复制到 `game/`，确保入口文件名与 `runtime.entry` 一致。
- Godot Web：将 HTML5/Web 导出目录复制到 `game/`，入口通常是 `index.html`。
- Vue、Svelte、Webpack：把生产构建目录复制到 `game/`，并将 public path/base 设置为相对路径。
- 纯 HTML/Canvas：直接把 `index.html`、脚本、样式和资源放到 `game/`。

静态游戏应避免 `/assets/foo.js` 这类指向域名根目录的路径。使用 `./assets/foo.js` 或构建工具提供的相对 base，否则浏览器会把资源请求发送到门户 Origin。

### 4. 校验并打包

在仓库根目录执行：

```bash
pnpm game-kit validate ../my-game/atri-game.json
pnpm game-kit pack ../my-game/atri-game.json --out ../my-arcade-game-1.0.0.atri
```

如果使用 npm CLI：

```bash
cd my-game
npx @atri/game-kit validate
npx @atri/game-kit pack --out my-arcade-game-1.0.0.atri
```

成功时会看到类似输出：

```text
valid: my-arcade-game@1.0.0 (static)
created .../my-arcade-game-1.0.0.atri
```

默认输出是经过加密认证的 `ATRIENC1` 容器，改扩展名、`unzip` 或 `tar` 都不能直接列出内容。请在打包前检查 `atri-game.json` 与 `game/` 目录，并保留打包命令、文件大小和 SHA-256 作为交付证据。CLI 会在打包前执行同一份 Manifest 与文件校验，服务器导入后会解密并再次执行安全校验。

解密后的包根目录至少应包含：

```text
atri-game.json
cover.webp
game/index.html
```

### 5. 从管理后台导入

1. 打开管理后台并登录。
2. 进入“游戏管理”。
3. 点击“导入游戏包”。
4. 选择 `.atri` 文件。
5. 选择分类。
6. 选择导入后的状态：
   - `草稿`：只保存，暂不展示。
   - `待审核`：进入审核队列。
   - `已发布`：立即出现在主页。
   - `已下架`：保留文件和数据，但不出现在公开目录。
7. 新游戏直接导入；同一个 `id` 发布新版本时勾选“覆盖”。

导入器会检查 Manifest、封面、入口页、路径穿越、重复文件、符号链接、文件数量和包大小。上传完成后，静态文件会写入独立的 Game Origin，门户页面不会加载游戏脚本。

### 6. 更新游戏版本

更新流程：

```bash
# 1. 修改游戏代码并重新构建到 game/
# 2. 修改 atri-game.json
"version": "1.1.0"

# 3. 校验、打包
pnpm game-kit validate ../my-game/atri-game.json
pnpm game-kit pack ../my-game/atri-game.json --out ../my-arcade-game-1.1.0.atri
```

在后台导入时使用相同 `id`，勾选“覆盖”。平台会原子替换该游戏的封面、Manifest 和静态构建；其他游戏的资源不受影响。

## 方案 B：接入已有的外部游戏

带后端的项目不需要打包服务端。先把游戏部署到自己的 HTTPS 域名，例如：

```text
https://games.example.com/my-game/
```

然后将清单中的 `runtime` 改成：

```json
{
  "kind": "external",
  "url": "https://games.example.com/my-game/",
  "openIn": "new-tab"
}
```

此时：

- `game/` 目录可以移除。
- Atri Games 保存清单和封面。
- 玩家点击开始后直接跳转到你的 HTTPS 游戏。
- 你的 Node、Python、Go、Java、PHP、SSR、WebSocket、数据库和部署方式全部由你自己控制。

外部游戏仍可以放入一个很小的 `.atri` 包中，只包含 `atri-game.json` 和封面，然后按静态包相同的后台入口导入。

## 可选：使用 SDK

SDK 不是接入前提。平台会在门户和 Game Origin 同时提供可直接加载的 ES Module：

```js
import { createAtriGame } from "/sdk/atri-game-sdk.js";
```

这条路径随当前平台版本部署，不需要 npm 发布，也不会增加打包依赖。本地联调或希望把 SDK 一起打进游戏构建时，可以把本仓库包作为文件依赖安装：

```bash
npm install --save file:../Atri-Games/packages/game-sdk
```

然后使用包名导入：

```js
import { createAtriGame } from "@atri/game-sdk";

const atri = createAtriGame();

atri.on("pause", () => game.pause());
atri.on("resume", () => game.resume());
atri.on("exit", () => {
  // 退出时保存本地临时状态、关闭音频等。
  game.saveBeforeLeaving();
});

atri.ready({ build: "1.0.0" });
```

`exit()` 会先触发本地 `exit` 事件，再通知 Atri 宿主或使用平台提供的返回地址。页面隐藏、离开页面和平台菜单打开会触发 `pause`；重新可见或菜单关闭会触发 `resume`。销毁单页游戏实例时调用 `atri.dispose()`，避免保留浏览器事件监听器。

没有 Atri 宿主时，SDK 会退化为本地行为或空操作，游戏仍可以直接在自己的域名运行。完整 API、事件类型与错误处理见 [`@atri/game-sdk` README](../packages/game-sdk/README.md)。

### 玩家展示资料

已登录且启用平台身份、存档或匹配的游戏可以读取最小展示资料：

```js
const player = atri.identity.getUser();

if (player) {
  nickname.textContent = player.displayName ?? `玩家 #${player.userNumber ?? player.id}`;
  avatar.src = player.avatarUrl ?? "/images/default-avatar.png";
}
```

返回对象仅包含 `id`、`userNumber`、`displayName` 和 `avatarUrl`。`userNumber` 从 1 开始递增，适合展示；它和 `id` 都不是授权凭据。游戏不会取得玩家邮箱、角色或平台登录令牌。兼容旧的启动交接时，资料可能只有 `{ id }`，匿名启动时可能为 `null`，因此请始终保留昵称和头像的本地回退。

### 错误处理

平台服务调用失败时，区分启动上下文和 HTTP 错误，不要让存档或匹配失败阻断单机核心流程：

```js
import { AtriGameContextError, AtriPlatformError } from "@atri/game-sdk";

try {
  await atri.storage.set("progress", { level: 3 });
} catch (error) {
  if (error instanceof AtriGameContextError) {
    showOfflineSaveNotice();
  } else if (error instanceof AtriPlatformError) {
    console.warn(error.status, error.code, error.message);
    showRetryNotice();
  } else {
    throw error;
  }
}
```

## 平台票据、SQLite 数据与匹配

声明了平台身份、玩家 SQLite 存档或内置匹配的静态游戏从门户启动时，平台会为已登录玩家签发一个短期、只绑定当前 `gameId` 的游戏票据。门户通过一次性的 `window.name` 交接把票据传给 Game Origin，地址栏不包含票据；平台 bootstrap 会在游戏脚本前读取并清空交接数据，SDK 再使用该上下文并在到期前自动续签。未声明这些能力的普通静态游戏不接收票据。平台登录 JWT 与游戏票据是两种不同凭据，游戏票据不能访问 `/me`、收藏或管理接口。

最小接入代码：

```js
import { createAtriGame } from "@atri/game-sdk";

const atri = createAtriGame();
atri.ready({ build: "1.0.0" });

// static .atri 默认可用；每个游戏、每个玩家自动隔离。
await atri.storage.set("progress", { level: 3, coins: 12 });
const progress = await atri.storage.get("progress");

// 仅在 matchmaking.enabled=true 时使用。默认按 mode + region 配对。
const queue = await atri.matchmaking.join({ mode: "ranked", region: "asia" });
const timer = setInterval(async () => {
  const status = await atri.matchmaking.status(queue.ticketId);
  if (status.status === "matched") {
    clearInterval(timer);
    startOnlineRound(status.matchId);
  }
}, 1000);
```

不使用 SDK 时也可以直接调用同一组 HTTP 接口。Game Origin 已经把 `/api/v1/*` 反向代理到平台 API，因此代码只需要使用相对地址：

```text
POST   /api/v1/games/<slug>/ticket                 平台登录 JWT → 游戏票据
POST   /api/v1/games/<slug>/ticket/refresh         当前游戏票据 → 同 Scope 新票据
GET    /api/v1/games/<slug>/data/<key>             游戏票据 → JSON 数据
PUT    /api/v1/games/<slug>/data/<key>             {"value": ...}
DELETE /api/v1/games/<slug>/data/<key>
POST   /api/v1/games/<slug>/matchmaking/tickets    {"mode":"ranked","region":"asia"}
GET    /api/v1/games/<slug>/matchmaking/tickets/<ticketId>
DELETE /api/v1/games/<slug>/matchmaking/tickets/<ticketId>
```

单个数据值上限为 256 KiB，每位玩家在单个游戏中最多 256 个键、合计 4 MiB，键名不能包含路径分隔符。匹配票据默认两分钟过期，首个相同 `mode`/`region` 的玩家与下一个玩家组成一个 `matchId`；这一步只负责配对，实际对局连接仍由游戏自己的 WebSocket/WebRTC/HTTP 房间服务承载。后续可以保持接口不变，将匹配实现切换到分片服务。玩家未登录时，普通静态游戏仍可启动；调用玩家数据或匹配接口时会得到登录提示。外部 URL 游戏不会收到 Atri 游戏票据，也不会使用这组内置接口。

## 常见问题

### `missing static entry: game/index.html`

`runtime.entry` 指向 `index.html` 时，文件必须位于 `game/index.html`。如果入口叫 `main.html`，同时修改清单：

```json
"runtime": {
  "kind": "static",
  "entry": "main.html",
  "openIn": "same-tab"
}
```

### `missing packaged cover`

`media.cover` 写的是相对于 Manifest 的路径。确认文件真实存在，例如：

```text
atri-game.json
cover.webp
```

并保持：

```json
"media": { "cover": "cover.webp" }
```

### 页面能打开但 JS/CSS 404

检查构建工具的 base/public path。静态包部署路径是：

```text
/games/<game-id>/play
```

优先使用相对路径 `./assets/...`。不要把资源写成门户根路径 `/assets/...`。

### 游戏需要后端

使用 `external`，先部署自己的 HTTPS 服务，再在 Manifest 中填写 `runtime.url`。`.atri` 包只负责登记入口和封面，平台不会在静态包中启动 Node/Python/Go 等服务端。

### 上传大小限制

默认限制由以下环境变量控制：

```text
ATRI_GAME_PACKAGE_MAX_BYTES=536870912
ATRI_GAME_PACKAGE_MAX_UNPACKED_BYTES=2147483648
ATRI_GAME_PACKAGE_MAX_FILES=20000
```

大体积 Unity/WebAssembly 项目应清理调试符号、未使用资源和重复压缩文件后再打包。

## 接入前检查清单

- [ ] `id`、版本和作者信息正确。
- [ ] `atri-game.json` 能通过 `validate`。
- [ ] 封面和入口文件都在包内。
- [ ] 直接打开 `game/index.html` 可以启动。
- [ ] 刷新、后退、窄屏和移动触控已经测试。
- [ ] 静态资源使用相对路径。
- [ ] 网络、后端、实时连接和隐私字段与实际行为一致。
- [ ] 包内没有 Token、密码、私钥或数据库文件。
- [ ] 新版本递增 `version`，覆盖时保持原 `id`。

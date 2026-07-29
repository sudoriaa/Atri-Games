# @atri/game-sdk

`@atri/game-sdk` 是 Atri Games 为浏览器游戏提供的可选、零运行时依赖桥接。它不会接管游戏引擎、渲染或游戏服务器；没有 Atri 宿主时，生命周期通知会保留为本地事件，平台服务调用会给出可处理的错误。

完整的游戏包、静态资源和外部后端接入说明见 [游戏接入指南](../../docs/GAME_INTEGRATION.md)。

## 安装

在 Atri Game Origin 中，直接使用平台随版本部署的模块：

```js
import { createAtriGame } from "/sdk/atri-game-sdk.js";
```

本地联调或需要把 SDK 打入构建产物时，使用仓库包：

```bash
npm install --save file:../Atri-Games/packages/game-sdk
```

```js
import { createAtriGame } from "@atri/game-sdk";
```

## 最小接入

```js
import { createAtriGame } from "@atri/game-sdk";

const atri = createAtriGame();

atri.on("pause", () => game.pause());
atri.on("resume", () => game.resume());
atri.on("exit", () => game.saveBeforeLeaving());

atri.ready({ build: "1.0.0" });
```

`ready()` 会向嵌入宿主发送就绪消息，并同步触发本地 `ready` 事件。`progress()` 和 `score()` 向宿主报告可选的加载进度和成绩，不会影响游戏自身的流程。

## 生命周期与退出

| 事件 | 回调参数 | 触发时机 |
| --- | --- | --- |
| `ready` | `Record<string, unknown>` | 调用 `ready()` 时 |
| `pause` | 无 | 页面隐藏、离开页面或平台菜单打开 |
| `resume` | 无 | 页面重新可见或平台菜单关闭 |
| `exit` | 无 | 游戏调用 `exit()` 时 |

`exit()` 先触发本地 `exit` 监听器，再通知宿主或导航到平台提供的返回地址。因此清理临时状态、暂停音频或写入游戏自身存档应监听 `exit`，不要只依赖页面卸载事件。

```js
atri.on("exit", () => {
  audio.stopAll();
  saveLocalCheckpoint();
});

exitButton.addEventListener("click", () => atri.exit());
```

调用 `dispose()` 会移除 SDK 注册的浏览器事件和所有本地监听器。单页游戏销毁场景或重新创建游戏实例时应调用它。

## 玩家展示资料

为启用平台身份、存档或匹配的已登录玩家，`identity.getUser()` 返回最小展示资料：

```js
const player = atri.identity.getUser();

if (player) {
  nameLabel.textContent = player.displayName ?? `玩家 #${player.userNumber ?? player.id}`;
  avatar.src = player.avatarUrl ?? "/images/default-avatar.png";
}
```

返回类型为：

```ts
interface AtriGameUser {
  id: string;
  userNumber?: number;
  displayName?: string;
  avatarUrl?: string;
}
```

`id` 是平台内部稳定标识；`userNumber` 是从 1 开始的公开连续编号，适合展示和玩家间的人工沟通，但两者都不是授权凭据。SDK 不会把邮箱、角色、平台登录令牌或其他账户字段暴露给游戏。旧平台交接或匿名启动可能只返回 `{ id }` 或 `null`，游戏应为缺失的头像和昵称准备回退显示。

`identity.getTicket()` 在平台允许时取得或续签游戏专用票据；如果票据响应带有玩家资料，SDK 会同步更新 `getUser()` 的结果。游戏不需要也不应自行解析、持久化或转发票据。

## 平台服务

在 Manifest 中声明对应服务后，SDK 会使用当前游戏、当前玩家范围内的 API：

```js
await atri.storage.set("progress", { level: 3, coins: 12 });
const progress = await atri.storage.get("progress");

const queue = await atri.matchmaking.join({ mode: "ranked", region: "asia" });
const status = await atri.matchmaking.status(queue.ticketId);
```

支持的方法：

| 模块 | 方法 |
| --- | --- |
| `storage` | `get(key)`、`set(key, value)`、`remove(key)` |
| `matchmaking` | `join(input)`、`status(ticketId)`、`cancel(ticketId)` |
| `identity` | `getTicket()`、`getUser()` |
| 实例 | `fullscreen(element?)`、`exitFullscreen()`、`exit()` |

存档和匹配仍需在 Manifest 中显式声明。外部 HTTPS 游戏使用自己的身份和后端，不会收到 Atri 游戏票据。

## 错误处理

SDK 导出的错误类可用于区分本地启动上下文问题和平台 HTTP 响应：

```js
import {
  AtriGameContextError,
  AtriPlatformError,
} from "@atri/game-sdk";

try {
  await atri.storage.get("progress");
} catch (error) {
  if (error instanceof AtriGameContextError) {
    // 缺少游戏 slug 或登录票据。保持单机流程可用。
  } else if (error instanceof AtriPlatformError) {
    console.warn(error.status, error.code, error.message);
  } else {
    throw error;
  }
}
```

`AtriPlatformError` 包含 HTTP `status` 和平台错误 `code`。`AtriSdkError` 是 SDK 自身错误的基类。平台服务失败不应阻断游戏的本地核心玩法，应在游戏内提供可理解的重试或降级状态。

## TypeScript

SDK 自带声明文件。生命周期事件带有精确的事件名和回调参数：

```ts
atri.on("ready", (details) => console.info(details.build));
atri.on("pause", () => game.pause());
atri.on("exit", () => game.saveBeforeLeaving());
```

仍可使用字符串监听自定义本地事件；这些事件的回调值类型为 `unknown`，因为 SDK 不定义它们的 payload。

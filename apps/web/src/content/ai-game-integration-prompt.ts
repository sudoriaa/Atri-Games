export type AtriRuntimeChoice = "auto" | "static" | "external";
export type AtriIdentityChoice = "none" | "optional" | "required";
export type AtriStorageChoice = "default" | "sqlite" | "none";

export interface AiGameIntegrationPromptConfig {
  projectName: string;
  gameId: string;
  authorName: string;
  techStack: string;
  buildCommand: string;
  buildOutput: string;
  runtime: AtriRuntimeChoice;
  externalUrl: string;
  identity: AtriIdentityChoice;
  storage: AtriStorageChoice;
  matchmaking: boolean;
}

export const defaultAiGameIntegrationPromptConfig: AiGameIntegrationPromptConfig = {
  projectName: "我的游戏",
  gameId: "my-ai-game",
  authorName: "开发者名称",
  techStack: "请从项目中自动识别",
  buildCommand: "请从 package.json 或工程配置中自动识别",
  buildOutput: "请从构建配置中自动识别",
  runtime: "auto",
  externalUrl: "",
  identity: "optional",
  storage: "default",
  matchmaking: false,
};

const clean = (value: string, fallback: string) => value.trim() || fallback;

export function normalizeAtriGameId(value: string) {
  const normalized = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64)
    .replace(/^-+|-+$/g, "");
  return normalized.length >= 3 ? normalized : "my-ai-game";
}

function createManifestExample(config: AiGameIntegrationPromptConfig) {
  const isExternal = config.runtime === "external";
  const effectiveIdentity = config.matchmaking ? "required" : config.identity;
  const integrations = ["lifecycle"];

  if (effectiveIdentity !== "none") integrations.push("identity");
  if (!isExternal && config.storage !== "none") integrations.push("cloud-save");

  const services: Record<string, unknown> = isExternal
    ? {
        networkRequired: true,
        ownBackend: true,
        realtime: [],
        platformIntegrations: [],
        identity: { mode: "none" },
        storage: { provider: "none", scope: "game" },
        matchmaking: { enabled: false, protocol: "http" },
      }
    : {
        networkRequired: effectiveIdentity === "required" || config.storage === "sqlite" || config.matchmaking,
        ownBackend: false,
        realtime: [],
        platformIntegrations: integrations,
        identity: { mode: effectiveIdentity },
        matchmaking: { enabled: config.matchmaking, protocol: "http" },
      };

  if (!isExternal && config.storage === "sqlite") {
    services.storage = { provider: "sqlite", scope: "player-game" };
  } else if (!isExternal && config.storage === "none") {
    services.storage = { provider: "none", scope: "game" };
  }

  return {
    $schema: "https://atri.games/schemas/game-manifest.schema.json",
    schemaVersion: 2,
    id: normalizeAtriGameId(config.gameId),
    version: "1.0.0",
    title: clean(config.projectName, "我的游戏"),
    summary: "请根据真实玩法填写 10—240 字的一句话简介。",
    description: "请根据项目真实内容说明目标、核心循环、操作方式、设备支持、联网行为和注意事项。",
    authors: [{ name: clean(config.authorName, "开发者名称") }],
    license: "请读取项目现有许可证并填写；没有时先向项目所有者确认",
    engine: {
      name: clean(config.techStack, "请从项目中自动识别"),
      framework: clean(config.techStack, "请从项目中自动识别"),
    },
    runtime: isExternal
      ? {
          kind: "external",
          url: clean(config.externalUrl, "https://example.com/game/"),
          openIn: "new-tab",
        }
      : {
          kind: "static",
          entry: "index.html",
          openIn: "same-tab",
          bridge: "optional",
        },
    services,
    privacy: {
      collectsPersonalData: false,
      dataSummary: "请按项目真实的数据收集、存储和联网行为填写；若收集个人数据，必须同时提供 HTTPS 隐私政策地址。",
    },
    media: {
      cover: "cover.webp",
      screenshots: [],
    },
    compatibility: {
      devices: ["desktop", "mobile"],
      inputs: ["keyboard", "mouse", "touch"],
      orientation: "any",
      minimumViewport: {
        width: 320,
        height: 480,
      },
    },
    tags: ["indie", "ai-made"],
    ai: {
      tools: ["请替换为本项目实际使用的 AI 工具"],
      disclosure: "本项目使用 AI 辅助开发与 Atri Games 适配；请把本句改成真实、具体的说明。",
    },
  };
}

function runtimeRequirement(config: AiGameIntegrationPromptConfig) {
  if (config.runtime === "static") {
    return [
      "本次已指定 `static`：必须产出包含浏览器构建文件的完整 `.atri` 包。",
      "不要把 Node、Python、Go、Java、PHP、数据库进程、SSR 服务或 WebSocket 服务塞进 `game/`；`game/` 只放浏览器可直接加载的静态文件。",
      "static 前端仍可调用已经单独部署的 HTTPS/WebSocket/WebRTC 后端。若项目如此，Manifest 必须如实设置 networkRequired、ownBackend 和 realtime；这不妨碍它同时使用 Atri 游戏票据或匹配队列。",
    ].join("\n");
  }
  if (config.runtime === "external") {
    return [
      "本次已指定 `external`：保留项目自己的前后端和数据库部署，只把已经上线的 HTTPS 游戏入口登记到 `.atri`。",
      `目标入口：${clean(config.externalUrl, "请从部署配置中确认真实 HTTPS URL；禁止用 example.com 交付")}`,
      "外部包只包含 Manifest、封面和可选截图，不包含 `game/`；平台内置身份、SQLite 和匹配能力必须显式关闭。",
    ].join("\n");
  }
  return [
    "本次选择“自动判断”。你必须先审计项目，再按下面的决策树选且只选一种运行方式：",
    "- 游戏入口能构建成纯 HTML/CSS/JS/Wasm/图片/音频等浏览器文件：选择 `static`。即使它会调用单独部署的 API、数据库服务、WebSocket 房间或 WebRTC 信令，也仍可使用 static 托管前端。",
    "- 游戏入口本身依赖 SSR、Node/Python/Go/Java/PHP 动态渲染、服务端路由、特殊响应头或其他无法拆分的常驻进程：选择 `external`。",
    "- static + 自有后端时，后端必须先独立部署到公网 HTTPS/WSS；包内只放前端，并将 ownBackend、networkRequired、realtime 与实际行为写入 Manifest。",
    "- 仅使用 Atri 内置玩家票据、SQLite 存档或内置匹配队列，不算自有后端，可以继续选择 `static`。",
    "- 无论选择哪种入口，都禁止把服务端文件塞进 `game/`。runtime 决定的是游戏入口由谁托管，不是简单地看项目有没有后端。",
    "最终报告必须写清证据和选择结果。下面 Manifest 示例以 `static` 为起点；若审计后选 `external`，必须按本提示词的 external 规则重写，不能机械照抄。",
  ].join("\n");
}

function byRuntime(config: AiGameIntegrationPromptConfig, staticRequirement: string, externalRequirement: string) {
  if (config.runtime === "external") return externalRequirement;
  if (config.runtime === "auto") {
    return [
      "若审计后选择 `static`：",
      staticRequirement,
      "",
      "若审计后选择 `external`：",
      externalRequirement,
    ].join("\n");
  }
  return staticRequirement;
}

function identityRequirement(config: AiGameIntegrationPromptConfig) {
  const effectiveIdentity = config.matchmaking ? "required" : config.identity;
  const storageForcesLogin = config.storage === "sqlite";
  const descriptions: Record<AtriIdentityChoice, string> = {
    none: "不申请 Atri identity scope，也不把 SDK 的 userId 当作完整玩家资料。若另行启用了存储或匹配，仍可使用对应 scope 的游戏票据调用该能力。",
    optional: "匿名玩家可以启动；登录玩家会获得当前游戏专用的短期票据，可使用平台存档等可选能力。代码必须优雅处理 `atri.authenticated === false`。",
    required: "启动前必须登录，目录会显示“需登录”。游戏内只使用平台签发的当前游戏票据，不接触门户登录 Token。",
  };
  const loginGate = config.matchmaking
    ? "\n因已开启 Atri 匹配，本次 static 分支的 identity mode 强制按 `required` 处理，并在启动前要求登录。"
    : storageForcesLogin && effectiveIdentity !== "required"
      ? `\n虽然 identity mode 保持 \`${effectiveIdentity}\`，显式 SQLite player scope 仍会让目录显示“需登录”并在启动前拦截匿名玩家；身份 mode 与有效启动门槛是两个不同概念。`
      : "";
  const staticRequirement = `${descriptions[effectiveIdentity]}${loginGate}`;
  const externalRequirement = [
    "external 游戏不会收到 Atri 游戏票据，Manifest 必须写 `identity: {\"mode\":\"none\"}`。",
    "保留并验证项目自己的登录/游客逻辑；不要读取 `atri_ticket`，也不要把外部账号冒充成 Atri 玩家身份。",
  ].join("\n");
  return byRuntime(config, staticRequirement, externalRequirement);
}

function storageRequirement(config: AiGameIntegrationPromptConfig) {
  const externalRequirement = [
    "external 游戏不使用 Atri SQLite，Manifest 必须写 `storage: {\"provider\":\"none\",\"scope\":\"game\"}`。",
    "保留项目自己的数据库、API 或设备本地存储，并在隐私说明中如实记录；不要调用 `atri.storage`。",
  ].join("\n");
  let staticRequirement: string;
  if (config.storage === "sqlite") {
    staticRequirement = [
      "显式使用平台 SQLite：Manifest 写 `storage: {\"provider\":\"sqlite\",\"scope\":\"player-game\"}`。",
      "这会要求玩家登录，并按 gameId + playerId 隔离。使用 SDK 的 `atri.storage.get/set/remove`，不要在浏览器直连 SQLite，也不要打包数据库文件。",
    ].join("\n");
  } else if (config.storage === "none") {
    staticRequirement = [
      "完全关闭平台存储：Manifest 必须写 `storage: {\"provider\":\"none\",\"scope\":\"game\"}`。",
      "允许继续使用纯本地 localStorage/IndexedDB（若项目本来如此），但要在隐私与数据说明中如实写明；不要调用 `atri.storage`。",
    ].join("\n");
  } else {
    staticRequirement = [
      "采用普通匿名静态包策略：Manifest 中省略 `services.storage`。",
      "省略存储时不会申请游戏票据，也不能调用 SDK 存档；如需云存档，必须选择显式 SQLite 选项。代码可保留本地/内存玩法，不得因不存在平台存档而阻断启动。",
    ].join("\n");
  }
  return byRuntime(config, staticRequirement, externalRequirement);
}

function matchmakingRequirement(config: AiGameIntegrationPromptConfig) {
  const externalRequirement = [
    "external 游戏不使用 Atri 内置匹配，Manifest 必须写 `matchmaking: {\"enabled\":false,\"protocol\":\"http\"}`。",
    "保留并验证项目自己的匹配/房间服务。外部后端负责排队、房间、连接鉴权、断线恢复和扩缩容；不要调用 `atri.matchmaking`。",
  ].join("\n");
  let staticRequirement: string;
  if (!config.matchmaking) {
    staticRequirement = [
      "本次不开启平台匹配：Manifest 写 `matchmaking: {\"enabled\":false,\"protocol\":\"http\"}`，不要出现排队按钮或虚假的在线匹配状态。",
      "若审计发现项目原本有自建匹配/联机后端，保留其逻辑。只要入口仍是浏览器静态构建即可继续用 static，同时把 ownBackend、networkRequired 和 realtime 如实声明；入口本身依赖服务端时才选择 external。",
    ].join("\n");
  } else {
    staticRequirement = [
      "开启 Atri 内置匹配：Manifest 写 `matchmaking: {\"enabled\":true,\"protocol\":\"http\"}`，身份模式必须为 `required`。",
      "用 SDK 完成 join → 每约 1 秒 status 轮询 → matched/cancelled/expired 分支 → 离开页面时 cancel 的完整状态机；重复点击不能创建重复队列票据。",
      "内置服务只把相同 game + mode + region 的玩家配成一个 `matchId`，票据约两分钟过期。它不承载实时对局数据。",
      "如果游戏需要玩家间同步，把自己的 WebSocket/WebRTC/HTTP 房间或信令服务独立部署到公网 HTTPS/WSS，并在 static Manifest 中设置 `networkRequired:true`、`ownBackend:true` 和真实的 `realtime`。static 前端可以同时使用 Atri matchId 和自有房间服务；只有游戏入口本身必须由自有服务动态提供时才改为 external。",
      "把 matchId 交给房间服务时，服务端还必须实施自己的短期连接凭据、玩家归属和重放防护；不要把 Atri 游戏票据转发给无关第三方。",
    ].join("\n");
  }
  return byRuntime(config, staticRequirement, externalRequirement);
}

function staticOnly(config: AiGameIntegrationPromptConfig, content: string) {
  if (config.runtime === "external") return "";
  if (config.runtime === "auto") {
    return `以下内容只在审计结果为 \`static\` 时执行；若最终选择 \`external\`，跳过这部分且不要接入 Atri SDK 或游戏票据。\n\n${content}`;
  }
  return content;
}

function ticketRules(config: AiGameIntegrationPromptConfig) {
  return staticOnly(
    config,
    `票据规则：

- 平台从门户启动声明了内置能力的 static 游戏时，会通过一次性的浏览器启动上下文交接票据；票据不会写入 URL、query、hash、浏览器历史、Referer 或服务器访问日志。普通 static 游戏不会因默认 SQLite 兜底而收到票据。
- 游戏必须尽早初始化官方 SDK。Game Origin 的启动 bootstrap 会在游戏自己的脚本之前读取并清空启动上下文，再交给 SDK；不要自行读取或持久化 \`window.name\`，不要自己解析 JWT、不要验证签名、不要把票据复制到 localStorage/IndexedDB/cookie。
- 需要 Hash Router 的游戏照常使用 \`#/route\`；平台不再向 hash 写入票据。为兼容早期启动链接，bootstrap 会仅删除遗留的 \`atri_ticket\`、\`atri_game\`、\`atri_api\` 参数，并保留原有游戏路由和其余 query。
- 游戏票据短期有效且只绑定当前玩家、当前 gameId 和声明的 scope；它不能调用门户 \`/me\`、收藏或任何管理接口。
- 不得把票据、用户 ID 或存档发送给广告、分析或其他无关第三方域名。错误日志不得打印完整票据。
- 有效启动门槛由能力组合决定：\`identity.required\`、显式 \`sqlite + player/player-game\`、或 \`matchmaking.enabled=true\` 任一成立，目录都会显示“需登录”并在启动前要求登录；其余 static 游戏必须允许匿名进入。`,
  );
}

function storageContract(config: AiGameIntegrationPromptConfig) {
  return staticOnly(
    config,
    `平台存储契约：

- SDK：\`atri.storage.get(key)\`、\`atri.storage.set(key, value)\`、\`atri.storage.remove(key)\`。
- 每个 value 必须是可 JSON 序列化的数据，单值不超过 256 KiB。
- 每位玩家在每个游戏最多 256 个键，合计最多 4 MiB。
- key 最长 80 个字符，不含 \`/\`、反斜杠等路径分隔符；使用稳定常量，例如 \`progress-v1\`、\`settings-v1\`。
- 给存档加 \`schemaVersion\`，读取时做类型校验和迁移；损坏或不存在时回到安全默认值。
- 高频状态先在内存中聚合并节流/防抖保存，不要每帧或每次按键写远端。
- 明确处理 401/403、404、409、配额、断网和超时；远端失败不应覆盖最后一个有效本地状态。
- 禁止在包里放 SQLite 文件、数据库密码或 SQL 驱动；浏览器只调用平台 API。`,
  );
}

function sdkRequirement(config: AiGameIntegrationPromptConfig) {
  if (config.runtime === "external") {
    return [
      "本次是 `external`：跳过 Atri SDK 适配，不读取 `atri_ticket`，不调用 `atri.storage` 或 `atri.matchmaking`。",
      "继续使用项目自己的身份、数据和联机客户端；只需要保证 external HTTPS 入口和 Manifest 描述准确。",
    ].join("\n");
  }
  const prefix = config.runtime === "auto"
    ? "只有审计结果为 `static` 时才执行本小节；若最终选择 `external`，整节跳过。"
    : "本次是 `static`，按实际启用的平台能力完成本小节。";
  return `${prefix}

SDK 不是普通游戏启动的前提。请把它封装在一个小型 adapter（如 \`src/platform/atri.ts\`），游戏其他模块只依赖 adapter 暴露的能力。Atri Game Origin 会提供：

~~~js
const sdkUrl = "/sdk/atri-game-sdk.js";

export async function connectAtriPlatform() {
  try {
    const { createAtriGame } = await import(/* @vite-ignore */ sdkUrl);
    const atri = createAtriGame();
    atri.ready({ build: "1.0.0" });
    return atri;
  } catch (error) {
    console.info("Atri SDK is not present; continuing in standalone mode.");
    return null;
  }
}
~~~

如果项目与 Atri Games 仓库相邻且希望打包 SDK，也可使用 \`file:../Atri-Games/packages/game-sdk\` 安装 \`@atri/game-sdk\`，再静态导入。不要假定 npm registry 已经发布最新版。无论哪种方式，在没有 Atri 宿主时，单机核心玩法仍应能启动。

使用存储时采用类似结构（按项目语言和状态模型改写，禁止逐字堆入不相关项目）：

~~~js
const atri = await connectAtriPlatform();

export async function loadProgress() {
  if (!atri?.authenticated) return loadLocalProgress();
  try {
    const record = await atri.storage.get("progress-v1");
    return migrateAndValidate(record.value);
  } catch (error) {
    return loadLocalProgress();
  }
}

export async function saveProgress(value) {
  saveLocalProgress(value);
  if (!atri?.authenticated) return false;
  try {
    await atri.storage.set("progress-v1", {
      schemaVersion: 1,
      updatedAt: new Date().toISOString(),
      ...value
    });
    return true;
  } catch (error) {
    markCloudSyncPending(error);
    return false;
  }
}
~~~

开启匹配时采用单实例状态机：

~~~js
const queue = await atri.matchmaking.join({ mode: "ranked", region: "asia" });
let activeTicketId = queue.ticketId;
let consecutiveNetworkFailures = 0;

async function pollQueue() {
  while (activeTicketId) {
    try {
      const status = await atri.matchmaking.status(activeTicketId);
      consecutiveNetworkFailures = 0;
      if (status.status === "matched") {
        activeTicketId = null;
        onMatched(status.matchId);
        return;
      }
      if (status.status === "cancelled" || status.status === "expired") {
        activeTicketId = null;
        onQueueEnded(status.status);
        return;
      }
    } catch (error) {
      consecutiveNetworkFailures += 1;
      if (consecutiveNetworkFailures >= 5) {
        activeTicketId = null;
        onQueueEnded("network-error", error);
        return;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}

export async function cancelQueue() {
  const id = activeTicketId;
  activeTicketId = null;
  if (!id) return;
  try {
    await atri.matchmaking.cancel(id);
  } catch (error) {
    reportQueueCleanupFailure(error);
  }
}
~~~

为 adapter 补最小测试：SDK 存在、SDK 缺失、未登录、读取失败、保存失败、重复排队、匹配成功、取消和过期。页面卸载时清理轮询、监听器并调用 \`atri.dispose()\`。`;
}

export function buildAiGameIntegrationPrompt(input: AiGameIntegrationPromptConfig) {
  const config: AiGameIntegrationPromptConfig = {
    ...input,
    projectName: clean(input.projectName, defaultAiGameIntegrationPromptConfig.projectName),
    gameId: normalizeAtriGameId(input.gameId),
    authorName: clean(input.authorName, defaultAiGameIntegrationPromptConfig.authorName),
    techStack: clean(input.techStack, defaultAiGameIntegrationPromptConfig.techStack),
    buildCommand: clean(input.buildCommand, defaultAiGameIntegrationPromptConfig.buildCommand),
    buildOutput: clean(input.buildOutput, defaultAiGameIntegrationPromptConfig.buildOutput),
    externalUrl: input.externalUrl.trim(),
  };
  const manifestExample = JSON.stringify(createManifestExample(config), null, 2);
  const loginRequired =
    config.runtime !== "external" && (config.identity === "required" || config.storage === "sqlite" || config.matchmaking);
  const identityPreference = config.runtime === "external"
    ? "external：关闭 Atri 内置身份，使用项目自己的账号体系"
    : config.matchmaking
      ? "required（static 分支因开启 Atri 匹配自动提升）"
      : config.identity;
  const storagePreference = config.runtime === "external"
    ? "external：关闭 Atri SQLite，使用项目自己的数据层"
    : config.storage;
  const matchmakingPreference = config.runtime === "external"
    ? "关闭 Atri 内置匹配，使用项目自己的匹配服务"
    : config.matchmaking
      ? "开启（仅 static 分支）"
      : "关闭";
  const catalogLoginLabel = config.runtime === "external"
    ? "Atri 内置登录标记不适用；由外部游戏自行处理账号要求"
    : config.runtime === "auto"
      ? `${loginRequired ? "若选择 static：是" : "若选择 static：否；匿名状态需正确降级"}；若选择 external：由外部游戏自行处理`
      : loginRequired
        ? "是"
        : "否；但匿名状态仍需正确降级";

  return `# Atri Games 项目自动适配与 .atri 交付任务

你现在是这个游戏项目的主程、构建工程师、兼容性工程师和交付负责人。请直接检查并修改我提供的完整项目，最终交付一个能在 Atri Games 管理后台直接导入的 \`.atri\` 文件。不要只写教程、伪代码、建议或待办清单；请实际修改文件、执行构建、修复错误、校验包内容并给出产物路径。不要重写玩法、视觉风格或无关业务；以最小必要改动完成平台兼容。

## 0. 我填写的项目资料

- 游戏名称：${config.projectName}
- 建议永久 ID：${config.gameId}
- 作者/团队：${config.authorName}
- 已知技术栈：${config.techStack}
- 预期构建命令：${config.buildCommand}
- 预期构建输出目录：${config.buildOutput}
- 运行方式选择：${config.runtime}
- 玩家身份偏好：${identityPreference}
- 数据存储偏好：${storagePreference}
- Atri 内置匹配：${matchmakingPreference}
- 目录是否应标记“需登录”：${catalogLoginLabel}

这些是目标和线索，不是让你盲目照抄的事实。你必须从仓库文件、依赖、构建日志和实际运行行为中验证它们。若发现冲突，选择能真实运行的实现，并在最终报告中列出“填写值 → 实际值 → 原因”。不得为了通过校验虚构 URL、许可证、隐私政策、支持设备、输入方式、联网能力或 AI 工具。

## 提交内容规范（强制自检）

本项目计划提交到面向中国大陆用户的 Atri Games。你在审计、改造、生成素材和打包前，必须检查游戏玩法、剧情、角色、画面、音频、文案、广告位、跳转链接、商城/支付设计与 AI 生成内容：

1. 禁止政治敏感、煽动对立、歪曲历史、损害国家统一或社会稳定的内容。
2. 禁止色情低俗、涉黄擦边、赌博博彩、代币变现、诈骗、违法犯罪及危险行为诱导。
3. 禁止违规引流、私域拉新、外部交易、虚假宣传、恶意弹窗和与游戏无关的推广链接。
4. 禁止仇恨歧视、侮辱骚扰、侵犯隐私、著作权或人格权，以及其他违反中国法律法规和公序良俗的设计。
5. 这同样适用于 AI 生成的素材、剧情、提示词和后续更新。发现风险时，先移除或重做相关内容；不要把风险内容包装进可选开关、彩蛋、隐藏路径、截图或外链。

最终报告必须增加“内容合规自检”一节，说明已检查的范围、调整项（如有）和仍需项目所有者确认的事项。不要对未核实的法律事实作出保证，也不要添加规避审核或规避平台规则的实现。

## 1. 完成定义

只有同时满足以下条件才算完成：

1. 原游戏核心玩法、画面、音频、输入和已有数据逻辑仍能工作。
2. 已明确选择 \`static\` 或 \`external\`，选择与项目真实架构一致。
3. 根目录存在合法的 \`atri-game.json\`（schemaVersion 2）、真实封面和可选截图。
4. static 项目的生产构建已复制到根目录的 \`game/\`；external 项目已有可公开访问的 HTTPS URL 且包内没有 \`game/\`。
5. 所有 Manifest 路径、布尔值、枚举、隐私信息、平台能力声明均与实际行为一致。
6. 使用平台能力时已完成 SDK 接入、未登录降级、错误状态、超时和页面离开清理；不使用时不增加强依赖。
7. 已执行真实的生产构建、Atri validate 和 Atri pack，并检查压缩包根目录。
8. 已测试桌面与声明支持的移动端，刷新、返回、深链、音频、Wasm/Worker、资源加载和网络错误均没有阻断性问题。
9. 包内没有源码密钥、服务器密码、私钥、平台管理员 Token、门户登录 Token、\`.env\`、数据库文件、日志、缓存或与运行无关的大型文件。
10. 最终回复包含修改清单、架构选择、命令和结果、校验结果、测试结果、剩余限制以及可直接上传的 \`.atri\` 绝对路径。

## 2. 先做只读审计，再开始编辑

先完成以下审计并形成内部检查表，然后连续执行修改，不要在每一步停下来等我确认：

- 识别 package manager 和锁文件，不要混用 npm/pnpm/yarn/bun。
- 阅读 README、AGENTS.md、package.json scripts、构建配置、入口 HTML、路由、Service Worker、环境变量示例和许可证。
- 识别实际引擎/框架及版本：纯 HTML、Vite、React、Vue、Svelte、Phaser、Pixi、Three.js、Unity WebGL、Godot Web、Wasm 或其他。
- 找到开发入口、生产构建命令、真实输出目录、入口 HTML 名称、public/base 路径、静态资源目录。
- 搜索根绝对资源路径（如 \`/assets/...\`）、localhost、硬编码域名、文件系统绝对路径、动态 import、fetch、WebSocket、Worker、AudioWorklet、Wasm、字体和大文件。
- 判断项目是否依赖常驻服务端、SSR、数据库、自建鉴权、房间服务、实时连接、动态上传、服务端环境变量或特殊响应头。
- 梳理所有玩家数据：进度、设置、关卡、货币、排行榜、用户资料、多人状态分别存在哪里；区分设备本地数据、Atri 平台数据和自有后端数据。
- 找到现有登录、保存、联机、匹配、暂停、退出、全屏逻辑，优先做适配层，避免把平台 API 散落到游戏循环中。
- 检查许可证、第三方素材许可、隐私政策、作者、仓库和主页信息。没有事实依据的字段使用明确的待确认说明，并在交付前报告，不要虚构。
- 记录基线：修改前至少跑一次现有测试或构建；若基线已失败，保存原始错误，随后区分“原有问题”和“本次引入问题”。

## 3. 运行方式决策（这是硬约束）

${runtimeRequirement(config)}

### static 的平台边界

- 默认的 \`.atri\` 是 \`ATRIENC1\` 加密认证容器，内部才是 ZIP；不得把扩展名改成 \`.zip\`、也不要尝试用 \`unzip\` / \`tar\` 直接查看。\`@atri/game-kit pack\` 会使用平台公钥自动生成该容器，管理后台会在私有服务端环境中认证、解密并继续校验。
- 这保护的是上传归档而不是已经发布给浏览器的前端资源：绝不把 API 密钥、私钥、密码、服务端源码或数据库文件打进 \`game/\`。需要私有服务端逻辑时使用独立后端或 \`external\`。
- 包根目录是 \`atri-game.json\`、封面、可选 \`screenshots/\` 和 \`game/\`，不能再套一层项目文件夹。
- \`runtime.entry\` 相对于 \`game/\`；例如 \`"entry":"index.html"\` 对应包内 \`game/index.html\`。
- 平台只托管 \`game/\` 中的浏览器文件，不执行包内的 server、Dockerfile、数据库迁移或后台进程。
- static 可以调用 Atri 托管的游戏 SDK、票据、SQLite JSON 存储和 HTTP 匹配队列。
- static 也可以从浏览器调用已经单独部署的自有 API/房间/信令服务；此时 Manifest 要设置 \`ownBackend:true\`、\`networkRequired:true\` 并声明真实 realtime 协议，后端本身不进入包内。

### external 的平台边界

- 先把完整游戏部署到自己的 HTTPS 地址，再打一个只含 Manifest、封面和可选截图的小型 \`.atri\` 登记包。
- \`runtime.url\` 必须是无账号密码的 HTTPS URL，且从公网浏览器可打开；localhost、局域网 IP、HTTP 占位地址都不算交付。
- external 继续使用自己的身份、数据库、匹配和实时后端。Atri 不向外部 URL 传内置游戏票据。
- Manifest 必须将 identity 设为 none、storage 设为 none/game、matchmaking 设为 false/http；不得声称使用平台内置服务。

## 4. 让 static 构建可在任意子路径运行

如果选择 static，按项目实际技术栈完成下面工作：

1. 生产资源使用相对 URL。Vite 通常设置 \`base: "./"\`；Webpack/Vue CLI/其他工具设置等价的 relative public path。禁止让输出 HTML 引用门户根目录的 \`/assets/*\`。
2. 保持入口和异步 chunk 可加载。动态 import、CSS url、字体、Wasm、Worker、AudioWorklet、Unity \`.data/.wasm/.js\` 压缩资源都要从当前构建基址或 \`import.meta.url\` 解析。
3. 路由必须能在 \`/playables/<game-id>/\` 下启动和刷新。优先使用 hash 路由或正确配置 basename/base；不要假定游戏部署在域名根目录。
4. API 地址不可错误地跟随相对静态目录。Atri SDK 使用平台上下文；自有 API/房间服务从受控配置读取 HTTPS/WSS 地址，并设置正确 CORS/CSP。只有入口本身依赖服务端渲染或动态路由时才改为 external。
5. Service Worker 的 scope、缓存键和更新逻辑不能污染其他游戏；若无法安全限定到当前子路径，移除或关闭 Service Worker。
6. 保证入口文档有正确 charset、viewport、根容器和可读的加载/失败状态。初始化异常要显示可操作错误，不能停在空白屏。
7. 保留用户手势后再启动音频/全屏；处理 \`visibilitychange\`、暂停、恢复和销毁，避免切回页面后出现重复计时器或重复网络连接。
8. 不要把源码目录、node_modules、测试快照、地图源文件、编辑器缓存复制到 \`game/\`，除非生产运行确实依赖。
9. 输出目录应可重复生成：先安全清理项目内精确的 \`game/\`，再复制本次 production build；禁止清理工作区根目录或用户目录。
10. 完成后从一个非根路径的静态服务器实际打开产物，用浏览器 Network/Console 确认 HTML、JS、CSS、图片、音频、Wasm 和 Worker 无 404、CORS 或 MIME 错误。

常见技术栈参考（只能在检测到对应项目时应用）：

- Vite/React/Vue/Svelte：设置相对 base，运行项目已有 build，把真实 dist 目录内容复制到 \`game/\`。
- Phaser/Pixi/Three/Canvas：额外检查纹理图集、关卡 JSON、音频、字体和运行时加载器中的前导斜杠。
- Unity WebGL：复制整个 WebGL Build；保留 loader、Build、TemplateData 及压缩文件配对，检查 WebAssembly MIME/压缩加载。
- Godot Web：复制完整 Web 导出；确认 \`.pck/.wasm\` 与入口同版本，并验证跨源隔离需求。确需平台无法提供的特殊 header 时选择 external。
- 纯 HTML：连同所有脚本、样式、资源复制；不要只复制 index.html。

## 5. Atri 平台能力要求

### 5.1 玩家身份

${identityRequirement(config)}

${ticketRules(config)}

### 5.2 SQLite 玩家数据

${storageRequirement(config)}

${storageContract(config)}

### 5.3 匹配队列

${matchmakingRequirement(config)}

### 5.4 SDK 适配层

${sdkRequirement(config)}

## 6. 创建准确的 atri-game.json

下面是按我当前选择生成的起点。你必须用审计得到的真实标题、简介、描述、作者、许可证、引擎、设备、输入、方向、最小视口、标签、AI 工具和隐私行为替换说明性文本。保留字段类型，删除无依据的可选字段。JSON 中不得有注释、尾逗号或占位域名。

~~~json
${manifestExample}
~~~

Manifest 约束逐项核对：

- \`schemaVersion\` 必须为数字 \`2\`。
- \`id\` 是永久标识，3—64 个字符，仅小写字母、数字和单连字符，版本更新时保持不变。
- \`version\` 使用语义化版本，例如 \`1.0.0\`；每次覆盖发布都递增。
- \`title\` 1—80 字符；\`summary\` 10—240 字符；\`description\` 最长 4000 字符。
- \`authors\` 1—20 个，每个 name 1—80 字符；作者 URL、repository、homepage 和 policyUrl 必须是无账号密码的 HTTPS。
- \`license\` 1—80 字符；必须来自项目真实许可证或所有者确认，不能由 AI 擅自选择。
- \`engine.name\` 1—80 字符，engine.version 最长 40，engine.framework 最长 80；按构建配置填写真实值。
- \`runtime.kind\` 只能是 \`static\` 或 \`external\`。static entry 必须是安全的相对 HTML 路径；external URL 必须是 HTTPS。
- 包内路径最长 240 字符，只能使用正斜杠分隔的安全相对路径；禁止绝对路径、反斜杠、空段、\`.\`、\`..\` 和路径穿越。
- \`services.networkRequired\`、\`ownBackend\` 是 JSON 布尔值，不是字符串。realtime 只从 websocket、server-sent-events、webrtc、other 中选。
- identity mode 只从 none、optional、required 中选。
- storage provider 为 none 时 scope 必须为 game；provider 为 sqlite 时 scope 只用 player-game 或 player。
- matchmaking protocol 当前实际 SDK 流程使用 http；schema 也接受 sse/websocket 作为版本化声明，但不要在没有实现时声称使用。
- \`privacy.collectsPersonalData=true\` 时必须提供真实可访问的 HTTPS \`policyUrl\`；dataSummary 10—800 字符并与实际代码一致。
- 封面/截图必须真实存在于包内，路径相对 Manifest，只允许 avif/jpg/jpeg/png/webp；截图最多 8 张。
- devices 只用 desktop/mobile/tablet；inputs 只用 keyboard/mouse/touch/gamepad；最小视口宽度为 240—7680、高度为 240—4320。
- tags 1—10 个、去重、每项不超过 40 字符。
- \`ai.tools\` 最多 20 项、去重、每项 1—80 字符；\`ai.disclosure\` 1—1000 字符并如实记录 AI 参与。不要把“AI 生成”当成许可证或隐私说明。
- Manifest 不允许额外未知字段。需要自定义配置时放在游戏自己的静态配置文件，不要随意扩展 Manifest。

能力组合必须一致：

- 普通匿名 static：identity none/optional；省略 storage；matchmaking false。匿名可玩，不会申请游戏票据或调用平台存档。
- 明确玩家云存档：storage sqlite + player-game/player，会标记“需登录”。
- 强制身份：identity required，会标记“需登录”。
- 内置匹配：matchmaking enabled true，会标记“需登录”，当前使用 HTTP 轮询。
- 完全不用平台数据：storage none + game。
- external：identity none + storage none/game + matchmaking false，使用自己的后端。

## 7. 组织交付目录

static 最终目录必须类似：

~~~text
project-root/
├── atri-game.json
├── cover.webp
├── screenshots/                 # 可选，最多 8 张
│   └── gameplay.webp
└── game/
    ├── index.html               # 或 runtime.entry 指定的 HTML
    ├── assets/
    └── ...全部生产运行文件
~~~

external 最终目录必须类似：

~~~text
project-root/
├── atri-game.json
├── cover.webp
└── screenshots/                 # 可选
~~~

封面必须是项目真实画面或明确提供的合法素材，建议 4:3 或 16:9、足够清晰、无敏感信息。不要拿网页空白截图、开发工具截图或虚构占位图充当最终封面。

请增加一个可重复执行的项目脚本（名称可按现有约定，例如 \`build:atri\`），完成“生产构建 → 精确清理 game/ → 复制输出 → validate → pack”。兼容当前操作系统，避免 shell 方言混用。该脚本只能删除项目内明确的生成目录，并在执行前解析/校验目标路径。

## 8. 校验与打包

优先使用项目实际可用的方式，不要为了命令好看而跳过执行：

~~~bash
# 在 Atri Games 仓库根目录，游戏项目位于相邻目录时
pnpm game-kit validate ../你的游戏/atri-game.json
pnpm game-kit pack ../你的游戏/atri-game.json --out ../你的游戏/${config.gameId}-1.0.0.atri
~~~

如果 \`@atri/game-kit\` 已经可从当前环境使用：

~~~bash
npx @atri/game-kit validate ./atri-game.json
npx @atri/game-kit pack ./atri-game.json --out ./${config.gameId}-1.0.0.atri
~~~

如果 npm 包不可用但有 Atri Games 源码，直接调用源码 CLI：

~~~bash
node ../Atri-Games/packages/game-kit/src/cli.mjs validate ./atri-game.json
node ../Atri-Games/packages/game-kit/src/cli.mjs pack ./atri-game.json --out ./${config.gameId}-1.0.0.atri
~~~

不要手工改扩展名后就声称完成。默认 \`.atri\` 是加密容器，不能也不需要用 \`tar\`、\`unzip\` 列目录；在 pack 前检查源目录，pack 后记录文件大小与 SHA-256，并确认命令输出 \`created\`。如果本项目要导入其他自建 Atri 服务，向部署者取得该服务的 RSA 公钥并在 pack 时传入 \`--public-key /path/to/atri-package-public.pem\`；不要把任何私钥提交到游戏项目。

检查点：

- 源目录第一层直接出现 \`atri-game.json\` 和 Manifest 指向的封面。
- static 源目录有 \`game/\` 且存在 \`game/<runtime.entry>\`；external 没有 \`game/\`。
- 源目录和构建产物中没有外层重复目录、\`../\` 路径、反斜杠路径、符号链接、重复文件名、秘密文件和 node_modules。
- Manifest 的每张截图、入口和封面都能在源目录中找到，大小非零。
- validate 输出 valid；任何 warning 都要逐条处理或在最终报告中解释。
- 默认导入上限是压缩包 512 MiB、解压后 2 GiB、最多 20,000 个文件。部署者可以调整上限，但交付应尽量明显低于默认值；Unity/Wasm 大包要移除调试符号、未使用资源、重复压缩件和 sourcemap。

## 9. 浏览器与功能验收

至少完成并记录以下测试：

1. 原项目测试/类型检查/生产构建通过；如果项目没有测试，明确写“未配置自动化测试”并做手动冒烟。
2. 用静态服务器在嵌套路径打开 \`game/\`，首屏无空白，Console 无未处理异常，Network 无关键 404。
3. 刷新入口和游戏内路由、浏览器后退/前进、重复进入退出均正常。
4. 键盘、鼠标、触摸和 gamepad 只声明实际验证过的输入；移动端测试 viewport、方向切换和安全区。
5. 音频解锁、全屏、暂停/恢复、标签页隐藏、页面销毁后无重复循环或后台声音。
6. 缓存清空和弱网下仍有明确加载状态；断网、平台 API 失败、存档配额和票据过期有可理解提示。
7. 未登录启动符合有效能力组合：\`identity.required\`、显式 \`sqlite + player/player-game\`、或 \`matchmaking.enabled=true\` 任一启用时，启动前要求登录；其余 optional/none static 游戏允许匿名玩法。
8. 登录后玩家 A 的数据与玩家 B 隔离；不同 gameId 的数据互不读取；存档 schema 迁移和损坏回退正常。
9. 若开启匹配，用两个独立玩家会话验证 waiting → matched，mode/region 不同不会误配，取消和约两分钟过期可收敛。
10. external URL 从无登录缓存的浏览器可通过 HTTPS 打开，CORS/CSP/cookie/SameSite/WebSocket 配置与其自有后端匹配。
11. 对最终 \`.atri\` 再执行一次 validate，并记录文件大小、SHA-256（如果环境提供）和绝对路径。

## 10. 常见失败必须主动修复

- \`missing static entry: game/index.html\`：构建产物没有复制到 game/，或 runtime.entry 与真实入口不一致。
- JS/CSS/图片 404：输出仍使用根绝对路径；修正 base/publicPath 并重新构建，不能手改一次性 dist 后结束。
- Wasm/Worker 加载失败：修正 URL 解析、MIME/压缩配对或跨源要求；确需特殊服务器能力时切换 external。
- 页面本地正常、平台空白：用嵌套路径重现，检查路由 basename、动态 chunk、CSP、Service Worker 和大小写路径。
- SDK 在本地 404：adapter 必须捕获 SDK 缺失并继续 standalone；平台环境再验证票据和 API。
- 401/403：确认玩家登录、Manifest scope、SDK 上下文和游戏 slug；不要改用门户 Token 或把管理员凭据写进前端。
- 存档 404：把“没有记录”当作默认新存档；不要当成整个游戏启动失败。
- 配额错误：压缩数据结构、拆分稳定键、节流写入并提示玩家；不要无限重试。
- external 校验失败：确认 URL 为 HTTPS，且显式关闭内置 identity/storage/matchmaking。
- 打包后后台拒绝：重新用 game-kit 校验，检查根目录层级、路径穿越、重复项、符号链接、文件数量和包大小。

## 11. 最终回复格式

全部执行后，严格按这个结构回复我：

1. **结果**：完成/部分完成；\`.atri\` 绝对路径、大小、SHA-256。
2. **架构判断**：static/external；列出判断证据；若与我填写的偏好不同，说明原因。
3. **平台能力**：identity、storage、matchmaking 的最终值；是否显示“需登录”；未登录行为。
4. **修改文件**：逐文件说明改动目的，特别标出构建 base、adapter、Manifest 和打包脚本。
5. **执行命令与结果**：基线、测试、类型检查、build、validate、pack、归档检查。
6. **Manifest 摘要**：id、version、entry/URL、封面、设备、输入、网络、隐私和 AI disclosure。
7. **验收证据**：桌面/移动、嵌套路径、资源、存档、匹配或 external HTTPS 的实际测试结果。
8. **剩余事项**：只列确实需要人工提供的事实或外部部署条件；不要把已能自动完成的工作推回给我。

现在开始：先读取项目和约束，记录基线，然后持续修改、构建、验证和打包，直到得到可直接导入 Atri Games 的最终 \`.atri\` 文件。`;
}

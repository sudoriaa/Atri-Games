# Atri Games

Atri Games 是一个面向 AI 辅助游戏创作者的开放游戏目录与发行入口。平台负责发现、展示、账号、收藏、启动统计和内容治理；每个游戏仍是一个可以独立访问、拥有任意技术栈与后端能力的 Web App。

当前仓库已经包含一套可运行的 MVP：

- 玩家站：精选首页、目录搜索与筛选、详情、独立启动、注册登录、收藏和个人资料。
- 管理后台：独立登录、数据仪表盘、游戏审核与 CRUD、分类管理、用户角色和停用状态。
- Go API：JWT 身份、RBAC、SQLite WAL、初始化数据、审计日志、限流和安全响应头。
- 通用接入层：`@atri/game-kit` 零依赖清单/打包 CLI、`@atri/game-sdk` 可选浏览器桥接，以及后台 `.atri` 游戏包导入。
- 六个首发示例：本地封面和独立 Canvas 试玩页，用于验证“目录 → 详情 → 游戏网页”的完整流程。
- 生产部署：多阶段 Docker 构建、Caddy 反向代理、持久化数据库、健康检查和 CI 冒烟测试。

## 产品边界

基础收录只要求游戏提供 Manifest 与稳定的 HTTPS 启动地址；静态构建也可以直接上传 `.atri` 包。SDK 是可选的，游戏不需要为了接入而改写引擎或后端。声明 `identity:required`、玩家级 SQLite 存储或开启内置匹配后，平台会在目录显示“需登录”并在启动前检查账号。

游戏可以使用纯 HTML/JavaScript、React、Vue、Phaser、Three.js、Godot Web、Unity Web，也可以连接自己的 API、数据库、WebSocket、WebRTC、Webhook 或第三方服务。`.atri` 静态游戏还可以按需打开平台身份、隔离 SQLite 数据和内置匹配；外部 URL 游戏继续使用自己的账号与后端。

```text
开发者仓库 ──构建/部署──> 独立游戏 Web App
                              ↑
游戏 Manifest ──> Catalog ────┼── 玩家从详情页直接打开
                              │
                              └── 可选：Identity / API / Webhook
```

## 技术架构

```text
apps/
├── web/       React + Vite 玩家站
├── admin/     React + Vite 管理后台
└── api/       Go 模块化单体 API
packages/
├── shared/    前端共享类型与 API Client
├── game-kit/  清单校验与 .atri 打包 CLI
└── game-sdk/ 可选零依赖浏览器生命周期桥接
catalog/       社区游戏清单
schemas/       Manifest JSON Schema
infra/         Caddy 与生产环境配置
```

首版使用 Go 模块化单体与 SQLite WAL，适合单机 8 核 8 GB 的部署目标，并保留以后迁移 PostgreSQL 的清晰数据边界。玩家站和管理后台独立构建、独立路由，但共享 API 契约。

更完整的设计见：

- [架构说明](docs/ARCHITECTURE.md)
- [实施路线图](ROADMAP.md)
- [平台 API 与 Webhook 草案](docs/PLATFORM_INTEGRATION.md)
- [第三方游戏接入与 `.atri` 打包教程](docs/GAME_INTEGRATION.md)
- [第三方游戏贡献指南](CONTRIBUTING.md)

## 本地运行

环境要求：

- Node.js 22.12+
- pnpm 10.32.1
- Go 1.26.5+

安装并验证：

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm check
pnpm api:test
```

分别启动三个进程：

```bash
pnpm dev:api
pnpm dev:web
pnpm dev:admin
```

访问地址：

- 玩家站：<http://localhost:5173>
- 管理后台：<http://localhost:5174/admin/>
- API 健康检查：<http://localhost:8080/api/v1/health>

本地种子管理员：

```text
邮箱：admin@atri.games
密码：AtriAdmin123!
```

这些默认值只用于本地开发。公开部署前必须设置新的 `ATRI_JWT_SECRET` 和管理员密码；完整变量见 [.env.example](.env.example)。

本地验证文件级删除时，把 `ATRI_ASSET_ROOT` 设为 `.env.example` 中的 `../../apps/web/public`（相对于 `apps/api` 的运行目录）。此配置让 API 与 Vite 使用同一份 `covers`、`demos` 文件；后台删除本地游戏时也会直接移除仓库中的对应文件，测试前请保留工作区副本或在测试后用 Git 恢复。

## 生产部署

复制生产模板并替换域名与两个密钥：

```bash
cp infra/production.env.example .env
docker compose up --detach --build
```

Caddy 会托管两个 SPA、代理 `/api/*`、维护 HTTPS 证书并发送安全头。SQLite 数据、后台管理的封面与本地试玩文件、Caddy 证书及配置分别保存在具名卷中。静态游戏使用独立的游戏 Origin（`ATRI_GAME_SITE_ADDRESS`），不会和门户的登录存储混用。生产容器以非 root 用户运行，并带只读文件系统、能力裁剪、资源限制和健康检查。

API 镜像的 `/assets` 内置仓库中的 `apps/web/public/covers`、`apps/web/public/demos` 与初始化标记，并预留 `avatars/`、`playables/` 目录。首次挂载新的 `ATRI_ASSET_VOLUME` 时，Docker 会把这些初始内容写入空卷；Caddy 所在的 Web 服务要等 API 健康后才启动，因此不会先挂载空卷。API 以读写方式挂载 `/assets`，Caddy 以只读方式挂载同一目录。门户 Origin 只把 `/playables/*` 重定向到游戏 Origin，游戏 Origin 才读取静态包；资源被后台删除后不会从 Web 镜像或 SPA 回退页面重新出现，后续常规镜像升级也不会重新填充已初始化的卷。

### MinIO 对象存储镜像

生产 Compose 默认启动一个只在内部网络可见的 MinIO 实例。API 会在启动时创建并校验 `ATRI_OBJECT_STORAGE_BUCKET`，随后把 `/assets` 中的 `avatars/`、`covers/`、`demos/` 与 `playables/` 完整镜像到对象存储；导入 `.atri`、上传/替换封面或头像、切换外链媒体和彻底删除游戏时，会立即同步对应的对象前缀。对象使用 `assets/` 前缀、SHA-256 元数据和 MIME/缓存头，重复同步会跳过内容未变化的文件并移除已不在本地资源卷中的旧对象。

本地资源卷仍是发布和删除事务的权威来源，Caddy 继续从该卷提供静态游戏文件。因此 MinIO 短暂重启不会导致已发布游戏停止访问；API 会记录并在下一次启动时完成全量对账。使用内置 MinIO 时，在生产 `.env` 中设置唯一的 `ATRI_MINIO_ROOT_USER` 与 `ATRI_MINIO_ROOT_PASSWORD`。若部署在已有的 S3/MinIO 服务上，将 `ATRI_OBJECT_STORAGE_PROVIDER=minio`，并设置 endpoint、访问密钥、bucket、region 与 TLS 选项即可。

加密 `.atri` 导入还需要在生产环境设置 `ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64`：它是与 `@atri/game-kit` 使用的 RSA 公钥配对的 PEM 私钥的 Base64 编码。私钥只放在部署机的私密环境文件，绝不提交到仓库或游戏项目。自建平台应生成自己的密钥对，把公钥通过 `atri-game pack --public-key ./platform-public.pem` 提供给开发者；默认打包公钥只对应本平台实例。

`docker compose down` 默认保留这些数据。只有明确需要同时清空数据库、托管资源和 Caddy 状态时才使用 `docker compose down --volumes`；下次启动会重新创建资源卷并写入内置示例文件。

管理后台中的“下架”只把游戏状态改为 `hidden`，公开页面、详情和启动接口随即隐藏该游戏，数据库记录、收藏、启动历史及文件保持不变。将状态重新改为“已发布”即可恢复展示。

“彻底删除”会删除游戏记录，并由外键级联清理收藏、启动历史和游戏包收据。API 同时清理资源卷中该游戏独占的 `/covers/...` 封面、`/demos/<bundle>/...` 试玩目录和 `/playables/<id>/...` 托管构建；仍被其他游戏引用的共享资源会保留。外部 HTTPS 地址及其服务不属于本地资源卷，因此不受文件清理影响。

仓库中的 `/demos/arcade/` 是六个种子游戏共用的公共运行时，不归属于任一游戏。每个种子游戏使用 `/demos/<slug>/index.html` 作为独占的无脚本启动包装页，再跳转到公共运行时；包装目录按 slug 归属，因此彻底删除单个游戏后，其原启动 URL 会立即返回 404，同时不会破坏其他游戏仍在使用的公共运行时。

## 游戏接入

### 两种运行方式

| 方式 | 适合项目 | 平台做什么 |
| --- | --- | --- |
| `external` | 任意已经部署的前后端项目、SSR、WebSocket、Python/Go/Java/Node 服务 | 保存清单与封面，点击后直接跳转 HTTPS 地址 |
| `static` | Unity/Godot Web、Wasm、Canvas、React/Vue、纯 HTML 等浏览器构建产物 | 校验并托管 `.atri` 包，在隔离的游戏 Origin 提供静态文件 |

平台不会执行上传包里的服务端代码；需要数据库、长连接或自定义运行时的项目只需选择 `external`，无需适配器。

从已有项目到后台导入的逐步命令、React/Vite、Unity Web、Godot Web、外部后端示例和常见错误处理，见[第三方游戏接入与 `.atri` 打包教程](docs/GAME_INTEGRATION.md)。

### 最短接入路径

```bash
npx @atri/game-kit init my-game
cd my-game
# static: 把构建产物放入 game/，准备 cover.webp
# external: 把 runtime.kind 改为 external 并填写 HTTPS URL
npx @atri/game-kit validate
npx @atri/game-kit pack --out my-game.atri
```

然后在管理后台点击“导入游戏包”，选择分类和状态即可。`game-kit pack` 默认输出经过加密认证的 `.atri` 容器；导入器会先在服务端解密，再校验路径穿越、符号链接、文件数、压缩包大小、Manifest、封面和入口页。勾选覆盖可发布同 ID 的新版本。完整字段见 [Manifest Schema](schemas/game-manifest.schema.json) 和 [示例清单](examples/game-manifest.json)。

旧版明文 ZIP `.atri` 仍可导入以兼容历史包，但改后缀即可查看内容，不能用于保护上传包。新包请通过 `@atri/game-kit pack` 生成；浏览器必须下载静态资源才能运行，因此归档加密不应被当作已发布前端代码或密钥的保护手段。任意语言的后端项目只需提交 `external` 清单。

### 可选 SDK

`@atri/game-sdk` 没有运行时依赖，提供 `ready`、`pause/resume`、进度/成绩事件、全屏和退出等浏览器桥接；声明平台服务后还可以使用统一游戏票据、SQLite 数据和匹配队列。没有宿主时这些方法会退化为本地行为或空操作，游戏主循环不依赖平台：

```js
import { createAtriGame } from "/sdk/atri-game-sdk.js";
const atri = createAtriGame();
atri.on("pause", () => game.pause());
atri.on("resume", () => game.resume());
atri.ready({ build: "1.0.0" });

await atri.storage.set("progress", { level: 3 });
const queue = await atri.matchmaking.join({ mode: "ranked", region: "asia" });
```

SDK 只负责可选生命周期消息和平台 API 薄封装，不接管渲染、网络或引擎；因此不同语言和框架仍使用各自的工程方式。完整的 Manifest 字段、票据生命周期和 HTTP 路径见 [第三方游戏接入教程](docs/GAME_INTEGRATION.md)。

### 资源优先原则

- 玩家点击开始后门户页面直接卸载，浏览器把 CPU、GPU、内存和网络优先给游戏。
- 目录路由按需懒加载，封面使用异步/懒加载，避免首页一次载入详情和游戏代码。
- 静态包放在独立 Origin，门户登录令牌与游戏存储隔离。
- API 只流式落盘上传，不把整个游戏包读入内存；上限由 `ATRI_GAME_PACKAGE_MAX_*` 配置。

旧版收录仍可按下列方式登记：

1. 独立部署游戏及所需后端，取得公开 HTTPS 地址。
2. 复制示例清单，填写启动地址、联网要求、兼容性、隐私和作品信息。
3. 按 Schema 校验，并在桌面与移动浏览器验证直接启动。
4. 在 `catalog/games/<game-id>/` 提交收录 PR。

Webhook URL 和密钥不进入公开 Manifest；它们由后续开发者控制台登记和轮换。

## 常用命令

```bash
pnpm typecheck    # 全部 TypeScript 类型检查
pnpm test         # 前端与共享包测试
pnpm build        # 两个前端的生产构建
pnpm check        # 类型检查 + 测试 + 构建
pnpm api:test     # Go API 测试
```

## License

[MIT](LICENSE)

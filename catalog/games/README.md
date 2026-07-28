# Game Catalog

每个 Git Catalog 收录使用一个独立目录：

```text
catalog/games/<game-id>/
├── manifest.json
├── cover.webp
└── screenshots/
```

提交前从 [`examples/game-manifest.json`](../../examples/game-manifest.json) 复制清单，并按 [`schemas/game-manifest.schema.json`](../../schemas/game-manifest.schema.json) 校验。

本目录只保存游戏元数据和经过压缩的展示媒体。生产后台还支持同一 Manifest 规范的 `.atri` 上传：静态构建放在包内的 `game/`，带自有后端的项目使用 `runtime.kind=external`。游戏前端、服务端代码、构建产物、大型资源和引擎缓存不需要进入这个 Git 目录。

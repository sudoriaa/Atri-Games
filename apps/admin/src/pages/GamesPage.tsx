import { gameRequiresLogin, gameUsesMatchmaking, gameUsesPlatformStorage, type Category, type Game, type GameInput, type GameStatus } from "@atri/shared";
import {
  CheckCircle2,
  ChevronDown,
  CircleAlert,
  Edit3,
  ExternalLink,
  EyeOff,
  ImagePlus,
  Link2,
  PackagePlus,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { AdminError, AdminLoading } from "../components/AdminState";
import { apiErrorMessage } from "../lib/api-error-message";
import { gameDeleteConfirmations, parseTagInput } from "../lib/admin-utils";
import { useAdminAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

const statusLabels: Record<GameStatus, string> = { draft: "草稿", review: "待审核", published: "已发布", hidden: "已下架" };
const emptyInput: GameInput = { slug: "", title: "", summary: "", description: "", authorName: "", coverUrl: "", launchUrl: "/demos/arcade/index.html", launchOpenIn: "same-tab", repositoryUrl: "", engine: "React", version: "0.1.0", status: "draft", categoryId: "", featured: false, networkRequired: false, ownBackend: false, requiresLogin: false, usesPlatformStorage: false, matchmakingEnabled: false, tags: [] };
const maxCoverBytes = 10 * 1024 * 1024;
const coverExtensions = new Set(["avif", "jpg", "jpeg", "png", "webp"]);
type GameAction = "approve" | "unpublish" | "delete";
type Notice = { tone: "success" | "error"; text: string };

export function GamesPage() {
  const { api } = useAdminAuth();
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [editor, setEditor] = useState<Game | "new" | null>(null);
  const [importerOpen, setImporterOpen] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [pendingAction, setPendingAction] = useState<{ gameId: string; action: GameAction } | null>(null);
  const filter = useMemo(() => ({ query, status, pageSize: 100 }), [query, status]);
  const games = useAsync(() => api.adminGames(filter), [api, filter]);
  const categories = useAsync(() => api.adminCategories(), [api]);
  const anyActionPending = pendingAction !== null;
  const categoriesReady = (categories.data?.length ?? 0) > 0;

  const unpublish = async (game: Game) => {
    if (!window.confirm(`确认下架“${game.title}”？下架后主页不再显示，数据库数据和本地文件都会保留。`)) return;
    setPendingAction({ gameId: game.id, action: "unpublish" });
    setNotice(null);
    try {
      await api.unpublishGame(game.id);
      setNotice({ tone: "success", text: `${game.title} 已下架，数据和本地文件保持不变` });
      games.reload();
    } catch (error) {
      setNotice({ tone: "error", text: apiErrorMessage(error) });
    } finally {
      setPendingAction(null);
    }
  };

  const approve = async (game: Game) => {
    if (!window.confirm(`确认通过审核并上架“${game.title}”？通过后游戏将公开出现在用户端首页与发现页。`)) return;
    setPendingAction({ gameId: game.id, action: "approve" });
    setNotice(null);
    try {
      await api.approveGame(game.id);
      setNotice({ tone: "success", text: `${game.title} 已通过审核并上架` });
      games.reload();
    } catch (error) {
      setNotice({ tone: "error", text: apiErrorMessage(error) });
    } finally {
      setPendingAction(null);
    }
  };

  const remove = async (game: Game) => {
    const confirmations = gameDeleteConfirmations(game.title);
    if (!window.confirm(confirmations[0]) || !window.confirm(confirmations[1])) return;
    setPendingAction({ gameId: game.id, action: "delete" });
    setNotice(null);
    try {
      await api.deleteGame(game.id);
      setNotice({ tone: "success", text: `${game.title} 的全部关联数据与本地文件已删除` });
      games.reload();
    } catch (error) {
      setNotice({ tone: "error", text: apiErrorMessage(error) });
    } finally {
      setPendingAction(null);
    }
  };

  return (
    <div className="admin-page">
      <header className="admin-page-header admin-page-header--actions"><div><p className="admin-kicker">CONTENT / GAMES</p><h1>游戏管理</h1><p>上传标准游戏包，或手工登记独立部署的游戏；下架与彻底删除保持各自独立。</p></div><div className="header-action-group"><button className="secondary-action secondary-action--compact" onClick={() => { setEditor(null); setImporterOpen(true); }} disabled={anyActionPending || !categoriesReady}><PackagePlus /> 导入游戏包</button><button className="primary-action primary-action--compact" onClick={() => { setImporterOpen(false); setEditor("new"); }} disabled={anyActionPending || !categoriesReady}><Plus /> 手工新建</button></div></header>
      <div className="admin-toolbar"><label className="admin-search"><Search /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索标题、作者或标签" /></label><label className="select-shell"><select value={status} onChange={(event) => setStatus(event.target.value)}><option value="">全部状态</option>{Object.entries(statusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select><ChevronDown /></label><span>{games.data?.total ?? 0} 条记录</span></div>
      {notice && <div className={`admin-notice admin-notice--${notice.tone}`} role={notice.tone === "error" ? "alert" : "status"} aria-live="polite">{notice.tone === "error" ? <CircleAlert /> : <CheckCircle2 />}{notice.text}<button type="button" onClick={() => setNotice(null)} aria-label="关闭提示"><X /></button></div>}
      {categories.error && <AdminError message={categories.error} retry={categories.reload} />}
      {games.loading && <AdminLoading />}
      {games.error && <AdminError message={games.error} retry={games.reload} />}
      {games.data && <div className="admin-table-wrap"><table className="admin-table games-table"><thead><tr><th>游戏</th><th>分类 / 引擎</th><th>状态</th><th>启动 / 收藏</th><th>更新</th><th><span className="sr-only">操作</span></th></tr></thead><tbody>{games.data.items.map((game) => {
        const approving = pendingAction?.gameId === game.id && pendingAction.action === "approve";
        const unpublishing = pendingAction?.gameId === game.id && pendingAction.action === "unpublish";
        const deleting = pendingAction?.gameId === game.id && pendingAction.action === "delete";
        const busy = approving || unpublishing || deleting;
        return <tr key={game.id} className={busy ? "is-updating" : ""} aria-busy={busy || undefined}><td><div className="game-cell"><img src={game.coverUrl} alt="" loading="lazy" decoding="async" /><div><b>{game.title}</b><small>{game.authorName} · v{game.version}</small>{game.ownerName && game.ownerName !== game.authorName && <small className="owner-name">由 {game.ownerName} 上传</small>}<span className="game-access-badges">{gameRequiresLogin(game) && <small className="access-badge">需登录</small>}{gameUsesPlatformStorage(game) && <small className="access-badge access-badge--muted">内置数据</small>}{gameUsesMatchmaking(game) && <small className="access-badge access-badge--muted">匹配</small>}</span></div></div></td><td><b>{game.categoryName}</b><small>{game.engine}{game.networkRequired ? " · 在线" : ""}</small></td><td><span className={`status-pill status-pill--${game.status}`}>{statusLabels[game.status]}</span>{game.featured && <small className="featured-text">精选</small>}</td><td><b>{game.playCount.toLocaleString()}</b><small>{game.favoriteCount.toLocaleString()} 收藏</small></td><td><time>{new Date(game.updatedAt).toLocaleDateString("zh-CN")}</time></td><td><div className="row-actions row-actions--games"><a href={game.launchUrl} target="_blank" rel="noreferrer" title={`打开 ${game.title}`} aria-label={`打开 ${game.title}`}><ExternalLink /></a><button type="button" onClick={() => setEditor(game)} title={`编辑 ${game.title}`} aria-label={`编辑 ${game.title}`} disabled={anyActionPending}><Edit3 /></button>{game.status === "review" && <button type="button" className="lifecycle-action approve-action" onClick={() => approve(game)} title="通过审核，立即公开上架" aria-label={`通过审核并上架 ${game.title}`} disabled={anyActionPending}><CheckCircle2 /><span>{approving ? "审核中…" : "通过审核"}</span></button>}{game.status === "published" && <button type="button" className="lifecycle-action unpublish-action" onClick={() => unpublish(game)} title="只从主页隐藏，保留全部数据和文件" aria-label={`下架 ${game.title}，保留全部数据和文件`} disabled={anyActionPending}><EyeOff /><span>{unpublishing ? "下架中…" : "下架"}</span></button>}<button type="button" className="lifecycle-action danger destructive-action" onClick={() => remove(game)} title="永久删除全部关联数据与对应本地文件" aria-label={`彻底删除 ${game.title}、关联数据与本地文件`} disabled={anyActionPending}><Trash2 /><span>{deleting ? "删除中…" : "删除"}</span></button></div></td></tr>;
      })}</tbody></table>{games.data.items.length === 0 && <div className="panel-empty">没有匹配的游戏</div>}</div>}
      {editor && <GameEditor game={editor === "new" ? null : editor} categories={categories.data ?? []} close={() => setEditor(null)} saved={(title) => { setEditor(null); setNotice({ tone: "success", text: `${title} 已保存` }); games.reload(); }} />}
      {importerOpen && <GamePackageImporter categories={categories.data ?? []} close={() => setImporterOpen(false)} imported={(game) => { setImporterOpen(false); setNotice({ tone: "success", text: `${game.title} 的游戏包已接入，可在列表中继续管理` }); games.reload(); }} />}
    </div>
  );
}

function GameEditor({ game, categories, close, saved }: { game: Game | null; categories: Category[]; close: () => void; saved: (title: string) => void }) {
  const { api } = useAdminAuth();
  const [form, setForm] = useState<GameInput>(() => game ? { slug: game.slug, title: game.title, summary: game.summary, description: game.description, authorName: game.authorName, coverUrl: game.coverUrl, launchUrl: game.launchUrl, launchOpenIn: game.launchOpenIn ?? "same-tab", repositoryUrl: game.repositoryUrl, engine: game.engine, version: game.version, status: game.status, categoryId: game.categoryId, featured: game.featured, networkRequired: game.networkRequired, ownBackend: game.ownBackend, requiresLogin: game.requiresLogin ?? game.loginRequired ?? false, usesPlatformStorage: game.usesPlatformStorage ?? false, matchmakingEnabled: game.matchmakingEnabled ?? false, tags: game.tags } : { ...emptyInput, categoryId: categories[0]?.id ?? "" });
  const [tagText, setTagText] = useState(form.tags.join(", "));
  const [coverMode, setCoverMode] = useState<"upload" | "url">("upload");
  const [coverFile, setCoverFile] = useState<File | null>(null);
  const [coverPreview, setCoverPreview] = useState(form.coverUrl);
  const [draggingCover, setDraggingCover] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const set = <K extends keyof GameInput>(key: K, value: GameInput[K]) => setForm((current) => ({ ...current, [key]: value }));

  useEffect(() => {
    if (!coverFile) {
      setCoverPreview(form.coverUrl);
      return;
    }
    const objectURL = URL.createObjectURL(coverFile);
    setCoverPreview(objectURL);
    return () => URL.revokeObjectURL(objectURL);
  }, [coverFile, form.coverUrl]);

  const selectCover = (file: File | null) => {
    if (busy || !file) return;
    const extension = file.name.split(".").pop()?.toLowerCase() ?? "";
    if (!coverExtensions.has(extension)) {
      setError("封面仅支持 AVIF、JPG、PNG 或 WebP 图片");
      return;
    }
    if (file.size > maxCoverBytes) {
      setError("封面图片不能超过 10 MB");
      return;
    }
    setCoverFile(file);
    setCoverMode("upload");
    setError("");
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!coverFile && !form.coverUrl.trim()) {
      setError("请选择一张封面图片，或切换到“已有 URL”填写封面地址");
      return;
    }
    setBusy(true);
    setError("");
    const input = { ...form, tags: parseTagInput(tagText) };
    try { if (game) await api.updateGame(game.id, input, coverFile ?? undefined); else await api.createGame(input, coverFile ?? undefined); saved(input.title); }
    catch (caught) { setError(apiErrorMessage(caught)); setBusy(false); }
  };

  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (!busy && event.target === event.currentTarget) close(); }}><section className="admin-modal" role="dialog" aria-modal="true" aria-labelledby="game-editor-title"><header><div><p className="admin-kicker">{game ? "EDIT RECORD" : "NEW RECORD"}</p><h2 id="game-editor-title">{game ? `编辑 ${game.title}` : "收录新游戏"}</h2></div><button type="button" onClick={close} disabled={busy} aria-label="关闭"><X /></button></header><form onSubmit={submit}>
    <div className="form-grid"><label><span>标题 *</span><input required value={form.title} onChange={(event) => set("title", event.target.value)} /></label><label><span>永久标识 *</span><input required pattern="[a-z0-9-]+" value={form.slug} onChange={(event) => set("slug", event.target.value.toLowerCase())} placeholder="my-game" /></label><label><span>作者 *</span><input required value={form.authorName} onChange={(event) => set("authorName", event.target.value)} /></label><label><span>版本 *</span><input required value={form.version} onChange={(event) => set("version", event.target.value)} /></label><label><span>分类 *</span><select required value={form.categoryId} onChange={(event) => set("categoryId", event.target.value)}><option value="">请选择</option>{categories.map((category) => <option value={category.id} key={category.id}>{category.name}</option>)}</select></label><label><span>引擎 / 框架 *</span><input required value={form.engine} onChange={(event) => set("engine", event.target.value)} /></label><label className="form-span-2"><span>摘要 *</span><input required minLength={10} maxLength={240} value={form.summary} onChange={(event) => set("summary", event.target.value)} /></label><label className="form-span-2"><span>详细说明 *</span><textarea required rows={5} maxLength={4000} value={form.description} onChange={(event) => set("description", event.target.value)} /></label><label><span>启动 URL *</span><input required value={form.launchUrl} onChange={(event) => set("launchUrl", event.target.value)} placeholder="https://... 或 /demos/..." /></label><label><span>打开方式 *</span><select value={form.launchOpenIn} onChange={(event) => set("launchOpenIn", event.target.value as GameInput["launchOpenIn"])}><option value="same-tab">当前标签页</option><option value="new-tab">新标签页</option></select></label>
      <fieldset className="cover-editor form-span-2">
        <legend>游戏封面 *</legend>
        <div className="cover-editor__modes" aria-label="封面来源">
          <button type="button" className={coverMode === "upload" ? "is-active" : ""} aria-pressed={coverMode === "upload"} onClick={() => setCoverMode("upload")} disabled={busy}><ImagePlus /> 上传图片</button>
          <button type="button" className={coverMode === "url" ? "is-active" : ""} aria-pressed={coverMode === "url"} onClick={() => { setCoverMode("url"); setCoverFile(null); }} disabled={busy}><Link2 /> 已有 URL</button>
        </div>
        <div className="cover-editor__content">
          <div className={`cover-preview${coverPreview ? " has-image" : ""}`}>
            {coverPreview ? <img src={coverPreview} alt="游戏封面预览" /> : <><ImagePlus /><span>选择图片后在这里预览</span></>}
          </div>
          {coverMode === "upload" ? <label
            className={`cover-dropzone${draggingCover ? " is-dragging" : ""}`}
            onDragEnter={(event) => { event.preventDefault(); if (!busy) setDraggingCover(true); }}
            onDragOver={(event) => { event.preventDefault(); event.dataTransfer.dropEffect = busy ? "none" : "copy"; }}
            onDragLeave={() => setDraggingCover(false)}
            onDrop={(event) => { event.preventDefault(); setDraggingCover(false); if (!busy) selectCover(event.dataTransfer.files?.[0] ?? null); }}
          >
            <ImagePlus />
            <span><b>{coverFile ? "更换封面图片" : game ? "选择新封面（可选）" : "选择或拖入封面图片"}</b><small>AVIF / JPG / PNG / WebP，最大 10 MB{game && !coverFile ? "；不选择则保留当前封面" : ""}</small></span>
            <input type="file" accept=".avif,.jpg,.jpeg,.png,.webp,image/avif,image/jpeg,image/png,image/webp" onChange={(event) => { selectCover(event.target.files?.[0] ?? null); event.currentTarget.value = ""; }} disabled={busy} />
          </label> : <label className="cover-url-field"><span>封面 URL</span><input required value={form.coverUrl} onChange={(event) => set("coverUrl", event.target.value)} placeholder="https://... 或 /covers/..." disabled={busy} /><small>仅用于已经托管在其他位置的封面；通常直接上传图片更方便。</small></label>}
        </div>
        {coverFile && <div className="cover-file-meta"><span><b>{coverFile.name}</b> · {(coverFile.size / 1024 / 1024).toFixed(2)} MB</span><button type="button" onClick={() => setCoverFile(null)} disabled={busy}><X /> 撤销本次选择</button></div>}
      </fieldset>
      <label><span>代码仓库</span><input value={form.repositoryUrl} onChange={(event) => set("repositoryUrl", event.target.value)} /></label><label className="form-span-2"><span>标签（逗号分隔）</span><input value={tagText} onChange={(event) => setTagText(event.target.value)} /></label><label><span>发布状态 *</span><select value={form.status} onChange={(event) => set("status", event.target.value as GameStatus)}>{Object.entries(statusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select><small className="field-help">已下架游戏改为“已发布”后会重新显示在主页。</small></label></div>
    <div className="check-row"><label><input type="checkbox" checked={form.featured} onChange={(event) => set("featured", event.target.checked)} /> 编辑精选</label><label><input type="checkbox" checked={form.networkRequired} onChange={(event) => set("networkRequired", event.target.checked)} /> 需要联网</label><label><input type="checkbox" checked={form.ownBackend} onChange={(event) => set("ownBackend", event.target.checked)} /> 独立后端</label></div><p className="field-help">统一票据、内置数据和匹配能力由 `.atri` 的 <code>services</code> 声明；覆盖导入新版本时会自动同步。</p>{error && <div className="admin-form-error" role="alert">{error}</div>}<footer><button type="button" className="secondary-action" onClick={close} disabled={busy}>取消</button><button type="submit" className="primary-action primary-action--compact" disabled={busy}>{busy ? "保存中…" : "保存游戏"}</button></footer>
  </form></section></div>;
}

function GamePackageImporter({ categories, close, imported }: { categories: Category[]; close: () => void; imported: (game: Game) => void }) {
  const { api } = useAdminAuth();
  const [file, setFile] = useState<File | null>(null);
  const [categoryId, setCategoryId] = useState(categories[0]?.id ?? "");
  const [status, setStatus] = useState<GameStatus>("review");
  const [replace, setReplace] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!file || !categoryId) {
      setError("请选择 .atri 游戏包和所属分类");
      return;
    }
    setBusy(true);
    setError("");
    try {
      imported(await api.importGamePackage(file, { categoryId, status, replace }));
    } catch (caught) {
      setError(apiErrorMessage(caught));
      setBusy(false);
    }
  };

  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (!busy && event.target === event.currentTarget) close(); }}><section className="admin-modal package-import-modal" role="dialog" aria-modal="true" aria-labelledby="package-import-title"><header><div><p className="admin-kicker">UNIVERSAL GAME PACKAGE</p><h2 id="package-import-title">导入 .atri 游戏包</h2></div><button type="button" onClick={close} disabled={busy} aria-label="关闭"><X /></button></header><form onSubmit={submit}>
    <div className="import-help-grid"><article><b>静态构建</b><p>适用于 Unity Web、Godot Web、WebAssembly、Canvas、React、Vue 等浏览器产物。包内放入 <code>game/</code>。</p></article><article><b>独立后端</b><p>适用于任意服务端语言和架构。清单填写已部署的 HTTPS 地址，平台只保存入口和展示资源。</p></article></div>
    <a className="package-prompt-link" href="/developers#prompt-builder" target="_blank" rel="noreferrer"><span><b>还没有 .atri？</b><small>生成详细提示词，让编程 AI 直接完成项目适配、校验和打包。</small></span><ExternalLink /></a>
    <label className="package-file-field"><span>游戏包 *</span><input type="file" required accept=".atri,.zip,application/zip" onChange={(event) => setFile(event.target.files?.[0] ?? null)} disabled={busy} /><small>{file ? `${file.name} · ${(file.size / 1024 / 1024).toFixed(2)} MB` : "使用 @atri/game-kit 校验并打包；上传过程不会把整个文件读入服务器内存。"}</small></label>
    <div className="form-grid package-options"><label><span>所属分类 *</span><select required value={categoryId} onChange={(event) => setCategoryId(event.target.value)} disabled={busy}><option value="">请选择</option>{categories.map((category) => <option value={category.id} key={category.id}>{category.name}</option>)}</select></label><label><span>导入后状态 *</span><select value={status} onChange={(event) => setStatus(event.target.value as GameStatus)} disabled={busy}>{Object.entries(statusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label></div>
    <label className="package-replace"><input type="checkbox" checked={replace} onChange={(event) => setReplace(event.target.checked)} disabled={busy} /><span><b>覆盖同 ID 的已有版本</b><small>仅在确认清单 ID 正确时启用；文件与数据库更新会作为同一次安装处理。</small></span></label>
    {error && <div className="admin-form-error" role="alert">{error}</div>}<footer><button type="button" className="secondary-action" onClick={close} disabled={busy}>取消</button><button type="submit" className="primary-action primary-action--compact" disabled={busy || !file || !categoryId}><PackagePlus />{busy ? "正在校验并安装…" : "校验并导入"}</button></footer>
  </form></section></div>;
}

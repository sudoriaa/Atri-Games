import {
  gameRequiresLogin,
  gameUsesMatchmaking,
  gameUsesPlatformStorage,
  type Category,
  type Game,
  type GameInput,
} from "@atri/shared";
import { ImagePlus, Link2, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useAuth } from "../lib/auth";

const maxCoverBytes = 10 * 1024 * 1024;
const coverExtensions = new Set(["avif", "jpg", "jpeg", "png", "webp"]);

function parseTags(text: string): string[] {
  return text
    .split(/[,，]/)
    .map((tag) => tag.trim())
    .filter(Boolean)
    .slice(0, 10);
}

function gameEditorInput(game: Game): GameInput {
  return {
    slug: game.slug,
    title: game.title,
    summary: game.summary,
    description: game.description,
    authorName: game.authorName,
    coverUrl: game.coverUrl,
    launchUrl: game.launchUrl,
    launchOpenIn: game.launchOpenIn ?? "same-tab",
    repositoryUrl: game.repositoryUrl,
    engine: game.engine,
    version: game.version,
    status: game.status,
    categoryId: game.categoryId,
    featured: game.featured,
    networkRequired: game.networkRequired,
    ownBackend: game.ownBackend,
    requiresLogin: game.requiresLogin ?? game.loginRequired ?? false,
    usesPlatformStorage: game.usesPlatformStorage ?? false,
    matchmakingEnabled: game.matchmakingEnabled ?? false,
    tags: game.tags,
  };
}

interface MyGameEditorProps {
  game: Game;
  categories: Category[];
  submitLabel: string;
  onCancel: () => void;
  onSave: (game: Game) => void;
}

/**
 * Shared editor used by both the upload flow (draft → review) and the "我的游戏"
 * management page. The .atri manifest slug is fixed; the user may rewrite the
 * display metadata and cover. Saving always re-submits for admin review.
 */
export function MyGameEditor({ game, categories, submitLabel, onCancel, onSave }: MyGameEditorProps) {
  const { api } = useAuth();
  const [form, setForm] = useState<GameInput>(() => gameEditorInput(game));
  const [tagText, setTagText] = useState(game.tags.join(", "));
  const [coverFile, setCoverFile] = useState<File | null>(null);
  const [coverPreview, setCoverPreview] = useState(game.coverUrl);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const set = <K extends keyof GameInput>(key: K, value: GameInput[K]) => setForm((current) => ({ ...current, [key]: value }));

  useEffect(() => {
    if (!coverFile) {
      setCoverPreview(form.coverUrl);
      return;
    }
    const objectUrl = URL.createObjectURL(coverFile);
    setCoverPreview(objectUrl);
    return () => URL.revokeObjectURL(objectUrl);
  }, [coverFile, form.coverUrl]);

  const isDirty =
    coverFile !== null ||
    form.title !== game.title ||
    form.summary !== game.summary ||
    form.description !== game.description ||
    form.authorName !== game.authorName ||
    form.version !== game.version ||
    form.categoryId !== game.categoryId ||
    form.launchUrl !== game.launchUrl ||
    form.launchOpenIn !== (game.launchOpenIn ?? "same-tab") ||
    (form.repositoryUrl ?? "") !== (game.repositoryUrl ?? "") ||
    form.engine !== game.engine ||
    form.coverUrl.trim() !== game.coverUrl ||
    parseTags(tagText).join(",") !== game.tags.join(",");

  const capabilityBadges: { label: string; active: boolean }[] = [
    { label: "需登录", active: gameRequiresLogin(game) },
    { label: "内置数据", active: gameUsesPlatformStorage(game) },
    { label: "匹配", active: gameUsesMatchmaking(game) },
  ];

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
    setError("");
  };

  const cancel = () => {
    if (isDirty && !window.confirm("有未保存的更改，确定要放弃吗？")) return;
    onCancel();
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const confirmMessage =
      game.status === "published"
        ? "提交后游戏将立即下架，需管理员重新审核通过后才能重新上架。确定提交？"
        : "提交后进入管理员审核，确定提交？";
    if (!window.confirm(confirmMessage)) return;

    setBusy(true);
    setError("");
    try {
      const updated = await api.updateMyGame(game.id, { ...form, tags: parseTags(tagText) }, coverFile ?? undefined);
      onSave(updated);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "提交失败，请稍后重试");
      setBusy(false);
    }
  };

  return (
    <section className="game-editor-panel">
      <header className="game-editor-panel__head">
        <div>
          <p className="kicker">EDIT GAME</p>
          <h2>{submitLabel}</h2>
          <p className="game-editor-panel__slug">永久标识 / slug：<code>{game.slug}</code>（来自 .atri 清单，不可修改）</p>
        </div>
      </header>
      <form onSubmit={submit}>
        <div className="editor-grid">
          <label>
            <span>标题 *</span>
            <input required maxLength={80} value={form.title} onChange={(event) => set("title", event.target.value)} />
          </label>
          <label>
            <span>作者 *</span>
            <input required maxLength={80} value={form.authorName} onChange={(event) => set("authorName", event.target.value)} />
          </label>
          <label>
            <span>版本 *</span>
            <input required maxLength={40} value={form.version} onChange={(event) => set("version", event.target.value)} />
          </label>
          <label>
            <span>分类 *</span>
            <select required value={form.categoryId} onChange={(event) => set("categoryId", event.target.value)}>
              <option value="">请选择</option>
              {categories.map((category) => (
                <option value={category.id} key={category.id}>{category.name}</option>
              ))}
            </select>
          </label>
          <label className="editor-grid__wide">
            <span>摘要 *</span>
            <input
              required
              minLength={10}
              maxLength={240}
              value={form.summary}
              onChange={(event) => set("summary", event.target.value)}
              placeholder="一句话介绍这个游戏（10–240 字）"
            />
          </label>
          <label className="editor-grid__wide">
            <span>详细说明 *</span>
            <textarea required rows={5} maxLength={4000} value={form.description} onChange={(event) => set("description", event.target.value)} />
          </label>
          <label>
            <span>启动 URL *</span>
            <input required value={form.launchUrl} onChange={(event) => set("launchUrl", event.target.value)} placeholder="https://… 或 /playables/…" />
          </label>
          <label>
            <span>打开方式 *</span>
            <select value={form.launchOpenIn} onChange={(event) => set("launchOpenIn", event.target.value as GameInput["launchOpenIn"])}>
              <option value="same-tab">当前标签页</option>
              <option value="new-tab">新标签页</option>
            </select>
          </label>
          <label>
            <span>引擎 / 框架 *</span>
            <input required maxLength={80} value={form.engine} onChange={(event) => set("engine", event.target.value)} />
          </label>
          <label>
            <span>代码仓库</span>
            <input value={form.repositoryUrl} onChange={(event) => set("repositoryUrl", event.target.value)} placeholder="https://github.com/…" />
          </label>
          <label className="editor-grid__wide">
            <span>标签（逗号分隔，最多 10 个）</span>
            <input value={tagText} onChange={(event) => setTagText(event.target.value)} placeholder="像素, 解谜, 节奏" />
          </label>
          <fieldset className="cover-editor editor-grid__wide">
            <legend>游戏封面 *</legend>
            <div className="cover-editor__preview">
              <div className={`cover-editor__frame${coverPreview ? " has-image" : ""}`}>
                {coverPreview ? <img src={coverPreview} alt="封面预览" /> : <><ImagePlus /><span>选择封面后在这里预览</span></>}
              </div>
              <div className="cover-editor__controls">
                <label className={`cover-upload-control${busy ? " is-disabled" : ""}`}>
                  <ImagePlus size={15} />
                  <span>上传图片</span>
                  <input
                    type="file"
                    accept=".avif,.jpg,.jpeg,.png,.webp,image/avif,image/jpeg,image/png,image/webp"
                    onChange={(event) => { selectCover(event.target.files?.[0] ?? null); event.currentTarget.value = ""; }}
                    disabled={busy}
                  />
                </label>
                <small>AVIF / JPG / PNG / WebP，最大 10 MB。不选择则保留当前封面。</small>
                {coverFile && <span className="cover-editor__file">已选择：{coverFile.name} · {(coverFile.size / 1024 / 1024).toFixed(2)} MB</span>}
              </div>
            </div>
            <label className="cover-url-field">
              <span><Link2 size={14} /> 封面链接</span>
              <input
                value={form.coverUrl}
                onChange={(event) => { set("coverUrl", event.target.value); setCoverFile(null); }}
                placeholder="https://… 或 /covers/…"
              />
              <small>平台托管的封面无需重复填写；仅在上传图片不可用时填写外部链接。</small>
            </label>
          </fieldset>
        </div>
        <div className="capability-hint">
          {capabilityBadges.some((badge) => badge.active) && (
            <span className="capability-badges">
              {capabilityBadges.filter((badge) => badge.active).map((badge) => <b key={badge.label}>{badge.label}</b>)}
            </span>
          )}
          <small>登录、内置数据与匹配能力由 .atri 包声明，提交时保持原状，不可在此修改。</small>
        </div>
        {error && <p className="avatar-field-error" role="alert">{error}</p>}
        <div className="editor-actions">
          <button type="button" className="button button--ghost button--small" onClick={cancel} disabled={busy}>取消</button>
          <button type="submit" className="button button--small" disabled={busy}>
            <Save size={16} /> {busy ? "提交中…" : submitLabel}
          </button>
        </div>
      </form>
    </section>
  );
}

import { type Game } from "@atri/shared";
import { ArrowRight, PackagePlus, UploadCloud } from "lucide-react";
import { useState } from "react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { MyGameEditor } from "../components/MyGameEditor";
import { ErrorState } from "../components/PageState";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function UploadGamePage() {
  const { api, user } = useAuth();
  const categories = useAsync(() => api.categories(), [api]);
  const navigate = useNavigate();
  const [file, setFile] = useState<File | null>(null);
  const [categoryId, setCategoryId] = useState("");
  const [draft, setDraft] = useState<Game | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  if (!user) return <Navigate to="/auth?next=/upload" replace />;

  const categoriesReady = (categories.data?.length ?? 0) > 0;

  const importPackage = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!file || !categoryId) {
      setError("请选择 .atri 游戏包和所属分类");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const game = await api.importMyGamePackage(file, categoryId);
      setDraft(game);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "解析失败，请检查游戏包后重试");
      setBusy(false);
    }
  };

  if (draft) {
    return (
      <div className="page-wrap upload-page">
        <header className="page-intro">
          <p className="kicker">SUBMIT YOUR GAME</p>
          <h1>确认游戏信息</h1>
          <p>已解析“{draft.title}”的元数据。检查以下内容，确认无误后提交审核。</p>
        </header>
        <MyGameEditor
          game={draft}
          categories={categories.data ?? []}
          submitLabel="提交审核"
          onCancel={() => setDraft(null)}
          onSave={(saved) => navigate("/my-games", { state: { notice: `“${saved.title}”已提交审核` } })}
        />
        <p className="upload-page__back"><Link to="/my-games">← 返回我的游戏</Link></p>
      </div>
    );
  }

  return (
    <div className="page-wrap upload-page">
      <header className="page-intro">
        <p className="kicker">SUBMIT YOUR GAME</p>
        <h1>上传新游戏</h1>
        <p>导入 .atri 游戏包，平台会解析出标题、简介、分类等元数据；你可以在下一步手动调整后再提交审核。</p>
      </header>
      {categories.error && <ErrorState message={categories.error} retry={categories.reload} />}
      <section className="upload-panel">
        <form onSubmit={importPackage}>
          <div className="upload-panel__steps">
            <div><b>01</b><span>选择游戏包</span></div>
            <div><b>02</b><span>核对信息</span></div>
            <div><b>03</b><span>等待审核</span></div>
          </div>
          <label className="package-file-field">
            <span>游戏包（.atri）*</span>
            <input
              type="file"
              required
              accept=".atri,.zip,application/zip"
              onChange={(event) => { setFile(event.target.files?.[0] ?? null); setError(""); }}
              disabled={busy}
            />
            <small>{file ? `${file.name} · ${(file.size / 1024 / 1024).toFixed(2)} MB` : "使用 @atri/game-kit 校验并打包；上传过程不会把整个文件读入服务器内存。"}</small>
          </label>
          <label>
            <span>所属分类 *</span>
            <select required value={categoryId} onChange={(event) => setCategoryId(event.target.value)} disabled={busy || !categoriesReady}>
              <option value="">请选择</option>
              {categories.data?.map((category) => (
                <option value={category.id} key={category.id}>{category.name}</option>
              ))}
            </select>
          </label>
          {error && <p className="avatar-field-error" role="alert">{error}</p>}
          <div className="editor-actions">
            <Link className="button button--ghost button--small" to="/my-games">取消</Link>
            <button type="submit" className="button button--small" disabled={busy || !file || !categoryId || !categoriesReady}>
              <PackagePlus size={16} /> {busy ? "正在解析…" : "解析并导入"}
            </button>
          </div>
        </form>
        <aside className="upload-panel__aside">
          <p className="kicker">HOW IT WORKS</p>
          <h2><UploadCloud size={26} /> 三步上架</h2>
          <ol>
            <li>导入 .atri 包后，平台会生成一份<span>私有草稿</span>，不会立即公开。</li>
            <li>核对并修改标题、简介、分类、标签、封面后提交审核。</li>
            <li>管理员通过后游戏才会出现在公开页；再次编辑会<span>立即下架</span>并重新审核。</li>
          </ol>
          <Link className="text-link" to="/developers">还没有 .atri？生成接入提示词 <ArrowRight size={15} /></Link>
        </aside>
      </section>
    </div>
  );
}

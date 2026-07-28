import type { Category } from "@atri/shared";
import { Edit3, FolderTree, Plus, Save, Trash2, X } from "lucide-react";
import { useState } from "react";
import { AdminError, AdminLoading } from "../components/AdminState";
import { useAdminAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function CategoriesPage() {
  const { api } = useAdminAuth();
  const categories = useAsync(() => api.adminCategories(), [api]);
  const [editing, setEditing] = useState<Category | "new" | null>(null);
  const [notice, setNotice] = useState("");

  const remove = async (category: Category) => {
    if (!window.confirm(`确认删除分类“${category.name}”？仍有游戏时不会执行。`)) return;
    try { await api.deleteCategory(category.id); setNotice("分类已删除"); categories.reload(); } catch (error) { setNotice(error instanceof Error ? error.message : "删除失败"); }
  };

  return <div className="admin-page"><header className="admin-page-header admin-page-header--actions"><div><p className="admin-kicker">CONTENT / TAXONOMY</p><h1>分类管理</h1><p>维护面向用户的游戏类型与浏览顺序。</p></div><button className="primary-action primary-action--compact" onClick={() => setEditing("new")}><Plus /> 新建分类</button></header>{notice && <div className="admin-notice">{notice}<button onClick={() => setNotice("")}><X /></button></div>}{categories.loading && <AdminLoading />}{categories.error && <AdminError message={categories.error} retry={categories.reload} />}{categories.data && <div className="category-admin-grid">{categories.data.map((category, index) => <article key={category.id}><span className="category-index">{String(index + 1).padStart(2, "0")}</span><FolderTree /><h2>{category.name}</h2><p>{category.description}</p><div><span><b>{category.gameCount ?? 0}</b> 个游戏</span><span>排序 {category.sortOrder}</span></div><footer><code>{category.id}</code><button onClick={() => setEditing(category)}><Edit3 /></button><button className="danger" onClick={() => remove(category)}><Trash2 /></button></footer></article>)}</div>}{editing && <CategoryEditor category={editing === "new" ? null : editing} close={() => setEditing(null)} saved={() => { setEditing(null); setNotice("分类已保存"); categories.reload(); }} />}</div>;
}

function CategoryEditor({ category, close, saved }: { category: Category | null; close: () => void; saved: () => void }) {
  const { api } = useAdminAuth();
  const [form, setForm] = useState({ id: category?.id ?? "", name: category?.name ?? "", description: category?.description ?? "", sortOrder: category?.sortOrder ?? 100 });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); setBusy(true); setError("");
    try { if (category) await api.updateCategory(category.id, { name: form.name, description: form.description, sortOrder: form.sortOrder }); else await api.createCategory(form); saved(); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "保存失败"); setBusy(false); }
  };
  const titleId = "category-editor-title";
  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (!busy && event.target === event.currentTarget) close(); }}><section className="admin-modal admin-modal--small" role="dialog" aria-modal="true" aria-labelledby={titleId}><header><div><p className="admin-kicker">{category ? "EDIT CATEGORY" : "NEW CATEGORY"}</p><h2 id={titleId}>{category ? category.name : "新建分类"}</h2></div><button type="button" onClick={close} disabled={busy} aria-label="关闭"><X /></button></header><form onSubmit={submit}><label><span>分类 ID</span><input required disabled={Boolean(category)} pattern="[a-z0-9-]+" value={form.id} onChange={(event) => setForm({ ...form, id: event.target.value.toLowerCase() })} /></label><label><span>显示名称</span><input required maxLength={40} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></label><label><span>说明</span><textarea rows={3} value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></label><label><span>排序</span><input required type="number" min={0} max={9999} value={form.sortOrder} onChange={(event) => setForm({ ...form, sortOrder: Number(event.target.value) })} /></label>{error && <div className="admin-form-error" role="alert">{error}</div>}<footer><button type="button" className="secondary-action" onClick={close} disabled={busy}>取消</button><button type="submit" className="primary-action primary-action--compact" disabled={busy}><Save /> {busy ? "保存中…" : "保存"}</button></footer></form></section></div>;
}

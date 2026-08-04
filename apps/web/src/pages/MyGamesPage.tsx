import { type Game, type GameStatus, type GameVersion } from "@atri/shared";
import { Edit3, History, Plus, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, Navigate, useLocation } from "react-router-dom";
import { MyGameEditor } from "../components/MyGameEditor";
import { VersionHistory } from "../components/VersionHistory";
import { ErrorState, LoadingState } from "../components/PageState";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

const statusLabels: Record<GameStatus, string> = { draft: "草稿", review: "待审核", published: "已发布", hidden: "已下架" };

export function MyGamesPage() {
  const { api, user } = useAuth();
  const location = useLocation();
  const [editing, setEditing] = useState<Game | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [noticeTone, setNoticeTone] = useState<"success" | "error">("success");
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const [versioning, setVersioning] = useState<Game | null>(null);
  const games = useAsync(() => api.myGames({ pageSize: 100 }), [api]);
  const categories = useAsync(() => api.categories(), [api]);

  useEffect(() => {
    const incoming = (location.state as { notice?: string } | null)?.notice;
    if (incoming) {
      setNotice(incoming);
      setNoticeTone("success");
      window.history.replaceState({}, "");
    }
  }, [location.state]);

  if (!user) return <Navigate to="/auth?next=/my-games" replace />;

  const remove = async (game: Game) => {
    if (!window.confirm(`确认删除“${game.title}”？删除后无法恢复，游戏包与上传的封面都会一并移除。`)) return;
    setPendingDelete(game.id);
    setNotice(null);
    try {
      await api.deleteMyGame(game.id);
      setNotice(`${game.title} 已删除`);
      setNoticeTone("success");
      games.reload();
    } catch (caught) {
      setNotice(caught instanceof Error ? caught.message : "删除失败，请稍后重试");
      setNoticeTone("error");
    } finally {
      setPendingDelete(null);
    }
  };

  return (
    <div className="page-wrap my-games">
      <header className="page-intro page-intro--compact my-games__head">
        <div>
          <p className="kicker">MY GAMES</p>
          <h1>我的游戏</h1>
          <p>在这里管理你上传的游戏。保存修改会进入管理员审核，通过后才会重新上架。</p>
        </div>
        <Link className="button button--small" to="/upload"><Plus size={16} /> 上传新游戏</Link>
      </header>
      {notice && <div className={`inline-notice inline-notice--${noticeTone}`} role={noticeTone === "error" ? "alert" : "status"} aria-live="polite">{notice}</div>}
      {categories.error && <ErrorState message={categories.error} retry={categories.reload} />}
      {games.loading && <LoadingState />}
      {games.error && <ErrorState message={games.error} retry={games.reload} />}
      {games.data &&
        (editing ? (
          <MyGameEditor
            game={editing}
            categories={categories.data ?? []}
            submitLabel="保存修改"
            onCancel={() => setEditing(null)}
            onSave={(saved) => {
              setEditing(null);
              setNotice(`${saved.title} 已提交审核`);
              setNoticeTone("success");
              games.reload();
            }}
          />
        ) : (
          <div className="my-games-list">
            {games.data.items.length === 0 ? (
              <div className="my-games-empty">
                <p className="kicker">NO GAMES YET</p>
                <h2>还没有上传过游戏</h2>
                <p>导入一个 .atri 游戏包，几分钟内就能提交你的第一个作品。</p>
                <Link className="button" to="/upload"><Plus size={17} /> 上传新游戏</Link>
              </div>
            ) : (
              games.data.items.map((game) => {
                const deleting = pendingDelete === game.id;
                return (
                  <article className="my-game-row" key={game.id} aria-busy={deleting || undefined}>
                    <div className="my-game-row__cover"><img src={game.coverUrl} alt="" loading="lazy" decoding="async" /></div>
                    <div className="my-game-row__body">
                      <h3>{game.title}</h3>
                      <p>{game.summary}</p>
                      <div className="my-game-row__meta">
                        <span className={`status-pill status-pill--${game.status}`}>{statusLabels[game.status]}</span>
                        <time>{new Date(game.updatedAt).toLocaleDateString("zh-CN")} 更新</time>
                      </div>
                    </div>
                    <div className="my-game-row__actions">
                      <button className="button button--ghost button--small" onClick={() => setEditing(game)} disabled={deleting}><Edit3 size={15} /> 编辑</button>
                      <button className="button button--ghost button--small" onClick={() => setVersioning(game)} disabled={deleting}><History size={15} /> 版本</button>
                      <button className="danger-link my-game-delete" onClick={() => remove(game)} disabled={deleting}>{deleting ? "删除中…" : "删除"}</button>
                    </div>
                  </article>
                );
              })
            )}
          </div>
        ))}
      {versioning && <OwnerVersionDialog game={versioning} close={() => setVersioning(null)} rolledBack={(game) => { setVersioning(null); setNotice(`${game.title} 已回滚并重新提交审核`); setNoticeTone("success"); games.reload(); }} />}
    </div>
  );
}

function OwnerVersionDialog({ game, close, rolledBack }: { game: Game; close: () => void; rolledBack: (game: Game) => void }) {
  const { api } = useAuth();
  const versions = useAsync(() => api.myGameVersions(game.id), [api, game.id]);
  const [busyId, setBusyId] = useState("");
  const [error, setError] = useState("");
  const rollback = async (version: GameVersion) => {
    if (!window.confirm(`将可编辑资料回滚到 v${version.version} 并重新提交审核？`)) return;
    setBusyId(version.id); setError("");
    try { rolledBack(await api.rollbackMyGame(game.id, version.id)); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "回滚失败"); setBusyId(""); }
  };
  return <div className="report-dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !busyId) close(); }}><section className="version-dialog" role="dialog" aria-modal="true" aria-label={`${game.title} 版本记录`}><header><div><p className="kicker">VERSION CONTROL</p><h2>{game.title}</h2><p>回滚会恢复历史元数据，并重新进入审核。</p></div><button className="icon-button" onClick={close} disabled={Boolean(busyId)} aria-label="关闭"><X /></button></header>{versions.loading && <LoadingState />}{versions.error && <ErrorState message={versions.error} retry={versions.reload} />}{versions.data && <VersionHistory versions={versions.data} onRollback={(version) => void rollback(version)} busyId={busyId} />}{error && <p className="avatar-field-error" role="alert">{error}</p>}</section></div>;
}

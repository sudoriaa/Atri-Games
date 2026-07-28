import { CalendarDays, Heart, LogOut, Mail, Save } from "lucide-react";
import { useState } from "react";
import { Link, Navigate } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function ProfilePage() {
  const { api, user, logout, updateUser } = useAuth();
  const [displayName, setDisplayName] = useState(user?.displayName ?? "");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const favorites = useAsync(() => user ? api.favorites() : Promise.resolve([]), [api, user]);

  if (!user) return <Navigate to="/auth?next=/profile" replace />;

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true); setMessage("");
    try { const updated = await api.updateMe({ displayName }); updateUser(updated); setMessage("档案已经保存"); }
    catch (error) { setMessage(error instanceof Error ? error.message : "保存失败"); }
    finally { setBusy(false); }
  };

  return (
    <div className="page-wrap profile-page">
      <header className="profile-hero"><span className="profile-avatar">{user.displayName.slice(0, 1).toUpperCase()}</span><div><p className="kicker">PLAYER PROFILE</p><h1>{user.displayName}</h1><p>你的 Atri 玩家档案与收藏概览。</p></div></header>
      <div className="profile-grid">
        <section className="profile-panel"><h2>基本信息</h2><form onSubmit={save}><label><span>显示昵称</span><input value={displayName} minLength={2} maxLength={40} onChange={(event) => setDisplayName(event.target.value)} /></label><label><span>账户邮箱</span><div className="readonly-row"><Mail size={17} />{user.email}</div></label>{message && <p className="inline-notice">{message}</p>}<button className="button button--small" disabled={busy}><Save size={16} /> 保存资料</button></form></section>
        <aside className="profile-panel profile-panel--stats"><h2>玩家统计</h2><div className="profile-stat"><Heart /><strong>{favorites.data?.length ?? 0}</strong><span>收藏的游戏</span></div><div className="profile-stat"><CalendarDays /><strong>{new Date(user.createdAt).getFullYear()}</strong><span>加入年份</span></div><Link className="text-link" to="/library">打开我的收藏 →</Link></aside>
      </div>
      <button className="danger-link" onClick={logout}><LogOut size={16} /> 退出当前账户</button>
    </div>
  );
}

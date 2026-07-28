import { ArrowRight, Eye, Gamepad2, Heart, Inbox, MousePointerClick, UsersRound } from "lucide-react";
import { Link } from "react-router-dom";
import { AdminError, AdminLoading } from "../components/AdminState";
import { adminActivityLabel } from "../lib/admin-utils";
import { useAdminAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function DashboardPage() {
  const { api, user } = useAdminAuth();
  const metrics = useAsync(() => api.dashboard(), [api]);
  const activity = useAsync(() => api.adminActivity(), [api]);
  const topGames = useAsync(() => api.adminGames({ status: "published", pageSize: 5 }), [api]);
  const cards = metrics.data ? [
    { label: "活跃账户", value: metrics.data.users, icon: UsersRound, color: "violet" },
    { label: "已发布游戏", value: metrics.data.publishedGames, icon: Gamepad2, color: "lime" },
    { label: "待审核", value: metrics.data.reviewGames, icon: Inbox, color: "orange" },
    { label: "今日启动", value: metrics.data.launchesToday, icon: MousePointerClick, color: "blue" },
  ] : [];

  return (
    <div className="admin-page">
      <header className="admin-page-header"><div><p className="admin-kicker">OVERVIEW / LIVE DATA</p><h1>下午好，{user?.displayName}。</h1><p>这里是 Atri Games 当前的内容与运营情况。</p></div><div className="live-badge"><span /> API ONLINE</div></header>
      {metrics.loading && <AdminLoading />}
      {metrics.error && <AdminError message={metrics.error} retry={metrics.reload} />}
      {metrics.data && <div className="metric-grid">{cards.map(({ label, value, icon: Icon, color }) => <article className={`metric-card metric-card--${color}`} key={label}><div><p>{label}</p><strong>{value.toLocaleString()}</strong></div><Icon /><small>实时统计</small></article>)}</div>}
      <div className="dashboard-grid">
        <section className="admin-panel"><div className="panel-heading"><div><p className="admin-kicker">POPULAR WORLDS</p><h2>热门游戏</h2></div><Link to="/games">管理全部 <ArrowRight size={15} /></Link></div>{topGames.loading && <AdminLoading />}{topGames.error && <AdminError message={topGames.error} retry={topGames.reload} />}{topGames.data && <div className="rank-list">{topGames.data.items.map((game, index) => <div key={game.id}><span className="rank-number">{String(index + 1).padStart(2, "0")}</span><img src={game.coverUrl} alt="" loading="lazy" decoding="async" /><div><b>{game.title}</b><small>{game.authorName} · {game.engine}</small></div><span><Eye size={14} />{game.playCount.toLocaleString()}</span><span><Heart size={14} />{game.favoriteCount.toLocaleString()}</span></div>)}</div>}</section>
        <section className="admin-panel"><div className="panel-heading"><div><p className="admin-kicker">AUDIT TRAIL</p><h2>最近操作</h2></div></div>{activity.loading && <AdminLoading />}{activity.error && <AdminError message={activity.error} retry={activity.reload} />}{activity.data && activity.data.length === 0 && <div className="panel-empty">暂无管理操作</div>}{activity.data && <div className="activity-list">{activity.data.slice(0, 8).map((item) => <div key={item.id}><span className="activity-dot" /><div><p><b>{item.actorName}</b> {adminActivityLabel(item.action)}</p><small>{item.detail || item.entityId}</small></div><time>{new Date(item.createdAt).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })}</time></div>)}</div>}</section>
      </div>
    </div>
  );
}

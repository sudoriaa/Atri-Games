import { Ban, CalendarDays, ExternalLink, Flag, Gamepad2, ShieldCheck, UserCheck, UserPlus } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { GameCard } from "../components/GameCard";
import { ErrorState, LoadingState } from "../components/PageState";
import { ReportDialog } from "../components/ReportDialog";
import { UserAvatar } from "../components/UserAvatar";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function CreatorPage() {
  const { id = "" } = useParams();
  const { api, user } = useAuth();
  const navigate = useNavigate();
  const profile = useAsync(() => api.creator(id), [api, id]);
  const [busy, setBusy] = useState(false);
  const [reportOpen, setReportOpen] = useState(false);

  const toggleFollow = async () => {
    if (!user) {
      navigate(`/auth?next=${encodeURIComponent(`/creators/${id}`)}`);
      return;
    }
    if (!profile.data || profile.data.id === user.id) return;
    setBusy(true);
    try {
      const state = profile.data.following ? await api.unfollowCreator(id) : await api.followCreator(id);
      profile.setData({ ...profile.data, ...state });
    } finally {
      setBusy(false);
    }
  };

  const toggleBlock = async () => {
    if (!user) {
      navigate(`/auth?next=${encodeURIComponent(`/creators/${id}`)}`);
      return;
    }
    if (!profile.data || profile.data.id === user.id) return;
    setBusy(true);
    try {
      const state = profile.data.blocked ? await api.unblockCreator(id) : await api.blockCreator(id);
      profile.setData({ ...profile.data, ...state });
    } finally {
      setBusy(false);
    }
  };

  if (profile.loading) return <LoadingState label="正在打开创作者档案" />;
  if (profile.error || !profile.data) return <ErrorState message={profile.error ?? "创作者不存在"} retry={profile.reload} />;

  const creator = profile.data;
  const ownProfile = user?.id === creator.id;
  return (
    <div className="page-wrap creator-page">
      <section className="creator-hero">
        <UserAvatar className="creator-hero__avatar" name={creator.displayName} src={creator.avatarUrl} decorative />
        <div className="creator-hero__copy"><p className="kicker">CREATOR PROFILE</p><h1>{creator.displayName}</h1><p>{creator.bio || "这位创作者正在准备自己的介绍。"}</p><div className="creator-hero__meta"><span>用户 ID: {creator.userNumber}</span><span><CalendarDays size={14} /> {new Date(creator.joinedAt).getFullYear()} 加入</span></div></div>
        <div className="creator-hero__actions">{ownProfile ? <Link className="button button--small" to="/profile">编辑档案</Link> : <>{creator.blocked ? <button className="button button--ghost button--small" onClick={toggleBlock} disabled={busy}><ShieldCheck size={16} />{busy ? "处理中…" : "取消屏蔽"}</button> : <><button className={`button button--small${creator.following ? " button--ghost" : ""}`} onClick={toggleFollow} disabled={busy}>{creator.following ? <UserCheck size={16} /> : <UserPlus size={16} />}{busy ? "处理中…" : creator.following ? "已关注" : "关注创作者"}</button><button className="creator-block-button" onClick={toggleBlock} disabled={busy}><Ban size={14} /> 屏蔽</button></>}</>}{creator.websiteUrl && <a className="button button--ghost button--small" href={creator.websiteUrl} target="_blank" rel="noreferrer">个人主页 <ExternalLink size={15} /></a>}{!ownProfile && <button className="creator-report-button" onClick={() => setReportOpen(true)}><Flag size={14} /> 举报</button>}</div>
      </section>
      <section className="creator-stats"><div><strong>{creator.gameCount}</strong><span>公开作品</span></div><div><strong>{creator.followerCount}</strong><span>关注者</span></div></section>
      <section className="creator-games"><div className="section-heading"><div><p className="kicker"><Gamepad2 size={13} /> PUBLISHED WORLDS</p><h2>公开作品</h2></div></div>{creator.games.length ? <div className="game-grid">{creator.games.map((game, index) => <GameCard key={game.id} game={game} index={index} />)}</div> : <div className="my-games-empty"><h2>还没有公开作品</h2><p>通过审核的游戏会展示在这里。</p></div>}</section>
      {reportOpen && <ReportDialog targetType="creator" targetId={creator.id} targetLabel={creator.displayName} onClose={() => setReportOpen(false)} />}
    </div>
  );
}

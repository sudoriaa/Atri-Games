import { Activity, ArrowUpRight, BellRing, UserPlus } from "lucide-react";
import { Link, Navigate } from "react-router-dom";
import { ErrorState, LoadingState } from "../components/PageState";
import { UserAvatar } from "../components/UserAvatar";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function FeedPage() {
  const { api, user } = useAuth();
  const feed = useAsync(() => user ? api.communityFeed() : Promise.resolve([]), [api, user]);
  if (!user) return <Navigate to="/auth?next=/feed" replace />;
  return <div className="page-wrap feed-page"><header className="page-intro page-intro--compact"><p className="kicker"><Activity size={13} /> FOLLOWING FEED</p><h1>关注动态</h1><p>来自你关注的创作者和游戏的发布、更新与版本记录。</p></header>{feed.loading && <LoadingState label="正在整理动态" />}{feed.error && <ErrorState message={feed.error} retry={feed.reload} />}{feed.data && (feed.data.length ? <div className="community-feed">{feed.data.map((event) => <article key={event.id}><UserAvatar className="community-feed__avatar" name={event.actorName} src={event.actorAvatarUrl} decorative /><div className="community-feed__body"><div><Link to={event.actorId ? `/creators/${event.actorId}` : "/discover"}>{event.actorName}</Link><time>{new Date(event.createdAt).toLocaleString("zh-CN")}</time></div><p>{event.summary}</p><Link className="community-feed__game" to={`/games/${event.gameSlug}`}>{event.gameCoverUrl && <img src={event.gameCoverUrl} alt="" />}<span><small>{event.kind === "game.published" ? "NEW RELEASE" : "GAME UPDATE"}</small><strong>{event.gameTitle}</strong></span><ArrowUpRight /></Link></div></article>)}</div> : <div className="feed-empty"><BellRing /><h2>动态流还是空的</h2><p>关注创作者或在游戏详情页关注更新，新发布和新版本会出现在这里。</p><Link className="button" to="/discover"><UserPlus size={16} /> 去发现游戏</Link></div>)}</div>;
}

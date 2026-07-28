import { gameRequiresLogin, type GameShareChannel, type LaunchResponse } from "@atri/shared";
import { ArrowLeft, ArrowUpRight, Cloud, Code2, ExternalLink, Heart, LogIn, MessageSquare, Share2, ShieldCheck, Star, WifiOff } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { CommentSection } from "../components/CommentSection";
import { ErrorState, LoadingState } from "../components/PageState";
import { ShareDialog } from "../components/ShareDialog";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

const GAME_HANDOFF_SOURCE = "atri-game-launch";
const GAME_HANDOFF_VERSION = 1;

// `window.name` survives the cross-origin navigation from the portal to Game
// Origin without becoming part of the URL, browser history, Referer header or
// server access logs. The game-origin bootstrap consumes and clears it before
// the package's own scripts execute.
function gameHandoff(response: LaunchResponse, slug: string) {
  if (!response.gameTicket) return "";
  return JSON.stringify({
    source: GAME_HANDOFF_SOURCE,
    version: GAME_HANDOFF_VERSION,
    ticket: response.gameTicket,
    gameSlug: slug,
    apiBaseUrl: response.apiBase ?? response.apiBaseUrl ?? "/api/v1",
  });
}

export function GamePage() {
  const { slug = "" } = useParams();
  const { api, user } = useAuth();
  const navigate = useNavigate();
  const game = useAsync(() => api.game(slug), [api, slug]);
  const [launching, setLaunching] = useState(false);
  const [favoriteBusy, setFavoriteBusy] = useState(false);
  const [likeBusy, setLikeBusy] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [notice, setNotice] = useState("");

  const launch = async () => {
    if (game.data && gameRequiresLogin(game.data) && !user) {
      navigate(`/auth?next=${encodeURIComponent(`/games/${slug}`)}`);
      return;
    }
    setLaunching(true);
    setNotice("");
    const popup = game.data?.launchOpenIn === "new-tab" ? window.open("about:blank", "_blank") : null;
    if (popup) popup.opener = null;
    try {
      const result = await api.launch(slug);
      const launchUrl = result.launchUrl;
      const handoff = gameHandoff(result, slug);
      if (result.openIn === "new-tab") {
        if (popup && !popup.closed) {
          if (handoff) popup.name = handoff;
          popup.location.replace(launchUrl);
        }
        else {
          // The user gesture normally created the blank popup above. When a
          // browser still blocks it, retain the authenticated game handoff by
          // falling back to the current tab instead of opening a URL-only tab.
          if (handoff) window.name = handoff;
          window.location.assign(launchUrl);
        }
      } else {
        popup?.close();
        if (handoff) window.name = handoff;
        window.location.assign(launchUrl);
      }
    } catch (error) {
      popup?.close();
      setNotice(error instanceof Error ? error.message : "启动失败");
      setLaunching(false);
    }
  };

  const toggleFavorite = async () => {
    if (!user) {
      navigate(`/auth?next=${encodeURIComponent(`/games/${slug}`)}`);
      return;
    }
    if (!game.data) return;
    setFavoriteBusy(true);
    try {
      if (game.data.isFavorite) await api.removeFavorite(game.data.id);
      else await api.addFavorite(game.data.id);
      game.setData({ ...game.data, isFavorite: !game.data.isFavorite, favoriteCount: game.data.favoriteCount + (game.data.isFavorite ? -1 : 1) });
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "操作失败");
    } finally {
      setFavoriteBusy(false);
    }
  };

  const toggleLike = async () => {
    if (!user) {
      navigate(`/auth?next=${encodeURIComponent(`/games/${slug}`)}`);
      return;
    }
    if (!game.data) return;
    setLikeBusy(true);
    setNotice("");
    try {
      const state = game.data.isLiked ? await api.unlikeGame(slug) : await api.likeGame(slug);
      game.setData({ ...game.data, isLiked: state.isLiked, likeCount: state.likeCount });
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "操作失败");
    } finally {
      setLikeBusy(false);
    }
  };

  // The dialog reports which surface the player used; the counter is settled
  // from the server's response so concurrent shares stay accurate.
  const recordShare = async (channel: GameShareChannel) => {
    try {
      const state = await api.recordShare(slug, channel);
      game.setData((current) => (current ? { ...current, shareCount: state.shareCount } : current));
    } catch {
      // A share already happened in the player's client; a failed count is not
      // worth interrupting them over.
    }
  };

  if (game.loading) return <LoadingState />;
  if (game.error || !game.data) return <ErrorState message={game.error ?? "游戏不存在"} retry={game.reload} />;

  return (
    <div className="game-detail">
      <div className="game-detail__back"><Link to="/discover"><ArrowLeft size={16} /> 返回目录</Link><span>CATALOGUE / {game.data.categoryName.toUpperCase()}</span></div>
      <section className="game-detail__hero">
        <div className="game-detail__art"><img src={game.data.coverUrl} alt={`${game.data.title} 封面`} decoding="async" /><span className="art-stamp">{game.data.featured ? "CURATOR'S PICK" : "OPEN CATALOGUE"}</span></div>
        <div className="game-detail__copy">
          <div className="game-detail__labels"><span>{game.data.categoryName}</span><span>v{game.data.version}</span></div>
          <h1>{game.data.title}</h1>
          <p className="game-detail__summary">{game.data.summary}</p>
          <p className="game-detail__author">A game by <strong>{game.data.authorName}</strong></p>
          <div className="game-detail__actions">
            <button className="button button--large" onClick={launch} disabled={launching}>{launching ? "正在打开…" : "开始游戏"}<ExternalLink size={18} /></button>
            <button className={`icon-button icon-button--border ${game.data.isLiked ? "is-liked" : ""}`} onClick={toggleLike} disabled={likeBusy} aria-label={game.data.isLiked ? "取消点赞" : "点赞游戏"} aria-pressed={game.data.isLiked}><Star fill={game.data.isLiked ? "currentColor" : "none"} /></button>
            <button className={`icon-button icon-button--border ${game.data.isFavorite ? "is-favorite" : ""}`} onClick={toggleFavorite} disabled={favoriteBusy} aria-label={game.data.isFavorite ? "取消收藏" : "收藏游戏"} aria-pressed={game.data.isFavorite}><Heart fill={game.data.isFavorite ? "currentColor" : "none"} /></button>
            <button className="icon-button icon-button--border" onClick={() => setShareOpen(true)} aria-label="分享游戏"><Share2 /></button>
          </div>
          <div className="game-detail__stats">
            <span><Star size={14} /> {game.data.likeCount.toLocaleString()} 点赞</span>
            <span><Heart size={14} /> {game.data.favoriteCount.toLocaleString()} 收藏</span>
            <span><Share2 size={14} /> {game.data.shareCount.toLocaleString()} 分享</span>
            <span><MessageSquare size={14} /> {game.data.commentCount.toLocaleString()} 留言</span>
          </div>
          {notice && <p className="inline-notice">{notice}</p>}
          <div className="launch-note"><ArrowUpRight size={18} /><p><strong>{gameRequiresLogin(game.data) ? "需要玩家账号" : "这是一个独立网页游戏"}</strong><br />{gameRequiresLogin(game.data) ? "注册或登录后，平台会为本次游戏发放短期玩家票据。" : "点击后将离开 Atri Games，进入创作者自己的游戏页面。"}</p></div>
        </div>
      </section>
      <section className="game-detail__body">
        <article><p className="kicker">ABOUT THIS WORLD</p><h2>关于游戏</h2><p>{game.data.description}</p><div className="tag-list">{game.data.tags.map((tag) => <span key={tag}>#{tag}</span>)}</div></article>
        <aside className="spec-sheet">
          <h3>游戏档案</h3>
          <dl>
            <div><dt><Code2 size={16} /> 技术引擎</dt><dd>{game.data.engine}</dd></div>
            <div><dt>{game.data.networkRequired ? <Cloud size={16} /> : <WifiOff size={16} />} 网络</dt><dd>{game.data.networkRequired ? "需要联网" : "支持离线"}</dd></div>
            <div><dt><ShieldCheck size={16} /> 服务</dt><dd>{game.data.ownBackend ? "独立后端" : "纯浏览器"}</dd></div>
            {gameRequiresLogin(game.data) && <div><dt><LogIn size={16} /> 访问</dt><dd>需登录</dd></div>}
            <div><dt><Heart size={16} /> 收藏</dt><dd>{game.data.favoriteCount.toLocaleString()} 人</dd></div>
          </dl>
          {game.data.repositoryUrl && <a href={game.data.repositoryUrl} target="_blank" rel="noreferrer">查看源代码 <ArrowUpRight size={15} /></a>}
        </aside>
      </section>
      <CommentSection
        slug={slug}
        onCountChange={(delta) =>
          game.setData((current) =>
            current ? { ...current, commentCount: Math.max(0, current.commentCount + delta) } : current,
          )
        }
      />
      {shareOpen && (
        <ShareDialog
          game={game.data}
          shareUrl={`${window.location.origin}/games/${game.data.slug}`}
          onClose={() => setShareOpen(false)}
          onShared={recordShare}
        />
      )}
    </div>
  );
}

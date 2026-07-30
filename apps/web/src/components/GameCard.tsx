import { gameRequiresLogin, type Game } from "@atri/shared";
import { ArrowUpRight, Cloud, Eye, Heart, LogIn, Star, WifiOff } from "lucide-react";
import { Link } from "react-router-dom";

type GameCardMetric = "favorites" | "likes" | "plays";

export function GameCard({ game, index = 0, variant = "card", metric = "favorites" }: { game: Game; index?: number; variant?: "card" | "list"; metric?: GameCardMetric }) {
  const engagement = metric === "likes"
    ? { Icon: Star, count: game.likeCount, label: "点赞" }
    : metric === "plays"
      ? { Icon: Eye, count: game.playCount, label: "游玩" }
      : { Icon: Heart, count: game.favoriteCount, label: "收藏" };

  return (
    <article className={variant === "list" ? "game-card game-card--list" : "game-card"} style={{ animationDelay: `${Math.min(index, 8) * 45}ms` }}>
      <Link className="game-card__cover" to={`/games/${game.slug}`} aria-label={`查看 ${game.title}`}>
        <img src={game.coverUrl} alt="" loading="lazy" decoding="async" />
        <span className="game-card__number">{String(index + 1).padStart(2, "0")}</span>
        {game.featured && <span className="featured-badge">编辑精选</span>}
        {gameRequiresLogin(game) && <span className="login-badge"><LogIn size={12} /> 需登录</span>}
      </Link>
      <div className="game-card__body">
        <div className="game-card__eyebrow">
          <span>{game.categoryName}</span>
          <span>{game.engine}</span>
        </div>
        <Link to={`/games/${game.slug}`}><h3>{game.title}</h3></Link>
        <p>{game.summary}</p>
        <div className="game-card__meta">
          <span>{game.networkRequired ? <Cloud size={14} /> : <WifiOff size={14} />}{game.networkRequired ? "在线" : "可离线"}</span>
          <span title={`${engagement.count.toLocaleString()} ${engagement.label}`}><engagement.Icon size={14} /> {engagement.count.toLocaleString()}</span>
          <Link className="circle-link" to={`/games/${game.slug}`} aria-label="查看详情"><ArrowUpRight size={17} /></Link>
        </div>
      </div>
    </article>
  );
}

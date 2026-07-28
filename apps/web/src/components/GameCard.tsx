import { gameRequiresLogin, type Game } from "@atri/shared";
import { ArrowUpRight, Cloud, Heart, LogIn, WifiOff } from "lucide-react";
import { Link } from "react-router-dom";

export function GameCard({ game, index = 0 }: { game: Game; index?: number }) {
  return (
    <article className="game-card" style={{ animationDelay: `${Math.min(index, 8) * 45}ms` }}>
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
          <span><Heart size={14} /> {game.favoriteCount.toLocaleString()}</span>
          <Link className="circle-link" to={`/games/${game.slug}`} aria-label="查看详情"><ArrowUpRight size={17} /></Link>
        </div>
      </div>
    </article>
  );
}

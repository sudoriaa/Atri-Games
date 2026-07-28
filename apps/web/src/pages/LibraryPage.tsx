import { Heart, LogIn } from "lucide-react";
import { Link } from "react-router-dom";
import { GameCard } from "../components/GameCard";
import { ErrorState, LoadingState } from "../components/PageState";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function LibraryPage() {
  const { api, user } = useAuth();
  const library = useAsync(() => user ? api.favorites() : Promise.resolve([]), [api, user]);

  if (!user) return <div className="page-wrap empty-state empty-state--page"><Heart /><h1>你的书架还没有开启</h1><p>登录后就能收藏感兴趣的游戏，随时从这里继续探索。</p><Link className="button" to="/auth?next=/library"><LogIn size={17} /> 登录并查看</Link></div>;

  return (
    <div className="page-wrap library-page">
      <header className="page-intro page-intro--compact"><p className="kicker">YOUR PRIVATE SHELF</p><h1>{user.displayName} 的收藏</h1><p>那些你想再打开一次的世界。</p></header>
      {library.loading && <LoadingState />}
      {library.error && <ErrorState message={library.error} retry={library.reload} />}
      {library.data?.length === 0 && <div className="empty-state"><Heart /><h2>书架还是空的</h2><p>在游戏详情页点击心形按钮，就能把它放到这里。</p><Link className="button button--ghost" to="/discover">去逛逛目录</Link></div>}
      {library.data && library.data.length > 0 && <div className="game-grid">{library.data.map((game, index) => <GameCard game={game} index={index} key={game.id} />)}</div>}
    </div>
  );
}

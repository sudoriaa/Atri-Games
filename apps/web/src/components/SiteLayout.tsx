import { Compass, Heart, LogOut, Menu, Search, UserRound, X } from "lucide-react";
import { useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { Brand } from "./Brand";

export function SiteLayout() {
  const { user, logout } = useAuth();
  const [menuOpen, setMenuOpen] = useState(false);
  const [search, setSearch] = useState("");
  const navigate = useNavigate();

  const submitSearch = (event: React.FormEvent) => {
    event.preventDefault();
    const query = search.trim();
    navigate(query ? `/discover?q=${encodeURIComponent(query)}` : "/discover");
    setMenuOpen(false);
  };

  return (
    <div className="site-shell">
      <header className="site-header">
        <Brand />
        <button className="icon-button menu-button" onClick={() => setMenuOpen((value) => !value)} aria-label="切换菜单">
          {menuOpen ? <X /> : <Menu />}
        </button>
        <nav className={menuOpen ? "site-nav is-open" : "site-nav"} aria-label="主导航">
          <NavLink to="/discover" onClick={() => setMenuOpen(false)}>
            <Compass size={17} /> 发现游戏
          </NavLink>
          <NavLink to="/library" onClick={() => setMenuOpen(false)}>
            <Heart size={17} /> 我的收藏
          </NavLink>
        </nav>
        <form className="header-search" onSubmit={submitSearch} role="search">
          <Search size={17} />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索游戏、作者或标签" aria-label="搜索游戏" />
          <kbd>↵</kbd>
        </form>
        <div className="header-account">
          {user ? (
            <>
              <Link className="account-chip" to="/profile">
                <span className="avatar">{user.displayName.slice(0, 1).toUpperCase()}</span>
                <span><small>欢迎回来</small>{user.displayName}</span>
              </Link>
              <button className="icon-button" onClick={logout} aria-label="退出登录"><LogOut size={18} /></button>
            </>
          ) : (
            <Link className="button button--small" to="/auth"><UserRound size={17} /> 登录</Link>
          )}
        </div>
      </header>
      <main>
        <Outlet />
      </main>
      <footer className="site-footer">
        <div>
          <Brand compact />
          <p>让每一个由想象力驱动的网页游戏，都有被发现的机会。</p>
        </div>
        <div className="footer-links">
          <Link to="/discover">浏览游戏</Link>
          <Link to="/developers">AI 接入提示词</Link>
          <a href="https://github.com/sudoriaa/Atri-Games/blob/main/docs/GAME_INTEGRATION.md" rel="noreferrer" target="_blank">完整文档</a>
          <a href="mailto:hello@atri.games">联系我们</a>
        </div>
        <small>© 2026 Atri Games · Built with curiosity.</small>
      </footer>
    </div>
  );
}

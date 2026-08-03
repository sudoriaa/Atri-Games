import { Compass, Heart, LogOut, Menu, Search, ShieldCheck, UserRound, X } from "lucide-react";
import { useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { adminConsoleUrl } from "../lib/admin-url";
import { Brand } from "./Brand";
import { UserAvatar } from "./UserAvatar";

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
              {user.role === "admin" && (
                <Link className="admin-entry" to={adminConsoleUrl} title="进入管理控制台">
                  <ShieldCheck size={16} /><span>管理端</span>
                </Link>
              )}
              <Link className="account-chip" to="/profile">
                <UserAvatar className="avatar" name={user.displayName} src={user.avatarUrl} decorative />
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
          <a href="https://github.com/sudoriaa/Atri-Games" rel="noreferrer" target="_blank" title="GitHub 开源仓库"><svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 .7a11.3 11.3 0 0 0-3.6 22c.6.1.8-.2.8-.5v-2.2c-3.3.7-4-1.4-4-1.4-.5-1.4-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1.1 1.8 2.8 1.3 3.5 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-6a4.7 4.7 0 0 1 1.2-3.2c-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2a11.4 11.4 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.6.2 2.8.1 3.1a4.7 4.7 0 0 1 1.2 3.2c0 4.7-2.9 5.7-5.6 6 .4.3.8 1 .8 2v3c0 .3.2.6.8.5A11.3 11.3 0 0 0 12 .7Z" /></svg> 开源仓库</a>
        </div>
        <small>© 2026 Atri Games · Built with curiosity.</small>
      </footer>
    </div>
  );
}

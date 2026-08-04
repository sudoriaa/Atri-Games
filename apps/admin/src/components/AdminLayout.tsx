import { ChevronLeft, ChevronRight, Flag, FolderTree, Gamepad2, Gauge, LogOut, Menu, Scale, ServerCog, UsersRound, X } from "lucide-react";
import { useEffect, useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useAdminAuth } from "../lib/auth";

const navigation = [
  { to: "/", label: "运营概览", icon: Gauge, end: true },
  { to: "/games", label: "游戏管理", icon: Gamepad2 },
  { to: "/categories", label: "分类管理", icon: FolderTree },
  { to: "/users", label: "用户管理", icon: UsersRound },
  { to: "/reports", label: "举报审核", icon: Flag },
  { to: "/appeals", label: "申诉审核", icon: Scale },
  { to: "/system", label: "系统状态", icon: ServerCog },
];

export function AdminLayout() {
  const { user, logout } = useAdminAuth();
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    if (!mobileOpen) return;

    const previousOverflow = document.body.style.overflow;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMobileOpen(false);
    };

    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [mobileOpen]);

  return (
    <div className={`admin-shell ${collapsed ? "is-collapsed" : ""}`}>
      <aside id="admin-navigation" className={`admin-sidebar ${mobileOpen ? "is-mobile-open" : ""}`}>
        <div className="admin-brand"><span className="admin-logo">A</span><span><b>ATRI</b><small>CONTROL ROOM</small></span></div>
        <button type="button" className="mobile-close" onClick={() => setMobileOpen(false)} aria-label="关闭菜单"><X /></button>
        <p className="nav-label">工作台</p>
        <nav>
          {navigation.map(({ to, label, icon: Icon, end }) => <NavLink end={end} key={to} to={to} onClick={() => setMobileOpen(false)}><Icon size={19} /><span>{label}</span></NavLink>)}
        </nav>
        <div className="sidebar-foot">
          <div className="admin-user"><span>{user?.displayName.slice(0, 1)}</span><div><b>{user?.displayName}</b><small>{user?.email}</small></div></div>
          <button type="button" onClick={logout} aria-label="退出管理端" title="退出管理端"><LogOut size={18} /></button>
        </div>
        <button type="button" className="collapse-button" onClick={() => setCollapsed((value) => !value)} aria-label={collapsed ? "展开侧栏" : "收起侧栏"}>{collapsed ? <ChevronRight /> : <ChevronLeft />}</button>
      </aside>
      {mobileOpen && <button type="button" className="admin-nav-scrim" onClick={() => setMobileOpen(false)} aria-label="关闭导航" />}
      <section className="admin-main">
        <header className="admin-mobile-header"><button type="button" onClick={() => setMobileOpen(true)} aria-label="打开导航" aria-expanded={mobileOpen} aria-controls="admin-navigation"><Menu /></button><b>ATRI CONTROL</b><span className="status-dot" title="API online" /></header>
        <Outlet />
      </section>
    </div>
  );
}

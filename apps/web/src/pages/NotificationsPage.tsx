import { Bell, CheckCheck } from "lucide-react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { ErrorState, LoadingState } from "../components/PageState";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function NotificationsPage() {
  const { api, user } = useAuth();
  const navigate = useNavigate();
  const notifications = useAsync(() => user ? api.notifications() : Promise.resolve({ items: [], unreadCount: 0 }), [api, user]);
  if (!user) return <Navigate to="/auth?next=/notifications" replace />;
  const changed = () => window.dispatchEvent(new Event("atri:notifications-changed"));
  const readAll = async () => { await api.readAllNotifications(); notifications.setData((current) => current ? { unreadCount: 0, items: current.items.map((item) => ({ ...item, read: true })) } : current); changed(); };
  const read = async (id: string) => { await api.readNotification(id); notifications.setData((current) => current ? { unreadCount: Math.max(0, current.unreadCount - (current.items.find((item) => item.id === id)?.read ? 0 : 1)), items: current.items.map((item) => item.id === id ? { ...item, read: true } : item) } : current); };
  const openNotification = async (id: string, link: string, alreadyRead: boolean) => { if (!alreadyRead) { await read(id); changed(); } navigate(link || "/notifications"); };
  return <div className="page-wrap notifications-page"><header className="page-intro page-intro--compact notifications-page__head"><div><p className="kicker"><Bell size={13} /> INBOX</p><h1>通知中心</h1><p>{notifications.data?.unreadCount ?? 0} 条未读通知</p></div>{Boolean(notifications.data?.unreadCount) && <button className="button button--ghost button--small" onClick={() => void readAll()}><CheckCheck size={15} /> 全部已读</button>}</header>{notifications.loading && <LoadingState />}{notifications.error && <ErrorState message={notifications.error} retry={notifications.reload} />}{notifications.data && (notifications.data.items.length ? <div className="notification-list">{notifications.data.items.map((item) => <Link key={item.id} to={item.link || "/notifications"} onClick={(event) => { event.preventDefault(); void openNotification(item.id, item.link, item.read); }} className={item.read ? "is-read" : ""}><span className="notification-list__dot" /><div><strong>{item.title}</strong><p>{item.body}</p><time>{new Date(item.createdAt).toLocaleString("zh-CN")}</time></div></Link>)}</div> : <div className="feed-empty"><Bell /><h2>暂时没有通知</h2><p>关注、评论回复和审核结果会集中出现在这里。</p></div>)}</div>;
}

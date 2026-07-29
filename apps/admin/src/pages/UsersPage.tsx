import type { UserRole, UserStatus } from "@atri/shared";
import { Search, Shield, UserCheck, UserX, X } from "lucide-react";
import { useMemo, useState } from "react";
import { AdminError, AdminLoading } from "../components/AdminState";
import { UserAvatar } from "../components/UserAvatar";
import { matchesUserQuery } from "../lib/admin-utils";
import { useAdminAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function UsersPage() {
  const { api, user: currentUser } = useAdminAuth();
  const users = useAsync(() => api.adminUsers(), [api]);
  const [query, setQuery] = useState("");
  const [notice, setNotice] = useState("");
  const [updatingId, setUpdatingId] = useState<string | null>(null);
  const filtered = useMemo(() => users.data?.filter((user) => matchesUserQuery(user, query)) ?? [], [query, users.data]);

  const change = async (id: string, role: UserRole, status: UserStatus) => {
    setUpdatingId(id);
    try {
      await api.updateUser(id, { role, status });
      setNotice("账户权限已更新");
      users.reload();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "更新失败");
    } finally {
      setUpdatingId(null);
    }
  };

  return (
    <div className="admin-page">
      <header className="admin-page-header">
        <div>
          <p className="admin-kicker">ACCESS / USERS</p>
          <h1>用户管理</h1>
          <p>控制平台账户状态与后台访问权限。</p>
        </div>
      </header>
      <div className="admin-toolbar">
        <label className="admin-search">
          <Search />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索昵称或邮箱" />
        </label>
        <span>{filtered.length} 个账户</span>
      </div>
      {notice && (
        <div className="admin-notice" aria-live="polite">
          {notice}
          <button type="button" onClick={() => setNotice("")} aria-label="关闭提示"><X /></button>
        </div>
      )}
      {users.loading && <AdminLoading />}
      {users.error && <AdminError message={users.error} retry={users.reload} />}
      {users.data && (
        <div className="admin-table-wrap">
          <table className="admin-table users-table">
            <thead>
              <tr><th>用户</th><th>角色</th><th>状态</th><th>加入时间</th><th>权限操作</th></tr>
            </thead>
            <tbody>
              {filtered.map((user) => {
                const isUpdating = updatingId === user.id;
                const isCurrent = currentUser?.id === user.id;
                return (
                  <tr key={user.id} className={isUpdating ? "is-updating" : ""} aria-busy={isUpdating || undefined}>
                    <td>
                      <div className="user-cell">
                        <UserAvatar name={user.displayName} src={user.avatarUrl} />
                        <div>
                          <b>{user.displayName}{isCurrent && <em>当前账户</em>}</b>
                          <small>{user.email}</small>
                          <small className="user-cell__number">用户 ID: {user.userNumber ?? "—"}</small>
                        </div>
                      </div>
                    </td>
                    <td>
                      <span className={`role-pill role-pill--${user.role}`}>
                        {user.role === "admin" ? <Shield /> : <UserCheck />}
                        {user.role === "admin" ? "管理员" : "普通用户"}
                      </span>
                    </td>
                    <td><span className={`status-pill status-pill--${user.status}`}>{user.status === "active" ? "正常" : "已停用"}</span></td>
                    <td>{new Date(user.createdAt).toLocaleDateString("zh-CN")}</td>
                    <td>
                      <div className="access-actions">
                        <select
                          aria-label={`设置 ${user.displayName} 的角色`}
                          value={user.role}
                          disabled={isCurrent || isUpdating}
                          onChange={(event) => change(user.id, event.target.value as UserRole, user.status)}
                        >
                          <option value="user">普通用户</option>
                          <option value="admin">管理员</option>
                        </select>
                        <button
                          type="button"
                          disabled={isCurrent || isUpdating}
                          className={user.status === "active" ? "danger" : "success"}
                          onClick={() => change(user.id, user.role, user.status === "active" ? "suspended" : "active")}
                        >
                          {user.status === "active" ? <><UserX />停用</> : <><UserCheck />恢复</>}
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {filtered.length === 0 && <div className="panel-empty">没有匹配的账户</div>}
        </div>
      )}
    </div>
  );
}

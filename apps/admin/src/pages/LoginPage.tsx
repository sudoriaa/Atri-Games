import { ArrowRight, KeyRound, Mail, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { useAdminAuth } from "../lib/auth";

export function LoginPage() {
  const { user, login } = useAdminAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  if (user?.role === "admin") return <Navigate to="/" replace />;

  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); setBusy(true); setError("");
    try { await login(email, password); navigate("/"); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "登录失败"); }
    finally { setBusy(false); }
  };

  return (
    <main className="admin-login">
      <section className="admin-login__intro"><span className="admin-login__logo">A</span><p>ATRI GAMES / INTERNAL</p><h1>Control<br />Room.</h1><blockquote>维护一个好目录，<br />意味着认真对待每一个入口。</blockquote><div className="security-note"><ShieldCheck /><span><b>独立管理入口</b>管理端使用单独会话与角色权限，操作会写入审计记录。</span></div></section>
      <section className="admin-login__form"><form onSubmit={submit}><p className="admin-kicker">AUTHENTICATION REQUIRED</p><h2>管理员登录</h2><p>输入管理账户，进入内容与用户控制台。</p><label><span>邮箱地址</span><div><Mail /><input required autoFocus autoComplete="username" type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="admin@example.com" /></div></label><label><span>密码</span><div><KeyRound /><input required autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="输入管理密码" /></div></label>{error && <div className="admin-form-error" role="alert">{error}</div>}<button type="submit" className="primary-action" disabled={busy}>{busy ? "验证中…" : "进入控制台"}<ArrowRight /></button><small>首次启动的默认账户可通过环境变量覆盖。</small></form></section>
    </main>
  );
}

import { ArrowLeft, ArrowRight, Gamepad2, LockKeyhole, Mail, UserRound } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Brand } from "../components/Brand";
import { useAuth } from "../lib/auth";

export function AuthPage() {
  const { login, register } = useAuth();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState({ email: "", password: "", displayName: "" });

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (mode === "login") await login({ email: form.email, password: form.password });
      else await register(form);
      navigate(params.get("next") || "/");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "操作没有完成");
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="auth-page">
      <section className="auth-art">
        <Link className="auth-back" to="/"><ArrowLeft size={16} /> 返回展馆</Link>
        <Brand />
        <div className="auth-art__disc"><Gamepad2 /><span>PLAYER<br />ONE</span></div>
        <blockquote>每一次登录，<br />都是回到未完待续的世界。</blockquote>
        <p>收藏喜欢的作品，记住每一个想再去一次的入口。</p>
      </section>
      <section className="auth-form-wrap">
        <div className="auth-form">
          <p className="kicker">PLAYER ACCESS</p>
          <h1>{mode === "login" ? "欢迎回来。" : "创建玩家档案。"}</h1>
          <p>{mode === "login" ? "登录后继续整理你的私人游戏书架。" : "只需要一分钟，之后遇到喜欢的游戏就能收藏。"}</p>
          <div className="auth-tabs"><button className={mode === "login" ? "active" : ""} onClick={() => { setMode("login"); setError(""); }}>登录</button><button className={mode === "register" ? "active" : ""} onClick={() => { setMode("register"); setError(""); }}>注册</button></div>
          <form onSubmit={submit}>
            {mode === "register" && <label><span>玩家昵称</span><div className="input-shell"><UserRound /><input required minLength={2} maxLength={40} value={form.displayName} onChange={(event) => setForm({ ...form, displayName: event.target.value })} placeholder="怎么称呼你？" /></div></label>}
            <label><span>邮箱</span><div className="input-shell"><Mail /><input required type="email" value={form.email} onChange={(event) => setForm({ ...form, email: event.target.value })} placeholder="you@example.com" /></div></label>
            <label><span>密码</span><div className="input-shell"><LockKeyhole /><input required type="password" minLength={8} value={form.password} onChange={(event) => setForm({ ...form, password: event.target.value })} placeholder="至少 8 位字符" /></div></label>
            {error && <div className="form-error">{error}</div>}
            <button className="button button--wide" disabled={busy}>{busy ? "正在进入…" : mode === "login" ? "进入我的书架" : "创建并进入"}<ArrowRight size={17} /></button>
          </form>
          <small>继续即表示你同意平台的服务条款与隐私说明。</small>
        </div>
      </section>
    </main>
  );
}

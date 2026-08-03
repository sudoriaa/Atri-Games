import { CalendarDays, Heart, Image, Link2, LogOut, Mail, Save, Trash2, Upload } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, Navigate } from "react-router-dom";
import { UserAvatar } from "../components/UserAvatar";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

const avatarFileTypes = new Set(["image/avif", "image/jpeg", "image/png", "image/webp"]);
const avatarFileName = /\.(avif|jpe?g|png|webp)$/i;

function isHttpsImageUrl(value: string) {
  try {
    return new URL(value).protocol === "https:";
  } catch {
    return false;
  }
}

function editableAvatarUrl(value?: string | null) {
  const normalized = value?.trim() ?? "";
  return isHttpsImageUrl(normalized) ? normalized : "";
}

export function ProfilePage() {
  const { api, user, logout, updateUser } = useAuth();
  const [displayName, setDisplayName] = useState(user?.displayName ?? "");
  const [avatarUrl, setAvatarUrl] = useState(() => editableAvatarUrl(user?.avatarUrl));
  const [avatarUrlDirty, setAvatarUrlDirty] = useState(false);
  const [avatarFile, setAvatarFile] = useState<File | null>(null);
  const [filePreviewUrl, setFilePreviewUrl] = useState("");
  const [avatarError, setAvatarError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const favorites = useAsync(() => user ? api.favorites() : Promise.resolve([]), [api, user]);

  useEffect(() => {
    setDisplayName(user?.displayName ?? "");
    setAvatarUrl(editableAvatarUrl(user?.avatarUrl));
    setAvatarUrlDirty(false);
    setAvatarFile(null);
    setAvatarError("");
  }, [user?.id]);

  useEffect(() => {
    if (!avatarFile) {
      setFilePreviewUrl("");
      return;
    }

    const objectUrl = URL.createObjectURL(avatarFile);
    setFilePreviewUrl(objectUrl);
    return () => URL.revokeObjectURL(objectUrl);
  }, [avatarFile]);

  if (!user) return <Navigate to="/auth?next=/profile" replace />;

  const normalizedAvatarUrl = avatarUrl.trim();
  const urlPreview = avatarUrlDirty ? (isHttpsImageUrl(normalizedAvatarUrl) ? normalizedAvatarUrl : "") : user.avatarUrl;
  const previewAvatarUrl = filePreviewUrl || urlPreview;
  const previewName = displayName.trim() || user.displayName;
  const userNumber = user.userNumber ?? "—";

  const selectAvatar = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = "";
    if (!file) return;

    if (!avatarFileTypes.has(file.type) && !avatarFileName.test(file.name)) {
      setAvatarError("请选择 JPG、PNG、WebP 或 AVIF 格式的图片。");
      return;
    }

    setAvatarFile(file);
    setAvatarUrl("");
    setAvatarUrlDirty(false);
    setAvatarError("");
  };

  const clearAvatar = () => {
    setAvatarFile(null);
    setAvatarUrl("");
    setAvatarUrlDirty(true);
    setAvatarError("");
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    const nextDisplayName = displayName.trim();
    const nextAvatarUrl = avatarUrl.trim();

    if (!nextDisplayName) {
      setAvatarError("显示昵称不能为空。");
      return;
    }
    if (avatarUrlDirty && nextAvatarUrl && !isHttpsImageUrl(nextAvatarUrl)) {
      setAvatarError("头像链接必须使用 HTTPS。");
      return;
    }
    if (!avatarFile && !avatarUrlDirty && nextDisplayName === user.displayName) {
      setMessage("没有需要保存的更改。");
      return;
    }

    setBusy(true);
    setMessage("");
    setAvatarError("");
    try {
      let updated;
      if (avatarFile) {
        updated = await api.uploadAvatar(avatarFile);
        if (nextDisplayName !== updated.displayName) {
          updated = await api.updateMe({ displayName: nextDisplayName });
        }
      } else {
        updated = await api.updateMe(
          avatarUrlDirty ? { displayName: nextDisplayName, avatarUrl: nextAvatarUrl } : { displayName: nextDisplayName },
        );
      }

      updateUser(updated);
      setDisplayName(updated.displayName);
      setAvatarUrl(editableAvatarUrl(updated.avatarUrl));
      setAvatarUrlDirty(false);
      setAvatarFile(null);
      setMessage("档案已经保存。");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "保存失败");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="page-wrap profile-page">
      <header className="profile-hero">
        <UserAvatar className="profile-avatar" name={previewName} src={previewAvatarUrl} decorative />
        <div>
          <p className="kicker">PLAYER PROFILE</p>
          <h1>{user.displayName}</h1>
          <p className="profile-user-number">用户 ID: {userNumber}</p>
          <p>你的 Atri 玩家档案与收藏概览。</p>
        </div>
      </header>
      <div className="profile-grid">
        <section className="profile-panel">
          <h2>基本信息</h2>
          <form onSubmit={save}>
            <fieldset className="profile-avatar-editor">
              <legend>头像</legend>
              <div className="profile-avatar-editor__preview">
                <UserAvatar className="profile-avatar profile-avatar--editor" name={previewName} src={previewAvatarUrl} decorative />
                <div>
                  <strong>{avatarFile ? "待上传的新头像" : avatarUrlDirty ? (avatarUrl.trim() ? "待保存的头像链接" : "待移除头像") : "当前头像"}</strong>
                  <small>上传图片后会在保存时更新；外部链接仅支持 HTTPS。</small>
                </div>
              </div>
              <div className="profile-avatar-editor__actions">
                <label className={`avatar-upload-control${busy ? " is-disabled" : ""}`}>
                  <Upload size={15} />
                  <span>上传图片</span>
                  <input type="file" accept="image/avif,image/jpeg,image/png,image/webp" onChange={selectAvatar} disabled={busy} />
                </label>
                <button
                  type="button"
                  className="avatar-clear-button"
                  onClick={clearAvatar}
                  disabled={busy || (!avatarFile && !avatarUrlDirty && !user.avatarUrl)}
                >
                  <Trash2 size={15} /> 清除头像
                </button>
              </div>
              {avatarFile && <p className="profile-avatar-editor__file"><Image size={15} /> {avatarFile.name}</p>}
              <label className="avatar-url-field">
                <span><Link2 size={14} /> 图片链接（HTTPS）</span>
                <input
                  type="url"
                  inputMode="url"
                  value={avatarUrl}
                  onChange={(event) => {
                    setAvatarUrl(event.target.value);
                    setAvatarUrlDirty(true);
                    setAvatarFile(null);
                    setAvatarError("");
                  }}
                  placeholder="https://example.com/avatar.png"
                  aria-describedby="avatar-url-help"
                  disabled={busy}
                />
                <small id="avatar-url-help">点击“清除头像”后保存可移除头像。平台托管的当前头像无需重复填写。</small>
              </label>
              {avatarError && <p className="avatar-field-error" role="alert">{avatarError}</p>}
            </fieldset>
            <label>
              <span>显示昵称</span>
              <input value={displayName} minLength={2} maxLength={40} onChange={(event) => setDisplayName(event.target.value)} disabled={busy} />
            </label>
            <label>
              <span>账户邮箱</span>
              <div className="readonly-row"><Mail size={17} />{user.email}</div>
            </label>
            <label>
              <span>用户编号</span>
              <div className="readonly-row readonly-row--number">用户 ID: {userNumber}</div>
            </label>
            {message && <p className="inline-notice" aria-live="polite">{message}</p>}
            <button className="button button--small" disabled={busy}><Save size={16} /> {busy ? "保存中…" : "保存资料"}</button>
          </form>
        </section>
        <aside className="profile-panel profile-panel--stats">
          <h2>玩家统计</h2>
          <div className="profile-stat"><Heart /><strong>{favorites.data?.length ?? 0}</strong><span>收藏的游戏</span></div>
          <div className="profile-stat"><CalendarDays /><strong>{new Date(user.createdAt).getFullYear()}</strong><span>加入年份</span></div>
          <Link className="text-link" to="/library">打开我的收藏 →</Link>
          <Link className="text-link" to="/my-games">管理我的游戏 →</Link>
        </aside>
      </div>
      <button className="danger-link" onClick={logout}><LogOut size={16} /> 退出当前账户</button>
    </div>
  );
}

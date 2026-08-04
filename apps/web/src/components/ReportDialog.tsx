import { Flag, Send, X } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import type { ReportTargetType } from "@atri/shared";
import { useAuth } from "../lib/auth";

export function ReportDialog({ targetType, targetId, targetLabel, onClose }: { targetType: ReportTargetType; targetId: string; targetLabel: string; onClose: () => void }) {
  const { api, user } = useAuth();
  const navigate = useNavigate();
  const [reason, setReason] = useState("不当内容");
  const [detail, setDetail] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!user) {
      navigate(`/auth?next=${encodeURIComponent(window.location.pathname)}`);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.reportContent({ targetType, targetId, reason, detail: detail.trim() });
      setDone(true);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "举报提交失败");
      setBusy(false);
    }
  };

  return (
    <div className="report-dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
      <section className="report-dialog" role="dialog" aria-modal="true" aria-labelledby="report-dialog-title">
        <header><div><p className="kicker"><Flag size={13} /> COMMUNITY SAFETY</p><h2 id="report-dialog-title">举报内容</h2></div><button className="icon-button" onClick={onClose} disabled={busy} aria-label="关闭"><X /></button></header>
        {done ? (
          <div className="report-dialog__done"><Flag /><h3>举报已提交</h3><p>管理员会在审核队列中查看记录和补充说明。</p><button className="button button--small" onClick={onClose}>完成</button></div>
        ) : (
          <form onSubmit={submit}>
            <p className="report-dialog__target">举报对象：<strong>{targetLabel}</strong></p>
            <label><span>原因</span><select value={reason} onChange={(event) => setReason(event.target.value)} disabled={busy}><option>不当内容</option><option>侵权或冒用</option><option>恶意行为</option><option>虚假或误导信息</option><option>垃圾内容</option><option>其他</option></select></label>
            <label><span>补充说明</span><textarea rows={5} maxLength={1000} value={detail} onChange={(event) => setDetail(event.target.value)} placeholder="请描述具体问题，便于管理员判断" disabled={busy} /></label>
            {error && <p className="avatar-field-error" role="alert">{error}</p>}
            <footer><button type="button" className="button button--ghost button--small" onClick={onClose} disabled={busy}>取消</button><button className="button button--small" disabled={busy}><Send size={14} /> {busy ? "提交中…" : "提交举报"}</button></footer>
          </form>
        )}
      </section>
    </div>
  );
}

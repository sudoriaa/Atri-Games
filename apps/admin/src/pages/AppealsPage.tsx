import type { AppealStatus, ModerationAppeal } from "@atri/shared";
import { CheckCircle2, CircleAlert, RotateCcw, Scale, SearchX } from "lucide-react";
import { useState } from "react";
import { AdminError, AdminLoading } from "../components/AdminState";
import { useAdminAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

const statusLabels: Record<AppealStatus, string> = { pending: "待复核", accepted: "已通过", rejected: "未通过" };

export function AppealsPage() {
  const { api } = useAdminAuth();
  const [status, setStatus] = useState<AppealStatus | "">("pending");
  const [notice, setNotice] = useState("");
  const appeals = useAsync(() => api.adminAppeals(status), [api, status]);
  return <div className="admin-page"><header className="admin-page-header admin-page-header--actions"><div><p className="admin-kicker">COMMUNITY / APPEALS</p><h1>申诉审核</h1><p>复核已经完成处置的举报决定；通过后原举报会重新进入待处理队列。</p></div><label className="select-shell"><select value={status} onChange={(event) => setStatus(event.target.value as AppealStatus | "")}><option value="">全部状态</option>{Object.entries(statusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label></header>{notice && <div className="admin-notice admin-notice--success"><CheckCircle2 />{notice}</div>}{appeals.loading && <AdminLoading />}{appeals.error && <AdminError message={appeals.error} retry={appeals.reload} />}{appeals.data && (appeals.data.length ? <div className="report-admin-list">{appeals.data.map((appeal) => <AppealCard key={appeal.id} appeal={appeal} resolved={(message) => { setNotice(message); appeals.reload(); }} />)}</div> : <div className="panel-empty report-admin-empty"><SearchX />当前筛选下没有申诉记录</div>)}</div>;
}

function AppealCard({ appeal, resolved }: { appeal: ModerationAppeal; resolved: (message: string) => void }) {
  const { api } = useAdminAuth();
  const [resolution, setResolution] = useState(appeal.resolution);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const settle = async (status: "accepted" | "rejected") => {
    if (!resolution.trim()) { setError("请填写复核说明"); return; }
    setBusy(true); setError("");
    try { await api.resolveAppeal(appeal.id, { status, resolution: resolution.trim() }); resolved(status === "accepted" ? "申诉已通过，举报已重新进入复核" : "申诉复核已结束"); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "申诉处置失败"); setBusy(false); }
  };
  return <article className={`report-admin-card appeal-admin-card report-admin-card--${appeal.status}`}><header><div><span className="report-target"><Scale />申诉</span><h2>{appeal.targetLabel}</h2></div><span className={`status-pill status-pill--${appeal.status === "pending" ? "review" : appeal.status === "accepted" ? "published" : "hidden"}`}>{statusLabels[appeal.status]}</span></header><dl><div><dt>申诉人</dt><dd>{appeal.appellantName}</dd></div><div><dt>原举报状态</dt><dd>{appeal.reportStatus === "resolved" ? "已处置" : appeal.reportStatus === "dismissed" ? "未采纳" : "待处理"}</dd></div><div><dt>提交时间</dt><dd>{new Date(appeal.createdAt).toLocaleString("zh-CN")}</dd></div></dl><p className="report-admin-card__detail"><RotateCcw />{appeal.reason}</p>{appeal.status === "pending" ? <div className="report-admin-card__resolution"><textarea rows={3} maxLength={1000} value={resolution} onChange={(event) => setResolution(event.target.value)} placeholder="记录复核依据和处理结论" disabled={busy} />{error && <p role="alert"><CircleAlert />{error}</p>}<div><button className="secondary-action" onClick={() => void settle("rejected")} disabled={busy}>维持原决定</button><button className="primary-action primary-action--compact" onClick={() => void settle("accepted")} disabled={busy}>同意重新复核</button></div></div> : <footer><b>复核记录</b><p>{appeal.resolution}</p><small>{appeal.resolvedByName || "管理员"} · {new Date(appeal.updatedAt).toLocaleString("zh-CN")}</small></footer>}</article>;
}

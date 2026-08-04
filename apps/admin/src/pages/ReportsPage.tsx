import type { ContentReport, ReportStatus } from "@atri/shared";
import { CheckCircle2, CircleAlert, Flag, SearchX } from "lucide-react";
import { useState } from "react";
import { AdminError, AdminLoading } from "../components/AdminState";
import { useAdminAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

const statusLabels: Record<ReportStatus, string> = { pending: "待处理", resolved: "已处置", dismissed: "已驳回" };
const targetLabels = { game: "游戏", comment: "留言", creator: "创作者" } as const;

export function ReportsPage() {
  const { api } = useAdminAuth();
  const [status, setStatus] = useState<ReportStatus | "">("pending");
  const [notice, setNotice] = useState("");
  const reports = useAsync(() => api.adminReports(status), [api, status]);
  return <div className="admin-page"><header className="admin-page-header admin-page-header--actions"><div><p className="admin-kicker">COMMUNITY / MODERATION</p><h1>举报审核</h1><p>集中处理游戏、留言和创作者档案举报，并保留完整处置记录。</p></div><label className="select-shell"><select value={status} onChange={(event) => setStatus(event.target.value as ReportStatus | "")}><option value="">全部状态</option>{Object.entries(statusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label></header>{notice && <div className="admin-notice admin-notice--success"><CheckCircle2 />{notice}</div>}{reports.loading && <AdminLoading />}{reports.error && <AdminError message={reports.error} retry={reports.reload} />}{reports.data && (reports.data.length ? <div className="report-admin-list">{reports.data.map((report) => <ReportCard key={report.id} report={report} resolved={(message) => { setNotice(message); reports.reload(); }} />)}</div> : <div className="panel-empty report-admin-empty"><SearchX />当前筛选下没有举报记录</div>)}</div>;
}

function ReportCard({ report, resolved }: { report: ContentReport; resolved: (message: string) => void }) {
  const { api } = useAdminAuth();
  const [resolution, setResolution] = useState(report.resolution);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const settle = async (status: "resolved" | "dismissed") => {
    if (!resolution.trim()) { setError("请填写处置说明"); return; }
    setBusy(true); setError("");
    try { await api.resolveReport(report.id, { status, resolution: resolution.trim() }); resolved(status === "resolved" ? "举报已处置" : "举报已驳回"); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "处置失败"); setBusy(false); }
  };
  return <article className={`report-admin-card report-admin-card--${report.status}`}><header><div><span className="report-target"><Flag />{targetLabels[report.targetType]}</span><h2>{report.targetLabel}</h2></div><span className={`status-pill status-pill--${report.status === "pending" ? "review" : report.status === "resolved" ? "published" : "hidden"}`}>{statusLabels[report.status]}</span></header><dl><div><dt>举报原因</dt><dd>{report.reason}</dd></div><div><dt>举报人</dt><dd>{report.reporterName}</dd></div><div><dt>提交时间</dt><dd>{new Date(report.createdAt).toLocaleString("zh-CN")}</dd></div></dl>{report.detail && <p className="report-admin-card__detail">{report.detail}</p>}{report.status === "pending" ? <div className="report-admin-card__resolution"><textarea rows={3} maxLength={1000} value={resolution} onChange={(event) => setResolution(event.target.value)} placeholder="记录核查结果、执行动作或驳回原因" disabled={busy} />{error && <p role="alert"><CircleAlert />{error}</p>}<div><button className="secondary-action" onClick={() => void settle("dismissed")} disabled={busy}>驳回举报</button><button className="primary-action primary-action--compact" onClick={() => void settle("resolved")} disabled={busy}>确认处置</button></div></div> : <footer><b>处置记录</b><p>{report.resolution}</p><small>{report.resolvedByName || "管理员"} · {new Date(report.updatedAt).toLocaleString("zh-CN")}</small></footer>}</article>;
}

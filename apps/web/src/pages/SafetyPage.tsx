import type { ContentReport, ModerationAppeal } from "@atri/shared";
import { CircleAlert, Flag, Gavel, RotateCcw, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { Navigate } from "react-router-dom";
import { ErrorState, LoadingState } from "../components/PageState";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

const reportStatus = { pending: "待处理", resolved: "已处置", dismissed: "未采纳" } as const;
const appealStatus = { pending: "复核中", accepted: "已通过", rejected: "未通过" } as const;
const targetType = { game: "游戏", comment: "留言", creator: "创作者" } as const;

export function SafetyPage() {
  const { api, user } = useAuth();
  const reports = useAsync(() => user ? api.myReports() : Promise.resolve([]), [api, user]);
  if (!user) return <Navigate to="/auth?next=/safety" replace />;
  const appealed = (reportId: string, appeal: ModerationAppeal) => reports.setData((current) => current?.map((item) => item.id === reportId ? { ...item, appeal } : item) ?? current);
  return <div className="page-wrap safety-page"><header className="page-intro page-intro--compact"><p className="kicker"><ShieldCheck size={13} /> COMMUNITY GOVERNANCE</p><h1>社区治理记录</h1><p>查看你提交的举报、处理说明和申诉复核进度。</p></header>{reports.loading && <LoadingState />}{reports.error && <ErrorState message={reports.error} retry={reports.reload} />}{reports.data && (reports.data.length ? <div className="safety-report-list">{reports.data.map((report) => <SafetyReport key={report.id} report={report} appealed={appealed} />)}</div> : <div className="feed-empty"><ShieldCheck /><h2>还没有治理记录</h2><p>你提交的内容举报会集中显示在这里。</p></div>)}</div>;
}

function SafetyReport({ report, appealed }: { report: ContentReport; appealed: (reportId: string, appeal: ModerationAppeal) => void }) {
  const { api } = useAuth();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submit = async () => {
    const value = reason.trim();
    if (!value) { setError("请填写需要重新复核的原因"); return; }
    setBusy(true); setError("");
    try { appealed(report.id, await api.createAppeal(report.id, value)); setOpen(false); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "申诉提交失败"); setBusy(false); }
  };
  return <article className={`safety-report safety-report--${report.status}`}><header><div><span><Flag size={14} /> {targetType[report.targetType]}</span><h2>{report.targetLabel}</h2></div><b>{reportStatus[report.status]}</b></header><dl><div><dt>举报原因</dt><dd>{report.reason}</dd></div><div><dt>提交时间</dt><dd>{new Date(report.createdAt).toLocaleString("zh-CN")}</dd></div></dl>{report.detail && <p>{report.detail}</p>}{report.resolution && <section className="safety-resolution"><Gavel size={17} /><div><b>处理说明</b><p>{report.resolution}</p><small>{report.resolvedByName || "管理员"} · {new Date(report.updatedAt).toLocaleString("zh-CN")}</small></div></section>}{report.appeal ? <section className={`safety-appeal safety-appeal--${report.appeal.status}`}><RotateCcw size={17} /><div><b>申诉{appealStatus[report.appeal.status]}</b><p>{report.appeal.reason}</p>{report.appeal.resolution && <small>{report.appeal.resolution} · {report.appeal.resolvedByName || "管理员"}</small>}</div></section> : report.status !== "pending" && (open ? <div className="safety-appeal-form"><textarea rows={4} maxLength={1000} value={reason} onChange={(event) => setReason(event.target.value)} placeholder="说明为什么需要重新复核此处理结果" disabled={busy} />{error && <p role="alert"><CircleAlert size={15} /> {error}</p>}<div><button className="button button--ghost button--small" onClick={() => setOpen(false)} disabled={busy}>取消</button><button className="button button--small" onClick={() => void submit()} disabled={busy}>{busy ? "提交中…" : "提交申诉"}</button></div></div> : <button className="safety-appeal-button" onClick={() => setOpen(true)}><RotateCcw size={14} /> 申请复核</button>)}</article>;
}

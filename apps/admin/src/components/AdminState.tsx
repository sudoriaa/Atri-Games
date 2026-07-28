import { AlertTriangle, LoaderCircle, RefreshCw } from "lucide-react";

export function AdminLoading() { return <div className="admin-state" role="status"><LoaderCircle className="spin" /><span>正在同步数据</span></div>; }
export function AdminError({ message, retry }: { message: string; retry?: () => void }) { return <div className="admin-state admin-state--error" role="alert"><AlertTriangle /><b>数据载入失败</b><span>{message}</span>{retry && <button type="button" onClick={retry}><RefreshCw size={15} /> 重试</button>}</div>; }

import { CircleAlert, LoaderCircle, RefreshCcw } from "lucide-react";

export function LoadingState({ label = "正在装载游戏世界" }: { label?: string }) {
  return <div className="page-state"><LoaderCircle className="spin" /><p>{label}</p></div>;
}

export function ErrorState({ message, retry }: { message: string; retry?: () => void }) {
  return (
    <div className="page-state page-state--error">
      <CircleAlert />
      <h2>这段信号暂时中断了</h2>
      <p>{message}</p>
      {retry && <button className="button button--ghost" onClick={retry}><RefreshCcw size={16} /> 再试一次</button>}
    </div>
  );
}

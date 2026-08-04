import type { GameVersion } from "@atri/shared";
import { History, RotateCcw } from "lucide-react";

function versionDate(value: string) {
  return new Date(value).toLocaleDateString("zh-CN", { year: "numeric", month: "short", day: "numeric" });
}

export function VersionHistory({ versions, onRollback, busyId = "" }: { versions: GameVersion[]; onRollback?: (version: GameVersion) => void; busyId?: string }) {
  return (
    <section className="version-history">
      <header><div><p className="kicker"><History size={13} /> CHANGELOG</p><h2>版本记录</h2></div><span>{versions.length} 个版本</span></header>
      {versions.length === 0 ? <p className="version-history__empty">还没有版本记录。</p> : <div className="version-history__list">{versions.map((version, index) => (
        <article key={version.id} className={index === 0 ? "is-current" : ""}>
          <div className="version-history__marker" />
          <div className="version-history__body"><div className="version-history__title"><strong>v{version.version}</strong>{index === 0 && <span>当前</span>}<time>{versionDate(version.createdAt)}</time></div><p>{version.releaseNotes}</p><div className="version-history__changes">{version.changes.map((change) => <span key={change}>{change}</span>)}</div>{version.createdByName && <small>由 {version.createdByName} 记录</small>}</div>
          {onRollback && index > 0 && <button className="button button--ghost button--small" onClick={() => onRollback(version)} disabled={Boolean(busyId)}><RotateCcw size={14} /> {busyId === version.id ? "回滚中…" : "回滚"}</button>}
        </article>
      ))}</div>}
    </section>
  );
}

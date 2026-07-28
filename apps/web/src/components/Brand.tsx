import { Link } from "react-router-dom";

export function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <Link className={`brand ${compact ? "brand--compact" : ""}`} to="/" aria-label="Atri Games 首页">
      <span className="brand-mark" aria-hidden="true">
        <i />
        <i />
        <i />
      </span>
      <span>
        <b>ATRI</b>
        {!compact && <small>GAMES</small>}
      </span>
    </Link>
  );
}

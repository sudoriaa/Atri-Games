/**
 * 管理控制台地址。生产环境由 Caddy 挂在同源 `/admin/`；
 * 开发时管理端跑在独立 Vite 端口，可通过 VITE_ADMIN_URL 覆盖
 * （例如 http://localhost:5174/）。
 */
export const adminConsoleUrl = import.meta.env.VITE_ADMIN_URL ?? "/admin/";

import { ApiError } from "@atri/shared";

/**
 * 把接口错误格式化为“消息（HTTP 状态 · 错误码）”，
 * 让管理员在导入/上传失败时能直接看到具体原因与定位信息。
 */
export function apiErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const meta = error.status > 0 ? `HTTP ${error.status}` : "网络错误";
    const code = error.code && error.code !== "request_failed" ? ` · ${error.code}` : "";
    return `${error.message}（${meta}${code}）`;
  }
  return error instanceof Error ? error.message : "操作失败，请稍后重试";
}

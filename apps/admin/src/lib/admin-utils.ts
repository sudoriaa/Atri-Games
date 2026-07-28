export function parseTagInput(value: string): string[] {
  const seen = new Set<string>();

  return value
    .split(/[,，]/)
    .map((tag) => tag.trim())
    .filter((tag) => {
      const normalized = tag.toLocaleLowerCase();
      if (!tag || seen.has(normalized)) return false;
      seen.add(normalized);
      return true;
    });
}

export function matchesUserQuery(
  user: { displayName: string; email: string },
  query: string,
): boolean {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  if (!normalizedQuery) return true;

  return `${user.displayName} ${user.email}`.toLocaleLowerCase().includes(normalizedQuery);
}

export function healthEndpoint(apiBaseUrl: string): string {
  const normalizedBase = apiBaseUrl.trim().replace(/\/+$/, "");
  return `${normalizedBase || "/api/v1"}/health`;
}

const activityLabels: Record<string, string> = {
  "game.created": "创建游戏",
  "game.updated": "更新游戏",
  "game.unpublished": "下架游戏",
  "game.deleted": "彻底删除游戏",
  "category.created": "创建分类",
  "category.updated": "更新分类",
  "category.deleted": "删除分类",
  "user.access.updated": "变更用户权限",
};

export function adminActivityLabel(action: string): string {
  return activityLabels[action] ?? action;
}

export function gameDeleteConfirmations(title: string): readonly [string, string] {
  return [
    `确认彻底删除“${title}”？这会永久删除全部数据库关联数据，以及封面、游戏包等对应本地文件。`,
    `最后确认：永久删除“${title}”及其全部本地文件？此操作不可撤销。`,
  ];
}

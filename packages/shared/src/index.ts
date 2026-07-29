export type UserRole = "user" | "admin";
export type UserStatus = "active" | "suspended";
export type GameStatus = "draft" | "review" | "published" | "hidden";
export type GameIdentityMode = "none" | "optional" | "required";
export type GameStorageProvider = "none" | "sqlite";
export type GameStorageScope = "player-game" | "player" | "game";
export type GameMatchmakingProtocol = "websocket" | "sse" | "http";

export interface GameIdentityService {
  mode: GameIdentityMode;
}

export interface GameStorageService {
  provider: GameStorageProvider;
  scope: GameStorageScope;
}

export interface GameMatchmakingService {
  enabled: boolean;
  protocol: GameMatchmakingProtocol;
}

export interface GameServices {
  identity?: GameIdentityService;
  storage?: GameStorageService;
  matchmaking?: GameMatchmakingService;
}

export interface GamePlatformContext {
  gameId?: string;
  slug?: string;
  requiresLogin?: boolean;
  usesPlatformStorage?: boolean;
  matchmakingEnabled?: boolean;
}

export interface User {
  id: string;
  /** Immutable public account number. Internal IDs remain opaque strings. */
  userNumber: number;
  email: string;
  displayName: string;
  avatarUrl: string;
  role: UserRole;
  status: UserStatus;
  createdAt: string;
}

export interface Category {
  id: string;
  name: string;
  description: string;
  sortOrder: number;
  gameCount?: number;
}

export interface Game {
  id: string;
  slug: string;
  title: string;
  summary: string;
  description: string;
  authorName: string;
  coverUrl: string;
  launchUrl: string;
  launchOpenIn: "same-tab" | "new-tab";
  repositoryUrl?: string;
  engine: string;
  version: string;
  status: GameStatus;
  categoryId: string;
  categoryName: string;
  featured: boolean;
  networkRequired: boolean;
  ownBackend: boolean;
  services?: GameServices;
  /** Derived by the API from identity/storage/matchmaking declarations. */
  requiresLogin?: boolean;
  /** Legacy/API alias accepted while clients roll forward. */
  loginRequired?: boolean;
  usesPlatformStorage?: boolean;
  matchmakingEnabled?: boolean;
  tags: string[];
  playCount: number;
  favoriteCount: number;
  isFavorite: boolean;
  likeCount: number;
  isLiked: boolean;
  /** Root comments plus replies. */
  commentCount: number;
  shareCount: number;
  createdAt: string;
  updatedAt: string;
  publishedAt?: string;
}

/** One message in a game's discussion. Replies nest exactly one level deep. */
export interface GameComment {
  id: string;
  gameId: string;
  parentId?: string;
  authorId: string;
  authorUserNumber: number;
  authorName: string;
  authorAvatarUrl: string;
  authorRole: UserRole;
  body: string;
  likeCount: number;
  isLiked: boolean;
  replyCount: number;
  /** True when the viewer authored the message or is an administrator. */
  canDelete: boolean;
  createdAt: string;
  updatedAt: string;
  replies?: GameComment[];
}

export interface GameCommentList {
  items: GameComment[];
  /** Root comments only — the unit the pager walks. */
  total: number;
  page: number;
  pageSize: number;
}

export type GameShareChannel = "link" | "card" | "native" | "qrcode";

export interface GameLikeState {
  likeCount: number;
  isLiked: boolean;
}

export interface GameShareState {
  shareCount: number;
  channel: GameShareChannel;
}

export interface GameInput {
  slug: string;
  title: string;
  summary: string;
  description: string;
  authorName: string;
  coverUrl: string;
  launchUrl: string;
  launchOpenIn: "same-tab" | "new-tab";
  repositoryUrl?: string;
  engine: string;
  version: string;
  status: GameStatus;
  categoryId: string;
  featured: boolean;
  networkRequired: boolean;
  ownBackend: boolean;
  requiresLogin: boolean;
  usesPlatformStorage: boolean;
  matchmakingEnabled: boolean;
  tags: string[];
}

export interface GamePackageImportOptions {
  categoryId: string;
  status: GameStatus;
  replace?: boolean;
}

export interface GameListResponse {
  items: Game[];
  total: number;
  page: number;
  pageSize: number;
}

export interface DashboardMetrics {
  users: number;
  publishedGames: number;
  reviewGames: number;
  launchesToday: number;
  favorites: number;
}

export interface ActivityItem {
  id: string;
  action: string;
  entityType: string;
  entityId: string;
  actorName: string;
  detail: string;
  createdAt: string;
}

export interface AuthResponse {
  token: string;
  user: User;
}

export interface GameSessionTicket {
  ticket: string;
  expiresAt: string;
  game?: { id: string; slug: string };
  user?: { id: string; userNumber: number; displayName: string; avatarUrl: string };
}

export interface GameDataRecord<T = unknown> {
  key: string;
  value: T;
  updatedAt: string;
}

export type MatchmakingStatus = "waiting" | "queued" | "matched" | "cancelled" | "expired";

export interface MatchmakingTicket {
  ticketId: string;
  status: MatchmakingStatus;
  gameId: string;
  mode: string;
  region: string;
  createdAt: string;
  expiresAt: string;
  position?: number;
  matchId?: string;
}

export interface LaunchResponse {
  launchUrl: string;
  openIn: "same-tab" | "new-tab";
  gameTicket?: string;
  expiresAt?: string;
  gameTicketExpiresAt?: string;
  apiBase?: string;
  apiBaseUrl?: string;
}

export function gameRequiresLogin(game: Pick<Game, "requiresLogin" | "loginRequired" | "services">): boolean {
  if (typeof game.requiresLogin === "boolean") return game.requiresLogin;
  if (typeof game.loginRequired === "boolean") return game.loginRequired;
  const identity = game.services?.identity?.mode;
  const storage = game.services?.storage;
  return (
    identity === "required" ||
    (storage?.provider === "sqlite" && (storage.scope === "player" || storage.scope === "player-game")) ||
    game.services?.matchmaking?.enabled === true
  );
}

export function gameUsesPlatformStorage(game: Pick<Game, "usesPlatformStorage" | "services">): boolean {
  if (typeof game.usesPlatformStorage === "boolean") return game.usesPlatformStorage;
  return game.services?.storage?.provider === "sqlite";
}

export function gameUsesMatchmaking(game: Pick<Game, "matchmakingEnabled" | "services">): boolean {
  if (typeof game.matchmakingEnabled === "boolean") return game.matchmakingEnabled;
  return game.services?.matchmaking?.enabled === true;
}

export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly code: string,
  ) {
    super(message);
  }
}

interface ApiErrorBody {
  error?: {
    code?: string;
    message?: string;
  };
}

export class ApiClient {
  constructor(
    private readonly baseUrl = "/api/v1",
    private readonly getToken: () => string | null = () => null,
  ) {}

  private async request<T>(path: string, init: RequestInit = {}, tokenOverride?: string | null): Promise<T> {
    const headers = new Headers(init.headers);
    const token = tokenOverride === undefined ? this.getToken() : tokenOverride;
    if (token) headers.set("Authorization", `Bearer ${token}`);
    if (typeof init.body === "string" && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }

    const response = await fetch(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      const body = (await response.json().catch(() => ({}))) as ApiErrorBody;
      throw new ApiError(
        body.error?.message ?? "请求没有完成，请稍后重试",
        response.status,
        body.error?.code ?? "request_failed",
      );
    }

    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }

  register(input: { email: string; password: string; displayName: string }) {
    return this.request<AuthResponse>("/auth/register", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  login(input: { email: string; password: string }) {
    return this.request<AuthResponse>("/auth/login", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  me() {
    return this.request<User>("/me");
  }

  updateMe(input: { displayName?: string; avatarUrl?: string }) {
    return this.request<User>("/me", { method: "PATCH", body: JSON.stringify(input) });
  }

  uploadAvatar(avatar: Blob) {
    const body = new FormData();
    body.append("avatar", avatar, typeof File !== "undefined" && avatar instanceof File ? avatar.name : "avatar");
    return this.request<User>("/me/avatar", { method: "POST", body });
  }

  categories() {
    return this.request<Category[]>("/categories");
  }

  games(params: Record<string, string | number | boolean | undefined> = {}) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== "") query.set(key, String(value));
    });
    const suffix = query.size ? `?${query.toString()}` : "";
    return this.request<GameListResponse>(`/games${suffix}`);
  }

  game(slug: string) {
    return this.request<Game>(`/games/${encodeURIComponent(slug)}`);
  }

  launch(slug: string) {
    return this.request<LaunchResponse>(`/games/${encodeURIComponent(slug)}/launch`, {
      method: "POST",
    });
  }

  /**
   * Exchange the signed-in platform session for a short-lived, game-scoped
   * ticket. The launch flow may already provide this ticket in the game
   * context; this method is useful for embedded games and reconnects.
   */
  async gameTicket(slug: string) {
    try {
      return await this.request<GameSessionTicket>(`/games/${encodeURIComponent(slug)}/ticket`, { method: "POST" });
    } catch (error) {
      if (!(error instanceof ApiError) || error.status !== 404) throw error;
      return this.request<GameSessionTicket>(`/games/${encodeURIComponent(slug)}/session-ticket`, { method: "POST" });
    }
  }

  gameSessionTicket(slug: string) {
    return this.gameTicket(slug);
  }

  gameData<T = unknown>(slug: string, key: string, ticket?: string | null) {
    return this.request<GameDataRecord<T>>(
      `/games/${encodeURIComponent(slug)}/data/${encodeURIComponent(key)}`,
      {},
      ticket,
    );
  }

  setGameData<T = unknown>(slug: string, key: string, value: T, ticket?: string | null) {
    return this.request<GameDataRecord<T>>(
      `/games/${encodeURIComponent(slug)}/data/${encodeURIComponent(key)}`,
      { method: "PUT", body: JSON.stringify({ value }) },
      ticket,
    );
  }

  deleteGameData(slug: string, key: string, ticket?: string | null) {
    return this.request<void>(
      `/games/${encodeURIComponent(slug)}/data/${encodeURIComponent(key)}`,
      { method: "DELETE" },
      ticket,
    );
  }

  joinMatchmaking(
    slug: string,
    input: { mode?: string; region?: string } = {},
    ticket?: string | null,
  ) {
    return this.request<MatchmakingTicket>(
      `/games/${encodeURIComponent(slug)}/matchmaking/tickets`,
      { method: "POST", body: JSON.stringify(input) },
      ticket,
    );
  }

  matchmakingStatus(slug: string, ticketId: string, ticket?: string | null) {
    return this.request<MatchmakingTicket>(
      `/games/${encodeURIComponent(slug)}/matchmaking/tickets/${encodeURIComponent(ticketId)}`,
      {},
      ticket,
    );
  }

  cancelMatchmaking(slug: string, ticketId: string, ticket?: string | null) {
    return this.request<void>(
      `/games/${encodeURIComponent(slug)}/matchmaking/tickets/${encodeURIComponent(ticketId)}`,
      { method: "DELETE" },
      ticket,
    );
  }

  likeGame(slug: string) {
    return this.request<GameLikeState>(`/games/${encodeURIComponent(slug)}/likes`, { method: "POST" });
  }

  unlikeGame(slug: string) {
    return this.request<GameLikeState>(`/games/${encodeURIComponent(slug)}/likes`, { method: "DELETE" });
  }

  /** Counts one share. Anonymous visitors are allowed to record a share. */
  recordShare(slug: string, channel: GameShareChannel = "link") {
    return this.request<GameShareState>(`/games/${encodeURIComponent(slug)}/shares`, {
      method: "POST",
      body: JSON.stringify({ channel }),
    });
  }

  gameComments(slug: string, params: { page?: number; pageSize?: number } = {}) {
    const query = new URLSearchParams();
    if (params.page) query.set("page", String(params.page));
    if (params.pageSize) query.set("pageSize", String(params.pageSize));
    const suffix = query.size ? `?${query.toString()}` : "";
    return this.request<GameCommentList>(`/games/${encodeURIComponent(slug)}/comments${suffix}`);
  }

  addGameComment(slug: string, body: string, parentId?: string) {
    return this.request<GameComment>(`/games/${encodeURIComponent(slug)}/comments`, {
      method: "POST",
      body: JSON.stringify(parentId ? { body, parentId } : { body }),
    });
  }

  deleteGameComment(slug: string, commentId: string) {
    return this.request<void>(
      `/games/${encodeURIComponent(slug)}/comments/${encodeURIComponent(commentId)}`,
      { method: "DELETE" },
    );
  }

  likeGameComment(slug: string, commentId: string) {
    return this.request<GameLikeState>(
      `/games/${encodeURIComponent(slug)}/comments/${encodeURIComponent(commentId)}/likes`,
      { method: "POST" },
    );
  }

  unlikeGameComment(slug: string, commentId: string) {
    return this.request<GameLikeState>(
      `/games/${encodeURIComponent(slug)}/comments/${encodeURIComponent(commentId)}/likes`,
      { method: "DELETE" },
    );
  }

  favorites() {
    return this.request<Game[]>("/me/favorites");
  }

  addFavorite(gameId: string) {
    return this.request<void>(`/me/favorites/${encodeURIComponent(gameId)}`, { method: "POST" });
  }

  removeFavorite(gameId: string) {
    return this.request<void>(`/me/favorites/${encodeURIComponent(gameId)}`, { method: "DELETE" });
  }

  dashboard() {
    return this.request<DashboardMetrics>("/admin/dashboard");
  }

  adminActivity() {
    return this.request<ActivityItem[]>("/admin/activity");
  }

  adminGames(params: Record<string, string | number | boolean | undefined> = {}) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== "") query.set(key, String(value));
    });
    const suffix = query.size ? `?${query.toString()}` : "";
    return this.request<GameListResponse>(`/admin/games${suffix}`);
  }

  createGame(input: GameInput, cover?: Blob) {
    if (!cover) {
      return this.request<Game>("/admin/games", { method: "POST", body: JSON.stringify(input) });
    }
    const body = gameMutationFormData(input, cover);
    return this.request<Game>("/admin/games", { method: "POST", body });
  }

  importGamePackage(file: Blob, options: GamePackageImportOptions) {
    const body = new FormData();
    body.append("package", file, typeof File !== "undefined" && file instanceof File ? file.name : "game.atri");
    body.append("categoryId", options.categoryId);
    body.append("status", options.status);
    body.append("replace", String(options.replace ?? false));
    return this.request<Game>("/admin/games/import", { method: "POST", body });
  }

  updateGame(id: string, input: GameInput, cover?: Blob) {
    const body = cover ? gameMutationFormData(input, cover) : JSON.stringify(input);
    return this.request<Game>(`/admin/games/${encodeURIComponent(id)}`, {
      method: "PUT",
      body,
    });
  }

  unpublishGame(id: string) {
    return this.request<Game>(`/admin/games/${encodeURIComponent(id)}/unpublish`, {
      method: "POST",
    });
  }

  deleteGame(id: string) {
    return this.request<void>(`/admin/games/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  adminUsers() {
    return this.request<User[]>("/admin/users");
  }

  updateUser(id: string, input: { role: UserRole; status: UserStatus }) {
    return this.request<User>(`/admin/users/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  }

  adminCategories() {
    return this.request<Category[]>("/admin/categories");
  }

  createCategory(input: Omit<Category, "gameCount">) {
    return this.request<Category>("/admin/categories", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  updateCategory(id: string, input: Omit<Category, "id" | "gameCount">) {
    return this.request<Category>(`/admin/categories/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(input),
    });
  }

  deleteCategory(id: string) {
    return this.request<void>(`/admin/categories/${encodeURIComponent(id)}`, { method: "DELETE" });
  }
}

function gameMutationFormData(input: GameInput, cover: Blob): FormData {
  const body = new FormData();
  body.append("game", JSON.stringify(input));
  const filename = typeof File !== "undefined" && cover instanceof File ? cover.name : "cover";
  body.append("cover", cover, filename);
  return body;
}

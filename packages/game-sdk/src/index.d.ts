export type AtriGameDetails = Record<string, unknown>;

/**
 * The intentionally small player profile available to a game. It is for
 * display only and never grants account or platform API access.
 */
export interface AtriGameUser {
  id: string;
  /** Public sequential player number. It is not an authorization credential. */
  userNumber?: number;
  displayName?: string;
  avatarUrl?: string;
}

export interface AtriGameContext {
  gameId?: string;
  gameSlug?: string;
  slug?: string;
  userId?: string;
  user?: AtriGameUser | null;
  ticket?: string;
  gameTicket?: string;
  userToken?: string;
  platformToken?: string;
  apiBaseUrl?: string;
  parentOrigin?: string;
  returnUrl?: string;
  [key: string]: unknown;
}

export interface AtriGameOptions {
  context?: AtriGameContext;
  ticket?: string;
  apiBaseUrl?: string;
  fetch?: typeof globalThis.fetch;
}

export type AtriGameLifecycleEvent = "ready" | "pause" | "resume" | "exit";

export interface AtriGameEventMap {
  ready: AtriGameDetails;
  pause: undefined;
  resume: undefined;
  exit: undefined;
}

export type AtriGameEventListener<Event extends AtriGameLifecycleEvent> = (value: AtriGameEventMap[Event]) => void;

export type AtriGameContextErrorCode = "authentication_required" | "game_context_missing";

/** Base class for errors created by the SDK itself. */
export declare class AtriSdkError extends Error {
  readonly code: string;
  constructor(message: string, code?: string);
}

/** HTTP error returned by an Atri platform endpoint. */
export declare class AtriPlatformError extends AtriSdkError {
  readonly status: number;
  constructor(message: string, options?: { status?: number; code?: string });
}

/** Missing or unusable local game launch context. */
export declare class AtriGameContextError extends AtriSdkError {
  readonly code: AtriGameContextErrorCode;
  constructor(message: string, code: AtriGameContextErrorCode);
}

export interface GameDataRecord<T = unknown> {
  key: string;
  value: T;
  updatedAt: string;
}

export interface MatchmakingTicket {
  ticketId: string;
  status: "waiting" | "queued" | "matched" | "cancelled" | "expired" | string;
  gameId: string;
  mode: string;
  region: string;
  createdAt: string;
  expiresAt: string;
  position?: number;
  matchId?: string;
}

export declare class AtriGame {
  constructor(options?: AtriGameOptions);
  readonly gameId: string | undefined;
  readonly userId: string | undefined;
  readonly gameSlug: string | undefined;
  readonly ticket: string | undefined;
  readonly authenticated: boolean;
  readonly storage: {
    get<T = unknown>(key: string): Promise<GameDataRecord<T>>;
    set<T = unknown>(key: string, value: T): Promise<GameDataRecord<T>>;
    remove(key: string): Promise<void>;
  };
  readonly matchmaking: {
    join(input?: { mode?: string; region?: string }): Promise<MatchmakingTicket>;
    status(ticketId: string): Promise<MatchmakingTicket>;
    cancel(ticketId: string): Promise<void>;
  };
  readonly identity: {
    getTicket(): Promise<string | undefined>;
    getUser(): AtriGameUser | null;
  };
  ready(details?: AtriGameDetails): this;
  progress(value: number, details?: AtriGameDetails): this;
  score(value: number, details?: AtriGameDetails): this;
  exit(): void;
  fullscreen(element?: Element): Promise<boolean>;
  exitFullscreen(): Promise<boolean>;
  requestTicket(): Promise<string | undefined>;
  getData<T = unknown>(key: string): Promise<GameDataRecord<T>>;
  setData<T = unknown>(key: string, value: T): Promise<GameDataRecord<T>>;
  removeData(key: string): Promise<void>;
  joinMatchmaking(input?: { mode?: string; region?: string }): Promise<MatchmakingTicket>;
  matchmakingStatus(ticketId: string): Promise<MatchmakingTicket>;
  cancelMatchmaking(ticketId: string): Promise<void>;
  on<Event extends AtriGameLifecycleEvent>(event: Event, listener: AtriGameEventListener<Event>): () => void;
  on(event: string, listener: (value?: unknown) => void): () => void;
  off<Event extends AtriGameLifecycleEvent>(event: Event, listener: AtriGameEventListener<Event>): void;
  off(event: string, listener: (value?: unknown) => void): void;
  dispose(): void;
}

export declare function createAtriGame(options?: AtriGameOptions): AtriGame;
export declare const version: string;

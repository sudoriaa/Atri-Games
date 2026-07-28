export interface AtriGameContext {
  gameId?: string;
  gameSlug?: string;
  slug?: string;
  userId?: string;
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
    getUser(): unknown;
  };
  ready(details?: Record<string, unknown>): this;
  progress(value: number, details?: Record<string, unknown>): this;
  score(value: number, details?: Record<string, unknown>): this;
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
  on(event: string, listener: (value?: unknown) => void): () => void;
  off(event: string, listener: (value?: unknown) => void): void;
  dispose(): void;
}

export declare function createAtriGame(options?: AtriGameOptions): AtriGame;
export declare const version: string;

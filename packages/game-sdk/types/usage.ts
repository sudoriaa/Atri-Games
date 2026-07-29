import {
  AtriGameContextError,
  AtriPlatformError,
  createAtriGame,
  type AtriGameDetails,
  type AtriGameUser,
} from "../src/index.js";

// Compile this file as a consumer would use the published declarations.
const atri = createAtriGame();

const stop = atri.on("ready", (details) => {
  const typedDetails: AtriGameDetails = details;
  const build: unknown = typedDetails.build;
  void build;

  // @ts-expect-error ready details are not an empty lifecycle payload.
  const noPayload: undefined = details;
  void noPayload;
});

atri.on("pause", (value) => {
  const noPayload: undefined = value;
  void noPayload;
});

atri.on("custom-local-event", (value) => {
  const unknownPayload: unknown = value;
  void unknownPayload;
});

const player: AtriGameUser | null = atri.identity.getUser();
if (player) {
  const id: string = player.id;
  const number: number | undefined = player.userNumber;
  const name: string | undefined = player.displayName;
  const avatar: string | undefined = player.avatarUrl;
  void id;
  void number;
  void name;
  void avatar;
}

const platformError = new AtriPlatformError("Rate limited", { status: 429, code: "rate_limited" });
const status: number = platformError.status;
const code: string = platformError.code;
void status;
void code;

const contextError = new AtriGameContextError("Sign in first", "authentication_required");
const contextCode: "authentication_required" | "game_context_missing" = contextError.code;
void contextCode;

stop();

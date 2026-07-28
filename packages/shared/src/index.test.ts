import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./index";

const fetchMock = vi.fn<typeof fetch>();

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ApiClient", () => {
  it("builds query strings from defined, non-empty values", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ items: [], total: 0, page: 2, pageSize: 12 }));

    await new ApiClient().games({
      search: "co-op games",
      page: 2,
      featured: false,
      omitted: undefined,
      empty: "",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/games?search=co-op+games&page=2&featured=false",
      expect.any(Object),
    );
  });

  it("sends bearer authorization and JSON bodies", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ displayName: "Atri" }));

    await new ApiClient("https://api.example.test", () => "token-123").updateMe({
      displayName: "Atri",
    });

    const [url, init] = fetchMock.mock.calls[0]!;
    const headers = new Headers(init?.headers);
    expect(url).toBe("https://api.example.test/me");
    expect(init?.method).toBe("PATCH");
    expect(init?.body).toBe(JSON.stringify({ displayName: "Atri" }));
    expect(headers.get("Authorization")).toBe("Bearer token-123");
    expect(headers.get("Content-Type")).toBe("application/json");
  });

  it("throws structured API errors", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ error: { code: "token_expired", message: "Session expired" } }, 401),
    );

    const error = await new ApiClient().me().catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      message: "Session expired",
      status: 401,
      code: "token_expired",
    });
  });

  it("returns undefined for 204 responses", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));

    await expect(new ApiClient().removeFavorite("game/42")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/me/favorites/game%2F42",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("uses separate endpoints for unpublishing and permanently deleting games", async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ id: "game/42", status: "hidden" }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));

    const client = new ApiClient();
    await expect(client.unpublishGame("game/42")).resolves.toMatchObject({ status: "hidden" });
    await expect(client.deleteGame("game/42")).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/admin/games/game%2F42/unpublish",
      expect.objectContaining({ method: "POST" }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/v1/admin/games/game%2F42",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("uploads game packages as multipart data without overriding the boundary", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ id: "game-1", slug: "package-game" }, 201));

    const packageFile = new File(["PK"], "package-game.atri", { type: "application/zip" });
    await new ApiClient().importGamePackage(packageFile, {
      categoryId: "arcade",
      status: "review",
      replace: true,
    });

    const [url, init] = fetchMock.mock.calls[0]!;
    const body = init?.body as FormData;
    const headers = new Headers(init?.headers);
    expect(url).toBe("/api/v1/admin/games/import");
    expect(init?.method).toBe("POST");
    expect(body).toBeInstanceOf(FormData);
    expect(body.get("categoryId")).toBe("arcade");
    expect(body.get("status")).toBe("review");
    expect(body.get("replace")).toBe("true");
    expect(headers.has("Content-Type")).toBe(false);
  });

  it("uploads a selected game cover with create and update mutations", async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ id: "game-1", slug: "cover-game" }, 201))
      .mockResolvedValueOnce(jsonResponse({ id: "game-1", slug: "cover-game" }));

    const input = {
      slug: "cover-game",
      title: "Cover Game",
      summary: "A summary long enough for the game.",
      description: "Description",
      authorName: "Studio",
      coverUrl: "",
      launchUrl: "/demos/cover-game",
      launchOpenIn: "same-tab" as const,
      repositoryUrl: "",
      engine: "Canvas",
      version: "1.0.0",
      status: "draft" as const,
      categoryId: "arcade",
      featured: false,
      networkRequired: false,
      ownBackend: false,
      requiresLogin: false,
      usesPlatformStorage: false,
      matchmakingEnabled: false,
      tags: [],
    };
    const cover = new File(["png"], "cover.png", { type: "image/png" });
    const client = new ApiClient();
    await client.createGame(input, cover);
    await client.updateGame("game/1", input, cover);

    for (const [index, expectedURL] of [
      [0, "/api/v1/admin/games"],
      [1, "/api/v1/admin/games/game%2F1"],
    ] as const) {
      const [url, init] = fetchMock.mock.calls[index]!;
      const body = init?.body as FormData;
      expect(url).toBe(expectedURL);
      expect(body).toBeInstanceOf(FormData);
      expect(JSON.parse(String(body.get("game")))).toMatchObject({ slug: "cover-game", coverUrl: "" });
      expect(body.get("cover")).toBeInstanceOf(File);
      expect(new Headers(init?.headers).has("Content-Type")).toBe(false);
    }
  });
});

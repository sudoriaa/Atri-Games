package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sudoriaa/atri-games/apps/api/internal/config"
	"github.com/sudoriaa/atri-games/apps/api/internal/data"
	"github.com/sudoriaa/atri-games/apps/api/internal/security"
)

const (
	testAdminEmail    = "admin@example.test"
	testAdminPassword = "AdminPass123!"
	testUserPassword  = "PlayerPass123!"
)

var cachedAdminHash struct {
	sync.Once
	value string
	err   error
}

type testAPI struct {
	handler   http.Handler
	store     *data.Store
	assetRoot string
}

type authResponse struct {
	Token string    `json:"token"`
	User  data.User `json:"user"`
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newTestAPI(t *testing.T) *testAPI {
	return newTestAPIWithConfig(t, nil)
}

func newTestAPIWithConfig(t *testing.T, configure func(*config.Config)) *testAPI {
	t.Helper()

	cachedAdminHash.Do(func() {
		cachedAdminHash.value, cachedAdminHash.err = security.HashPassword(testAdminPassword)
	})
	if cachedAdminHash.err != nil {
		t.Fatalf("hash admin password: %v", cachedAdminHash.err)
	}

	store, err := data.Open(filepath.Join(t.TempDir(), "atri-games-test.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test store: %v", err)
		}
	})
	if err := store.MigrateAndSeed(testAdminEmail, cachedAdminHash.value); err != nil {
		t.Fatalf("migrate test store: %v", err)
	}

	assetRoot := t.TempDir()
	cfg := config.Config{
		Address:     "127.0.0.1:0",
		AssetRoot:   assetRoot,
		CORSOrigins: []string{"https://atri.example"},
	}
	if configure != nil {
		configure(&cfg)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(cfg, store, security.NewTokenManager("test-secret-with-enough-entropy", time.Hour), logger)
	return &testAPI{handler: server.http.Handler, store: store, assetRoot: assetRoot}
}

func (api *testAPI) request(t *testing.T, method, target string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	return response
}

func (api *testAPI) requestRaw(t *testing.T, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	return response
}

func decodeResponse[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %d %q: %v", response.Code, response.Body.String(), err)
	}
	return value
}

func requireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()

	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
}

func requireError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	requireStatus(t, response, status)
	body := decodeResponse[errorResponse](t, response)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q; body = %s", body.Error.Code, code, response.Body.String())
	}
	if body.Error.Message == "" {
		t.Fatal("error message is empty")
	}
}

func registerUser(t *testing.T, api *testAPI, email string) authResponse {
	t.Helper()

	response := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email":       email,
		"password":    testUserPassword,
		"displayName": "Test Player",
	}, "")
	requireStatus(t, response, http.StatusCreated)
	auth := decodeResponse[authResponse](t, response)
	if auth.Token == "" || auth.User.ID == "" {
		t.Fatalf("registration returned incomplete auth response: %+v", auth)
	}
	return auth
}

func loginAdmin(t *testing.T, api *testAPI) authResponse {
	t.Helper()

	response := api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    strings.ToUpper(testAdminEmail),
		"password": testAdminPassword,
	}, "")
	requireStatus(t, response, http.StatusOK)
	return decodeResponse[authResponse](t, response)
}

func TestAuthenticationAndAuthorization(t *testing.T) {
	api := newTestAPI(t)

	if !security.CheckPassword(dummyPasswordHash, "atri-dummy-password") {
		t.Fatal("dummy password hash is invalid")
	}
	requireError(t, api.request(t, http.MethodGet, "/api/v1/me", nil, ""), http.StatusUnauthorized, "authentication_required")
	requireError(t, api.requestRaw(t, http.MethodPost, "/api/v1/auth/register", `{"email":"a@example.test","password":"PlayerPass123!","displayName":"Player","extra":true}`, ""), http.StatusBadRequest, "invalid_json")
	requireError(t, api.requestRaw(t, http.MethodPost, "/api/v1/auth/register", `{} {}`, ""), http.StatusBadRequest, "invalid_json")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":       "long-password@example.test",
		"password":    strings.Repeat("x", 73),
		"displayName": "Long Password",
	}, ""), http.StatusUnprocessableEntity, "invalid_input")

	registerResponse := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":       "  PLAYER@Example.Test ",
		"password":    testUserPassword,
		"displayName": "  Player One  ",
	}, "")
	requireStatus(t, registerResponse, http.StatusCreated)
	if cache := registerResponse.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("registration cache control = %q, want no-store", cache)
	}
	if strings.Contains(registerResponse.Body.String(), "passwordHash") || strings.Contains(registerResponse.Body.String(), testUserPassword) {
		t.Fatalf("registration response leaked password material: %s", registerResponse.Body.String())
	}
	player := decodeResponse[authResponse](t, registerResponse)
	if player.User.Email != "player@example.test" || player.User.DisplayName != "Player One" || player.User.Role != "user" {
		t.Fatalf("unexpected normalized user: %+v", player.User)
	}

	meResponse := api.request(t, http.MethodGet, "/api/v1/me", nil, player.Token)
	requireStatus(t, meResponse, http.StatusOK)
	if me := decodeResponse[data.User](t, meResponse); me.ID != player.User.ID {
		t.Fatalf("me user ID = %q, want %q", me.ID, player.User.ID)
	}
	updateMeResponse := api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"displayName": "  Updated Player  ",
	}, player.Token)
	requireStatus(t, updateMeResponse, http.StatusOK)
	if updated := decodeResponse[data.User](t, updateMeResponse); updated.DisplayName != "Updated Player" {
		t.Fatalf("updated display name = %q, want %q", updated.DisplayName, "Updated Player")
	}
	requireError(t, api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"displayName": "x",
	}, player.Token), http.StatusUnprocessableEntity, "invalid_display_name")

	requireError(t, api.request(t, http.MethodGet, "/api/v1/me", nil, "not-a-token"), http.StatusUnauthorized, "invalid_token")
	requireError(t, api.request(t, http.MethodGet, "/api/v1/admin/dashboard", nil, player.Token), http.StatusForbidden, "admin_required")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":       "player@example.test",
		"password":    testUserPassword,
		"displayName": "Duplicate",
	}, ""), http.StatusConflict, "already_exists")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    player.User.Email,
		"password": "incorrect-password",
	}, ""), http.StatusUnauthorized, "invalid_credentials")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    "missing-user@example.test",
		"password": "incorrect-password",
	}, ""), http.StatusUnauthorized, "invalid_credentials")

	loginResponse := api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    strings.ToUpper(player.User.Email),
		"password": testUserPassword,
	}, "")
	requireStatus(t, loginResponse, http.StatusOK)
	if loggedIn := decodeResponse[authResponse](t, loginResponse); loggedIn.User.ID != player.User.ID {
		t.Fatalf("login user ID = %q, want %q", loggedIn.User.ID, player.User.ID)
	}

	admin := loginAdmin(t, api)
	requireStatus(t, api.request(t, http.MethodGet, "/api/v1/admin/dashboard", nil, admin.Token), http.StatusOK)
	usersResponse := api.request(t, http.MethodGet, "/api/v1/admin/users", nil, admin.Token)
	requireStatus(t, usersResponse, http.StatusOK)
	if users := decodeResponse[[]data.User](t, usersResponse); len(users) != 2 {
		t.Fatalf("admin users count = %d, want 2", len(users))
	}
	if strings.Contains(usersResponse.Body.String(), "passwordHash") || strings.Contains(usersResponse.Body.String(), testUserPassword) {
		t.Fatalf("admin users response leaked password material: %s", usersResponse.Body.String())
	}
	requireError(t, api.request(t, http.MethodPatch, "/api/v1/admin/users/"+player.User.ID, map[string]string{
		"role": "owner", "status": "active",
	}, admin.Token), http.StatusUnprocessableEntity, "invalid_access")
	requireError(t, api.request(t, http.MethodPatch, "/api/v1/admin/users/"+admin.User.ID, map[string]string{
		"role": "user", "status": "active",
	}, admin.Token), http.StatusConflict, "self_lockout")

	suspendResponse := api.request(t, http.MethodPatch, "/api/v1/admin/users/"+player.User.ID, map[string]string{
		"role": "user", "status": "suspended",
	}, admin.Token)
	requireStatus(t, suspendResponse, http.StatusOK)
	if suspended := decodeResponse[data.User](t, suspendResponse); suspended.Status != "suspended" {
		t.Fatalf("updated status = %q, want suspended", suspended.Status)
	}
	requireError(t, api.request(t, http.MethodGet, "/api/v1/me", nil, player.Token), http.StatusUnauthorized, "invalid_account")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": player.User.Email, "password": testUserPassword,
	}, ""), http.StatusForbidden, "account_suspended")
}

func TestPublicGameListingDetailAndLaunch(t *testing.T) {
	api := newTestAPI(t)

	listResponse := api.request(t, http.MethodGet, "/api/v1/games", nil, "")
	requireStatus(t, listResponse, http.StatusOK)
	if cache := listResponse.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("anonymous game list cache control = %q", cache)
	}
	if vary := listResponse.Header().Values("Vary"); !headerValuesContain(vary, "Authorization") {
		t.Fatalf("anonymous game list Vary = %#v, want Authorization", vary)
	}
	list := decodeResponse[data.GameList](t, listResponse)
	if list.Total < 1 || len(list.Items) != list.Total {
		t.Fatalf("unexpected seeded game list: %+v", list)
	}
	for _, game := range list.Items {
		if game.Status != "published" {
			t.Fatalf("public list exposed %q game %q", game.Status, game.ID)
		}
	}
	categoriesResponse := api.request(t, http.MethodGet, "/api/v1/categories", nil, "")
	requireStatus(t, categoriesResponse, http.StatusOK)
	if cache := categoriesResponse.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("anonymous category list cache control = %q", cache)
	}
	if categories := decodeResponse[[]data.Category](t, categoriesResponse); len(categories) == 0 {
		t.Fatal("public category list is empty")
	}

	featuredResponse := api.request(t, http.MethodGet, "/api/v1/games?featured=true&pageSize=100", nil, "")
	requireStatus(t, featuredResponse, http.StatusOK)
	featured := decodeResponse[data.GameList](t, featuredResponse)
	if featured.Total == 0 {
		t.Fatal("expected at least one featured seed game")
	}
	for _, game := range featured.Items {
		if !game.Featured {
			t.Fatalf("featured filter returned non-featured game %q", game.ID)
		}
	}

	pageResponse := api.request(t, http.MethodGet, "/api/v1/games?page=2&pageSize=2", nil, "")
	requireStatus(t, pageResponse, http.StatusOK)
	page := decodeResponse[data.GameList](t, pageResponse)
	if page.Page != 2 || page.PageSize != 2 || len(page.Items) > 2 {
		t.Fatalf("unexpected pagination response: %+v", page)
	}

	detailResponse := api.request(t, http.MethodGet, "/api/v1/games/neon-relay", nil, "")
	requireStatus(t, detailResponse, http.StatusOK)
	before := decodeResponse[data.Game](t, detailResponse)
	if before.Slug != "neon-relay" || before.IsFavorite {
		t.Fatalf("unexpected public game detail: %+v", before)
	}

	launchResponse := api.request(t, http.MethodPost, "/api/v1/games/neon-relay/launch", nil, "")
	requireStatus(t, launchResponse, http.StatusOK)
	launch := decodeResponse[map[string]string](t, launchResponse)
	if launch["launchUrl"] != before.LaunchURL {
		t.Fatalf("launch URL = %q, want %q", launch["launchUrl"], before.LaunchURL)
	}

	afterResponse := api.request(t, http.MethodGet, "/api/v1/games/neon-relay", nil, "")
	requireStatus(t, afterResponse, http.StatusOK)
	if after := decodeResponse[data.Game](t, afterResponse); after.PlayCount != before.PlayCount+1 {
		t.Fatalf("play count = %d, want %d", after.PlayCount, before.PlayCount+1)
	}
	admin := loginAdmin(t, api)
	dashboardResponse := api.request(t, http.MethodGet, "/api/v1/admin/dashboard", nil, admin.Token)
	requireStatus(t, dashboardResponse, http.StatusOK)
	if metrics := decodeResponse[data.DashboardMetrics](t, dashboardResponse); metrics.LaunchesToday != 1 {
		t.Fatalf("launches today = %d, want 1", metrics.LaunchesToday)
	}

	requireError(t, api.request(t, http.MethodGet, "/api/v1/games/missing-game", nil, ""), http.StatusNotFound, "not_found")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/games/missing-game/launch", nil, ""), http.StatusNotFound, "not_found")

	malformedAuth := httptest.NewRequest(http.MethodGet, "/api/v1/games", nil)
	malformedAuth.Header.Set("Authorization", "Basic abc")
	malformedResponse := httptest.NewRecorder()
	api.handler.ServeHTTP(malformedResponse, malformedAuth)
	requireError(t, malformedResponse, http.StatusUnauthorized, "invalid_token")
}

func TestFavoriteLifecycle(t *testing.T) {
	api := newTestAPI(t)
	player := registerUser(t, api, "favorite-player@example.test")

	requireError(t, api.request(t, http.MethodGet, "/api/v1/me/favorites", nil, ""), http.StatusUnauthorized, "authentication_required")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/me/favorites/game-does-not-exist", nil, player.Token), http.StatusNotFound, "not_found")

	addResponse := api.request(t, http.MethodPost, "/api/v1/me/favorites/game_neon", nil, player.Token)
	requireStatus(t, addResponse, http.StatusNoContent)
	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/me/favorites/game_neon", nil, player.Token), http.StatusNoContent)

	favoritesResponse := api.request(t, http.MethodGet, "/api/v1/me/favorites", nil, player.Token)
	requireStatus(t, favoritesResponse, http.StatusOK)
	favorites := decodeResponse[[]data.Game](t, favoritesResponse)
	if len(favorites) != 1 || favorites[0].ID != "game_neon" || !favorites[0].IsFavorite {
		t.Fatalf("unexpected favorites: %+v", favorites)
	}

	authenticatedDetail := api.request(t, http.MethodGet, "/api/v1/games/neon-relay", nil, player.Token)
	requireStatus(t, authenticatedDetail, http.StatusOK)
	if cache := authenticatedDetail.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("authenticated game cache control = %q, want no-store", cache)
	}
	authenticatedGame := decodeResponse[data.Game](t, authenticatedDetail)
	if !authenticatedGame.IsFavorite || authenticatedGame.FavoriteCount < 1 {
		t.Fatalf("favorite state missing from authenticated detail: %+v", authenticatedGame)
	}
	anonymousDetail := api.request(t, http.MethodGet, "/api/v1/games/neon-relay", nil, "")
	requireStatus(t, anonymousDetail, http.StatusOK)
	if cache := anonymousDetail.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("anonymous game detail cache control = %q, want no-store", cache)
	}
	anonymousGame := decodeResponse[data.Game](t, anonymousDetail)
	if anonymousGame.IsFavorite || anonymousGame.FavoriteCount != authenticatedGame.FavoriteCount {
		t.Fatalf("unexpected anonymous favorite state: %+v", anonymousGame)
	}

	requireStatus(t, api.request(t, http.MethodDelete, "/api/v1/me/favorites/game_neon", nil, player.Token), http.StatusNoContent)
	requireStatus(t, api.request(t, http.MethodDelete, "/api/v1/me/favorites/game_neon", nil, player.Token), http.StatusNoContent)
	emptyResponse := api.request(t, http.MethodGet, "/api/v1/me/favorites", nil, player.Token)
	requireStatus(t, emptyResponse, http.StatusOK)
	if favorites := decodeResponse[[]data.Game](t, emptyResponse); len(favorites) != 0 {
		t.Fatalf("favorites after removal = %+v", favorites)
	}
}

func TestAdminGameAndCategoryCRUD(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)

	categoryInput := map[string]any{
		"id": "test-category", "name": "Test Category", "description": "For API tests", "sortOrder": 55,
	}
	categoryResponse := api.request(t, http.MethodPost, "/api/v1/admin/categories", categoryInput, admin.Token)
	requireStatus(t, categoryResponse, http.StatusCreated)
	category := decodeResponse[data.Category](t, categoryResponse)
	if category.ID != "test-category" {
		t.Fatalf("created category ID = %q", category.ID)
	}
	requireError(t, api.request(t, http.MethodPost, "/api/v1/admin/categories", categoryInput, admin.Token), http.StatusConflict, "already_exists")

	updateCategoryResponse := api.request(t, http.MethodPut, "/api/v1/admin/categories/test-category", map[string]any{
		"name": "Updated Category", "description": "Updated by API test", "sortOrder": 56,
	}, admin.Token)
	requireStatus(t, updateCategoryResponse, http.StatusOK)
	updatedCategory := decodeResponse[data.Category](t, updateCategoryResponse)
	if updatedCategory.ID != "test-category" || updatedCategory.Name != "Updated Category" {
		t.Fatalf("unexpected updated category: %+v", updatedCategory)
	}
	adminCategoriesResponse := api.request(t, http.MethodGet, "/api/v1/admin/categories", nil, admin.Token)
	requireStatus(t, adminCategoriesResponse, http.StatusOK)
	if cache := adminCategoriesResponse.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("admin categories cache control = %q, want no-store", cache)
	}

	gameInput := data.GameInput{
		Slug:          "api-test-game",
		Title:         "  API Test Game  ",
		Summary:       "A reliable summary for the API test game.",
		Description:   "This game exists to exercise the complete administration lifecycle.",
		AuthorName:    "Test Studio",
		CoverURL:      "/covers/api-test.webp",
		LaunchURL:     "/demos/arcade/index.html?game=api-test-game",
		RepositoryURL: "https://example.test/repository",
		Engine:        "Canvas",
		Version:       "0.1.0",
		Status:        "draft",
		CategoryID:    category.ID,
		Tags:          []string{" test ", "api", "test", " "},
	}
	createGameResponse := api.request(t, http.MethodPost, "/api/v1/admin/games", gameInput, admin.Token)
	requireStatus(t, createGameResponse, http.StatusCreated)
	game := decodeResponse[data.Game](t, createGameResponse)
	if game.ID == "" || game.Status != "draft" || game.PublishedAt != "" ||
		game.Title != "API Test Game" || len(game.Tags) != 2 || game.Tags[0] != "test" || game.Tags[1] != "api" {
		t.Fatalf("unexpected created game: %+v", game)
	}

	requireError(t, api.request(t, http.MethodGet, "/api/v1/games/"+game.Slug, nil, ""), http.StatusNotFound, "not_found")
	adminListResponse := api.request(t, http.MethodGet, "/api/v1/admin/games?status=draft&query=API+Test&pageSize=100", nil, admin.Token)
	requireStatus(t, adminListResponse, http.StatusOK)
	adminList := decodeResponse[data.GameList](t, adminListResponse)
	if adminList.Total != 1 || len(adminList.Items) != 1 || adminList.Items[0].ID != game.ID {
		t.Fatalf("admin filter did not return created draft: %+v", adminList)
	}

	gameInput.Status = "published"
	gameInput.Title = "Published API Test Game"
	updateGameResponse := api.request(t, http.MethodPut, "/api/v1/admin/games/"+game.ID, gameInput, admin.Token)
	requireStatus(t, updateGameResponse, http.StatusOK)
	published := decodeResponse[data.Game](t, updateGameResponse)
	if published.Status != "published" || published.PublishedAt == "" {
		t.Fatalf("game was not published correctly: %+v", published)
	}
	requireStatus(t, api.request(t, http.MethodGet, "/api/v1/games/"+game.Slug, nil, ""), http.StatusOK)

	unpublishResponse := api.request(t, http.MethodPost, "/api/v1/admin/games/"+game.ID+"/unpublish", nil, admin.Token)
	requireStatus(t, unpublishResponse, http.StatusOK)
	unpublished := decodeResponse[data.Game](t, unpublishResponse)
	if unpublished.Status != "hidden" || unpublished.PublishedAt != published.PublishedAt {
		t.Fatalf("game was not unpublished without data loss: published=%+v unpublished=%+v", published, unpublished)
	}
	requireError(t, api.request(t, http.MethodGet, "/api/v1/games/"+game.Slug, nil, ""), http.StatusNotFound, "not_found")
	idempotentUnpublish := api.request(t, http.MethodPost, "/api/v1/admin/games/"+game.ID+"/unpublish", nil, admin.Token)
	requireStatus(t, idempotentUnpublish, http.StatusOK)
	if again := decodeResponse[data.Game](t, idempotentUnpublish); again.Status != "hidden" || again.PublishedAt != published.PublishedAt {
		t.Fatalf("idempotent unpublish changed retained state: %+v", again)
	}
	gameInput.Status = "hidden"
	gameInput.Title = "Edited While Unpublished"
	hiddenEditResponse := api.request(t, http.MethodPut, "/api/v1/admin/games/"+game.ID, gameInput, admin.Token)
	requireStatus(t, hiddenEditResponse, http.StatusOK)
	if edited := decodeResponse[data.Game](t, hiddenEditResponse); edited.Status != "hidden" || edited.PublishedAt != published.PublishedAt {
		t.Fatalf("editing unpublished game lost publication history: %+v", edited)
	}
	requireError(t, api.request(t, http.MethodPost, "/api/v1/admin/games/missing-game/unpublish", nil, admin.Token), http.StatusNotFound, "not_found")

	requireError(t, api.request(t, http.MethodDelete, "/api/v1/admin/categories/"+category.ID, nil, admin.Token), http.StatusConflict, "resource_in_use")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/admin/games", data.GameInput{
		Slug: "bad-url", Title: "Bad URL", Summary: "A long enough summary", Description: "Description",
		AuthorName: "Test", CoverURL: "/covers/test.webp", LaunchURL: `/\outside.example`,
		Engine: "Canvas", Version: "1.0.0", Status: "draft", CategoryID: category.ID,
	}, admin.Token), http.StatusUnprocessableEntity, "invalid_url")
	requireError(t, api.request(t, http.MethodPost, "/api/v1/admin/games", data.GameInput{
		Slug: "blank-description", Title: "Blank Description", Summary: strings.Repeat(" ", 12), Description: " ",
		AuthorName: "Test", CoverURL: "/covers/test.webp", LaunchURL: "/games/test",
		Engine: "Canvas", Version: "1.0.0", Status: "draft", CategoryID: category.ID,
	}, admin.Token), http.StatusUnprocessableEntity, "invalid_description")

	activityResponse := api.request(t, http.MethodGet, "/api/v1/admin/activity", nil, admin.Token)
	requireStatus(t, activityResponse, http.StatusOK)
	activities := decodeResponse[[]data.Activity](t, activityResponse)
	if len(activities) < 4 {
		t.Fatalf("expected category/game audit activity, got %+v", activities)
	}

	requireStatus(t, api.request(t, http.MethodDelete, "/api/v1/admin/games/"+game.ID, nil, admin.Token), http.StatusNoContent)
	requireError(t, api.request(t, http.MethodDelete, "/api/v1/admin/games/"+game.ID, nil, admin.Token), http.StatusNotFound, "not_found")
	requireStatus(t, api.request(t, http.MethodDelete, "/api/v1/admin/categories/"+category.ID, nil, admin.Token), http.StatusNoContent)
	requireError(t, api.request(t, http.MethodDelete, "/api/v1/admin/categories/"+category.ID, nil, admin.Token), http.StatusNotFound, "not_found")
}

func TestAdminImportsAndDeletesStaticGamePackage(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	manifest := `{
		"schemaVersion":2,
		"id":"package-game",
		"version":"1.2.3",
		"title":"Package Game",
		"summary":"A complete static package imported through the administration API.",
		"description":"This fixture proves that a framework-independent browser build can be installed and removed.",
		"authors":[{"name":"Package Team"}],
		"license":"MIT",
		"repository":"https://example.test/package-game",
		"engine":{"name":"WebAssembly","framework":"Custom"},
		"runtime":{"kind":"static","entry":"index.html","openIn":"new-tab","bridge":"optional"},
		"services":{"networkRequired":false,"ownBackend":false},
		"privacy":{"collectsPersonalData":false,"dataSummary":"No personal data is collected by this fixture."},
		"media":{"cover":"cover.webp"},
		"compatibility":{"devices":["desktop"],"inputs":["keyboard"],"orientation":"any"},
		"tags":["package","static"]
	}`
	archive := buildGamePackage(t, map[string]string{
		"atri-game.json":  manifest,
		"cover.webp":      "cover fixture",
		"game/index.html": "<!doctype html><title>Package Game</title>",
	})
	importResponse := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
	requireStatus(t, importResponse, http.StatusCreated)
	game := decodeResponse[data.Game](t, importResponse)
	if game.Slug != "package-game" || game.CoverURL != "/covers/package-game/cover.webp" ||
		game.LaunchURL != "/playables/package-game/index.html" || game.LaunchOpenIn != "new-tab" || game.Engine != "WebAssembly / Custom" {
		t.Fatalf("imported game = %+v", game)
	}
	if _, err := os.Stat(filepath.Join(api.assetRoot, "covers", "package-game", "cover.webp")); err != nil {
		t.Fatalf("imported cover missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(api.assetRoot, "playables", "package-game", "index.html")); err != nil {
		t.Fatalf("imported game entry missing: %v", err)
	}
	requireStatus(t, api.request(t, http.MethodGet, "/api/v1/games/package-game", nil, ""), http.StatusOK)
	launchResponse := api.request(t, http.MethodPost, "/api/v1/games/package-game/launch", nil, "")
	requireStatus(t, launchResponse, http.StatusOK)
	if launch := decodeResponse[map[string]string](t, launchResponse); launch["launchUrl"] != "/playables/package-game/index.html" || launch["openIn"] != "new-tab" {
		t.Fatalf("launch response = %+v", launch)
	}

	externalManifest := `{
		"$schema":"https://atri.games/schemas/game-manifest.schema.json",
		"schemaVersion":2,
		"id":"package-game",
		"version":"2.0.0",
		"title":"Package Game Online",
		"summary":"The same game can switch to an independently hosted runtime.",
		"description":"This replacement proves that a game can move from a static build to any external web stack.",
		"authors":[{"name":"Package Team"}],
		"license":"MIT",
		"engine":{"name":"Custom Web","framework":"Any backend"},
		"runtime":{"kind":"external","url":"https://games.example.test/package-game","openIn":"same-tab"},
		"services":{"networkRequired":true,"ownBackend":true,"realtime":["websocket"]},
		"privacy":{"collectsPersonalData":false,"dataSummary":"No personal data is collected by this fixture."},
		"media":{"cover":"cover.png"},
		"compatibility":{"devices":["desktop","mobile"],"inputs":["keyboard","touch"],"orientation":"any"},
		"tags":["package","external"],
		"ai":{"tools":["none"],"disclosure":"No generated content is used by this fixture."}
	}`
	externalArchive := buildGamePackage(t, map[string]string{
		"atri-game.json": externalManifest,
		"cover.png":      "external cover fixture",
	})
	replacement := api.importPackage(t, externalArchive, "arcade", "published", true, admin.Token)
	requireStatus(t, replacement, http.StatusOK)
	replaced := decodeResponse[data.Game](t, replacement)
	if replaced.LaunchURL != "https://games.example.test/package-game" || replaced.LaunchOpenIn != "same-tab" ||
		replaced.NetworkRequired != true || replaced.OwnBackend != true {
		t.Fatalf("external replacement = %+v", replaced)
	}
	if _, err := os.Stat(filepath.Join(api.assetRoot, "playables", "package-game")); !os.IsNotExist(err) {
		t.Fatalf("old static bundle remained after external replacement: %v", err)
	}

	duplicate := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
	requireError(t, duplicate, http.StatusConflict, "game_exists")
	requireStatus(t, api.request(t, http.MethodDelete, "/api/v1/admin/games/"+game.ID, nil, admin.Token), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(api.assetRoot, "covers", "package-game")); !os.IsNotExist(err) {
		t.Fatalf("cover directory remained after delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(api.assetRoot, "playables", "package-game")); !os.IsNotExist(err) {
		t.Fatalf("playable directory remained after delete: %v", err)
	}
}

func TestHeadersCORSAndMethodHandling(t *testing.T) {
	api := newTestAPI(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://atri.example")
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	requireStatus(t, response, http.StatusOK)
	if response.Header().Get("Access-Control-Allow-Origin") != "https://atri.example" {
		t.Fatalf("CORS origin = %q", response.Header().Get("Access-Control-Allow-Origin"))
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", response.Header().Get("X-Content-Type-Options"))
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/api/v1/games", nil)
	preflight.Header.Set("Origin", "https://atri.example")
	preflightResponse := httptest.NewRecorder()
	api.handler.ServeHTTP(preflightResponse, preflight)
	requireStatus(t, preflightResponse, http.StatusNoContent)
	if !strings.Contains(preflightResponse.Header().Get("Access-Control-Allow-Methods"), http.MethodPost) {
		t.Fatalf("preflight methods = %q", preflightResponse.Header().Get("Access-Control-Allow-Methods"))
	}

	requireStatus(t, api.request(t, http.MethodDelete, "/api/v1/health", nil, ""), http.StatusMethodNotAllowed)
}

func (api *testAPI) importPackage(t *testing.T, archive []byte, categoryID, status string, replace bool, token string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("package", "fixture.atri")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(archive); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"categoryId": categoryID,
		"status":     status,
		"replace":    fmt.Sprint(replace),
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/games/import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	return response
}

func buildGamePackage(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func headerValuesContain(values []string, want string) bool {
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), want) {
				return true
			}
		}
	}
	return false
}

func TestAuthLimiterUsesClientHostInsteadOfSourcePort(t *testing.T) {
	limiter := newAuthLimiter()
	handler := limiter.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for attempt := 1; attempt <= 30; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/login", nil)
		request.RemoteAddr = fmt.Sprintf("198.51.100.10:%d", 40000+attempt)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		requireStatus(t, response, http.StatusNoContent)
	}

	limitedRequest := httptest.NewRequest(http.MethodPost, "/login", nil)
	limitedRequest.RemoteAddr = "198.51.100.10:50000"
	limitedResponse := httptest.NewRecorder()
	handler.ServeHTTP(limitedResponse, limitedRequest)
	requireError(t, limitedResponse, http.StatusTooManyRequests, "rate_limited")
	if limitedResponse.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response omitted Retry-After")
	}

	otherClient := httptest.NewRequest(http.MethodPost, "/login", nil)
	otherClient.RemoteAddr = "198.51.100.11:40001"
	otherResponse := httptest.NewRecorder()
	handler.ServeHTTP(otherResponse, otherClient)
	requireStatus(t, otherResponse, http.StatusNoContent)
}

func TestAuthLimiterPrunesExpiredClients(t *testing.T) {
	limiter := newAuthLimiter()
	limiter.entries["expired-client"] = limitEntry{count: 1, reset: time.Now().Add(-time.Minute)}

	handler := limiter.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/login", nil)
	request.RemoteAddr = "198.51.100.20:40000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	requireStatus(t, response, http.StatusNoContent)

	limiter.mu.Lock()
	_, exists := limiter.entries["expired-client"]
	limiter.mu.Unlock()
	if exists {
		t.Fatal("expired limiter entry was not pruned")
	}
}

func TestWindowLimiterResetsAfterWindow(t *testing.T) {
	limiter := newWindowLimiter(2, time.Minute)
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if allowed, _ := limiter.allow("player|game", now); !allowed {
		t.Fatal("first request was rate limited")
	}
	if allowed, _ := limiter.allow("player|game", now.Add(time.Second)); !allowed {
		t.Fatal("second request was rate limited")
	}
	if allowed, retryAfter := limiter.allow("player|game", now.Add(2*time.Second)); allowed || retryAfter <= 0 {
		t.Fatalf("third request = allowed:%v retry:%v, want limited with retry", allowed, retryAfter)
	}
	if allowed, _ := limiter.allow("player|game", now.Add(time.Minute)); !allowed {
		t.Fatal("request after the window did not reset")
	}
}

func TestClientHost(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
	}{
		{name: "remote IPv4", remoteAddr: "198.51.100.10:42000", want: "198.51.100.10"},
		{name: "remote IPv6", remoteAddr: "[2001:db8::1]:42000", want: "2001:db8::1"},
		{name: "valid forwarded client", remoteAddr: "172.20.0.2:42000", forwarded: "203.0.113.7, 172.20.0.1", want: "203.0.113.7"},
		{name: "invalid forwarded client", remoteAddr: "172.20.0.2:42000", forwarded: "not-an-ip", want: "172.20.0.2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/login", nil)
			request.RemoteAddr = test.remoteAddr
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			if got := clientHost(request); got != test.want {
				t.Fatalf("clientHost() = %q, want %q", got, test.want)
			}
		})
	}
}

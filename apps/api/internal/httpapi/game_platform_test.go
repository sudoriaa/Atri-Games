package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/sudoriaa/atri-games/apps/api/internal/security"
)

func TestGameTicketStorageAndMatchmakingAPI(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	manifest := `{
		"schemaVersion":2,
		"id":"platform-fixture",
		"version":"1.0.0",
		"title":"Platform Fixture",
		"summary":"A fixture that exercises the built-in player platform services.",
		"description":"This package uses the built-in identity, SQLite storage and matchmaking APIs.",
		"authors":[{"name":"Atri Test"}],
		"license":"MIT",
		"engine":{"name":"Canvas"},
		"runtime":{"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"},
		"services":{
			"networkRequired":true,
			"ownBackend":false,
			"identity":{"mode":"required"},
			"storage":{"provider":"sqlite","scope":"player-game"},
			"matchmaking":{"enabled":true,"protocol":"http"}
		},
		"privacy":{"collectsPersonalData":false,"dataSummary":"No personal data is collected by this fixture."},
		"media":{"cover":"cover.webp"},
		"compatibility":{"devices":["desktop"],"inputs":["keyboard"],"orientation":"any"},
		"tags":["platform"]
	}`
	archive := buildGamePackage(t, map[string]string{
		"atri-game.json":  manifest,
		"cover.webp":      "cover",
		"game/index.html": "<!doctype html><title>Platform Fixture</title>",
	})
	importResponse := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
	requireStatus(t, importResponse, http.StatusCreated)
	game := decodeResponse[map[string]any](t, importResponse)
	if game["requiresLogin"] != true || game["usesPlatformStorage"] != true || game["matchmakingEnabled"] != true {
		t.Fatalf("catalog capability hints = %+v", game)
	}
	requireError(t, api.request(t, http.MethodPost, "/api/v1/games/platform-fixture/launch", nil, ""), http.StatusUnauthorized, "authentication_required")

	player := registerUser(t, api, "platform-api-player@example.test")
	launchResponse := api.request(t, http.MethodPost, "/api/v1/games/platform-fixture/launch", nil, player.Token)
	requireStatus(t, launchResponse, http.StatusOK)
	launchPayload := decodeResponse[map[string]any](t, launchResponse)
	if ticket, _ := launchPayload["gameTicket"].(string); ticket == "" {
		t.Fatalf("declared platform launch did not return a game ticket: %+v", launchPayload)
	}
	gameID, _ := game["id"].(string)
	if gameID == "" {
		t.Fatalf("imported game has no id: %+v", game)
	}
	ticketResponse := api.request(t, http.MethodPost, "/api/v1/games/platform-fixture/ticket", nil, player.Token)
	requireStatus(t, ticketResponse, http.StatusOK)
	ticketPayload := decodeResponse[struct {
		Ticket string `json:"ticket"`
	}](t, ticketResponse)
	if ticketPayload.Ticket == "" {
		t.Fatal("game ticket is empty")
	}
	refreshResponse := api.request(t, http.MethodPost, "/api/v1/games/platform-fixture/ticket/refresh", nil, ticketPayload.Ticket)
	requireStatus(t, refreshResponse, http.StatusOK)
	refreshedPayload := decodeResponse[struct {
		Ticket string `json:"ticket"`
	}](t, refreshResponse)
	if refreshedPayload.Ticket == "" {
		t.Fatal("refreshed game ticket is empty")
	}
	ticketPayload.Ticket = refreshedPayload.Ticket
	requireError(t, api.request(t, http.MethodGet, "/api/v1/me", nil, ticketPayload.Ticket), http.StatusUnauthorized, "invalid_token")

	// A platform JWT is not accepted by game data endpoints.
	requireError(t, api.request(t, http.MethodPut, "/api/v1/games/platform-fixture/data/progress", map[string]any{"value": map[string]any{"level": 1}}, player.Token), http.StatusUnauthorized, "invalid_game_ticket")
	dataResponse := api.request(t, http.MethodPut, "/api/v1/games/platform-fixture/data/progress", map[string]any{"value": map[string]any{"level": 3, "coins": 12}}, ticketPayload.Ticket)
	requireStatus(t, dataResponse, http.StatusOK)
	readResponse := api.request(t, http.MethodGet, "/api/v1/games/platform-fixture/data/progress", nil, ticketPayload.Ticket)
	requireStatus(t, readResponse, http.StatusOK)
	deleteResponse := api.request(t, http.MethodDelete, "/api/v1/games/platform-fixture/data/progress", nil, ticketPayload.Ticket)
	requireStatus(t, deleteResponse, http.StatusNoContent)

	storageOnlyTicket, _, err := security.NewTokenManager("test-secret-with-enough-entropy", time.Hour).
		IssueGameTicket(player.User.ID, gameID, "platform-fixture", []string{"storage"}, time.Minute)
	if err != nil {
		t.Fatalf("issue storage-only ticket: %v", err)
	}
	storageRefresh := api.request(t, http.MethodPost, "/api/v1/games/platform-fixture/ticket/refresh", nil, storageOnlyTicket)
	requireStatus(t, storageRefresh, http.StatusOK)
	storageOnlyTicket = decodeResponse[struct {
		Ticket string `json:"ticket"`
	}](t, storageRefresh).Ticket
	requireError(
		t,
		api.request(t, http.MethodPost, "/api/v1/games/platform-fixture/matchmaking/tickets", map[string]string{"mode": "ranked"}, storageOnlyTicket),
		http.StatusForbidden,
		"insufficient_game_scope",
	)

	matchResponse := api.request(t, http.MethodPost, "/api/v1/games/platform-fixture/matchmaking/tickets", map[string]string{"mode": "ranked", "region": "asia"}, ticketPayload.Ticket)
	requireStatus(t, matchResponse, http.StatusCreated)
	match := decodeResponse[map[string]any](t, matchResponse)
	ticketID, _ := match["ticketId"].(string)
	if ticketID == "" || match["status"] != "waiting" {
		t.Fatalf("first matchmaking ticket = %+v", match)
	}
	cancelResponse := api.request(t, http.MethodDelete, "/api/v1/games/platform-fixture/matchmaking/tickets/"+ticketID, nil, ticketPayload.Ticket)
	requireStatus(t, cancelResponse, http.StatusNoContent)
	requireStatus(t, api.request(t, http.MethodGet, "/api/v1/games/platform-fixture/data/progress", nil, ticketPayload.Ticket), http.StatusNotFound)
}

func TestPlainStaticPackageLaunchDoesNotIssueGameTicket(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	manifest := `{
		"schemaVersion":2,
		"id":"plain-static-fixture",
		"version":"1.0.0",
		"title":"Plain Static Fixture",
		"summary":"A package with no declared built-in player services.",
		"description":"This fixture intentionally relies only on normal static runtime defaults.",
		"authors":[{"name":"Atri Test"}],
		"license":"MIT",
		"engine":{"name":"HTML"},
		"runtime":{"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"},
		"services":{"networkRequired":false,"ownBackend":false},
		"privacy":{"collectsPersonalData":false,"dataSummary":"No personal data is collected by this fixture."},
		"media":{"cover":"cover.webp"},
		"compatibility":{"devices":["desktop"],"inputs":["keyboard"],"orientation":"any"},
		"tags":["static"]
	}`
	archive := buildGamePackage(t, map[string]string{
		"atri-game.json":  manifest,
		"cover.webp":      "cover",
		"game/index.html": "<!doctype html><title>Plain Static Fixture</title>",
	})
	importResponse := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
	requireStatus(t, importResponse, http.StatusCreated)

	player := registerUser(t, api, "plain-static-player@example.test")
	launchResponse := api.request(t, http.MethodPost, "/api/v1/games/plain-static-fixture/launch", nil, player.Token)
	requireStatus(t, launchResponse, http.StatusOK)
	launchPayload := decodeResponse[map[string]any](t, launchResponse)
	if _, exists := launchPayload["gameTicket"]; exists {
		t.Fatalf("plain static launch unexpectedly returned a game ticket: %+v", launchPayload)
	}
	if _, exists := launchPayload["apiBase"]; exists {
		t.Fatalf("plain static launch unexpectedly returned platform API context: %+v", launchPayload)
	}
}

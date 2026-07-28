package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestGamePlatformStorageAndMatchmaking(t *testing.T) {
	store := newTestStore(t)
	player, err := store.CreateUser("platform-player@example.test", "hash", "Platform Player")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	raw := `{
		"runtime":{"kind":"static"},
		"services":{
			"identity":{"mode":"required"},
			"storage":{"provider":"sqlite","scope":"player-game"},
			"matchmaking":{"enabled":true,"protocol":"websocket"}
		}
	}`
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, "game_neon", "platform-receipt", "static", raw); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	platform, err := store.GamePlatformBySlug("neon-relay", true)
	if err != nil {
		t.Fatalf("GamePlatformBySlug: %v", err)
	}
	if !platform.SessionAllowed() || !platform.RequiresLogin || !platform.UsesPlatformStorage || !platform.MatchmakingEnabled || platform.MatchmakingProtocol != "websocket" {
		t.Fatalf("unexpected platform capabilities: %+v", platform)
	}
	if scopes := platform.Scopes(); !reflect.DeepEqual(scopes, []string{"identity", "storage", "matchmaking"}) {
		t.Fatalf("platform scopes = %v, want identity/storage/matchmaking", scopes)
	}

	value := json.RawMessage(`{"level":3,"coins":12}`)
	item, err := store.PutGameData(platform, player.ID, "progress", value)
	if err != nil {
		t.Fatalf("PutGameData: %v", err)
	}
	if string(item.Value) != string(value) {
		t.Fatalf("stored value = %s, want %s", item.Value, value)
	}
	read, err := store.GetGameData(platform, player.ID, "progress")
	if err != nil || string(read.Value) != string(value) {
		t.Fatalf("GetGameData = %+v, %v", read, err)
	}
	if err := store.DeleteGameData(platform, player.ID, "progress"); err != nil {
		t.Fatalf("DeleteGameData: %v", err)
	}
	if _, err := store.GetGameData(platform, player.ID, "progress"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetGameData after delete = %v, want ErrNotFound", err)
	}

	first, err := store.CreateMatchTicket(platform, player.ID, "ranked", "asia")
	if err != nil {
		t.Fatalf("CreateMatchTicket first: %v", err)
	}
	if first.Status != "waiting" || first.ID == "" {
		t.Fatalf("first ticket = %+v", first)
	}
	if _, err := store.CreateMatchTicket(platform, player.ID, "ranked", "asia"); !errors.Is(err, ErrMatchTicketExists) {
		t.Fatalf("duplicate waiting ticket error = %v, want ErrMatchTicketExists", err)
	}
	other, err := store.CreateUser("platform-other@example.test", "hash", "Other")
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	second, err := store.CreateMatchTicket(platform, other.ID, "ranked", "asia")
	if err != nil {
		t.Fatalf("CreateMatchTicket second: %v", err)
	}
	if second.Status != "matched" || second.MatchID == "" {
		t.Fatalf("second ticket = %+v", second)
	}
	matched, err := store.MatchTicketByID(platform.GameID, player.ID, first.ID)
	if err != nil || matched.Status != "matched" || matched.MatchID != second.MatchID {
		t.Fatalf("matched first ticket = %+v, %v", matched, err)
	}
}

func TestStaticPackageDefaultsToSQLiteWithoutLoginRequirement(t *testing.T) {
	store := newTestStore(t)
	raw := `{"runtime":{"kind":"static"},"services":{"networkRequired":false,"ownBackend":false}}`
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, "game_neon", "platform-default-receipt", "static", raw); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	platform, err := store.GamePlatformBySlug("neon-relay", true)
	if err != nil {
		t.Fatalf("GamePlatformBySlug: %v", err)
	}
	if !platform.UsesPlatformStorage || platform.RequiresLogin || platform.SessionAllowed() {
		t.Fatalf("unexpected defaults: %+v", platform)
	}
	if scopes := platform.Scopes(); len(scopes) != 0 {
		t.Fatalf("default package scopes = %v, want none", scopes)
	}
}

func TestLegacyRequiresLoginDeclaresIdentityTicketCapability(t *testing.T) {
	store := newTestStore(t)
	raw := `{"runtime":{"kind":"static"},"platform":{"requiresLogin":true}}`
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, "game_neon", "legacy-login-receipt", "static", raw); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	platform, err := store.GamePlatformBySlug("neon-relay", true)
	if err != nil {
		t.Fatalf("GamePlatformBySlug: %v", err)
	}
	if !platform.IdentityDeclared || !platform.RequiresLogin || !platform.SessionAllowed() {
		t.Fatalf("legacy login capability was not declared: %+v", platform)
	}
	if scopes := platform.Scopes(); !reflect.DeepEqual(scopes, []string{"identity"}) {
		t.Fatalf("legacy login scopes = %v, want identity", scopes)
	}
}

func TestMigrationBackfillsImportedPackageCapabilityHints(t *testing.T) {
	store := newTestStore(t)
	raw := `{"runtime":{"kind":"static"},"services":{"networkRequired":false,"ownBackend":false}}`
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, "game_neon", "backfill-capability-receipt", "static", raw); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE games SET requires_login=1,platform_storage=0,matchmaking_enabled=1 WHERE id='game_neon'`); err != nil {
		t.Fatalf("set stale hints: %v", err)
	}
	if err := store.MigrateAndSeed("admin@example.test", "replacement-hash"); err != nil {
		t.Fatalf("MigrateAndSeed: %v", err)
	}
	game, err := store.GameByID("game_neon", "")
	if err != nil {
		t.Fatalf("GameByID: %v", err)
	}
	if game.RequiresLogin || !game.UsesPlatformStorage || game.MatchmakingEnabled {
		t.Fatalf("backfilled capability hints = %+v", game)
	}
}

func TestGameScopedSQLiteDataIsNotPlayerWritable(t *testing.T) {
	store := newTestStore(t)
	player, err := store.CreateUser("shared-data@example.test", "hash", "Shared Data")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	raw := `{"runtime":{"kind":"static"},"services":{"storage":{"provider":"sqlite","scope":"game"}}}`
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, "game_neon", "shared-data-receipt", "static", raw); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	platform, err := store.GamePlatformBySlug("neon-relay", true)
	if err != nil {
		t.Fatalf("GamePlatformBySlug: %v", err)
	}
	if _, err := store.PutGameData(platform, player.ID, "config", json.RawMessage(`{"enabled":true}`)); !errors.Is(err, ErrGameStorageDisabled) {
		t.Fatalf("PutGameData game scope error = %v, want ErrGameStorageDisabled", err)
	}
}

func TestGameDataEnforcesPerPlayerKeyQuota(t *testing.T) {
	store := newTestStore(t)
	player, err := store.CreateUser("quota-data@example.test", "hash", "Quota Data")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	raw := `{"runtime":{"kind":"static"},"services":{"storage":{"provider":"sqlite","scope":"player-game"}}}`
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, "game_neon", "quota-data-receipt", "static", raw); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	platform, err := store.GamePlatformBySlug("neon-relay", true)
	if err != nil {
		t.Fatalf("GamePlatformBySlug: %v", err)
	}
	for index := 0; index < maxGameDataKeys; index++ {
		if _, err := store.db.Exec(
			`INSERT INTO game_player_data(game_id,user_id,data_key,value_json) VALUES(?,?,?,?)`,
			platform.GameID, player.ID, fmt.Sprintf("key-%03d", index), `{"ok":true}`,
		); err != nil {
			t.Fatalf("seed quota key %d: %v", index, err)
		}
	}
	if _, err := store.PutGameData(platform, player.ID, "key-000", json.RawMessage(`{"ok":"updated"}`)); err != nil {
		t.Fatalf("updating an existing key at quota: %v", err)
	}
	if _, err := store.PutGameData(platform, player.ID, "one-too-many", json.RawMessage(`{"ok":true}`)); !errors.Is(err, ErrGameStorageQuota) {
		t.Fatalf("new key above quota error = %v, want ErrGameStorageQuota", err)
	}
}

func TestPlayerScopedDataRemainsIsolatedAcrossGames(t *testing.T) {
	store := newTestStore(t)
	player, err := store.CreateUser("player-scope@example.test", "hash", "Player Scope")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	raw := `{"runtime":{"kind":"static"},"services":{"storage":{"provider":"sqlite","scope":"player"}}}`
	for index, gameID := range []string{"game_neon", "game_circuit"} {
		receipts := []string{"player-scope-receipt-a", "player-scope-receipt-b"}
		if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, gameID, receipts[index], "static", raw); err != nil {
			t.Fatalf("insert package %s: %v", gameID, err)
		}
	}
	first, err := store.GamePlatformBySlug("neon-relay", true)
	if err != nil {
		t.Fatalf("first platform: %v", err)
	}
	second, err := store.GamePlatformBySlug("circuit-bloom", true)
	if err != nil {
		t.Fatalf("second platform: %v", err)
	}
	if _, err := store.PutGameData(first, player.ID, "settings", json.RawMessage(`{"theme":"dark"}`)); err != nil {
		t.Fatalf("PutGameData player scope: %v", err)
	}
	if _, err := store.GetGameData(second, player.ID, "settings"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second game read error = %v, want ErrNotFound", err)
	}
	read, err := store.GetGameData(first, player.ID, "settings")
	if err != nil || string(read.Value) != `{"theme":"dark"}` {
		t.Fatalf("first game player data = %+v, %v", read, err)
	}
}

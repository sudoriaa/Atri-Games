package data

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnpublishGameRetainsGameData(t *testing.T) {
	store := newTestStore(t)
	player, err := store.CreateUser("unpublish@example.test", "hash", "Unpublish Player")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.AddFavorite(player.ID, "game_neon"); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	if _, err := store.RecordLaunch("neon-relay", player.ID); err != nil {
		t.Fatalf("RecordLaunch: %v", err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO game_player_data(game_id,user_id,data_key,value_json) VALUES(?,?,?,?)`,
		"game_neon", player.ID, "progress", `{"level":2}`,
	); err != nil {
		t.Fatalf("seed game data: %v", err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO matchmaking_tickets(id,game_id,user_id,status,expires_at) VALUES(?,?,?,'waiting','2099-01-01T00:00:00Z')`,
		"match-unpublish", "game_neon", player.ID,
	); err != nil {
		t.Fatalf("seed matchmaking ticket: %v", err)
	}
	before, err := store.GameByID("game_neon", player.ID)
	if err != nil {
		t.Fatalf("GameByID before unpublish: %v", err)
	}

	hidden, err := store.UnpublishGame("usr_admin", before.ID)
	if err != nil {
		t.Fatalf("UnpublishGame: %v", err)
	}
	if hidden.Status != "hidden" || hidden.PublishedAt != before.PublishedAt ||
		hidden.PlayCount != before.PlayCount || hidden.FavoriteCount != before.FavoriteCount {
		t.Fatalf("unpublish changed retained data: before=%+v after=%+v", before, hidden)
	}
	if _, err := store.GameBySlug(before.Slug, "", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("public GameBySlug after unpublish error = %v, want ErrNotFound", err)
	}

	again, err := store.UnpublishGame("usr_admin", before.ID)
	if err != nil {
		t.Fatalf("idempotent UnpublishGame: %v", err)
	}
	if again.Status != "hidden" || again.PublishedAt != before.PublishedAt {
		t.Fatalf("idempotent unpublish changed game: %+v", again)
	}

	assertTableCount(t, store, "favorites", "game_id", before.ID, 1)
	assertTableCount(t, store, "play_events", "game_id", before.ID, 1)
	assertTableCount(t, store, "game_player_data", "game_id", before.ID, 1)
	assertTableCount(t, store, "matchmaking_tickets", "game_id", before.ID, 1)
}

func TestDeleteGameRemovesExclusiveAssetsAndAssociatedData(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	coverPath := writeTestAsset(t, root, "covers/exclusive-game.webp")
	demoIndex := writeTestAsset(t, root, "demos/exclusive-game/index.html")
	demoRoot := filepath.Dir(demoIndex)

	game := createLifecycleGame(t, store, "exclusive-game", "/covers/exclusive-game.webp", "/demos/exclusive-game/index.html?game=exclusive-game")
	player, err := store.CreateUser("delete@example.test", "hash", "Delete Player")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.AddFavorite(player.ID, game.ID); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	if _, err := store.RecordLaunch(game.Slug, player.ID); err != nil {
		t.Fatalf("RecordLaunch: %v", err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO game_player_data(game_id,user_id,data_key,value_json) VALUES(?,?,?,?)`,
		game.ID, player.ID, "progress", `{"level":4}`,
	); err != nil {
		t.Fatalf("seed game data: %v", err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO matchmaking_tickets(id,game_id,user_id,status,expires_at) VALUES(?,?,?,'waiting','2099-01-01T00:00:00Z')`,
		"match-delete", game.ID, player.ID,
	); err != nil {
		t.Fatalf("seed matchmaking ticket: %v", err)
	}

	if err := store.DeleteGame("usr_admin", game.ID, root); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}
	assertMissingPath(t, coverPath)
	assertMissingPath(t, demoRoot)
	if _, err := store.GameByID(game.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GameByID after delete error = %v, want ErrNotFound", err)
	}
	assertTableCount(t, store, "favorites", "game_id", game.ID, 0)
	assertTableCount(t, store, "play_events", "game_id", game.ID, 0)
	assertTableCount(t, store, "game_player_data", "game_id", game.ID, 0)
	assertTableCount(t, store, "matchmaking_tickets", "game_id", game.ID, 0)
	assertTableCount(t, store, "audit_logs", "entity_id", game.ID, 0)
}

func TestDeleteGameRetainsAssetsReferencedByAnotherGame(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	coverPath := writeTestAsset(t, root, "covers/shared-owner.webp")
	demoIndex := writeTestAsset(t, root, "demos/shared-owner/index.html")
	demoRoot := filepath.Dir(demoIndex)

	owner := createLifecycleGame(t, store, "shared-owner", "/covers/shared-owner.webp?revision=1", "/demos/shared-owner/index.html?game=owner")
	consumer := createLifecycleGame(t, store, "shared-consumer", "/covers/shared-owner.webp?revision=2", "/demos/shared-owner/alternate/entry.html?game=consumer")

	if err := store.DeleteGame("usr_admin", owner.ID, root); err != nil {
		t.Fatalf("DeleteGame asset owner: %v", err)
	}
	assertExistingPath(t, coverPath)
	assertExistingPath(t, demoRoot)

	if err := store.DeleteGame("usr_admin", consumer.ID, root); err != nil {
		t.Fatalf("DeleteGame non-owning consumer: %v", err)
	}
	assertExistingPath(t, coverPath)
	assertExistingPath(t, demoRoot)
}

func TestDeleteGameSucceedsWhenFinalTrashRemovalFails(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	coverPath := writeTestAsset(t, root, "covers/finalize-failure.webp")
	game := createLifecycleGame(t, store, "finalize-failure", "/covers/finalize-failure.webp", "https://games.example.test/index.html")

	store.removeAssets = func(string) error {
		return errors.New("injected final removal failure")
	}
	if err := store.DeleteGame("usr_admin", game.ID, root); err != nil {
		t.Fatalf("DeleteGame reported a post-commit cleanup failure: %v", err)
	}
	assertMissingPath(t, coverPath)
	if _, err := store.GameByID(game.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("game remained after committed deletion: %v", err)
	}
	assertExistingPath(t, filepath.Join(root, ".atri-trash"))
	store.removeAssets = os.RemoveAll
	if err := store.RecoverManagedAssets(root); err != nil {
		t.Fatalf("RecoverManagedAssets final cleanup: %v", err)
	}
	assertMissingPath(t, filepath.Join(root, ".atri-trash"))
}

func TestDeleteGameIgnoresUnsafeAndExternalAssetURLs(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	outsidePath := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.txt")
	if err := os.WriteFile(outsidePath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write outside fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outsidePath) })

	game := createLifecycleGame(t, store, "unsafe-assets", "/covers/../"+filepath.Base(outsidePath), "https://games.example.test/index.html")
	if err := store.DeleteGame("usr_admin", game.ID, root); err != nil {
		t.Fatalf("DeleteGame with unmanaged URLs: %v", err)
	}
	assertExistingPath(t, outsidePath)
}

func TestDeleteGameAssetStagingFailureRetainsDatabaseAndFiles(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	coverPath := writeTestAsset(t, root, "covers/staging-failure.webp")
	if err := os.WriteFile(filepath.Join(root, ".atri-trash"), []byte("blocks staging"), 0o600); err != nil {
		t.Fatalf("write trash blocker: %v", err)
	}
	game := createLifecycleGame(t, store, "staging-failure", "/covers/staging-failure.webp", "https://games.example.test/index.html")
	player, err := store.CreateUser("staging-failure@example.test", "hash", "Staging Failure")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.AddFavorite(player.ID, game.ID); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}

	if err := store.DeleteGame("usr_admin", game.ID, root); err == nil {
		t.Fatal("DeleteGame succeeded despite an unusable staging path")
	}
	assertExistingPath(t, coverPath)
	if _, err := store.GameByID(game.ID, ""); err != nil {
		t.Fatalf("game was deleted after asset staging failure: %v", err)
	}
	assertTableCount(t, store, "favorites", "game_id", game.ID, 1)
}

func TestDeleteGameDatabaseFailureRestoresStagedAssets(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	coverPath := writeTestAsset(t, root, "covers/rollback-delete.webp")
	demoIndex := writeTestAsset(t, root, "demos/rollback-delete/index.html")
	demoRoot := filepath.Dir(demoIndex)
	game := createLifecycleGame(t, store, "rollback-delete", "/covers/rollback-delete.webp", "/demos/rollback-delete/index.html")

	if _, err := store.db.Exec(`CREATE TRIGGER block_game_delete
		BEFORE DELETE ON games BEGIN SELECT RAISE(ABORT, 'blocked by test'); END`); err != nil {
		t.Fatalf("create delete blocker: %v", err)
	}

	if err := store.DeleteGame("usr_admin", game.ID, root); err == nil {
		t.Fatal("DeleteGame succeeded despite a database trigger failure")
	}
	assertExistingPath(t, coverPath)
	assertExistingPath(t, demoRoot)
	if _, err := store.GameByID(game.ID, ""); err != nil {
		t.Fatalf("game was deleted after database failure: %v", err)
	}
}

func TestDeleteGameOnlyRemovesSlugOwnedAssets(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	unownedCover := writeTestAsset(t, root, "covers/common.webp")
	unownedDemo := writeTestAsset(t, root, "demos/common/index.html")
	game := createLifecycleGame(t, store, "ownership-game", "/covers/common.webp", "/demos/common/index.html")

	assertTableCount(t, store, "game_assets", "game_id", game.ID, 0)
	if err := store.DeleteGame("usr_admin", game.ID, root); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}
	assertExistingPath(t, unownedCover)
	assertExistingPath(t, filepath.Dir(unownedDemo))
}

func TestGameAssetOwnershipHistorySurvivesUpdates(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	oldCover := writeTestAsset(t, root, "covers/history-old.webp")
	oldDemo := writeTestAsset(t, root, "demos/history-old/index.html")
	newCover := writeTestAsset(t, root, "covers/history-new/art/cover.webp")
	newDemo := writeTestAsset(t, root, "demos/history-new/index.html")
	commonCover := writeTestAsset(t, root, "covers/common-history.webp")
	commonDemo := writeTestAsset(t, root, "demos/common-history/index.html")

	game := createLifecycleGame(t, store, "history-old", "/covers/history-old.webp", "/demos/history-old/index.html")
	update := lifecycleGameInput("history-new", "/covers/history-new/art/cover.webp", "/demos/history-new/index.html")
	game, err := store.UpdateGame("usr_admin", game.ID, update)
	if err != nil {
		t.Fatalf("UpdateGame to new owned namespace: %v", err)
	}
	update.CoverURL = "/covers/common-history.webp"
	update.LaunchURL = "/demos/common-history/index.html"
	if _, err := store.UpdateGame("usr_admin", game.ID, update); err != nil {
		t.Fatalf("UpdateGame to unowned assets: %v", err)
	}

	assertTableCount(t, store, "game_assets", "game_id", game.ID, 4)
	if err := store.DeleteGame("usr_admin", game.ID, root); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}
	assertMissingPath(t, oldCover)
	assertMissingPath(t, filepath.Dir(oldDemo))
	assertMissingPath(t, newCover)
	assertMissingPath(t, filepath.Dir(newDemo))
	assertExistingPath(t, commonCover)
	assertExistingPath(t, filepath.Dir(commonDemo))
}

func TestDeleteGameRetainsAssetsInAnotherGamesOwnershipHistory(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	sharedCover := writeTestAsset(t, root, "covers/shared-history.webp")
	sharedDemo := writeTestAsset(t, root, "demos/shared-history/index.html")
	ownerCover := writeTestAsset(t, root, "covers/history-owner.webp")
	ownerDemo := writeTestAsset(t, root, "demos/history-owner/index.html")
	consumerCover := writeTestAsset(t, root, "covers/history-consumer.webp")
	consumerDemo := writeTestAsset(t, root, "demos/history-consumer/index.html")

	owner := createLifecycleGame(t, store, "shared-history", "/covers/shared-history.webp", "/demos/shared-history/index.html")
	var err error
	owner, err = store.UpdateGame(
		"usr_admin",
		owner.ID,
		lifecycleGameInput("history-owner", "/covers/history-owner.webp", "/demos/history-owner/index.html"),
	)
	if err != nil {
		t.Fatalf("rename owner: %v", err)
	}

	consumer := createLifecycleGame(t, store, "shared-history", "/covers/shared-history.webp", "/demos/shared-history/index.html")
	consumer, err = store.UpdateGame(
		"usr_admin",
		consumer.ID,
		lifecycleGameInput("history-consumer", "/covers/history-consumer.webp", "/demos/history-consumer/index.html"),
	)
	if err != nil {
		t.Fatalf("rename consumer: %v", err)
	}

	if err := store.DeleteGame("usr_admin", owner.ID, root); err != nil {
		t.Fatalf("delete first historical owner: %v", err)
	}
	assertExistingPath(t, sharedCover)
	assertExistingPath(t, filepath.Dir(sharedDemo))
	assertMissingPath(t, ownerCover)
	assertMissingPath(t, filepath.Dir(ownerDemo))

	if err := store.DeleteGame("usr_admin", consumer.ID, root); err != nil {
		t.Fatalf("delete final historical owner: %v", err)
	}
	assertMissingPath(t, sharedCover)
	assertMissingPath(t, filepath.Dir(sharedDemo))
	assertMissingPath(t, consumerCover)
	assertMissingPath(t, filepath.Dir(consumerDemo))
}

func TestManagedDemoURLAcceptsBundleRootForms(t *testing.T) {
	tests := []string{
		"/demos/root-form",
		"/demos/root-form/",
		"/demos/root-form/index.html",
		"/demos/root-form/nested/start.html?mode=test",
	}
	for _, rawURL := range tests {
		item, ok := managedAssetFromURL(rawURL, false)
		if !ok {
			t.Fatalf("managedAssetFromURL(%q) was not recognized", rawURL)
		}
		if item.relative != "demos/root-form" || !item.directory {
			t.Fatalf("managedAssetFromURL(%q) = %+v", rawURL, item)
		}
	}
}

func TestRecoverManagedAssetsHandlesBothCrashSides(t *testing.T) {
	t.Run("database row exists so assets are restored", func(t *testing.T) {
		store := newTestStore(t)
		root := t.TempDir()
		coverPath := writeTestAsset(t, root, "covers/crash-restore.webp")
		game := createLifecycleGame(t, store, "crash-restore", "/covers/crash-restore.webp", "https://games.example.test/index.html")
		items := ownedAssetsForTest("/covers/crash-restore.webp", "https://games.example.test/index.html", game.Slug)
		stage, err := stageManagedAssets(root, game.ID, items, os.RemoveAll)
		if err != nil {
			t.Fatalf("stageManagedAssets: %v", err)
		}
		assertMissingPath(t, coverPath)
		assertExistingPath(t, filepath.Join(stage.trashRoot, assetManifestName))

		if err := store.RecoverManagedAssets(root); err != nil {
			t.Fatalf("RecoverManagedAssets: %v", err)
		}
		assertExistingPath(t, coverPath)
		assertMissingPath(t, filepath.Join(root, ".atri-trash"))
	})

	t.Run("database row is gone so staged assets are finalized", func(t *testing.T) {
		store := newTestStore(t)
		root := t.TempDir()
		coverPath := writeTestAsset(t, root, "covers/crash-finalize.webp")
		game := createLifecycleGame(t, store, "crash-finalize", "/covers/crash-finalize.webp", "https://games.example.test/index.html")
		items := ownedAssetsForTest("/covers/crash-finalize.webp", "https://games.example.test/index.html", game.Slug)
		stage, err := stageManagedAssets(root, game.ID, items, os.RemoveAll)
		if err != nil {
			t.Fatalf("stageManagedAssets: %v", err)
		}
		if _, err := store.db.Exec(`DELETE FROM games WHERE id=?`, game.ID); err != nil {
			t.Fatalf("delete game fixture: %v", err)
		}

		if err := store.RecoverManagedAssets(root); err != nil {
			t.Fatalf("RecoverManagedAssets: %v", err)
		}
		assertMissingPath(t, coverPath)
		assertMissingPath(t, stage.trashRoot)
	})
}

func TestRollbackFailureKeepsManifestAndStagedAsset(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	coverPath := writeTestAsset(t, root, "covers/rollback-failure.webp")
	game := createLifecycleGame(t, store, "rollback-failure", "/covers/rollback-failure.webp", "https://games.example.test/index.html")
	items := ownedAssetsForTest("/covers/rollback-failure.webp", "https://games.example.test/index.html", game.Slug)
	stage, err := stageManagedAssets(root, game.ID, items, os.RemoveAll)
	if err != nil {
		t.Fatalf("stageManagedAssets: %v", err)
	}

	coversRoot := filepath.Dir(coverPath)
	if err := os.Remove(coversRoot); err != nil {
		t.Fatalf("remove original parent: %v", err)
	}
	if err := os.WriteFile(coversRoot, []byte("blocks restore"), 0o600); err != nil {
		t.Fatalf("block original parent: %v", err)
	}
	if err := stage.rollback(); err == nil {
		t.Fatal("rollback succeeded despite a blocked original parent")
	}
	assertExistingPath(t, filepath.Join(stage.trashRoot, assetManifestName))
	assertExistingPath(t, stage.items[0].staged)
}

func TestInitialGameSeedRunsOnlyForANewDatabase(t *testing.T) {
	t.Run("deleted seed does not return", func(t *testing.T) {
		store := newTestStore(t)
		if err := store.DeleteGame("usr_admin", "game_neon", t.TempDir()); err != nil {
			t.Fatalf("DeleteGame seed: %v", err)
		}
		if err := store.MigrateAndSeed("admin@example.test", "replacement-hash"); err != nil {
			t.Fatalf("MigrateAndSeed restart: %v", err)
		}
		if _, err := store.GameByID("game_neon", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted seed returned after restart: %v", err)
		}
	})

	t.Run("legacy database is preserved", func(t *testing.T) {
		store := newTestStore(t)
		if _, err := store.db.Exec(`DELETE FROM games`); err != nil {
			t.Fatalf("clear legacy games: %v", err)
		}
		if _, err := store.db.Exec(`DELETE FROM app_meta WHERE key='initial_game_seed_v1'`); err != nil {
			t.Fatalf("remove marker to model legacy database: %v", err)
		}
		if err := store.MigrateAndSeed("admin@example.test", "replacement-hash"); err != nil {
			t.Fatalf("MigrateAndSeed legacy database: %v", err)
		}
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM games`).Scan(&count); err != nil {
			t.Fatalf("count legacy games: %v", err)
		}
		if count != 0 {
			t.Fatalf("legacy database was reseeded with %d games", count)
		}
	})
}

func TestMigrateAndSeedMovesLegacySeedLaunchURLsToOwnedWrappers(t *testing.T) {
	store := newTestStore(t)
	const (
		gameID = "game_neon"
		oldURL = "/demos/arcade/index.html?game=neon-relay"
		newURL = "/demos/neon-relay/index.html"
	)
	if _, err := store.db.Exec(`UPDATE games SET launch_url=? WHERE id=?`, oldURL, gameID); err != nil {
		t.Fatalf("restore legacy launch URL: %v", err)
	}
	if _, err := store.db.Exec(`DELETE FROM game_assets WHERE game_id=? AND is_directory=1`, gameID); err != nil {
		t.Fatalf("remove wrapper ownership fixture: %v", err)
	}

	if err := store.MigrateAndSeed("admin@example.test", "replacement-hash"); err != nil {
		t.Fatalf("MigrateAndSeed: %v", err)
	}
	var launchURL string
	if err := store.db.QueryRow(`SELECT launch_url FROM games WHERE id=?`, gameID).Scan(&launchURL); err != nil {
		t.Fatalf("read migrated launch URL: %v", err)
	}
	if launchURL != newURL {
		t.Fatalf("migrated launch URL = %q, want %q", launchURL, newURL)
	}
	var owned int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM game_assets WHERE game_id=? AND path='demos/neon-relay' AND is_directory=1`,
		gameID,
	).Scan(&owned); err != nil {
		t.Fatalf("count migrated wrapper ownership: %v", err)
	}
	if owned != 1 {
		t.Fatalf("migrated wrapper ownership rows = %d, want 1", owned)
	}
}

func createLifecycleGame(t *testing.T, store *Store, slug, coverURL, launchURL string) Game {
	t.Helper()
	game, err := store.CreateGame("usr_admin", lifecycleGameInput(slug, coverURL, launchURL))
	if err != nil {
		t.Fatalf("CreateGame %s: %v", slug, err)
	}
	return game
}

func lifecycleGameInput(slug, coverURL, launchURL string) GameInput {
	return GameInput{
		Slug:        slug,
		Title:       "Lifecycle " + slug,
		Summary:     "A lifecycle test game summary.",
		Description: "A lifecycle test game description.",
		AuthorName:  "Lifecycle Tests",
		CoverURL:    coverURL,
		LaunchURL:   launchURL,
		Engine:      "Canvas",
		Version:     "1.0.0",
		Status:      "published",
		CategoryID:  "arcade",
	}
}

func ownedAssetsForTest(coverURL, launchURL, slug string) map[string]managedAsset {
	items := make(map[string]managedAsset)
	addOwnedManagedAsset(items, coverURL, slug, true)
	addOwnedManagedAsset(items, launchURL, slug, false)
	return items
}

func writeTestAsset(t *testing.T, root, relative string) string {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("create asset directory: %v", err)
	}
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write asset fixture: %v", err)
	}
	return target
}

func assertTableCount(t *testing.T, store *Store, table, column, value string, want int) {
	t.Helper()
	var count int
	query := "SELECT COUNT(*) FROM " + table + " WHERE " + column + "=?"
	if err := store.db.QueryRow(query, value).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s rows for %s = %d, want %d", table, value, count, want)
	}
}

func assertExistingPath(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected path %s to exist: %v", path, err)
	}
}

func assertMissingPath(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected path %s to be removed, got %v", path, err)
	}
}

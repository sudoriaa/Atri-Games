package data

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "store-test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := store.MigrateAndSeed("admin@example.test", "admin-password-hash"); err != nil {
		t.Fatalf("MigrateAndSeed: %v", err)
	}
	return store
}

func TestStoreCoreLifecycle(t *testing.T) {
	store := newTestStore(t)

	if err := store.MigrateAndSeed("admin@example.test", "replacement-hash"); err != nil {
		t.Fatalf("idempotent MigrateAndSeed: %v", err)
	}
	admin, err := store.UserByID("usr_admin")
	if err != nil {
		t.Fatalf("UserByID admin: %v", err)
	}
	if admin.UserNumber != 1 || admin.AvatarURL != "" {
		t.Fatalf("seed admin profile = %+v, want public user #1 without avatar", admin)
	}
	publicGames, err := store.Games(GameFilter{})
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	if publicGames.Total != 6 || publicGames.Page != 1 || publicGames.PageSize != 24 {
		t.Fatalf("unexpected seeded games: %+v", publicGames)
	}
	for _, game := range publicGames.Items {
		if game.Status != "published" {
			t.Fatalf("public Games returned %q game %q", game.Status, game.ID)
		}
	}

	categories, err := store.Categories()
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	wantCategoryIDs := []string{
		"arcade", "adventure", "puzzle", "rpg", "strategy", "simulation", "narrative", "card",
		"rhythm", "sports-racing", "shooter", "survival-horror", "sandbox", "casual-party", "multiplayer", "educational",
	}
	if len(categories) != len(wantCategoryIDs) {
		t.Fatalf("category count = %d, want %d", len(categories), len(wantCategoryIDs))
	}
	for index, wantID := range wantCategoryIDs {
		if categories[index].ID != wantID {
			t.Fatalf("category %d ID = %q, want %q", index, categories[index].ID, wantID)
		}
	}

	player, err := store.CreateUser("PLAYER@EXAMPLE.TEST", "player-hash", "Player")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if player.Email != "player@example.test" || player.CreatedAt == "" || player.UserNumber != 2 {
		t.Fatalf("unexpected player: %+v", player)
	}
	if _, err := store.CreateUser("player@example.test", "duplicate-hash", "Duplicate"); err == nil {
		t.Fatal("duplicate email was accepted")
	}
	found, err := store.UserByEmail("Player@Example.Test")
	if err != nil || found.ID != player.ID {
		t.Fatalf("case-insensitive UserByEmail = %+v, %v", found, err)
	}
	updatedPlayer, err := store.UpdateProfile(player.ID, "Updated Player", "https://images.example.test/player.webp")
	if err != nil || updatedPlayer.DisplayName != "Updated Player" || updatedPlayer.AvatarURL != "https://images.example.test/player.webp" {
		t.Fatalf("UpdateProfile = %+v, %v", updatedPlayer, err)
	}
	if _, err := store.UpdateProfile("missing-user", "Missing", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateProfile missing error = %v, want ErrNotFound", err)
	}
	users, err := store.ListUsers()
	if err != nil || len(users) != 2 {
		t.Fatalf("ListUsers = %d users, %v", len(users), err)
	}

	if _, err := store.UpdateUserAccess(admin.ID, admin.ID, "user", "active"); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("UpdateUserAccess last admin error = %v, want ErrLastAdmin", err)
	}
	updatedAccess, err := store.UpdateUserAccess(admin.ID, player.ID, "user", "suspended")
	if err != nil || updatedAccess.Status != "suspended" {
		t.Fatalf("UpdateUserAccess = %+v, %v", updatedAccess, err)
	}
	if _, err := store.UpdateUserAccess(admin.ID, "missing-user", "user", "active"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateUserAccess missing error = %v, want ErrNotFound", err)
	}

	category, err := store.CreateCategory(admin.ID, Category{
		ID: "store-test", Name: "Store Test", Description: "Store test category", SortOrder: 80,
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	category, err = store.UpdateCategory(admin.ID, category.ID, Category{
		Name: "Updated Store Test", Description: "Updated category", SortOrder: 81,
	})
	if err != nil || category.Name != "Updated Store Test" {
		t.Fatalf("UpdateCategory = %+v, %v", category, err)
	}
	if _, err := store.UpdateCategory(admin.ID, "missing-category", Category{Name: "Missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateCategory missing error = %v, want ErrNotFound", err)
	}

	input := GameInput{
		Slug:          "store-test-game",
		Title:         "Store Test Game",
		Summary:       "A summary for direct store testing.",
		Description:   "A detailed description for direct store testing.",
		AuthorName:    "Test Studio",
		CoverURL:      "/covers/store-test.webp",
		LaunchURL:     "/games/store-test",
		RepositoryURL: "https://example.test/store-test",
		Engine:        "Canvas",
		Version:       "1.0.0",
		Status:        "review",
		CategoryID:    category.ID,
	}
	game, err := store.CreateGame(admin.ID, "", input)
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	if game.Status != "review" || game.PublishedAt != "" || game.Tags == nil {
		t.Fatalf("unexpected created game: %+v", game)
	}
	if _, err := store.GameBySlug(game.Slug, "", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unpublished GameBySlug error = %v, want ErrNotFound", err)
	}
	reviewGames, err := store.Games(GameFilter{Admin: true, Status: "review", Query: "Store Test"})
	if err != nil || reviewGames.Total != 1 || reviewGames.Items[0].ID != game.ID {
		t.Fatalf("admin review Games = %+v, %v", reviewGames, err)
	}
	if err := store.AddFavorite(player.ID, game.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddFavorite unpublished error = %v, want ErrNotFound", err)
	}

	input.Status = "published"
	input.Tags = []string{"store", "test"}
	game, err = store.UpdateGame(admin.ID, game.ID, input)
	if err != nil {
		t.Fatalf("UpdateGame publish: %v", err)
	}
	if game.PublishedAt == "" || game.Status != "published" {
		t.Fatalf("published game lacks publication state: %+v", game)
	}
	if err := store.AddFavorite(player.ID, game.ID); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	if err := store.AddFavorite(player.ID, game.ID); err != nil {
		t.Fatalf("idempotent AddFavorite: %v", err)
	}
	if err := store.AddFavorite(player.ID, "missing-game"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddFavorite missing error = %v, want ErrNotFound", err)
	}
	favorites, err := store.FavoriteGames(player.ID)
	if err != nil || len(favorites) != 1 || favorites[0].ID != game.ID || !favorites[0].IsFavorite {
		t.Fatalf("FavoriteGames = %+v, %v", favorites, err)
	}
	if err := store.DeleteCategory(admin.ID, category.ID); err == nil {
		t.Fatal("deleted a category that is still in use")
	}

	beforeLaunches, err := store.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard before launch: %v", err)
	}
	if beforeLaunches.Favorites != 1 {
		t.Fatalf("Dashboard favorites = %d, want 1", beforeLaunches.Favorites)
	}
	launch, err := store.RecordLaunch(game.Slug, player.ID)
	if err != nil || launch.URL != game.LaunchURL || launch.OpenIn != "same-tab" {
		t.Fatalf("RecordLaunch = %+v, %v", launch, err)
	}
	if _, err := store.RecordLaunch("missing-game", player.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RecordLaunch missing error = %v, want ErrNotFound", err)
	}
	afterLaunches, err := store.Dashboard()
	if err != nil || afterLaunches.LaunchesToday != beforeLaunches.LaunchesToday+1 {
		t.Fatalf("Dashboard after launch = %+v, %v; before = %+v", afterLaunches, err, beforeLaunches)
	}

	if err := store.RemoveFavorite(player.ID, game.ID); err != nil {
		t.Fatalf("RemoveFavorite: %v", err)
	}
	if err := store.RemoveFavorite(player.ID, game.ID); err != nil {
		t.Fatalf("idempotent RemoveFavorite: %v", err)
	}

	publishedAt := game.PublishedAt
	input.Status = "hidden"
	game, err = store.UpdateGame(admin.ID, game.ID, input)
	if err != nil {
		t.Fatalf("UpdateGame hide: %v", err)
	}
	if game.PublishedAt != publishedAt {
		t.Fatalf("hidden game lost publishedAt history: got %q want %q", game.PublishedAt, publishedAt)
	}
	assetRoot := t.TempDir()
	if err := store.DeleteGame(admin.ID, game.ID, assetRoot); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}
	if err := store.DeleteGame(admin.ID, game.ID, assetRoot); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteGame missing error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteCategory(admin.ID, category.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	if err := store.DeleteCategory(admin.ID, category.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteCategory missing error = %v, want ErrNotFound", err)
	}

	activities, err := store.Activity(500)
	if err != nil || len(activities) < 4 {
		t.Fatalf("Activity = %d items, %v", len(activities), err)
	}
}

func TestMigrateAndSeedBackfillsExpandedCategories(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "category-backfill.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.Exec(`CREATE TABLE categories (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		sort_order INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create existing categories table: %v", err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO categories(id,name,description,sort_order) VALUES(?,?,?,?)`,
		"arcade", "自定义街机", "保留管理员配置", 777,
	); err != nil {
		t.Fatalf("insert existing category: %v", err)
	}

	if err := store.MigrateAndSeed("admin@example.test", "admin-password-hash"); err != nil {
		t.Fatalf("MigrateAndSeed: %v", err)
	}
	categories, err := store.Categories()
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(categories) != 16 {
		t.Fatalf("category count after backfill = %d, want 16", len(categories))
	}
	byID := make(map[string]Category, len(categories))
	for _, category := range categories {
		byID[category.ID] = category
	}
	if arcade := byID["arcade"]; arcade.Name != "自定义街机" || arcade.Description != "保留管理员配置" || arcade.SortOrder != 777 {
		t.Fatalf("existing category was overwritten: %+v", arcade)
	}
	if adventure := byID["adventure"]; adventure.Name != "动作冒险" || adventure.SortOrder != 15 {
		t.Fatalf("new category was not backfilled: %+v", adventure)
	}
}

func TestMigrateAddsLaunchOpenInToExistingGamesTable(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "legacy-store.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.db.Exec(`CREATE TABLE games (
		id TEXT PRIMARY KEY,
		slug TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		summary TEXT NOT NULL,
		description TEXT NOT NULL,
		author_name TEXT NOT NULL,
		cover_url TEXT NOT NULL,
		launch_url TEXT NOT NULL,
		repository_url TEXT NOT NULL DEFAULT '',
		engine TEXT NOT NULL,
		version TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'draft',
		category_id TEXT NOT NULL,
		featured INTEGER NOT NULL DEFAULT 0,
		network_required INTEGER NOT NULL DEFAULT 0,
		own_backend INTEGER NOT NULL DEFAULT 0,
		tags_json TEXT NOT NULL DEFAULT '[]',
		play_count INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		published_at TEXT
	)`); err != nil {
		t.Fatalf("create legacy games table: %v", err)
	}
	if err := store.MigrateAndSeed("admin@example.test", "admin-password-hash"); err != nil {
		t.Fatalf("MigrateAndSeed legacy database: %v", err)
	}
	found := map[string]bool{}
	rows, err := store.db.Query(`PRAGMA table_info(games)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		switch name {
		case "launch_open_in", "requires_login", "platform_storage", "matchmaking_enabled":
			found[name] = true
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"launch_open_in", "requires_login", "platform_storage", "matchmaking_enabled"} {
		if !found[name] {
			t.Fatalf("legacy games table was not migrated with %s", name)
		}
	}
	for _, table := range []string{"game_player_data", "matchmaking_tickets"} {
		var exists bool
		if err := store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("legacy database was not migrated with %s", table)
		}
	}
}

func TestMigrateBackfillsUserNumbersAndPreservesSequence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "legacy-users.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE COLLATE NOCASE,
		password_hash TEXT NOT NULL,
		display_name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy users table: %v", err)
	}
	for _, row := range []struct {
		id, email, name, createdAt string
	}{
		{"usr_existing_early", "early@example.test", "Early", "2024-01-01T00:00:00Z"},
		{"usr_existing_later", "later@example.test", "Later", "2024-01-02T00:00:00Z"},
	} {
		if _, err := store.db.Exec(`INSERT INTO users(id,email,password_hash,display_name,role,status,created_at) VALUES(?,?,?,?,?,?,?)`, row.id, row.email, "hash", row.name, "user", "active", row.createdAt); err != nil {
			t.Fatalf("insert legacy user %s: %v", row.id, err)
		}
	}

	if err := store.MigrateAndSeed("admin@example.test", "admin-password-hash"); err != nil {
		t.Fatalf("MigrateAndSeed legacy users: %v", err)
	}
	for _, want := range []struct {
		id     string
		number int64
	}{
		{"usr_admin", 1},
		{"usr_existing_early", 2},
		{"usr_existing_later", 3},
	} {
		user, err := store.UserByID(want.id)
		if err != nil {
			t.Fatalf("UserByID(%s): %v", want.id, err)
		}
		if user.UserNumber != want.number || user.AvatarURL != "" {
			t.Fatalf("migrated user %s = %+v, want number %d and empty avatar", want.id, user, want.number)
		}
	}

	created, err := store.CreateUser("next@example.test", "hash", "Next")
	if err != nil {
		t.Fatalf("CreateUser after migration: %v", err)
	}
	if created.UserNumber != 4 {
		t.Fatalf("first post-migration user number = %d, want 4", created.UserNumber)
	}
	if _, err := store.db.Exec(`DELETE FROM users WHERE id=?`, created.ID); err != nil {
		t.Fatalf("delete highest user for sequence test: %v", err)
	}
	if err := store.MigrateAndSeed("admin@example.test", "admin-password-hash"); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	afterDelete, err := store.CreateUser("after-delete@example.test", "hash", "After Delete")
	if err != nil {
		t.Fatalf("CreateUser after deletion: %v", err)
	}
	if afterDelete.UserNumber != 5 {
		t.Fatalf("user number after highest deletion = %d, want 5", afterDelete.UserNumber)
	}
}

func TestGamesFiltersAndCorruptTagsFallback(t *testing.T) {
	store := newTestStore(t)

	featured := false
	result, err := store.Games(GameFilter{
		CategoryID: "puzzle",
		Featured:   &featured,
		Page:       -10,
		PageSize:   101,
	})
	if err != nil {
		t.Fatalf("Games filters: %v", err)
	}
	if result.Page != 1 || result.PageSize != 24 {
		t.Fatalf("Games did not normalize pagination: %+v", result)
	}
	for _, game := range result.Items {
		if game.CategoryID != "puzzle" || game.Featured {
			t.Fatalf("Games returned an item outside filters: %+v", game)
		}
	}

	if _, err := store.db.Exec(`UPDATE games SET tags_json='not-json' WHERE id='game_neon'`); err != nil {
		t.Fatalf("corrupt game tags: %v", err)
	}
	game, err := store.GameByID("game_neon", "")
	if err != nil {
		t.Fatalf("GameByID corrupt tags: %v", err)
	}
	if game.Tags == nil || len(game.Tags) != 0 {
		t.Fatalf("corrupt tags fallback = %#v, want empty array", game.Tags)
	}
}

func TestUpdateUserAccessKeepsAnActiveAdminUnderConcurrency(t *testing.T) {
	store := newTestStore(t)
	secondAdmin, err := store.CreateUser("second-admin@example.test", "hash", "Second Admin")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secondAdmin, err = store.UpdateUserAccess("usr_admin", secondAdmin.ID, "admin", "active")
	if err != nil {
		t.Fatalf("promote second admin: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		_, err := store.UpdateUserAccess(secondAdmin.ID, "usr_admin", "user", "active")
		results <- err
	}()
	go func() {
		defer group.Done()
		<-start
		_, err := store.UpdateUserAccess("usr_admin", secondAdmin.ID, "user", "active")
		results <- err
	}()
	close(start)
	group.Wait()
	close(results)

	successes := 0
	lastAdminErrors := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrLastAdmin):
			lastAdminErrors++
		default:
			t.Fatalf("unexpected concurrent access update error: %v", err)
		}
	}
	if successes != 1 || lastAdminErrors != 1 {
		t.Fatalf("concurrent updates produced %d successes and %d last-admin errors", successes, lastAdminErrors)
	}

	users, err := store.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	activeAdmins := 0
	for _, user := range users {
		if user.Role == "admin" && user.Status == "active" {
			activeAdmins++
		}
	}
	if activeAdmins != 1 {
		t.Fatalf("active admin count = %d, want 1", activeAdmins)
	}
}

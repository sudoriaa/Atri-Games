package data

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestImportGameInstallsStaticPackageAtomically(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	source := filepath.Join(root, ".atri-imports", "package-fixture")
	coverSource := writeTestAsset(t, source, "cover.webp")
	bundleSource := filepath.Join(source, "game")
	if err := os.MkdirAll(bundleSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleSource, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}

	input := lifecycleGameInput(
		"import-fixture",
		"/covers/import-fixture/cover.webp",
		"/playables/import-fixture/index.html",
	)
	game, err := store.ImportGame("usr_admin", "", ImportedGame{
		Input:        input,
		Kind:         "static",
		ManifestJSON: `{"schemaVersion":2,"id":"import-fixture"}`,
		CoverSource:  coverSource,
		BundleSource: bundleSource,
	}, root, false)
	if err != nil {
		t.Fatalf("ImportGame: %v", err)
	}
	if game.Slug != input.Slug || game.LaunchURL != input.LaunchURL {
		t.Fatalf("imported game = %+v", game)
	}
	assertExistingPath(t, filepath.Join(root, "covers", "import-fixture", "cover.webp"))
	assertExistingPath(t, filepath.Join(root, "playables", "import-fixture", "index.html"))
	assertMissingPath(t, filepath.Join(source, "cover.webp"))
	assertMissingPath(t, filepath.Join(source, "game"))
	assertTableCount(t, store, "game_packages", "game_id", game.ID, 1)
	if err := store.DeleteGame("usr_admin", game.ID, root); err != nil {
		t.Fatalf("DeleteGame imported package: %v", err)
	}
	assertMissingPath(t, filepath.Join(root, "covers", "import-fixture"))
	assertMissingPath(t, filepath.Join(root, "playables", "import-fixture"))
	assertTableCount(t, store, "game_packages", "game_id", game.ID, 0)
}

func TestImportGameReplacementRemovesOldBundle(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	oldCover := writeTestAsset(t, root, "covers/replacement/cover.webp")
	oldBundle := writeTestAsset(t, root, "playables/replacement/index.html")
	createLifecycleGame(t, store, "replacement", "/covers/replacement/cover.webp", "/playables/replacement/index.html")
	_ = oldCover
	_ = oldBundle

	source := filepath.Join(root, ".atri-imports", "package-replacement")
	newCover := writeTestAsset(t, source, "cover.webp")
	newBundle := filepath.Join(source, "game")
	if err := os.MkdirAll(newBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newBundle, "index.html"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := lifecycleGameInput("replacement", "/covers/replacement/cover.webp", "/playables/replacement/index.html")
	input.Version = "2.0.0"
	if _, err := store.ImportGame("usr_admin", "", ImportedGame{
		Input:        input,
		Kind:         "static",
		ManifestJSON: `{"schemaVersion":2,"id":"replacement","version":"2.0.0"}`,
		CoverSource:  newCover,
		BundleSource: newBundle,
	}, root, true); err != nil {
		t.Fatalf("replacement ImportGame: %v", err)
	}
	assertExistingPath(t, filepath.Join(root, "covers", "replacement", "cover.webp"))
	assertExistingPath(t, filepath.Join(root, "playables", "replacement", "index.html"))

	externalSource := filepath.Join(root, ".atri-imports", "package-replacement-external")
	externalCover := writeTestAsset(t, externalSource, "cover.webp")
	externalInput := lifecycleGameInput("replacement", "/covers/replacement/cover.webp", "https://games.example.test/replacement")
	externalInput.Version = "3.0.0"
	if _, err := store.ImportGame("usr_admin", "", ImportedGame{
		Input:        externalInput,
		Kind:         "external",
		ManifestJSON: `{"schemaVersion":2,"id":"replacement","version":"3.0.0"}`,
		CoverSource:  externalCover,
	}, root, true); err != nil {
		t.Fatalf("external replacement ImportGame: %v", err)
	}
	assertMissingPath(t, filepath.Join(root, "playables", "replacement"))
	assertTableCount(t, store, "game_assets", "game_id", gameIDBySlug(t, store, "replacement"), 1)
}

func TestImportGameDatabaseFailureRestoresFiles(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	source := filepath.Join(root, ".atri-imports", "package-failure")
	coverSource := writeTestAsset(t, source, "cover.webp")
	bundleSource := filepath.Join(source, "game")
	if err := os.MkdirAll(bundleSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleSource, "index.html"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER block_import_insert BEFORE INSERT ON games BEGIN SELECT RAISE(ABORT, 'blocked import'); END`); err != nil {
		t.Fatal(err)
	}
	input := lifecycleGameInput("import-failure", "/covers/import-failure/cover.webp", "/playables/import-failure/index.html")
	if _, err := store.ImportGame("usr_admin", "", ImportedGame{
		Input:        input,
		Kind:         "static",
		ManifestJSON: `{}`,
		CoverSource:  coverSource,
		BundleSource: bundleSource,
	}, root, false); err == nil {
		t.Fatal("ImportGame succeeded despite database trigger")
	} else {
		t.Logf("expected import error: %v", err)
	}
	assertMissingPath(t, coverSource)
	assertMissingPath(t, filepath.Join(bundleSource, "index.html"))
	assertMissingPath(t, filepath.Join(root, "covers", "import-failure"))
	assertMissingPath(t, filepath.Join(root, "playables", "import-failure"))
	if _, err := store.GameBySlug(input.Slug, "", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("game exists after failed import: %v", err)
	}
}

func TestRecoverGameImportsRollsBackUncommittedStage(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	oldCover := writeTestAsset(t, root, "covers/recover-stage/cover.webp")
	oldBundle := writeTestAsset(t, root, "playables/recover-stage/index.html")
	if err := os.WriteFile(oldCover, []byte("old-cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldBundle, []byte("old-bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, ".atri-imports", "package-recover-stage")
	newCover := writeTestAsset(t, source, "cover.webp")
	newBundle := filepath.Join(source, "game")
	if err := os.MkdirAll(newBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newBundle, "index.html"), []byte("new-bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage, err := prepareImportStage(root, ImportedGame{
		Input:        lifecycleGameInput("recover-stage", "/covers/recover-stage/cover.webp", "/playables/recover-stage/index.html"),
		Kind:         "static",
		ManifestJSON: `{}`,
		CoverSource:  newCover,
		BundleSource: newBundle,
	}, "receipt-uncommitted", os.RemoveAll)
	if err != nil {
		t.Fatalf("prepareImportStage: %v", err)
	}
	if err := stage.activate(); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := store.RecoverGameImports(root); err != nil {
		t.Fatalf("RecoverGameImports: %v", err)
	}
	assertFileContents(t, oldCover, "old-cover")
	assertFileContents(t, oldBundle, "old-bundle")
}

func TestRecoverGameImportsFinalizesCommittedStage(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	game := createLifecycleGame(t, store, "recover-committed", "/covers/recover-committed/cover.webp", "/playables/recover-committed/index.html")
	oldCover := writeTestAsset(t, root, "covers/recover-committed/cover.webp")
	oldBundle := writeTestAsset(t, root, "playables/recover-committed/index.html")
	_ = oldCover
	_ = oldBundle
	source := filepath.Join(root, ".atri-imports", "package-recover-committed")
	newCover := writeTestAsset(t, source, "cover.webp")
	newBundle := filepath.Join(source, "game")
	if err := os.MkdirAll(newBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newBundle, "index.html"), []byte("committed-bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage, err := prepareImportStage(root, ImportedGame{
		Input:        lifecycleGameInput("recover-committed", "/covers/recover-committed/cover.webp", "/playables/recover-committed/index.html"),
		Kind:         "static",
		ManifestJSON: `{}`,
		CoverSource:  newCover,
		BundleSource: newBundle,
	}, "receipt-committed", os.RemoveAll)
	if err != nil {
		t.Fatalf("prepareImportStage: %v", err)
	}
	if err := stage.activate(); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`, game.ID, "receipt-committed", "static", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverGameImports(root); err != nil {
		t.Fatalf("RecoverGameImports: %v", err)
	}
	assertFileContents(t, filepath.Join(root, "covers", "recover-committed", "cover.webp"), "fixture")
	assertFileContents(t, filepath.Join(root, "playables", "recover-committed", "index.html"), "committed-bundle")
	if _, err := os.Stat(stage.stageRoot); !os.IsNotExist(err) {
		t.Fatalf("committed stage remains: %v", err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(value) != want {
		t.Fatalf("%s = %q, want %q", path, value, want)
	}
}

func gameIDBySlug(t *testing.T, store *Store, slug string) string {
	t.Helper()
	game, err := store.GameBySlug(slug, "", false)
	if err != nil {
		t.Fatalf("GameBySlug(%s): %v", slug, err)
	}
	return game.ID
}

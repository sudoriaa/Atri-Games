package data

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverGameCoverUploadsRollsBackUncommittedStage(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	input, upload := stagedCoverFixture(t, root, "cover-recover-rollback", []byte("new-cover"), ".png")
	stage, err := prepareGameCoverStage(root, input, upload, "receipt-uncommitted", "", os.RemoveAll)
	if err != nil {
		t.Fatalf("prepareGameCoverStage: %v", err)
	}
	if err := stage.activate(); err != nil {
		t.Fatalf("activate: %v", err)
	}
	assertExistingPath(t, stage.target)

	if err := store.RecoverGameCoverUploads(root); err != nil {
		t.Fatalf("RecoverGameCoverUploads: %v", err)
	}
	assertMissingPath(t, stage.target)
	assertMissingPath(t, stage.stageRoot)
	assertTableCount(t, store, "game_cover_receipts", "receipt_token", "receipt-uncommitted", 0)
}

func TestRecoverGameCoverUploadsFinalizesCommittedStage(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	slug := "cover-recover-committed"
	oldContent := []byte("old-cover")
	oldDigest := sha256Hex(oldContent)
	oldURL, err := ManagedGameCoverURL(slug, oldDigest, ".png")
	if err != nil {
		t.Fatal(err)
	}
	oldPath := writeCoverFixture(t, root, oldURL, oldContent)
	game := createLifecycleGame(t, store, slug, oldURL, "https://games.example.test/cover-recover")

	input, upload := stagedCoverFixture(t, root, slug, []byte("new-cover"), ".webp")
	input.Title = "Committed replacement cover"
	stage, err := prepareGameCoverStage(
		root,
		input,
		upload,
		"receipt-committed",
		stringsTrimLeadingSlash(oldURL),
		os.RemoveAll,
	)
	if err != nil {
		t.Fatalf("prepareGameCoverStage: %v", err)
	}
	if err := stage.activate(); err != nil {
		t.Fatalf("activate: %v", err)
	}
	receipt := &gameCoverReceipt{
		Token:      stage.manifest.ReceiptToken,
		TargetPath: stage.manifest.Target,
		OldPath:    stage.manifest.OldTarget,
	}
	if _, err := store.updateGame("usr_admin", game.ID, input, receipt); err != nil {
		t.Fatalf("commit replacement: %v", err)
	}

	if err := store.RecoverGameCoverUploads(root); err != nil {
		t.Fatalf("RecoverGameCoverUploads: %v", err)
	}
	assertExistingPath(t, stage.target)
	assertMissingPath(t, oldPath)
	assertMissingPath(t, stage.stageRoot)
	assertTableCount(t, store, "game_cover_receipts", "receipt_token", "receipt-committed", 0)
	persisted, err := store.GameByID(game.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.CoverURL != input.CoverURL || persisted.Title != input.Title {
		t.Fatalf("persisted game = %+v", persisted)
	}
}

func TestManagedAssetRecoveryRunsBeforeCommittedCoverRecovery(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	slug := "cover-delete-recovery-order"
	oldContent := []byte("old-cover")
	oldDigest := sha256Hex(oldContent)
	oldURL, err := ManagedGameCoverURL(slug, oldDigest, ".png")
	if err != nil {
		t.Fatal(err)
	}
	writeCoverFixture(t, root, oldURL, oldContent)
	game := createLifecycleGame(t, store, slug, oldURL, "https://games.example.test/recovery-order")

	input, upload := stagedCoverFixture(t, root, slug, []byte("new-cover"), ".webp")
	stage, err := prepareGameCoverStage(
		root,
		input,
		upload,
		"receipt-delete-recovery-order",
		stringsTrimLeadingSlash(oldURL),
		os.RemoveAll,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.activate(); err != nil {
		t.Fatal(err)
	}
	receipt := &gameCoverReceipt{
		Token:      stage.manifest.ReceiptToken,
		TargetPath: stage.manifest.Target,
		OldPath:    stage.manifest.OldTarget,
	}
	if _, err := store.updateGame("usr_admin", game.ID, input, receipt); err != nil {
		t.Fatal(err)
	}

	assets := ownedAssetsForTest(input.CoverURL, input.LaunchURL, input.Slug)
	deletionStage, err := stageManagedAssets(root, game.ID, assets, os.RemoveAll)
	if err != nil {
		t.Fatal(err)
	}
	assertMissingPath(t, stage.target)
	if deletionStage.trashRoot == "" {
		t.Fatal("managed deletion fixture did not stage the current cover")
	}

	// This is the startup order in cmd/server: restore or finalize an
	// interrupted deletion before a committed cover receipt verifies files.
	if err := store.RecoverManagedAssets(root); err != nil {
		t.Fatalf("RecoverManagedAssets: %v", err)
	}
	if err := store.RecoverGameCoverUploads(root); err != nil {
		t.Fatalf("RecoverGameCoverUploads: %v", err)
	}
	assertExistingPath(t, stage.target)
	assertMissingPath(t, stage.stageRoot)
	assertMissingPath(t, deletionStage.trashRoot)
}

func TestRecoverGameCoverUploadsCleansRawAndPreManifestUploads(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	uploadRoot, err := GameCoverUploadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadRoot, "cover-interrupted"), []byte("raw"), 0o600); err != nil {
		t.Fatal(err)
	}
	unfinished := filepath.Join(uploadRoot, "install-unfinished")
	if err := os.Mkdir(unfinished, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unfinished, "incoming"), []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.RecoverGameCoverUploads(root); err != nil {
		t.Fatalf("RecoverGameCoverUploads: %v", err)
	}
	assertMissingPath(t, uploadRoot)
}

func TestRecoverGameCoverUploadsRejectsEscapingManifest(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	uploadRoot, err := GameCoverUploadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	stageRoot := filepath.Join(uploadRoot, "install-invalid")
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := gameCoverManifest{
		Version:       gameCoverManifestVersion,
		ReceiptToken:  "receipt-invalid",
		Target:        "../outside.png",
		Digest:        sha256Hex([]byte("outside")),
		Extension:     ".png",
		CreatedTarget: true,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageRoot, gameCoverManifestName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "outside.png")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.RecoverGameCoverUploads(root); err == nil {
		t.Fatal("RecoverGameCoverUploads accepted an escaping manifest")
	}
	assertExistingPath(t, sentinel)
	assertExistingPath(t, stageRoot)
}

func TestReplacingUploadedCoverKeepsPackageAndSharedCovers(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	packageCover := writeTestAsset(t, root, "covers/package-cover/cover.webp")
	packageGame := createLifecycleGame(
		t,
		store,
		"package-cover",
		"/covers/package-cover/cover.webp",
		"https://games.example.test/package-cover",
	)
	packageInput, packageUpload := stagedCoverFixture(t, root, packageGame.Slug, []byte("manual-package-cover"), ".png")
	if _, err := store.UpdateGameWithCover("usr_admin", packageGame.ID, packageInput, packageUpload, root); err != nil {
		t.Fatalf("UpdateGameWithCover package cover: %v", err)
	}
	assertExistingPath(t, packageCover)

	sharedContent := []byte("shared-cover")
	sharedDigest := sha256Hex(sharedContent)
	sharedURL, err := ManagedGameCoverURL("shared-cover-owner", sharedDigest, ".png")
	if err != nil {
		t.Fatal(err)
	}
	sharedPath := writeCoverFixture(t, root, sharedURL, sharedContent)
	owner := createLifecycleGame(t, store, "shared-cover-owner", sharedURL, "https://games.example.test/owner")
	createLifecycleGame(t, store, "shared-cover-consumer", sharedURL+"?revision=1", "https://games.example.test/consumer")
	ownerInput, ownerUpload := stagedCoverFixture(t, root, owner.Slug, []byte("owner-replacement"), ".webp")
	if _, err := store.UpdateGameWithCover("usr_admin", owner.ID, ownerInput, ownerUpload, root); err != nil {
		t.Fatalf("UpdateGameWithCover shared cover: %v", err)
	}
	assertExistingPath(t, sharedPath)
}

func stagedCoverFixture(t *testing.T, root, slug string, content []byte, extension string) (GameInput, GameCoverUpload) {
	t.Helper()
	uploadRoot, err := GameCoverUploadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.CreateTemp(uploadRoot, "cover-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Write(content); err != nil {
		source.Close()
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256Hex(content)
	coverURL, err := ManagedGameCoverURL(slug, digest, extension)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycleGameInput(slug, coverURL, "https://games.example.test/"+slug), GameCoverUpload{
		SourcePath: source.Name(),
		Extension:  extension,
		SHA256:     digest,
	}
}

func writeCoverFixture(t *testing.T, root, coverURL string, content []byte) string {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(stringsTrimLeadingSlash(coverURL)))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func stringsTrimLeadingSlash(value string) string {
	for len(value) > 0 && value[0] == '/' {
		value = value[1:]
	}
	return value
}

func TestManagedContentHashCoverPath(t *testing.T) {
	validDigest := sha256Hex([]byte("cover"))
	validURL := "/covers/game-slug/cover-" + validDigest + ".webp"
	relative, digest, ok := managedContentHashCoverPath(validURL)
	if !ok || relative != stringsTrimLeadingSlash(validURL) || digest != validDigest {
		t.Fatalf("managedContentHashCoverPath(%q) = %q, %q, %v", validURL, relative, digest, ok)
	}
	for _, invalid := range []string{
		"/covers/game-slug/cover.webp",
		"/covers/game-slug/cover-" + validDigest + ".gif",
		"/covers/game-slug/cover-" + validDigest + ".webp?revision=1",
		"/covers/../game-slug/cover-" + validDigest + ".webp",
		"https://example.test/covers/game-slug/cover-" + validDigest + ".webp",
	} {
		if _, _, ok := managedContentHashCoverPath(invalid); ok {
			t.Fatalf("managedContentHashCoverPath accepted %q", invalid)
		}
	}
}

func TestGameCoverStageRollbackPreservesPreexistingTarget(t *testing.T) {
	root := t.TempDir()
	content := []byte("same-cover")
	digest := sha256Hex(content)
	coverURL, err := ManagedGameCoverURL("preexisting-cover", digest, ".png")
	if err != nil {
		t.Fatal(err)
	}
	target := writeCoverFixture(t, root, coverURL, content)
	input, upload := stagedCoverFixture(t, root, "preexisting-cover", content, ".png")
	stage, err := prepareGameCoverStage(root, input, upload, "receipt-preexisting", "", os.RemoveAll)
	if err != nil {
		t.Fatal(err)
	}
	if stage.manifest.CreatedTarget {
		t.Fatal("preexisting target marked as newly created")
	}
	if err := stage.activate(); err != nil {
		t.Fatal(err)
	}
	if err := stage.rollback(); err != nil {
		t.Fatal(err)
	}
	if contentAfter, err := os.ReadFile(target); err != nil || string(contentAfter) != string(content) {
		t.Fatalf("preexisting cover changed: %q, %v", contentAfter, err)
	}
	if _, err := os.Stat(stage.stageRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stage remains after rollback: %v", err)
	}
}

func TestGameCoverStageRejectsSymlinkedDestinationDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	content := []byte("outside-cover")
	digest := sha256Hex(content)
	coverURL, err := ManagedGameCoverURL("symlink-cover", digest, ".png")
	if err != nil {
		t.Fatal(err)
	}
	outsideTarget := filepath.Join(outside, filepath.Base(filepath.FromSlash(coverURL)))
	if err := os.WriteFile(outsideTarget, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "covers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "covers", "symlink-cover")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	input, upload := stagedCoverFixture(t, root, "symlink-cover", content, ".png")
	if _, err := prepareGameCoverStage(root, input, upload, "receipt-symlink", "", os.RemoveAll); err == nil {
		t.Fatal("prepareGameCoverStage accepted a symlinked destination directory")
	}
	if value, err := os.ReadFile(outsideTarget); err != nil || string(value) != string(content) {
		t.Fatalf("outside cover changed: %q, %v", value, err)
	}
}

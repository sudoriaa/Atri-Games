package objectstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sudoriaa/atri-games/apps/api/internal/config"
)

func TestNewUsesDisabledStoreForLocalProvider(t *testing.T) {
	store, err := New(config.Config{ObjectStorageProvider: "local"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if store.Provider() != "local" {
		t.Fatalf("provider = %q, want local", store.Provider())
	}
	if err := SyncManagedAssetRoot(context.Background(), store, t.TempDir()); err != nil {
		t.Fatalf("disabled root sync: %v", err)
	}
}

func TestNewRejectsIncompleteMinIOConfig(t *testing.T) {
	_, err := New(config.Config{ObjectStorageProvider: "minio", ObjectStorageBucket: "atri-games"})
	if err == nil {
		t.Fatal("New accepted incomplete MinIO config")
	}
}

func TestCollectFilesRejectsSymbolicLinksAndSortsRegularFiles(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "covers", "test-game")
	if err := os.MkdirAll(filepath.Join(prefix, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "z.webp"), []byte("z"), 0o644); err != nil {
		t.Fatalf("write z: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "nested", "a.png"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	files, err := collectFiles(prefix)
	if err != nil {
		t.Fatalf("collect files: %v", err)
	}
	if len(files) != 2 || files[0].relative != "nested/a.png" || files[1].relative != "z.webp" {
		t.Fatalf("collected files = %#v", files)
	}

	link := filepath.Join(prefix, "escape")
	if err := os.Symlink(filepath.Join(root, "outside"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := collectFiles(prefix); err == nil {
		t.Fatal("collectFiles accepted a symbolic link")
	}
}

func TestManagedPrefixAllowsOnlyPublicAssetNamespaces(t *testing.T) {
	for _, value := range []string{"avatars", "avatars/usr_abc", "covers", "covers/game-1", "demos/game-1", "playables/game-1"} {
		if _, err := managedPrefix(value); err != nil {
			t.Fatalf("managedPrefix(%q): %v", value, err)
		}
	}
	for _, value := range []string{"", "covers/../secret", "private/game", "covers/game/extra", ".atri-imports/game"} {
		if _, err := managedPrefix(value); err == nil {
			t.Fatalf("managedPrefix accepted %q", value)
		}
	}
}

func TestContentMetadata(t *testing.T) {
	if got := contentType("game.wasm"); got != "application/wasm" {
		t.Fatalf("wasm content type = %q", got)
	}
	if got := cacheControl("index.html"); got != "no-cache" {
		t.Fatalf("html cache control = %q", got)
	}
	if got := cacheControl("asset.js"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache control = %q", got)
	}
}

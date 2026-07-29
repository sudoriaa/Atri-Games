package data

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndRemoveManagedAvatar(t *testing.T) {
	root := t.TempDir()
	content := []byte("avatar-content")
	upload := writeAvatarUpload(t, root, content, ".png")

	managedURL, created, err := InstallAvatar(root, "usr_player", upload)
	if err != nil {
		t.Fatalf("InstallAvatar: %v", err)
	}
	if !created {
		t.Fatal("InstallAvatar reported an initial file as already installed")
	}
	wantURL, err := ManagedAvatarURL("usr_player", upload.SHA256, upload.Extension)
	if err != nil {
		t.Fatal(err)
	}
	if managedURL != wantURL {
		t.Fatalf("managed avatar URL = %q, want %q", managedURL, wantURL)
	}
	target := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(managedURL, "/")))
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("installed avatar missing: %v", err)
	}
	if _, err := os.Stat(upload.SourcePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged avatar remains after installation: %v", err)
	}

	duplicate := writeAvatarUpload(t, root, content, ".png")
	duplicateURL, duplicateCreated, err := InstallAvatar(root, "usr_player", duplicate)
	if err != nil {
		t.Fatalf("duplicate InstallAvatar: %v", err)
	}
	if duplicateURL != managedURL || duplicateCreated {
		t.Fatalf("duplicate InstallAvatar = %q/%t, want %q/false", duplicateURL, duplicateCreated, managedURL)
	}
	if _, err := os.Stat(duplicate.SourcePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("duplicate staged avatar remains: %v", err)
	}

	if err := RemoveManagedAvatar(root, "usr_player", "https://images.example.test/avatar.png"); err != nil {
		t.Fatalf("RemoveManagedAvatar external URL: %v", err)
	}
	if err := RemoveManagedAvatar(root, "usr_other", managedURL); err == nil {
		t.Fatal("RemoveManagedAvatar removed an avatar owned by another user")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("other-user removal changed avatar: %v", err)
	}
	if err := RemoveManagedAvatar(root, "usr_player", managedURL); err != nil {
		t.Fatalf("RemoveManagedAvatar: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed avatar remains after removal: %v", err)
	}
}

func TestInstallAvatarRejectsOutsideOrLinkedUploads(t *testing.T) {
	root := t.TempDir()
	content := []byte("avatar-content")
	digest := avatarDigest(content)

	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallAvatar(root, "usr_player", AvatarUpload{SourcePath: outside, Extension: ".png", SHA256: digest}); err == nil {
		t.Fatal("InstallAvatar accepted an upload outside its private workspace")
	}

	uploadRoot, err := AvatarUploadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(uploadRoot, "avatar-link")
	if err := os.Symlink(outside, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := InstallAvatar(root, "usr_player", AvatarUpload{SourcePath: linked, Extension: ".png", SHA256: digest}); err == nil {
		t.Fatal("InstallAvatar accepted a symbolic-link upload")
	}
}

func writeAvatarUpload(t *testing.T, root string, content []byte, extension string) AvatarUpload {
	t.Helper()
	uploadRoot, err := AvatarUploadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(uploadRoot, "avatar-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return AvatarUpload{SourcePath: file.Name(), Extension: extension, SHA256: avatarDigest(content)}
}

func avatarDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

package data

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

const avatarUploadWorkspaceName = ".atri-avatar-uploads"

// AvatarUpload describes an image that has already been inspected by the HTTP
// layer and placed in the private avatar upload workspace. InstallAvatar
// verifies all of these values again before the file becomes public.
type AvatarUpload struct {
	SourcePath string
	Extension  string
	SHA256     string
}

// ManagedAvatarURL returns the immutable public path for an uploaded avatar.
// The digest in the name makes replacing an avatar cache-safe without trusting
// client-provided filenames.
func ManagedAvatarURL(userID, digest, extension string) (string, error) {
	if !safeBundleName(userID) || !validCoverDigest(digest) || !validCoverExtension(extension) {
		return "", errors.New("invalid managed avatar")
	}
	return "/" + pathpkg.Join("avatars", userID, "avatar-"+digest+extension), nil
}

// AvatarUploadRoot returns the private staging directory used while a multipart
// avatar upload is streamed. It is intentionally outside every public asset
// namespace and must never be served by Caddy or mirrored to object storage.
func AvatarUploadRoot(assetRoot string) (string, error) {
	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("asset root does not exist: %s", assetRoot)
	}
	uploadRoot := filepath.Join(root, avatarUploadWorkspaceName)
	if err := ensurePlainDirectory(uploadRoot, 0o700); err != nil {
		return "", fmt.Errorf("prepare avatar upload workspace: %w", err)
	}
	if err := os.Chmod(uploadRoot, 0o700); err != nil {
		return "", fmt.Errorf("protect avatar upload workspace: %w", err)
	}
	return uploadRoot, nil
}

// InstallAvatar moves a verified private upload into the calling user's
// content-addressed public namespace. It returns created=false when the same
// digest is already installed, which lets the caller avoid deleting a file it
// did not create after an unrelated database failure.
func InstallAvatar(assetRoot, userID string, upload AvatarUpload) (url string, created bool, err error) {
	managedURL, err := ManagedAvatarURL(userID, upload.SHA256, upload.Extension)
	if err != nil {
		return "", false, err
	}

	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil {
		return "", false, err
	}
	if !exists {
		return "", false, fmt.Errorf("asset root does not exist: %s", assetRoot)
	}
	uploadRoot, err := AvatarUploadRoot(root)
	if err != nil {
		return "", false, err
	}

	source, err := filepath.Abs(strings.TrimSpace(upload.SourcePath))
	if err != nil || strings.TrimSpace(upload.SourcePath) == "" {
		return "", false, errors.New("invalid avatar upload source")
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return "", false, fmt.Errorf("inspect uploaded avatar source: %w", err)
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return "", false, errors.New("uploaded avatar source is a symbolic link")
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil || filepath.Dir(resolvedSource) != uploadRoot || !pathWithin(uploadRoot, resolvedSource) {
		return "", false, errors.New("uploaded avatar is outside the private upload workspace")
	}
	info, err := os.Lstat(resolvedSource)
	if err != nil {
		return "", false, fmt.Errorf("inspect uploaded avatar: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false, errors.New("uploaded avatar is not a regular file")
	}
	digest, err := fileSHA256(resolvedSource)
	if err != nil {
		return "", false, fmt.Errorf("hash uploaded avatar: %w", err)
	}
	if digest != upload.SHA256 {
		return "", false, errors.New("uploaded avatar changed before installation")
	}

	relative := strings.TrimPrefix(managedURL, "/")
	target, ok := safeManifestTarget(root, relative)
	if !ok || filepath.Dir(target) != filepath.Join(root, "avatars", userID) {
		return "", false, errors.New("managed avatar destination escaped its user directory")
	}
	if err := verifyManagedAvatarParent(root, target, true); err != nil {
		return "", false, err
	}

	if targetInfo, statErr := os.Lstat(target); statErr == nil {
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() {
			return "", false, errors.New("managed avatar destination is not a regular file")
		}
		if err := verifyManagedAvatarFileAtRoot(root, target, upload.SHA256); err != nil {
			return "", false, err
		}
		if err := os.Remove(resolvedSource); err != nil {
			return "", false, fmt.Errorf("remove duplicate avatar upload: %w", err)
		}
		return managedURL, false, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", false, fmt.Errorf("inspect managed avatar destination: %w", statErr)
	}

	if err := ensurePlainDirectory(filepath.Join(root, "avatars"), 0o755); err != nil {
		return "", false, fmt.Errorf("prepare avatar root: %w", err)
	}
	if err := ensurePlainDirectory(filepath.Dir(target), 0o755); err != nil {
		return "", false, fmt.Errorf("prepare user avatar directory: %w", err)
	}
	if err := os.Rename(resolvedSource, target); err != nil {
		return "", false, fmt.Errorf("install avatar: %w", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		return managedURL, true, fmt.Errorf("set avatar permissions: %w", err)
	}
	if err := verifyManagedAvatarFileAtRoot(root, target, upload.SHA256); err != nil {
		return managedURL, true, err
	}
	return managedURL, true, nil
}

// RemoveManagedAvatar deletes a content-addressed avatar only when rawURL is
// a local managed path for userID. External URLs are deliberately a no-op so
// profile updates never attempt to manage third-party media.
func RemoveManagedAvatar(assetRoot, userID, rawURL string) error {
	relative, ownerID, digest, managed := managedAvatarPath(rawURL)
	if !managed {
		return nil
	}
	if ownerID != userID {
		return errors.New("managed avatar belongs to a different user")
	}

	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil || !exists {
		return err
	}
	target, ok := safeManifestTarget(root, relative)
	if !ok {
		return errors.New("managed avatar escaped asset root")
	}
	exists, err = pathExists(target)
	if err != nil || !exists {
		return err
	}
	if err := verifyManagedAvatarFileAtRoot(root, target, digest); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	// The directory can contain at most this user's managed avatars. Removing
	// it when empty keeps the public namespace tidy and cannot affect another
	// user's files because target was owner-checked above.
	_ = os.Remove(filepath.Dir(target))
	return nil
}

// IsManagedAvatarURL reports whether rawURL is exactly a content-addressed
// local avatar owned by userID. HTTP handlers use this to reject arbitrary
// same-origin paths when accepting an avatar URL in JSON.
func IsManagedAvatarURL(userID, rawURL string) bool {
	_, ownerID, _, managed := managedAvatarPath(rawURL)
	return managed && ownerID == userID
}

func managedAvatarPath(rawURL string) (relative, ownerID, digest string, ok bool) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(parsed.Path, "\\\x00") {
		return "", "", "", false
	}
	cleanPath := pathpkg.Clean(parsed.Path)
	if cleanPath != parsed.Path || !strings.HasPrefix(cleanPath, "/") {
		return "", "", "", false
	}
	segments := strings.Split(strings.TrimPrefix(cleanPath, "/"), "/")
	if len(segments) != 3 || segments[0] != "avatars" || !safeBundleName(segments[1]) {
		return "", "", "", false
	}
	extension := pathpkg.Ext(segments[2])
	if !validCoverExtension(extension) || !strings.HasPrefix(segments[2], "avatar-") {
		return "", "", "", false
	}
	digest = strings.TrimSuffix(strings.TrimPrefix(segments[2], "avatar-"), extension)
	if !validCoverDigest(digest) {
		return "", "", "", false
	}
	return pathpkg.Join(segments...), segments[1], digest, true
}

func verifyManagedAvatarFileAtRoot(root, filename, expectedDigest string) error {
	if err := verifyManagedAvatarParent(root, filename, false); err != nil {
		return err
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("inspect managed avatar: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("managed avatar is not a regular file")
	}
	digest, err := fileSHA256(filename)
	if err != nil {
		return err
	}
	if digest != expectedDigest {
		return errors.New("managed avatar digest changed")
	}
	return nil
}

func verifyManagedAvatarParent(root, filename string, allowMissing bool) error {
	if !pathWithin(root, filename) {
		return errors.New("managed avatar escaped asset root")
	}
	relative, err := filepath.Rel(root, filename)
	if err != nil {
		return err
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if len(segments) != 3 || segments[0] != "avatars" || !safeBundleName(segments[1]) {
		return errors.New("managed avatar has an invalid parent")
	}
	for _, directory := range []string{
		filepath.Join(root, "avatars"),
		filepath.Join(root, "avatars", segments[1]),
	} {
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) && allowMissing {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect managed avatar parent: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("managed avatar parent is not a plain directory")
		}
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(filename))
	if err != nil {
		return fmt.Errorf("resolve managed avatar parent: %w", err)
	}
	if !pathWithin(root, resolvedParent) {
		return errors.New("managed avatar parent escaped asset root")
	}
	return nil
}

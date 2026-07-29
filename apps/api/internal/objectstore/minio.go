// Package objectstore mirrors managed public game assets to an S3-compatible
// object store. The local asset volume remains the authoritative staging area:
// imports, replacements, and destructive cleanup retain their durable
// filesystem transactions, while MinIO receives only complete, verified files.
package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/sudoriaa/atri-games/apps/api/internal/config"
)

const metadataSHA256 = "sha256"

// Store is deliberately narrow: callers only mirror known managed prefixes.
// It never accepts arbitrary user-provided keys and therefore cannot turn the
// object store into a general file upload endpoint.
type Store interface {
	Provider() string
	Ensure(ctx context.Context) error
	Sync(ctx context.Context, assetRoot string, prefixes ...string) error
}

// DisabledStore makes local development and legacy deployments explicit while
// keeping the API call sites free of nil checks.
type DisabledStore struct{}

func (DisabledStore) Provider() string                              { return "local" }
func (DisabledStore) Ensure(context.Context) error                  { return nil }
func (DisabledStore) Sync(context.Context, string, ...string) error { return nil }

// SyncManagedAssetRoot performs the startup reconciliation. It includes the
// legacy root-level covers shipped with the initial catalog, game packages,
// and immutable player avatars, then removes stale objects from the same
// namespaces.
func SyncManagedAssetRoot(ctx context.Context, store Store, assetRoot string) error {
	return store.Sync(ctx, assetRoot, "avatars", "covers", "demos", "playables")
}

type minioStore struct {
	client *minio.Client
	bucket string
	prefix string
	region string
}

// New constructs an optional object store from the application configuration.
// The connection itself is verified by Ensure, so startup can fail closed when
// a requested MinIO mirror is unavailable.
func New(cfg config.Config) (Store, error) {
	if err := cfg.ValidateObjectStorage(); err != nil {
		return nil, err
	}
	if cfg.ObjectStorageProvider == "" || cfg.ObjectStorageProvider == "local" {
		return DisabledStore{}, nil
	}
	endpoint := strings.TrimSpace(cfg.ObjectStorageEndpoint)
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" || strings.Contains(endpoint, "/") {
		return nil, fmt.Errorf("invalid MinIO endpoint %q", cfg.ObjectStorageEndpoint)
	}
	prefix, err := validatePrefix(cfg.ObjectStoragePrefix)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.ObjectStorageAccessKey, cfg.ObjectStorageSecretKey, ""),
		Secure: cfg.ObjectStorageUseSSL,
		Region: cfg.ObjectStorageRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("create MinIO client: %w", err)
	}
	return &minioStore{
		client: client,
		bucket: strings.TrimSpace(cfg.ObjectStorageBucket),
		prefix: prefix,
		region: strings.TrimSpace(cfg.ObjectStorageRegion),
	}, nil
}

func (store *minioStore) Provider() string { return "minio" }

func (store *minioStore) Ensure(ctx context.Context) error {
	exists, err := store.client.BucketExists(ctx, store.bucket)
	if err != nil {
		return fmt.Errorf("check MinIO bucket %q: %w", store.bucket, err)
	}
	if exists {
		return nil
	}
	if err := store.client.MakeBucket(ctx, store.bucket, minio.MakeBucketOptions{Region: store.region}); err != nil {
		return fmt.Errorf("create MinIO bucket %q: %w", store.bucket, err)
	}
	return nil
}

// Sync mirrors each complete managed prefix (avatars/<user>, covers/<slug>,
// playables/<slug>, or demos/<slug>) and removes obsolete objects under that
// same prefix. It rejects dot workspaces, symbolic links and unsafe paths so
// private upload/import/delete workspaces never leave the local volume.
func (store *minioStore) Sync(ctx context.Context, assetRoot string, prefixes ...string) error {
	for _, rawPrefix := range prefixes {
		relative, err := managedPrefix(rawPrefix)
		if err != nil {
			return err
		}
		if err := store.syncPrefix(ctx, assetRoot, relative); err != nil {
			return err
		}
	}
	return nil
}

func (store *minioStore) syncPrefix(ctx context.Context, assetRoot, relative string) error {
	root, err := filepath.Abs(strings.TrimSpace(assetRoot))
	if err != nil {
		return fmt.Errorf("resolve asset root: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("inspect asset root: %w", err)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("asset root is not a directory")
	}
	localPrefix := filepath.Join(root, filepath.FromSlash(relative))
	if !pathWithin(root, localPrefix) {
		return fmt.Errorf("object storage prefix escaped asset root: %s", relative)
	}

	files, err := collectFiles(localPrefix)
	if err != nil {
		return fmt.Errorf("collect managed assets %s: %w", relative, err)
	}
	remotePrefix := store.objectKey(relative)
	expected := make(map[string]struct{}, len(files))
	for _, item := range files {
		key := store.objectKey(pathpkg.Join(relative, item.relative))
		expected[key] = struct{}{}
		if err := store.putIfChanged(ctx, key, item.path); err != nil {
			return fmt.Errorf("mirror %s: %w", item.relative, err)
		}
	}

	objects := store.client.ListObjects(ctx, store.bucket, minio.ListObjectsOptions{
		Prefix:    ensureTrailingSlash(remotePrefix),
		Recursive: true,
	})
	for object := range objects {
		if object.Err != nil {
			return fmt.Errorf("list MinIO objects below %s: %w", relative, object.Err)
		}
		if _, exists := expected[object.Key]; exists {
			continue
		}
		if err := store.client.RemoveObject(ctx, store.bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return fmt.Errorf("remove obsolete MinIO object %s: %w", object.Key, err)
		}
	}
	return nil
}

type localFile struct {
	path     string
	relative string
}

func collectFiles(prefix string) ([]localFile, error) {
	info, err := os.Lstat(prefix)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("managed object prefix is not a plain directory: %s", prefix)
	}
	result := make([]localFile, 0)
	err = filepath.WalkDir(prefix, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link in managed assets: %s", filename)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular managed asset: %s", filename)
		}
		relative, err := filepath.Rel(prefix, filename)
		if err != nil || relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("resolve managed asset path: %s", filename)
		}
		result = append(result, localFile{path: filename, relative: filepath.ToSlash(relative)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].relative < result[j].relative })
	return result, nil
}

func (store *minioStore) putIfChanged(ctx context.Context, key, filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	digest, err := fileSHA256(filename)
	if err != nil {
		return err
	}
	remote, statErr := store.client.StatObject(ctx, store.bucket, key, minio.StatObjectOptions{})
	if statErr == nil && remote.Size == info.Size() && strings.EqualFold(metadataValue(remote.UserMetadata, "X-Amz-Meta-Sha256"), digest) {
		return nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = store.client.PutObject(ctx, store.bucket, key, file, info.Size(), minio.PutObjectOptions{
		ContentType:  contentType(filename),
		CacheControl: cacheControl(filename),
		UserMetadata: map[string]string{metadataSHA256: digest},
	})
	return err
}

func metadataValue(metadata minio.StringMap, key string) string {
	for candidate, value := range metadata {
		if strings.EqualFold(candidate, key) {
			return value
		}
	}
	return ""
}

func (store *minioStore) objectKey(relative string) string {
	if store.prefix == "" {
		return relative
	}
	return pathpkg.Join(store.prefix, relative)
}

func normalizePrefix(value string) string {
	clean := strings.Trim(strings.TrimSpace(value), "/")
	if clean == "" || clean == "." {
		return ""
	}
	return clean
}

func validatePrefix(value string) (string, error) {
	clean := normalizePrefix(value)
	if clean == "" {
		return "", nil
	}
	for _, segment := range strings.Split(clean, "/") {
		if !safeSegment(segment) {
			return "", fmt.Errorf("invalid MinIO object prefix")
		}
	}
	return clean, nil
}

func ensureTrailingSlash(value string) string {
	if value == "" || strings.HasSuffix(value, "/") {
		return value
	}
	return value + "/"
}

func managedPrefix(value string) (string, error) {
	clean := pathpkg.Clean(strings.Trim(strings.TrimSpace(value), "/"))
	segments := strings.Split(clean, "/")
	if len(segments) == 1 && (segments[0] == "avatars" || segments[0] == "covers" || segments[0] == "demos" || segments[0] == "playables") {
		return clean, nil
	}
	if len(segments) != 2 || (segments[0] != "avatars" && segments[0] != "covers" && segments[0] != "demos" && segments[0] != "playables") || !safeSegment(segments[1]) {
		return "", fmt.Errorf("invalid managed object prefix %q", value)
	}
	return clean, nil
}

func safeSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func fileSHA256(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func contentType(filename string) string {
	suffix := strings.ToLower(filepath.Ext(filename))
	if suffix == ".wasm" {
		return "application/wasm"
	}
	if result := mime.TypeByExtension(suffix); result != "" {
		return result
	}
	return "application/octet-stream"
}

func cacheControl(filename string) string {
	if strings.HasSuffix(strings.ToLower(filename), ".html") {
		return "no-cache"
	}
	return "public, max-age=31536000, immutable"
}

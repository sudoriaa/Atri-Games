package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	assetManifestVersion = 1
	assetManifestName    = "manifest.json"
)

type managedAsset struct {
	relative  string
	directory bool
}

type stagedAsset struct {
	original string
	staged   string
}

type assetStage struct {
	gameID    string
	root      string
	trashRoot string
	items     []stagedAsset
	removeAll func(string) error
}

type assetManifest struct {
	Version int                 `json:"version"`
	GameID  string              `json:"gameId"`
	Items   []assetManifestItem `json:"items"`
}

type assetManifestItem struct {
	Original string `json:"original"`
	Staged   string `json:"staged"`
}

// UnpublishGame hides a game from every public query while retaining its
// publication history, favorites, launches, and managed assets.
func (s *Store) UnpublishGame(actorID, id string) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Game{}, err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRow(`SELECT status FROM games WHERE id=?`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Game{}, ErrNotFound
		}
		return Game{}, err
	}

	if status != "hidden" {
		if _, err := tx.Exec(`UPDATE games
			SET status='hidden',updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
			WHERE id=?`, id); err != nil {
			return Game{}, err
		}
		if err := auditTx(tx, actorID, "game.unpublished", "game", id, status+" -> hidden"); err != nil {
			return Game{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Game{}, err
	}
	return s.GameByID(id, "")
}

// ApproveGame publishes a game that was submitted for review. Approving an
// already-published game is idempotent; any other status is rejected with
// ErrGameNotReviewable. A previously published game whose review cycle cleared
// published_at gets a fresh publication timestamp.
func (s *Store) ApproveGame(actorID, id string) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Game{}, err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRow(`SELECT status FROM games WHERE id=?`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Game{}, ErrNotFound
		}
		return Game{}, err
	}
	if status == "published" {
		return s.GameByID(id, "")
	}
	if status != "review" {
		return Game{}, ErrGameNotReviewable
	}
	if _, err := tx.Exec(`UPDATE games
		SET status='published',published_at=COALESCE(published_at,strftime('%Y-%m-%dT%H:%M:%SZ','now')),updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE id=?`, id); err != nil {
		return Game{}, err
	}
	if err := auditTx(tx, actorID, "game.approved", "game", id, "review -> published"); err != nil {
		return Game{}, err
	}
	if err := tx.Commit(); err != nil {
		return Game{}, err
	}
	return s.GameByID(id, "")
}

// RecoverManagedAssets resolves interrupted game deletions. A manifest whose
// game still exists is rolled back; a manifest whose game is gone is finalized.
func (s *Store) RecoverManagedAssets(assetRoot string) error {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()
	return s.recoverManagedAssets(assetRoot)
}

func (s *Store) recoverManagedAssets(assetRoot string) error {
	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil || !exists {
		return err
	}
	trashBase := filepath.Join(root, ".atri-trash")
	info, err := os.Lstat(trashBase)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect asset trash: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("asset trash path is not a private directory: %s", trashBase)
	}

	entries, err := os.ReadDir(trashBase)
	if err != nil {
		return fmt.Errorf("read asset trash: %w", err)
	}
	var result error
	for _, entry := range entries {
		stagePath := filepath.Join(trashBase, entry.Name())
		if !entry.IsDir() {
			result = errors.Join(result, fmt.Errorf("unexpected entry in asset trash: %s", stagePath))
			continue
		}
		stage, err := loadAssetStage(root, stagePath, s.removeAssets)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("load deletion stage %s: %w", entry.Name(), err))
			continue
		}

		var gameExists bool
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM games WHERE id=?)`, stage.gameID).Scan(&gameExists); err != nil {
			result = errors.Join(result, fmt.Errorf("inspect deletion stage game %s: %w", stage.gameID, err))
			continue
		}
		if gameExists {
			if err := stage.rollback(); err != nil {
				result = errors.Join(result, fmt.Errorf("restore deletion stage %s: %w", entry.Name(), err))
			}
			continue
		}
		if err := stage.finalize(); err != nil {
			result = errors.Join(result, fmt.Errorf("finalize deletion stage %s: %w", entry.Name(), err))
		}
	}
	if result == nil {
		_ = os.Remove(trashBase)
	}
	return result
}

// DeleteGame permanently removes the database record, its cascading
// favorites/play events, and exclusively referenced assets owned by the
// game's slug namespace.
//
// Managed assets are first described by a durable manifest, then renamed into
// a private staging directory on the same filesystem. Database or staging
// failures restore every renamed path.
func (s *Store) DeleteGame(actorID, id, assetRoot string) error {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	if err := s.recoverManagedAssets(assetRoot); err != nil {
		return fmt.Errorf("recover interrupted asset deletion: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var coverURL, launchURL string
	if err := tx.QueryRow(`SELECT cover_url,launch_url FROM games WHERE id=?`, id).Scan(&coverURL, &launchURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	assets := make(map[string]managedAsset)
	assetRows, err := tx.Query(`SELECT path,is_directory FROM game_assets WHERE game_id=?`, id)
	if err != nil {
		return err
	}
	for assetRows.Next() {
		var relative string
		var directory bool
		if err := assetRows.Scan(&relative, &directory); err != nil {
			assetRows.Close()
			return err
		}
		item, ok := managedAssetFromRecord(relative, directory)
		if !ok {
			assetRows.Close()
			return fmt.Errorf("invalid managed asset record for game %s: %s", id, relative)
		}
		assets[canonicalAssetKey(item.relative)] = item
	}
	if err := assetRows.Close(); err != nil {
		return err
	}
	if err := assetRows.Err(); err != nil {
		return err
	}

	rows, err := tx.Query(`SELECT cover_url,launch_url FROM games WHERE id<>?`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var otherCoverURL, otherLaunchURL string
		if err := rows.Scan(&otherCoverURL, &otherLaunchURL); err != nil {
			rows.Close()
			return err
		}
		removeSharedAsset(assets, otherCoverURL, true)
		removeSharedAsset(assets, otherLaunchURL, false)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sharedAssetRows, err := tx.Query(`SELECT path,is_directory FROM game_assets WHERE game_id<>?`, id)
	if err != nil {
		return err
	}
	for sharedAssetRows.Next() {
		var relative string
		var directory bool
		if err := sharedAssetRows.Scan(&relative, &directory); err != nil {
			sharedAssetRows.Close()
			return err
		}
		item, ok := managedAssetFromRecord(relative, directory)
		if ok {
			delete(assets, canonicalAssetKey(item.relative))
		}
	}
	if err := sharedAssetRows.Close(); err != nil {
		return err
	}
	if err := sharedAssetRows.Err(); err != nil {
		return err
	}

	stage, err := stageManagedAssets(assetRoot, id, assets, s.removeAssets)
	if err != nil {
		return err
	}
	restore := func(cause error) error {
		if restoreErr := stage.rollback(); restoreErr != nil {
			return errors.Join(cause, fmt.Errorf("restore staged game assets: %w", restoreErr))
		}
		return cause
	}

	result, err := tx.Exec(`DELETE FROM games WHERE id=?`, id)
	if err != nil {
		return restore(err)
	}
	if count, countErr := result.RowsAffected(); countErr != nil {
		return restore(countErr)
	} else if count == 0 {
		return restore(ErrNotFound)
	}

	if _, err := tx.Exec(`DELETE FROM audit_logs WHERE entity_type='game' AND entity_id=?`, id); err != nil {
		return restore(err)
	}
	if err := tx.Commit(); err != nil {
		var gameExists bool
		queryErr := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM games WHERE id=?)`, id).Scan(&gameExists)
		if queryErr != nil {
			// Preserve the durable deletion stage while the commit outcome is
			// unknown. Startup recovery will restore or finalize it.
			return errors.Join(err, queryErr)
		}
		if !gameExists {
			_ = stage.finalize()
			return nil
		}
		return restore(err)
	}

	// The database commit is the point of no return. A failed final removal
	// leaves assets isolated below .atri-trash for the next startup/deletion to
	// retry, but the committed deletion itself remains successful.
	_ = stage.finalize()
	return nil
}

func auditTx(tx *sql.Tx, actorID, action, entityType, entityID, detail string) error {
	_, err := tx.Exec(
		`INSERT INTO audit_logs(id,actor_user_id,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?,?)`,
		newID("audit"), actorID, action, entityType, entityID, detail,
	)
	return err
}

func backfillGameAssets(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id,slug,cover_url,launch_url FROM games`)
	if err != nil {
		return err
	}
	type gameAssetSource struct {
		id, slug, coverURL, launchURL string
	}
	var sources []gameAssetSource
	for rows.Next() {
		var source gameAssetSource
		if err := rows.Scan(&source.id, &source.slug, &source.coverURL, &source.launchURL); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, source)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, source := range sources {
		if err := trackGameAssetsTx(tx, source.id, source.slug, source.coverURL, source.launchURL); err != nil {
			return err
		}
	}
	return nil
}

func trackGameAssetsTx(tx *sql.Tx, gameID, slug, coverURL, launchURL string) error {
	items := make(map[string]managedAsset)
	addOwnedManagedAsset(items, coverURL, slug, true)
	addOwnedManagedAsset(items, launchURL, slug, false)
	for _, item := range items {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO game_assets(game_id,path,is_directory) VALUES(?,?,?)`,
			gameID, filepath.ToSlash(item.relative), item.directory,
		); err != nil {
			return err
		}
	}
	return nil
}

func addOwnedManagedAsset(items map[string]managedAsset, rawURL, slug string, cover bool) {
	item, ok := managedAssetFromURL(rawURL, cover)
	if !ok || !managedAssetOwnedBySlug(item, slug, cover) {
		return
	}
	items[canonicalAssetKey(item.relative)] = item
}

func removeSharedAsset(items map[string]managedAsset, rawURL string, cover bool) {
	if item, ok := managedAssetFromURL(rawURL, cover); ok {
		delete(items, canonicalAssetKey(item.relative))
	}
}

func managedAssetFromURL(rawURL string, cover bool) (managedAsset, bool) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.Path == "" || strings.ContainsAny(parsed.Path, "\\\x00") {
		return managedAsset{}, false
	}

	rawPath := parsed.Path
	if !cover {
		rawPath = strings.TrimSuffix(rawPath, "/")
	}
	cleanPath := pathpkg.Clean(rawPath)
	if cleanPath != rawPath || !strings.HasPrefix(cleanPath, "/") {
		return managedAsset{}, false
	}
	segments := strings.Split(strings.TrimPrefix(cleanPath, "/"), "/")

	if cover {
		if len(segments) < 2 || segments[0] != "covers" {
			return managedAsset{}, false
		}
		for _, segment := range segments[1:] {
			if !safePathSegment(segment) {
				return managedAsset{}, false
			}
		}
		if len(segments) >= 3 && safeBundleName(segments[1]) {
			return managedAsset{relative: pathpkg.Join(segments[0], segments[1]), directory: true}, true
		}
		return managedAsset{relative: pathpkg.Join(segments...), directory: false}, true
	}

	// /games/<slug>/play[/<entry>]  →  playables/<slug>
	if len(segments) >= 3 && segments[0] == "games" && safeBundleName(segments[1]) && segments[2] == "play" {
		for _, segment := range segments[3:] {
			if !safePathSegment(segment) {
				return managedAsset{}, false
			}
		}
		return managedAsset{relative: pathpkg.Join("playables", segments[1]), directory: true}, true
	}

	if len(segments) < 2 || (segments[0] != "demos" && segments[0] != "playables") || !safeBundleName(segments[1]) {
		return managedAsset{}, false
	}
	for _, segment := range segments[2:] {
		if !safePathSegment(segment) {
			return managedAsset{}, false
		}
	}
	return managedAsset{relative: pathpkg.Join(segments[0], segments[1]), directory: true}, true
}

func managedAssetFromRecord(relative string, directory bool) (managedAsset, bool) {
	if relative == "" || strings.Contains(relative, "\\") || pathpkg.IsAbs(relative) ||
		pathpkg.Clean(relative) != relative {
		return managedAsset{}, false
	}
	segments := strings.Split(relative, "/")
	if directory {
		if len(segments) != 2 ||
			(segments[0] != "demos" && segments[0] != "playables" && segments[0] != "covers") ||
			!safeBundleName(segments[1]) {
			return managedAsset{}, false
		}
		return managedAsset{relative: relative, directory: true}, true
	}
	if len(segments) < 2 || segments[0] != "covers" {
		return managedAsset{}, false
	}
	for _, segment := range segments[1:] {
		if !safePathSegment(segment) {
			return managedAsset{}, false
		}
	}
	return managedAsset{relative: relative, directory: false}, true
}

func managedAssetOwnedBySlug(item managedAsset, slug string, cover bool) bool {
	if !safeBundleName(slug) {
		return false
	}
	segments := strings.Split(filepath.ToSlash(item.relative), "/")
	if cover {
		if item.directory {
			return len(segments) == 2 && segments[0] == "covers" && segments[1] == slug
		}
		if len(segments) == 2 {
			filename := segments[1]
			return strings.HasPrefix(filename, slug+".") && len(filename) > len(slug)+1
		}
		return len(segments) == 2 && segments[1] == slug
	}
	return len(segments) == 2 &&
		(segments[0] == "demos" || segments[0] == "playables") &&
		segments[1] == slug
}

func safePathSegment(value string) bool {
	return value != "" && value != "." && value != ".."
}

func safeBundleName(value string) bool {
	if !safePathSegment(value) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func canonicalAssetKey(value string) string {
	value = filepath.Clean(filepath.FromSlash(value))
	if runtime.GOOS == "windows" {
		value = strings.ToLower(value)
	}
	return value
}

func managedAssetDetail(items map[string]managedAsset) string {
	if len(items) == 0 {
		return "managed_assets=none"
	}
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, "/"+filepath.ToSlash(item.relative))
	}
	sort.Strings(paths)
	return "managed_assets=" + strings.Join(paths, ",")
}

func stageManagedAssets(assetRoot, gameID string, items map[string]managedAsset, removeAll func(string) error) (*assetStage, error) {
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	stage := &assetStage{gameID: gameID, removeAll: removeAll}
	if len(items) == 0 {
		return stage, nil
	}

	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("asset root does not exist: %s", assetRoot)
	}
	stage.root = root

	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	type existingAsset struct {
		item   managedAsset
		target string
	}
	existing := make([]existingAsset, 0, len(keys))
	for _, key := range keys {
		item := items[key]
		target := filepath.Join(root, filepath.FromSlash(item.relative))
		if !pathWithin(root, target) {
			return nil, fmt.Errorf("managed asset escaped asset root: %s", item.relative)
		}
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect managed asset %s: %w", item.relative, err)
		}
		resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			return nil, fmt.Errorf("resolve managed asset parent %s: %w", item.relative, err)
		}
		if !pathWithin(root, resolvedParent) {
			return nil, fmt.Errorf("managed asset parent escaped asset root: %s", item.relative)
		}
		if item.directory {
			if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return nil, fmt.Errorf("managed demo bundle is not a directory: %s", item.relative)
			}
		} else if info.IsDir() {
			return nil, fmt.Errorf("managed cover is a directory: %s", item.relative)
		}
		existing = append(existing, existingAsset{item: item, target: target})
	}
	if len(existing) == 0 {
		return stage, nil
	}

	trashBase, err := ensureAssetTrash(root)
	if err != nil {
		return nil, err
	}
	stage.trashRoot = filepath.Join(trashBase, newID("delete"))
	filesRoot := filepath.Join(stage.trashRoot, "files")
	if err := os.MkdirAll(filesRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create deletion stage: %w", err)
	}

	for index, entry := range existing {
		destination := filepath.Join(filesRoot, fmt.Sprintf("%03d-%s", index, filepath.Base(entry.target)))
		stage.items = append(stage.items, stagedAsset{original: entry.target, staged: destination})
	}
	if err := stage.writeManifest(); err != nil {
		cleanupErr := stage.removeAll(stage.trashRoot)
		return nil, errors.Join(fmt.Errorf("write deletion manifest: %w", err), cleanupErr)
	}

	for index, entry := range existing {
		if err := os.Rename(entry.target, stage.items[index].staged); err != nil {
			return nil, errors.Join(
				fmt.Errorf("stage managed asset %s: %w", entry.item.relative, err),
				stage.rollback(),
			)
		}
	}
	return stage, nil
}

func (s *assetStage) writeManifest() error {
	manifest := assetManifest{
		Version: assetManifestVersion,
		GameID:  s.gameID,
		Items:   make([]assetManifestItem, 0, len(s.items)),
	}
	for _, item := range s.items {
		original, err := filepath.Rel(s.root, item.original)
		if err != nil || filepath.IsAbs(original) || !pathWithin(s.root, item.original) {
			return fmt.Errorf("resolve original manifest path: %s", item.original)
		}
		staged, err := filepath.Rel(s.root, item.staged)
		if err != nil || filepath.IsAbs(staged) || !pathWithin(s.root, item.staged) {
			return fmt.Errorf("resolve staged manifest path: %s", item.staged)
		}
		manifest.Items = append(manifest.Items, assetManifestItem{
			Original: filepath.ToSlash(original),
			Staged:   filepath.ToSlash(staged),
		})
	}

	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(s.trashRoot, assetManifestName+".tmp")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(s.trashRoot, assetManifestName))
}

func loadAssetStage(root, trashRoot string, removeAll func(string) error) (*assetStage, error) {
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	if !pathWithin(filepath.Join(root, ".atri-trash"), trashRoot) {
		return nil, errors.New("deletion stage escaped asset trash")
	}
	encoded, err := os.ReadFile(filepath.Join(trashRoot, assetManifestName))
	if err != nil {
		return nil, err
	}
	var manifest assetManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != assetManifestVersion || strings.TrimSpace(manifest.GameID) == "" || len(manifest.Items) == 0 {
		return nil, errors.New("invalid deletion manifest metadata")
	}

	stage := &assetStage{
		gameID:    manifest.GameID,
		root:      root,
		trashRoot: trashRoot,
		removeAll: removeAll,
		items:     make([]stagedAsset, 0, len(manifest.Items)),
	}
	seenOriginal := make(map[string]struct{}, len(manifest.Items))
	seenStaged := make(map[string]struct{}, len(manifest.Items))
	for _, item := range manifest.Items {
		original, ok := safeManifestTarget(root, item.Original)
		if !ok ||
			(!strings.HasPrefix(item.Original, "covers/") &&
				!strings.HasPrefix(item.Original, "demos/") &&
				!strings.HasPrefix(item.Original, "playables/")) {
			return nil, fmt.Errorf("invalid original manifest path: %s", item.Original)
		}
		staged, ok := safeManifestTarget(root, item.Staged)
		if !ok || !pathWithin(trashRoot, staged) || filepath.Dir(staged) != filepath.Join(trashRoot, "files") {
			return nil, fmt.Errorf("invalid staged manifest path: %s", item.Staged)
		}
		originalKey := canonicalAssetKey(original)
		stagedKey := canonicalAssetKey(staged)
		if _, exists := seenOriginal[originalKey]; exists {
			return nil, fmt.Errorf("duplicate original manifest path: %s", item.Original)
		}
		if _, exists := seenStaged[stagedKey]; exists {
			return nil, fmt.Errorf("duplicate staged manifest path: %s", item.Staged)
		}
		seenOriginal[originalKey] = struct{}{}
		seenStaged[stagedKey] = struct{}{}
		stage.items = append(stage.items, stagedAsset{original: original, staged: staged})
	}
	return stage, nil
}

func safeManifestTarget(root, relative string) (string, bool) {
	if relative == "" || strings.Contains(relative, "\\") || pathpkg.Clean(relative) != relative ||
		pathpkg.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", false
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	return target, pathWithin(root, target)
}

func (s *assetStage) rollback() error {
	if s == nil || s.trashRoot == "" {
		return nil
	}
	var result error
	for index := len(s.items) - 1; index >= 0; index-- {
		item := s.items[index]
		originalExists, originalErr := pathExists(item.original)
		stagedExists, stagedErr := pathExists(item.staged)
		if originalErr != nil {
			result = errors.Join(result, originalErr)
			continue
		}
		if stagedErr != nil {
			result = errors.Join(result, stagedErr)
			continue
		}
		switch {
		case originalExists && !stagedExists:
			continue
		case !originalExists && stagedExists:
			if err := os.MkdirAll(filepath.Dir(item.original), 0o755); err != nil {
				result = errors.Join(result, err)
				continue
			}
			if err := os.Rename(item.staged, item.original); err != nil {
				result = errors.Join(result, err)
			}
		case originalExists && stagedExists:
			result = errors.Join(result, fmt.Errorf("both original and staged assets exist: %s", item.original))
		default:
			result = errors.Join(result, fmt.Errorf("both original and staged assets are missing: %s", item.original))
		}
	}
	if result != nil {
		return result
	}
	if err := s.removeAll(s.trashRoot); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(s.trashRoot))
	return nil
}

func (s *assetStage) finalize() error {
	if s == nil || s.trashRoot == "" {
		return nil
	}
	if err := s.removeAll(s.trashRoot); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(s.trashRoot))
	return nil
}

func ensureAssetTrash(root string) (string, error) {
	trashBase := filepath.Join(root, ".atri-trash")
	if info, err := os.Lstat(trashBase); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("asset trash path is not a private directory: %s", trashBase)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(trashBase, 0o700); err != nil {
			return "", fmt.Errorf("create asset trash: %w", err)
		}
	} else {
		return "", fmt.Errorf("inspect asset trash: %w", err)
	}
	return trashBase, nil
}

func resolveManagedAssetRoot(assetRoot string) (string, bool, error) {
	root := strings.TrimSpace(assetRoot)
	if root == "" {
		return "", false, errors.New("ATRI_ASSET_ROOT is empty")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false, fmt.Errorf("resolve asset root: %w", err)
	}
	rootInfo, err := os.Stat(absoluteRoot)
	if errors.Is(err, os.ErrNotExist) {
		return absoluteRoot, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect asset root: %w", err)
	}
	if !rootInfo.IsDir() {
		return "", false, fmt.Errorf("asset root is not a directory: %s", absoluteRoot)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return "", false, fmt.Errorf("resolve asset root links: %w", err)
	}
	return resolvedRoot, true, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

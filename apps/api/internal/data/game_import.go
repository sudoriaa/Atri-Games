package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	importManifestName    = "install.json"
	importManifestVersion = 1
)

// ImportedGame is the database-facing part of a validated .atri package.
// Files are installed by ImportGame atomically with the catalog mutation.
type ImportedGame struct {
	Input        GameInput
	Kind         string
	ManifestJSON string
	CoverSource  string
	BundleSource string
}

type importSwap struct {
	target  string
	backup  string
	new     string
	touched bool
}

type importManifest struct {
	Version      int          `json:"version"`
	Slug         string       `json:"slug"`
	ReceiptToken string       `json:"receiptToken"`
	Items        []importItem `json:"items"`
}

type importItem struct {
	Target string `json:"target"`
	Backup string `json:"backup,omitempty"`
	New    string `json:"new,omitempty"`
}

type importStage struct {
	root      string
	stageRoot string
	manifest  importManifest
	swaps     []importSwap
	removeAll func(string) error
}

// ImportGame installs a validated package and its catalog record. The
// filesystem swap and SQLite transaction are serialized with other game
// lifecycle operations. A durable install manifest allows startup recovery
// after a process interruption between the two sides of the change.
func (s *Store) ImportGame(actorID string, packageData ImportedGame, assetRoot string, replace bool) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil {
		return Game{}, err
	}
	if !exists {
		return Game{}, fmt.Errorf("asset root does not exist: %s", assetRoot)
	}
	if packageData.Kind != "external" && packageData.Kind != "static" {
		return Game{}, errors.New("invalid imported runtime kind")
	}
	if packageData.ManifestJSON == "" || packageData.CoverSource == "" {
		return Game{}, errors.New("incomplete imported package")
	}
	if packageData.Input.LaunchOpenIn == "" {
		packageData.Input.LaunchOpenIn = "same-tab"
	}
	if !pathWithinImportWorkspace(root, packageData.CoverSource) {
		return Game{}, fmt.Errorf("imported cover is outside the package workspace: %s", packageData.CoverSource)
	}
	if packageData.BundleSource != "" && !pathWithinImportWorkspace(root, packageData.BundleSource) {
		return Game{}, errors.New("imported game bundle is outside the package workspace")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Game{}, err
	}
	defer tx.Rollback()

	var existingID string
	err = tx.QueryRow(`SELECT id FROM games WHERE slug=?`, packageData.Input.Slug).Scan(&existingID)
	switch {
	case err == nil && !replace:
		return Game{}, ErrGameAlreadyExists
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return Game{}, err
	}
	for _, relative := range []string{
		"covers/" + packageData.Input.Slug,
		"playables/" + packageData.Input.Slug,
	} {
		var shared bool
		if err := tx.QueryRow(
			`SELECT EXISTS(
				SELECT 1 FROM game_assets
				WHERE game_id<>? AND (path=? OR path LIKE ?)
			)`,
			existingID, relative, relative+"/%",
		).Scan(&shared); err != nil {
			return Game{}, err
		}
		if shared {
			return Game{}, fmt.Errorf("%w: %s", ErrAssetShared, relative)
		}
	}

	receiptToken := newID("import")
	stage, err := prepareImportStage(root, packageData, receiptToken, s.removeAssets)
	if err != nil {
		return Game{}, err
	}
	restore := func(cause error) error {
		if restoreErr := stage.rollback(); restoreErr != nil {
			return errors.Join(cause, fmt.Errorf("restore imported assets: %w", restoreErr))
		}
		return cause
	}
	if err := stage.activate(); err != nil {
		return Game{}, restore(err)
	}

	tags, _ := json.Marshal(packageData.Input.Tags)
	if existingID == "" {
		_, err = tx.Exec(`INSERT INTO games(
			id,slug,title,summary,description,author_name,cover_url,launch_url,launch_open_in,repository_url,engine,version,status,category_id,featured,network_required,own_backend,requires_login,platform_storage,matchmaking_enabled,tags_json,published_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CASE WHEN ?='published' THEN strftime('%Y-%m-%dT%H:%M:%SZ','now') ELSE NULL END)`,
			newID("game"), packageData.Input.Slug, packageData.Input.Title, packageData.Input.Summary,
			packageData.Input.Description, packageData.Input.AuthorName, packageData.Input.CoverURL, packageData.Input.LaunchURL, packageData.Input.LaunchOpenIn,
			packageData.Input.RepositoryURL, packageData.Input.Engine, packageData.Input.Version, packageData.Input.Status,
			packageData.Input.CategoryID, packageData.Input.Featured, packageData.Input.NetworkRequired, packageData.Input.OwnBackend,
			packageData.Input.RequiresLogin, packageData.Input.UsesPlatformStorage, packageData.Input.MatchmakingEnabled,
			string(tags), packageData.Input.Status)
	} else {
		_, err = tx.Exec(`UPDATE games SET
			title=?,summary=?,description=?,author_name=?,cover_url=?,launch_url=?,launch_open_in=?,repository_url=?,engine=?,version=?,status=?,category_id=?,featured=?,network_required=?,own_backend=?,requires_login=?,platform_storage=?,matchmaking_enabled=?,tags_json=?,
			published_at=CASE WHEN ?='published' AND published_at IS NULL THEN strftime('%Y-%m-%dT%H:%M:%SZ','now') WHEN ? IN ('draft','review') THEN NULL ELSE published_at END,
			updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
			packageData.Input.Title, packageData.Input.Summary, packageData.Input.Description, packageData.Input.AuthorName,
			packageData.Input.CoverURL, packageData.Input.LaunchURL, packageData.Input.LaunchOpenIn, packageData.Input.RepositoryURL, packageData.Input.Engine,
			packageData.Input.Version, packageData.Input.Status, packageData.Input.CategoryID, packageData.Input.Featured,
			packageData.Input.NetworkRequired, packageData.Input.OwnBackend, packageData.Input.RequiresLogin,
			packageData.Input.UsesPlatformStorage, packageData.Input.MatchmakingEnabled, string(tags), packageData.Input.Status,
			packageData.Input.Status, existingID)
	}
	if err != nil {
		return Game{}, restore(err)
	}
	if existingID != "" {
		if _, err := tx.Exec(
			`DELETE FROM game_assets
			 WHERE game_id=? AND path IN (?,?)`,
			existingID,
			"covers/"+packageData.Input.Slug,
			"playables/"+packageData.Input.Slug,
		); err != nil {
			return Game{}, restore(err)
		}
	}

	var gameID string
	if existingID != "" {
		gameID = existingID
	} else if err := tx.QueryRow(`SELECT id FROM games WHERE slug=?`, packageData.Input.Slug).Scan(&gameID); err != nil {
		return Game{}, restore(err)
	}
	if err := trackGameAssetsTx(tx, gameID, packageData.Input.Slug, packageData.Input.CoverURL, packageData.Input.LaunchURL); err != nil {
		return Game{}, restore(err)
	}
	if _, err := tx.Exec(
		`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json)
		 VALUES(?,?,?,?)
		 ON CONFLICT(game_id) DO UPDATE SET receipt_token=excluded.receipt_token,kind=excluded.kind,manifest_json=excluded.manifest_json,imported_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')`,
		gameID, receiptToken, packageData.Kind, packageData.ManifestJSON,
	); err != nil {
		return Game{}, restore(err)
	}
	action := "game.created"
	if existingID != "" {
		action = "game.updated"
	}
	if err := auditTx(tx, actorID, action, "game", gameID, "imported "+packageData.Kind+" package"); err != nil {
		return Game{}, restore(err)
	}
	if err := tx.Commit(); err != nil {
		var committed bool
		queryErr := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM game_packages WHERE receipt_token=?)`, receiptToken).Scan(&committed)
		if queryErr == nil && committed {
			finalizeErr := stage.finalize()
			game, gameErr := s.gameBy("id", gameID, "", false)
			if gameErr == nil {
				return game, nil
			}
			return Game{}, errors.Join(err, finalizeErr, gameErr)
		}
		if queryErr != nil {
			// Keep the durable stage untouched when the commit outcome cannot
			// be observed. Startup recovery will reconcile it with the receipt.
			return Game{}, errors.Join(err, queryErr)
		}
		return Game{}, restore(err)
	}
	_ = stage.finalize()
	return s.GameByID(gameID, "")
}

// RecoverGameImports resolves install manifests left by an interrupted
// package import. It is safe to call on every startup.
func (s *Store) RecoverGameImports(assetRoot string) error {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()
	return s.recoverGameImports(assetRoot)
}

func (s *Store) recoverGameImports(assetRoot string) error {
	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil || !exists {
		return err
	}
	importsRoot := filepath.Join(root, ".atri-imports")
	if info, err := os.Lstat(importsRoot); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("import workspace is not a private directory: %s", importsRoot)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect import workspace: %w", err)
	}
	entries, err := os.ReadDir(importsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read import workspace: %w", err)
	}
	var result error
	for _, entry := range entries {
		stageRoot := filepath.Join(importsRoot, entry.Name())
		if !entry.IsDir() {
			if removeErr := s.removeAssets(stageRoot); removeErr != nil {
				result = errors.Join(result, removeErr)
			}
			continue
		}
		stage, err := loadImportStage(root, stageRoot, s.removeAssets)
		if errors.Is(err, os.ErrNotExist) {
			// An upload was interrupted before the durable install manifest.
			if removeErr := s.removeAssets(stageRoot); removeErr != nil {
				result = errors.Join(result, removeErr)
			}
			continue
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("load import stage %s: %w", entry.Name(), err))
			continue
		}
		var committed bool
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM game_packages WHERE receipt_token=?)`, stage.manifest.ReceiptToken).Scan(&committed); err != nil {
			result = errors.Join(result, err)
			continue
		}
		if committed {
			if err := stage.finalize(); err != nil {
				result = errors.Join(result, err)
			}
		} else if err := stage.rollback(); err != nil {
			result = errors.Join(result, err)
		}
	}
	if result == nil {
		_ = os.Remove(importsRoot)
	}
	return result
}

func prepareImportStage(root string, packageData ImportedGame, receiptToken string, removeAll func(string) error) (*importStage, error) {
	importsRoot := filepath.Join(root, ".atri-imports")
	stageRoot, err := os.MkdirTemp(importsRoot, "install-")
	if err != nil {
		return nil, fmt.Errorf("create install stage: %w", err)
	}
	stage := &importStage{
		root:      root,
		stageRoot: stageRoot,
		removeAll: removeAll,
		manifest: importManifest{
			Version:      importManifestVersion,
			Slug:         packageData.Input.Slug,
			ReceiptToken: receiptToken,
		},
	}
	if stage.removeAll == nil {
		stage.removeAll = os.RemoveAll
	}
	newRoot := filepath.Join(stageRoot, "new")
	backupRoot := filepath.Join(stageRoot, "backup")
	if err := os.MkdirAll(newRoot, 0o700); err != nil {
		return nil, errors.Join(err, stage.removeAll(stageRoot))
	}
	coverSourceInfo, err := os.Lstat(packageData.CoverSource)
	if err != nil || !coverSourceInfo.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("inspect imported cover: %w", err), stage.removeAll(stageRoot))
	}
	coverExt := strings.ToLower(filepath.Ext(packageData.CoverSource))
	if coverExt == "" {
		return nil, errors.Join(errors.New("imported cover has no extension"), stage.removeAll(stageRoot))
	}
	newCover := filepath.Join(newRoot, "cover")
	if err := os.MkdirAll(newCover, 0o700); err != nil {
		return nil, errors.Join(err, stage.removeAll(stageRoot))
	}
	newCoverFile := filepath.Join(newCover, "cover"+coverExt)
	if err := os.Rename(packageData.CoverSource, newCoverFile); err != nil {
		return nil, errors.Join(fmt.Errorf("stage imported cover: %w", err), stage.removeAll(stageRoot))
	}
	stage.swaps = append(stage.swaps, importSwap{
		target: filepath.Join(root, "covers", packageData.Input.Slug),
		backup: filepath.Join(backupRoot, "cover"),
		new:    newCover,
	})
	if packageData.BundleSource != "" {
		bundleInfo, err := os.Lstat(packageData.BundleSource)
		if err != nil || !bundleInfo.IsDir() || bundleInfo.Mode()&os.ModeSymlink != 0 {
			return nil, errors.Join(errors.New("imported game bundle is not a directory"), stage.removeAll(stageRoot))
		}
		newBundle := filepath.Join(newRoot, "bundle")
		if err := os.Rename(packageData.BundleSource, newBundle); err != nil {
			return nil, errors.Join(fmt.Errorf("stage imported game bundle: %w", err), stage.removeAll(stageRoot))
		}
		stage.swaps = append(stage.swaps, importSwap{
			target: filepath.Join(root, "playables", packageData.Input.Slug),
			backup: filepath.Join(backupRoot, "bundle"),
			new:    newBundle,
		})
	} else {
		stage.swaps = append(stage.swaps, importSwap{
			target: filepath.Join(root, "playables", packageData.Input.Slug),
			backup: filepath.Join(backupRoot, "bundle"),
		})
	}
	for _, item := range stage.swaps {
		target, err := filepath.Rel(root, item.target)
		if err != nil || filepath.IsAbs(target) || !pathWithin(root, item.target) {
			return nil, errors.Join(errors.New("import target escaped asset root"), stage.removeAll(stageRoot))
		}
		backup, err := filepath.Rel(root, item.backup)
		if err != nil || filepath.IsAbs(backup) || !pathWithin(stageRoot, item.backup) {
			return nil, errors.Join(errors.New("import backup escaped stage"), stage.removeAll(stageRoot))
		}
		newPath := ""
		if item.new != "" {
			newPath, err = filepath.Rel(root, item.new)
			if err != nil || filepath.IsAbs(newPath) || !pathWithin(stageRoot, item.new) {
				return nil, errors.Join(errors.New("import new path escaped stage"), stage.removeAll(stageRoot))
			}
		}
		stage.manifest.Items = append(stage.manifest.Items, importItem{
			Target: filepath.ToSlash(target),
			Backup: filepath.ToSlash(backup),
			New:    filepath.ToSlash(newPath),
		})
	}
	if err := stage.writeManifest(); err != nil {
		return nil, errors.Join(err, stage.removeAll(stageRoot))
	}
	return stage, nil
}

func (s *importStage) writeManifest() error {
	raw, err := json.MarshalIndent(s.manifest, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(s.stageRoot, importManifestName+".tmp")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
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
	return os.Rename(temporary, filepath.Join(s.stageRoot, importManifestName))
}

func (s *importStage) activate() error {
	for index, item := range s.swaps {
		if err := os.MkdirAll(filepath.Dir(item.target), 0o755); err != nil {
			return err
		}
		if info, err := os.Lstat(item.target); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to replace symlinked game asset: %s", item.target)
			}
			if err := os.MkdirAll(filepath.Dir(s.manifestPath(index).backup), 0o700); err != nil {
				return err
			}
			if err := os.Rename(item.target, s.manifestPath(index).backup); err != nil {
				return err
			}
			item.touched = true
			s.swaps[index] = item
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if item.new != "" {
			if err := os.Rename(item.new, item.target); err != nil {
				return err
			}
			item.touched = true
		}
		s.swaps[index] = item
	}
	return nil
}

func (s *importStage) manifestPath(index int) importSwap {
	return s.swaps[index]
}

func (s *importStage) rollback() error {
	var result error
	for index := len(s.swaps) - 1; index >= 0; index-- {
		item := s.swaps[index]
		if !item.touched {
			continue
		}
		if item.new != "" {
			// The new directory may have moved to target already.
			if exists, _ := pathExists(item.target); exists {
				if err := s.removeAll(item.target); err != nil {
					result = errors.Join(result, err)
				}
			}
			if exists, _ := pathExists(item.new); exists {
				if err := s.removeAll(item.new); err != nil {
					result = errors.Join(result, err)
				}
			}
		} else if exists, _ := pathExists(item.target); exists {
			if err := s.removeAll(item.target); err != nil {
				result = errors.Join(result, err)
			}
		}
		if exists, _ := pathExists(item.backup); exists {
			if err := os.MkdirAll(filepath.Dir(item.target), 0o755); err != nil {
				result = errors.Join(result, err)
			} else if err := os.Rename(item.backup, item.target); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	if result != nil {
		return result
	}
	return s.finalize()
}

func (s *importStage) finalize() error {
	if s == nil || s.stageRoot == "" {
		return nil
	}
	if err := s.removeAll(s.stageRoot); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(s.stageRoot))
	return nil
}

func loadImportStage(root, stageRoot string, removeAll func(string) error) (*importStage, error) {
	raw, err := os.ReadFile(filepath.Join(stageRoot, importManifestName))
	if err != nil {
		return nil, err
	}
	var manifest importManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != importManifestVersion || !safeBundleName(manifest.Slug) ||
		strings.TrimSpace(manifest.ReceiptToken) == "" || len(manifest.Items) == 0 {
		return nil, errors.New("invalid import manifest")
	}
	stage := &importStage{root: root, stageRoot: stageRoot, manifest: manifest, removeAll: removeAll}
	if stage.removeAll == nil {
		stage.removeAll = os.RemoveAll
	}
	for _, item := range manifest.Items {
		target, ok := safeManifestTarget(root, item.Target)
		if !ok {
			return nil, errors.New("invalid import target")
		}
		backup, ok := safeManifestTarget(root, item.Backup)
		if !ok || !pathWithin(stageRoot, backup) {
			return nil, errors.New("invalid import backup")
		}
		newPath := ""
		if item.New != "" {
			newPath, ok = safeManifestTarget(root, item.New)
			if !ok || !pathWithin(stageRoot, newPath) {
				return nil, errors.New("invalid import new path")
			}
		}
		backupExists, _ := pathExists(backup)
		newExists := false
		if newPath != "" {
			newExists, _ = pathExists(newPath)
		}
		targetExists, _ := pathExists(target)
		stage.swaps = append(stage.swaps, importSwap{
			target:  target,
			backup:  backup,
			new:     newPath,
			touched: backupExists || (newPath != "" && !newExists && targetExists),
		})
	}
	return stage, nil
}

func pathWithinImportWorkspace(assetRoot, candidate string) bool {
	resolvedAssetRoot, assetErr := filepath.EvalSymlinks(assetRoot)
	importRoot := filepath.Join(assetRoot, ".atri-imports")
	resolvedImportRoot, importErr := filepath.EvalSymlinks(importRoot)
	resolvedCandidate, candidateErr := filepath.EvalSymlinks(candidate)
	if assetErr != nil || importErr != nil || candidateErr != nil {
		return false
	}
	return pathWithin(resolvedAssetRoot, resolvedImportRoot) && pathWithin(resolvedImportRoot, resolvedCandidate)
}

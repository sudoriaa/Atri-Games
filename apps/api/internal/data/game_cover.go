package data

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

const (
	gameCoverManifestName    = "install.json"
	gameCoverManifestVersion = 1
)

// GameCoverUpload describes an already validated image kept in the private
// upload workspace. The data layer verifies the digest again before moving it
// into the game's managed cover namespace.
type GameCoverUpload struct {
	SourcePath string
	Extension  string
	SHA256     string
}

type gameCoverReceipt struct {
	Token      string
	TargetPath string
	OldPath    string
}

type storedGameCoverReceipt struct {
	GameID     string
	TargetPath string
	OldPath    string
}

type gameCoverManifest struct {
	Version       int    `json:"version"`
	ReceiptToken  string `json:"receiptToken"`
	Target        string `json:"target"`
	OldTarget     string `json:"oldTarget,omitempty"`
	Digest        string `json:"digest"`
	Extension     string `json:"extension"`
	CreatedTarget bool   `json:"createdTarget"`
}

type gameCoverStage struct {
	root      string
	stageRoot string
	target    string
	incoming  string
	oldTarget string
	manifest  gameCoverManifest
	removeAll func(string) error
}

// ManagedGameCoverURL returns the immutable public URL used for a manual cover.
func ManagedGameCoverURL(slug, digest, extension string) (string, error) {
	if !safeBundleName(slug) || !validCoverDigest(digest) || !validCoverExtension(extension) {
		return "", errors.New("invalid managed game cover")
	}
	return "/" + pathpkg.Join("covers", slug, "cover-"+digest+extension), nil
}

// GameCoverUploadRoot returns the resolved private workspace used while a
// multipart cover is streamed. Resolving the asset root here keeps uploads
// valid when ATRI_ASSET_ROOT itself is a symbolic link.
func GameCoverUploadRoot(assetRoot string) (string, error) {
	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("asset root does not exist: %s", assetRoot)
	}
	uploadRoot := filepath.Join(root, ".atri-cover-uploads")
	if err := ensurePlainDirectory(uploadRoot, 0o700); err != nil {
		return "", fmt.Errorf("prepare cover upload workspace: %w", err)
	}
	if err := os.Chmod(uploadRoot, 0o700); err != nil {
		return "", fmt.Errorf("protect cover upload workspace: %w", err)
	}
	return uploadRoot, nil
}

// CreateGameWithCover installs an uploaded cover and creates the catalog
// record under the same lifecycle lock used by imports and permanent deletion.
// A receipt committed in the same SQLite transaction makes an ambiguous commit
// distinguishable from an ordinary validation or constraint failure.
func (s *Store) CreateGameWithCover(actorID, ownerID string, input GameInput, upload GameCoverUpload, assetRoot string) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	receiptToken := newID("cover")
	stage, err := prepareGameCoverStage(assetRoot, input, upload, receiptToken, "", s.removeAssets)
	if err != nil {
		return Game{}, err
	}
	if err := stage.activate(); err != nil {
		return Game{}, errors.Join(err, stage.rollback())
	}
	receipt := &gameCoverReceipt{
		Token:      receiptToken,
		TargetPath: stage.manifest.Target,
		OldPath:    stage.manifest.OldTarget,
	}
	game, mutationErr := s.createGame(actorID, ownerID, input, receipt)
	return s.resolveGameCoverMutation(stage, game, mutationErr)
}

// UpdateGameWithCover installs an uploaded cover without changing package
// runtime files. Existing package capability declarations remain protected by
// updateGame. A replaced manual content-hash cover is removed only after the
// database receipt is confirmed and only while it remains unreferenced.
func (s *Store) UpdateGameWithCover(actorID, id string, input GameInput, upload GameCoverUpload, assetRoot string) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	current, err := s.GameByID(id, "")
	if err != nil {
		return Game{}, err
	}
	oldPath, err := s.exclusiveReplacedGameCover(current, input, upload)
	if err != nil {
		return Game{}, err
	}
	receiptToken := newID("cover")
	stage, err := prepareGameCoverStage(assetRoot, input, upload, receiptToken, oldPath, s.removeAssets)
	if err != nil {
		return Game{}, err
	}
	if err := stage.activate(); err != nil {
		return Game{}, errors.Join(err, stage.rollback())
	}
	receipt := &gameCoverReceipt{
		Token:      receiptToken,
		TargetPath: stage.manifest.Target,
		OldPath:    stage.manifest.OldTarget,
	}
	game, mutationErr := s.updateGame(actorID, id, input, receipt)
	return s.resolveGameCoverMutation(stage, game, mutationErr)
}

// UpdateGameWithCoverCleanup handles the JSON update path. If an administrator
// switches from a previously uploaded content-hash cover to an external or
// package URL, the now-unreferenced manual file is reclaimed after the
// successful database commit. Cleanup errors deliberately do not turn an
// already committed update into an apparent request failure.
func (s *Store) UpdateGameWithCoverCleanup(actorID, id string, input GameInput, assetRoot string) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	current, err := s.GameByID(id, "")
	if err != nil {
		return Game{}, err
	}
	oldPath, _, oldManaged := managedContentHashCoverReferencePath(current.CoverURL)
	if newPath, _, newManaged := managedContentHashCoverReferencePath(input.CoverURL); newManaged &&
		canonicalAssetKey(newPath) == canonicalAssetKey(oldPath) {
		oldManaged = false
	}
	game, err := s.updateGame(actorID, id, input, nil)
	if err != nil {
		return Game{}, err
	}
	if oldManaged {
		_ = s.removeUnreferencedGameCover(assetRoot, oldPath)
	}
	return game, nil
}

func (s *Store) resolveGameCoverMutation(stage *gameCoverStage, game Game, mutationErr error) (Game, error) {
	if mutationErr == nil {
		// The successful mutation and subsequent read prove that the commit is
		// complete. Finalization failures retain the stage and receipt so the
		// next startup can retry without making the successful request fail.
		_ = s.finalizeGameCoverReceipt(stage)
		return game, nil
	}

	receipt, committed, queryErr := s.gameCoverReceipt(stage.manifest.ReceiptToken)
	if queryErr != nil {
		// Keep the durable stage untouched while the commit outcome is unknown.
		return Game{}, errors.Join(mutationErr, queryErr)
	}
	if !committed {
		return Game{}, errors.Join(mutationErr, stage.rollback())
	}

	committedGame, gameErr := s.GameByID(receipt.GameID, "")
	finalizeErr := s.finalizeGameCoverReceipt(stage)
	if gameErr == nil {
		return committedGame, nil
	}
	return Game{}, errors.Join(mutationErr, finalizeErr, gameErr)
}

// RecoverGameCoverUploads reconciles interrupted cover installations. A stage
// with a committed receipt is finalized; a stage without one is rolled back.
// Raw uploads and pre-manifest stages are safe to discard on every startup.
func (s *Store) RecoverGameCoverUploads(assetRoot string) error {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil || !exists {
		return err
	}
	uploadRoot := filepath.Join(root, ".atri-cover-uploads")
	info, err := os.Lstat(uploadRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect cover upload workspace: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("cover upload workspace is not a private directory: %s", uploadRoot)
	}

	entries, err := os.ReadDir(uploadRoot)
	if err != nil {
		return fmt.Errorf("read cover upload workspace: %w", err)
	}
	var result error
	for _, entry := range entries {
		entryPath := filepath.Join(uploadRoot, entry.Name())
		entryInfo, infoErr := os.Lstat(entryPath)
		if infoErr != nil {
			result = errors.Join(result, infoErr)
			continue
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			result = errors.Join(result, fmt.Errorf("unexpected symbolic link in cover upload workspace: %s", entry.Name()))
			continue
		}
		if !entryInfo.IsDir() {
			if removeErr := os.Remove(entryPath); removeErr != nil {
				result = errors.Join(result, removeErr)
			}
			continue
		}

		stage, loadErr := loadGameCoverStage(root, entryPath, s.removeAssets)
		if errors.Is(loadErr, os.ErrNotExist) {
			// The process stopped before the durable manifest was installed.
			if removeErr := s.removeAssets(entryPath); removeErr != nil {
				result = errors.Join(result, removeErr)
			}
			continue
		}
		if loadErr != nil {
			// Preserve an invalid manifest for inspection rather than acting on
			// paths that have not passed the containment checks below.
			result = errors.Join(result, fmt.Errorf("load cover stage %s: %w", entry.Name(), loadErr))
			continue
		}

		_, committed, receiptErr := s.gameCoverReceipt(stage.manifest.ReceiptToken)
		if receiptErr != nil {
			result = errors.Join(result, receiptErr)
			continue
		}
		if committed {
			if finalizeErr := s.finalizeGameCoverReceipt(stage); finalizeErr != nil {
				result = errors.Join(result, finalizeErr)
			}
		} else if rollbackErr := stage.rollback(); rollbackErr != nil {
			result = errors.Join(result, rollbackErr)
		}
	}
	if result == nil {
		_ = os.Remove(uploadRoot)
	}
	return result
}

func (s *Store) exclusiveReplacedGameCover(current Game, input GameInput, upload GameCoverUpload) (string, error) {
	expectedURL, err := ManagedGameCoverURL(input.Slug, upload.SHA256, upload.Extension)
	if err != nil {
		return "", err
	}
	if current.CoverURL == expectedURL {
		return "", nil
	}
	oldPath, _, ok := managedContentHashCoverPath(current.CoverURL)
	if !ok {
		return "", nil
	}
	shared, err := s.gameCoverPathReferencedByOther(oldPath, current.ID)
	if err != nil {
		return "", err
	}
	if shared {
		return "", nil
	}
	return oldPath, nil
}

func prepareGameCoverStage(
	assetRoot string,
	input GameInput,
	upload GameCoverUpload,
	receiptToken string,
	oldPath string,
	removeAll func(string) error,
) (*gameCoverStage, error) {
	expectedURL, err := ManagedGameCoverURL(input.Slug, upload.SHA256, upload.Extension)
	if err != nil {
		return nil, err
	}
	if input.CoverURL != expectedURL {
		return nil, errors.New("uploaded cover URL does not match its managed destination")
	}
	if strings.TrimSpace(receiptToken) == "" {
		return nil, errors.New("cover receipt token is empty")
	}
	if oldPath != "" {
		if _, _, ok := managedContentHashCoverPath("/" + filepath.ToSlash(oldPath)); !ok {
			return nil, errors.New("invalid replaced game cover")
		}
	}

	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("asset root does not exist: %s", assetRoot)
	}
	uploadRoot, err := GameCoverUploadRoot(root)
	if err != nil {
		return nil, err
	}
	source, err := filepath.Abs(upload.SourcePath)
	if err != nil {
		return nil, err
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil || filepath.Dir(resolvedSource) != uploadRoot || !pathWithin(uploadRoot, resolvedSource) {
		return nil, errors.New("uploaded cover is outside the private upload workspace")
	}
	info, err := os.Lstat(resolvedSource)
	if err != nil {
		return nil, fmt.Errorf("inspect uploaded cover: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("uploaded cover is not a regular file")
	}
	digest, err := fileSHA256(resolvedSource)
	if err != nil {
		return nil, fmt.Errorf("hash uploaded cover: %w", err)
	}
	if digest != upload.SHA256 {
		return nil, errors.New("uploaded cover changed before installation")
	}

	targetRelative := strings.TrimPrefix(expectedURL, "/")
	target, ok := safeManifestTarget(root, targetRelative)
	if !ok || filepath.Dir(target) != filepath.Join(root, "covers", input.Slug) {
		return nil, errors.New("managed cover destination escaped its game directory")
	}
	if err := verifyManagedCoverParent(root, target, true); err != nil {
		return nil, err
	}
	createdTarget := true
	if targetInfo, statErr := os.Lstat(target); statErr == nil {
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() {
			return nil, errors.New("managed cover destination is not a regular file")
		}
		if err := verifyManagedCoverFileAtRoot(root, target, upload.SHA256); err != nil {
			return nil, err
		}
		createdTarget = false
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect managed cover destination: %w", statErr)
	}

	stageRoot, err := os.MkdirTemp(uploadRoot, "install-")
	if err != nil {
		return nil, fmt.Errorf("create cover install stage: %w", err)
	}
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	stage := &gameCoverStage{
		root:      root,
		stageRoot: stageRoot,
		target:    target,
		incoming:  filepath.Join(stageRoot, "incoming"),
		removeAll: removeAll,
		manifest: gameCoverManifest{
			Version:       gameCoverManifestVersion,
			ReceiptToken:  receiptToken,
			Target:        filepath.ToSlash(targetRelative),
			OldTarget:     filepath.ToSlash(oldPath),
			Digest:        upload.SHA256,
			Extension:     upload.Extension,
			CreatedTarget: createdTarget,
		},
	}
	if oldPath != "" {
		stage.oldTarget, ok = safeManifestTarget(root, filepath.ToSlash(oldPath))
		if !ok || stage.oldTarget == stage.target {
			return nil, errors.Join(errors.New("invalid replaced cover target"), stage.removeAll(stageRoot))
		}
	}
	if err := os.Rename(resolvedSource, stage.incoming); err != nil {
		return nil, errors.Join(fmt.Errorf("stage uploaded cover: %w", err), stage.removeAll(stageRoot))
	}
	if err := stage.writeManifest(); err != nil {
		return nil, errors.Join(fmt.Errorf("write cover install manifest: %w", err), stage.removeAll(stageRoot))
	}
	return stage, nil
}

func (stage *gameCoverStage) writeManifest() error {
	raw, err := json.MarshalIndent(stage.manifest, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(stage.stageRoot, gameCoverManifestName+".tmp")
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
	return os.Rename(temporary, filepath.Join(stage.stageRoot, gameCoverManifestName))
}

func (stage *gameCoverStage) activate() error {
	if !stage.manifest.CreatedTarget {
		return verifyManagedCoverFileAtRoot(stage.root, stage.target, stage.manifest.Digest)
	}
	incomingExists, incomingErr := pathExists(stage.incoming)
	targetExists, targetErr := pathExists(stage.target)
	if incomingErr != nil || targetErr != nil {
		return errors.Join(incomingErr, targetErr)
	}
	switch {
	case incomingExists && !targetExists:
		coversRoot := filepath.Join(stage.root, "covers")
		if err := ensurePlainDirectory(coversRoot, 0o755); err != nil {
			return fmt.Errorf("prepare cover root: %w", err)
		}
		if err := ensurePlainDirectory(filepath.Dir(stage.target), 0o755); err != nil {
			return fmt.Errorf("prepare game cover directory: %w", err)
		}
		if err := os.Rename(stage.incoming, stage.target); err != nil {
			return fmt.Errorf("install game cover: %w", err)
		}
		if err := os.Chmod(stage.target, 0o644); err != nil {
			return fmt.Errorf("set game cover permissions: %w", err)
		}
	case !incomingExists && targetExists:
		// Activation completed before the process stopped.
	case incomingExists && targetExists:
		return errors.New("both staged and installed game covers exist")
	default:
		return errors.New("both staged and installed game covers are missing")
	}
	return verifyManagedCoverFileAtRoot(stage.root, stage.target, stage.manifest.Digest)
}

func (stage *gameCoverStage) rollback() error {
	if stage == nil || stage.stageRoot == "" {
		return nil
	}
	var result error
	if stage.manifest.CreatedTarget {
		if exists, err := pathExists(stage.target); err != nil {
			result = errors.Join(result, err)
		} else if exists {
			if err := verifyManagedCoverFileAtRoot(stage.root, stage.target, stage.manifest.Digest); err != nil {
				result = errors.Join(result, err)
			} else if err := os.Remove(stage.target); err != nil {
				result = errors.Join(result, err)
			} else {
				_ = os.Remove(filepath.Dir(stage.target))
			}
		}
	}
	if result != nil {
		return result
	}
	if err := stage.removeAll(stage.stageRoot); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(stage.stageRoot))
	return nil
}

func loadGameCoverStage(root, stageRoot string, removeAll func(string) error) (*gameCoverStage, error) {
	raw, err := os.ReadFile(filepath.Join(stageRoot, gameCoverManifestName))
	if err != nil {
		return nil, err
	}
	var manifest gameCoverManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != gameCoverManifestVersion ||
		strings.TrimSpace(manifest.ReceiptToken) == "" ||
		!validCoverDigest(manifest.Digest) ||
		!validCoverExtension(manifest.Extension) {
		return nil, errors.New("invalid cover install manifest")
	}
	uploadRoot := filepath.Join(root, ".atri-cover-uploads")
	if filepath.Dir(stageRoot) != uploadRoot || !pathWithin(uploadRoot, stageRoot) {
		return nil, errors.New("cover install stage escaped its workspace")
	}
	target, ok := safeManifestTarget(root, manifest.Target)
	if !ok {
		return nil, errors.New("invalid cover install target")
	}
	targetRelative, targetDigest, ok := managedContentHashCoverPath("/" + filepath.ToSlash(manifest.Target))
	if !ok || targetRelative != manifest.Target || targetDigest != manifest.Digest ||
		pathpkg.Ext(manifest.Target) != manifest.Extension {
		return nil, errors.New("cover install target does not match its digest")
	}
	stage := &gameCoverStage{
		root:      root,
		stageRoot: stageRoot,
		target:    target,
		incoming:  filepath.Join(stageRoot, "incoming"),
		manifest:  manifest,
		removeAll: removeAll,
	}
	if stage.removeAll == nil {
		stage.removeAll = os.RemoveAll
	}
	if manifest.OldTarget != "" {
		oldRelative, _, valid := managedContentHashCoverPath("/" + filepath.ToSlash(manifest.OldTarget))
		stage.oldTarget, ok = safeManifestTarget(root, manifest.OldTarget)
		if !valid || !ok || oldRelative != manifest.OldTarget || stage.oldTarget == stage.target {
			return nil, errors.New("invalid replaced cover in install manifest")
		}
	}
	return stage, nil
}

func (s *Store) finalizeGameCoverReceipt(stage *gameCoverStage) error {
	receipt, committed, err := s.gameCoverReceipt(stage.manifest.ReceiptToken)
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("game cover receipt is not committed")
	}
	if receipt.TargetPath != stage.manifest.Target || receipt.OldPath != stage.manifest.OldTarget {
		return errors.New("game cover receipt does not match its install manifest")
	}

	targetReferenced, err := s.gameCoverPathReferenced(stage.manifest.Target)
	if err != nil {
		return err
	}
	if targetReferenced {
		if err := verifyManagedCoverFileAtRoot(stage.root, stage.target, stage.manifest.Digest); err != nil {
			return err
		}
	} else if stage.manifest.CreatedTarget {
		if exists, existsErr := pathExists(stage.target); existsErr != nil {
			return existsErr
		} else if exists {
			if err := verifyManagedCoverFileAtRoot(stage.root, stage.target, stage.manifest.Digest); err != nil {
				return err
			}
			if err := os.Remove(stage.target); err != nil {
				return err
			}
			_ = os.Remove(filepath.Dir(stage.target))
		}
	}

	if stage.oldTarget != "" {
		oldURL := "/" + filepath.ToSlash(stage.manifest.OldTarget)
		oldReferenced, err := s.gameCoverPathReferenced(stage.manifest.OldTarget)
		if err != nil {
			return err
		}
		if !oldReferenced {
			_, oldDigest, _ := managedContentHashCoverPath(oldURL)
			if exists, existsErr := pathExists(stage.oldTarget); existsErr != nil {
				return existsErr
			} else if exists {
				if err := verifyManagedCoverFileAtRoot(stage.root, stage.oldTarget, oldDigest); err != nil {
					return err
				}
				if err := os.Remove(stage.oldTarget); err != nil {
					return err
				}
				_ = os.Remove(filepath.Dir(stage.oldTarget))
			}
		}
	}

	if err := stage.removeAll(stage.stageRoot); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(stage.stageRoot))
	_, err = s.db.Exec(`DELETE FROM game_cover_receipts WHERE receipt_token=?`, stage.manifest.ReceiptToken)
	return err
}

func (s *Store) gameCoverPathReferenced(relative string) (bool, error) {
	return s.gameCoverPathReferencedByOther(relative, "")
}

func (s *Store) gameCoverPathReferencedByOther(relative, excludedGameID string) (bool, error) {
	rows, err := s.db.Query(`SELECT id,cover_url FROM games`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var gameID, coverURL string
		if err := rows.Scan(&gameID, &coverURL); err != nil {
			return false, err
		}
		if excludedGameID != "" && gameID == excludedGameID {
			continue
		}
		if candidate, _, ok := managedContentHashCoverReferencePath(coverURL); ok &&
			canonicalAssetKey(candidate) == canonicalAssetKey(relative) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) removeUnreferencedGameCover(assetRoot, relative string) error {
	referenced, err := s.gameCoverPathReferenced(relative)
	if err != nil || referenced {
		return err
	}
	normalized, digest, ok := managedContentHashCoverPath("/" + filepath.ToSlash(relative))
	if !ok || normalized != filepath.ToSlash(relative) {
		return errors.New("invalid unreferenced game cover")
	}
	root, exists, err := resolveManagedAssetRoot(assetRoot)
	if err != nil || !exists {
		return err
	}
	target, ok := safeManifestTarget(root, normalized)
	if !ok {
		return errors.New("unreferenced game cover escaped asset root")
	}
	exists, err = pathExists(target)
	if err != nil || !exists {
		return err
	}
	if err := verifyManagedCoverFileAtRoot(root, target, digest); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(target))
	return nil
}

func (s *Store) gameCoverReceipt(token string) (storedGameCoverReceipt, bool, error) {
	var receipt storedGameCoverReceipt
	err := s.db.QueryRow(
		`SELECT game_id,target_path,old_path FROM game_cover_receipts WHERE receipt_token=?`,
		token,
	).Scan(&receipt.GameID, &receipt.TargetPath, &receipt.OldPath)
	if errors.Is(err, sql.ErrNoRows) {
		return storedGameCoverReceipt{}, false, nil
	}
	return receipt, err == nil, err
}

func recordGameCoverReceiptTx(tx *sql.Tx, gameID string, receipt *gameCoverReceipt) error {
	if receipt == nil {
		return nil
	}
	if strings.TrimSpace(receipt.Token) == "" {
		return errors.New("game cover receipt token is empty")
	}
	if relative, _, ok := managedContentHashCoverPath("/" + filepath.ToSlash(receipt.TargetPath)); !ok || relative != receipt.TargetPath {
		return errors.New("game cover receipt target is invalid")
	}
	if receipt.OldPath != "" {
		if relative, _, ok := managedContentHashCoverPath("/" + filepath.ToSlash(receipt.OldPath)); !ok || relative != receipt.OldPath {
			return errors.New("game cover receipt old target is invalid")
		}
	}
	_, err := tx.Exec(
		`INSERT INTO game_cover_receipts(receipt_token,game_id,target_path,old_path) VALUES(?,?,?,?)`,
		receipt.Token,
		gameID,
		receipt.TargetPath,
		receipt.OldPath,
	)
	return err
}

func managedContentHashCoverPath(rawURL string) (string, string, bool) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(parsed.Path, "\\\x00") {
		return "", "", false
	}
	return managedContentHashCoverParsedPath(parsed.Path)
}

func managedContentHashCoverReferencePath(rawURL string) (string, string, bool) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		strings.ContainsAny(parsed.Path, "\\\x00") {
		return "", "", false
	}
	return managedContentHashCoverParsedPath(parsed.Path)
}

func managedContentHashCoverParsedPath(rawPath string) (string, string, bool) {
	cleanPath := pathpkg.Clean(rawPath)
	if cleanPath != rawPath || !strings.HasPrefix(cleanPath, "/") {
		return "", "", false
	}
	segments := strings.Split(strings.TrimPrefix(cleanPath, "/"), "/")
	if len(segments) != 3 || segments[0] != "covers" || !safeBundleName(segments[1]) {
		return "", "", false
	}
	extension := pathpkg.Ext(segments[2])
	if !validCoverExtension(extension) {
		return "", "", false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(segments[2], "cover-"), extension)
	if !strings.HasPrefix(segments[2], "cover-") || !validCoverDigest(digest) {
		return "", "", false
	}
	return pathpkg.Join(segments...), digest, true
}

func verifyManagedCoverFile(filename, expectedDigest string) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("inspect managed game cover: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("managed game cover is not a regular file")
	}
	digest, err := fileSHA256(filename)
	if err != nil {
		return err
	}
	if digest != expectedDigest {
		return errors.New("managed game cover digest changed")
	}
	return nil
}

func verifyManagedCoverFileAtRoot(root, filename, expectedDigest string) error {
	if err := verifyManagedCoverParent(root, filename, false); err != nil {
		return err
	}
	return verifyManagedCoverFile(filename, expectedDigest)
}

func verifyManagedCoverParent(root, filename string, allowMissing bool) error {
	if !pathWithin(root, filename) {
		return errors.New("managed game cover escaped asset root")
	}
	relative, err := filepath.Rel(root, filename)
	if err != nil {
		return err
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if len(segments) != 3 || segments[0] != "covers" || !safeBundleName(segments[1]) {
		return errors.New("managed game cover has an invalid parent")
	}
	for _, directory := range []string{
		filepath.Join(root, "covers"),
		filepath.Join(root, "covers", segments[1]),
	} {
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) && allowMissing {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect managed game cover parent: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("managed game cover parent is not a plain directory")
		}
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(filename))
	if err != nil {
		return fmt.Errorf("resolve managed game cover parent: %w", err)
	}
	if !pathWithin(root, resolvedParent) {
		return errors.New("managed game cover parent escaped asset root")
	}
	return nil
}

func ensurePlainDirectory(target string, mode os.FileMode) error {
	if err := os.MkdirAll(target, mode); err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a plain directory")
	}
	return nil
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

func validCoverDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validCoverExtension(value string) bool {
	switch value {
	case ".avif", ".jpg", ".png", ".webp":
		return true
	default:
		return false
	}
}

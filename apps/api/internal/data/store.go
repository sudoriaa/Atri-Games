package data

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrLastAdmin         = errors.New("at least one active admin is required")
	ErrGameAlreadyExists = errors.New("game already exists")
	ErrAssetShared       = errors.New("managed asset is referenced by another game")
	// ErrGameNotReviewable is returned when approving a game whose status is
	// neither "review" nor already "published".
	ErrGameNotReviewable = errors.New("game is not in a reviewable state")
	ErrInvalidFollow     = errors.New("invalid follow relationship")
	ErrInvalidBlock      = errors.New("invalid block relationship")
	ErrForbidden         = errors.New("operation forbidden")
	ErrInvalidReport     = errors.New("invalid content report")
	ErrInvalidAppeal     = errors.New("invalid moderation appeal")
	ErrAppealExists      = errors.New("moderation appeal already exists")
)

const maxUserNumber int64 = 1<<63 - 1

type Store struct {
	db           *sql.DB
	gameMu       sync.Mutex
	removeAssets func(string) error
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?" + url.Values{
		"_pragma": []string{
			"busy_timeout(5000)",
			"journal_mode(WAL)",
			"foreign_keys(1)",
			"synchronous(NORMAL)",
		},
		"_txlock": []string{"immediate"},
	}.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, removeAssets: os.RemoveAll}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) MigrateAndSeed(adminEmail, adminHash string) error {
	var gamesTableExisted bool
	if err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM sqlite_master WHERE type='table' AND name='games'
	)`).Scan(&gamesTableExisted); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS app_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			user_number INTEGER NOT NULL UNIQUE CHECK(user_number > 0),
			email TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			display_name TEXT NOT NULL,
			avatar_url TEXT NOT NULL DEFAULT '',
			bio TEXT NOT NULL DEFAULT '',
			website_url TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL CHECK(role IN ('user', 'admin')) DEFAULT 'user',
			status TEXT NOT NULL CHECK(status IN ('active', 'suspended')) DEFAULT 'active',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS user_number_sequence (
			singleton INTEGER PRIMARY KEY CHECK(singleton=1),
			next_value INTEGER NOT NULL CHECK(next_value > 0)
		)`,
		`CREATE TABLE IF NOT EXISTS categories (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS games (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			summary TEXT NOT NULL,
			description TEXT NOT NULL,
			author_name TEXT NOT NULL,
			cover_url TEXT NOT NULL,
			launch_url TEXT NOT NULL,
			launch_open_in TEXT NOT NULL DEFAULT 'same-tab',
			repository_url TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL,
			version TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('draft', 'review', 'published', 'hidden')) DEFAULT 'draft',
			owner_user_id TEXT,
			category_id TEXT NOT NULL REFERENCES categories(id) ON UPDATE CASCADE ON DELETE RESTRICT,
			featured INTEGER NOT NULL DEFAULT 0,
			network_required INTEGER NOT NULL DEFAULT 0,
			own_backend INTEGER NOT NULL DEFAULT 0,
			requires_login INTEGER NOT NULL DEFAULT 0,
			platform_storage INTEGER NOT NULL DEFAULT 0,
			matchmaking_enabled INTEGER NOT NULL DEFAULT 0,
			tags_json TEXT NOT NULL DEFAULT '[]',
			play_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			published_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS game_assets (
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			is_directory INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(game_id, path)
		)`,
		`CREATE TABLE IF NOT EXISTS game_packages (
			game_id TEXT PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
			receipt_token TEXT NOT NULL UNIQUE,
			kind TEXT NOT NULL CHECK(kind IN ('external', 'static')),
			manifest_json TEXT NOT NULL,
			imported_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS game_cover_receipts (
			receipt_token TEXT PRIMARY KEY,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			target_path TEXT NOT NULL,
			old_path TEXT NOT NULL DEFAULT '',
			committed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS game_player_data (
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			data_key TEXT NOT NULL,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(game_id, user_id, data_key)
		)`,
		`CREATE TABLE IF NOT EXISTS matchmaking_tickets (
			id TEXT PRIMARY KEY,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			mode TEXT NOT NULL DEFAULT 'default',
			region TEXT NOT NULL DEFAULT 'global',
			status TEXT NOT NULL CHECK(status IN ('waiting','matched','cancelled','expired')),
			match_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			expires_at TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS favorites (
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(user_id, game_id)
		)`,
		`CREATE TABLE IF NOT EXISTS game_likes (
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(user_id, game_id)
		)`,
		`CREATE TABLE IF NOT EXISTS game_comments (
			id TEXT PRIMARY KEY,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			parent_id TEXT REFERENCES game_comments(id) ON DELETE CASCADE,
			body TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'visible',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS game_comment_likes (
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			comment_id TEXT NOT NULL REFERENCES game_comments(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(user_id, comment_id)
		)`,
		`CREATE TABLE IF NOT EXISTS game_share_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			channel TEXT NOT NULL DEFAULT 'link',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS creator_follows (
			follower_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			creator_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(follower_user_id, creator_user_id),
			CHECK(follower_user_id != creator_user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS user_blocks (
			blocker_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			blocked_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(blocker_user_id, blocked_user_id),
			CHECK(blocker_user_id != blocked_user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS game_follows (
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY(user_id, game_id)
		)`,
		`CREATE TABLE IF NOT EXISTS game_versions (
			id TEXT PRIMARY KEY,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			version TEXT NOT NULL,
			release_notes TEXT NOT NULL DEFAULT '',
			snapshot_json TEXT NOT NULL,
			created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS community_events (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			game_id TEXT REFERENCES games(id) ON DELETE CASCADE,
			summary TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			game_id TEXT REFERENCES games(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			body TEXT NOT NULL DEFAULT '',
			link TEXT NOT NULL DEFAULT '',
			read_at TEXT,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS content_reports (
			id TEXT PRIMARY KEY,
			reporter_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			reason TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			resolution TEXT NOT NULL DEFAULT '',
			resolved_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS moderation_appeals (
			id TEXT PRIMARY KEY,
			report_id TEXT NOT NULL UNIQUE REFERENCES content_reports(id) ON DELETE CASCADE,
			appellant_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			report_status TEXT NOT NULL,
			reason TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			resolution TEXT NOT NULL DEFAULT '',
			resolved_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS play_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
			action TEXT NOT NULL,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_games_status_category ON games(status, category_id)`,
		`CREATE INDEX IF NOT EXISTS idx_games_published ON games(featured DESC, play_count DESC, published_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_assets_path ON game_assets(path)`,
		`CREATE INDEX IF NOT EXISTS idx_game_packages_token ON game_packages(receipt_token)`,
		`CREATE INDEX IF NOT EXISTS idx_game_player_data_updated ON game_player_data(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_matchmaking_waiting ON matchmaking_tickets(game_id,mode,region,status,created_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_matchmaking_user_waiting ON matchmaking_tickets(game_id,user_id) WHERE status='waiting'`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_created ON play_events(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_game_likes_game ON game_likes(game_id)`,
		`CREATE INDEX IF NOT EXISTS idx_game_comments_thread ON game_comments(game_id,parent_id,created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_game_comments_user ON game_comments(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_game_comment_likes_comment ON game_comment_likes(comment_id)`,
		`CREATE INDEX IF NOT EXISTS idx_game_share_events_game ON game_share_events(game_id)`,
		`CREATE INDEX IF NOT EXISTS idx_creator_follows_creator ON creator_follows(creator_user_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_user_blocks_blocked ON user_blocks(blocked_user_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_follows_game ON game_follows(game_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_versions_game ON game_versions(game_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_community_events_created ON community_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id,read_at,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_content_reports_status ON content_reports(status,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_moderation_appeals_status ON moderation_appeals(status,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureUserColumns(tx); err != nil {
		return err
	}
	if err := ensureLaunchOpenInColumn(tx); err != nil {
		return err
	}
	if err := ensurePlatformColumns(tx); err != nil {
		return err
	}
	if err := ensureGameOwnerColumn(tx); err != nil {
		return err
	}
	// The owner index must be created after the column is guaranteed to exist
	// (ensureGameOwnerColumn), because on pre-existing databases the ALTER TABLE
	// runs during migration and the CREATE INDEX would otherwise fail.
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_games_owner_user ON games(owner_user_id)`); err != nil {
		return err
	}
	if err := backfillPlatformColumns(tx); err != nil {
		return err
	}
	if err := s.seed(tx, adminEmail, adminHash, !gamesTableExisted); err != nil {
		return err
	}
	if err := backfillGameVersionsTx(tx); err != nil {
		return err
	}
	if err := backfillUserNumbers(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_user_number ON users(user_number)`); err != nil {
		return err
	}
	if err := ensureUserNumberSequence(tx); err != nil {
		return err
	}
	if err := migrateSeedLaunchURLs(tx); err != nil {
		return err
	}
	if err := migratePlayableURLsToGamePath(tx); err != nil {
		return err
	}
	if err := normalizeGamePlayURLs(tx); err != nil {
		return err
	}
	if err := backfillGameAssets(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ensureUserColumns upgrades databases created before public account numbers
// and avatars were introduced. user_number stays nullable during the ALTER so
// historical rows can receive deterministic values in backfillUserNumbers.
func ensureUserColumns(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !columns["user_number"] {
		if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN user_number INTEGER`); err != nil {
			return err
		}
	}
	if !columns["avatar_url"] {
		if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !columns["bio"] {
		if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !columns["website_url"] {
		if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN website_url TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

type userNumberRow struct {
	id     string
	number sql.NullInt64
}

// backfillUserNumbers makes the seed administrator public user #1 and gives
// pre-existing users stable numbers by their original creation order. Later
// startups preserve already valid values, including any gaps caused by a
// future account-deletion feature.
func backfillUserNumbers(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id,user_number FROM users
		ORDER BY CASE WHEN id='usr_admin' THEN 0 ELSE 1 END, created_at ASC, id ASC`)
	if err != nil {
		return err
	}
	items := []userNumberRow{}
	for rows.Next() {
		var item userNumberRow
		if err := rows.Scan(&item.id, &item.number); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var (
		adminFound   bool
		needsRebuild bool
		maxNumber    int64
		seen         = map[int64]struct{}{}
	)
	for _, item := range items {
		if item.id == "usr_admin" {
			adminFound = true
			if !item.number.Valid || item.number.Int64 != 1 {
				needsRebuild = true
			}
		}
		if !item.number.Valid {
			continue
		}
		if item.number.Int64 <= 0 {
			needsRebuild = true
			continue
		}
		if _, duplicate := seen[item.number.Int64]; duplicate {
			needsRebuild = true
		}
		seen[item.number.Int64] = struct{}{}
		if item.number.Int64 > maxNumber {
			maxNumber = item.number.Int64
		}
	}
	if !adminFound {
		return errors.New("seed administrator is missing")
	}

	if needsRebuild {
		// Move every row through an unused positive range first. This keeps the
		// repair safe when a partially applied migration already has a UNIQUE
		// user_number index.
		if maxNumber > maxUserNumber-2*int64(len(items)) {
			return errors.New("user number range is exhausted")
		}
		temporaryStart := maxNumber + int64(len(items)) + 1
		if temporaryStart <= maxNumber {
			return errors.New("user number range is exhausted")
		}
		for index, item := range items {
			if _, err := tx.Exec(`UPDATE users SET user_number=? WHERE id=?`, temporaryStart+int64(index), item.id); err != nil {
				return err
			}
		}
		for index, item := range items {
			if _, err := tx.Exec(`UPDATE users SET user_number=? WHERE id=?`, int64(index+1), item.id); err != nil {
				return err
			}
		}
		return nil
	}

	for _, item := range items {
		if item.number.Valid {
			continue
		}
		if maxNumber == maxUserNumber {
			return errors.New("user number range is exhausted")
		}
		maxNumber++
		if _, err := tx.Exec(`UPDATE users SET user_number=? WHERE id=?`, maxNumber, item.id); err != nil {
			return err
		}
	}
	return nil
}

// ensureUserNumberSequence preserves the next value independently of current
// rows. That prevents a future account deletion from causing public user IDs
// to be reused, while also repairing an old or partially initialized volume.
func ensureUserNumberSequence(tx *sql.Tx) error {
	var maxNumber int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(user_number),0) FROM users`).Scan(&maxNumber); err != nil {
		return err
	}
	if maxNumber == maxUserNumber {
		return errors.New("user number range is exhausted")
	}
	nextValue := maxNumber + 1
	_, err := tx.Exec(`INSERT INTO user_number_sequence(singleton,next_value) VALUES(1,?)
		ON CONFLICT(singleton) DO UPDATE SET next_value=CASE
			WHEN excluded.next_value > next_value THEN excluded.next_value
			ELSE next_value
		END`, nextValue)
	return err
}

func ensureLaunchOpenInColumn(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(games)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "launch_open_in" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.Exec(`ALTER TABLE games ADD COLUMN launch_open_in TEXT NOT NULL DEFAULT 'same-tab'`)
	return err
}

// ensurePlatformColumns keeps existing SQLite volumes compatible with the
// platform capability hints introduced after the original catalog schema.
// SQLite cannot add a CHECK-constrained column with a portable ALTER, so the
// columns deliberately use integer booleans and conservative zero defaults.
func ensurePlatformColumns(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(games)`)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		found[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, column := range []string{
		"requires_login INTEGER NOT NULL DEFAULT 0",
		"platform_storage INTEGER NOT NULL DEFAULT 0",
		"matchmaking_enabled INTEGER NOT NULL DEFAULT 0",
	} {
		name := strings.Fields(column)[0]
		if found[name] {
			continue
		}
		if _, err := tx.Exec(`ALTER TABLE games ADD COLUMN ` + column); err != nil {
			return err
		}
	}
	return nil
}

// ensureGameOwnerColumn upgrades databases created before user-submitted
// games were supported. Ownership is enforced at the application layer, so the
// column is added without a foreign key for portability across existing volumes.
func ensureGameOwnerColumn(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(games)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "owner_user_id" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.Exec(`ALTER TABLE games ADD COLUMN owner_user_id TEXT`)
	return err
}

func backfillPlatformColumns(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT game_id,kind,manifest_json FROM game_packages`)
	if err != nil {
		return err
	}
	type packageRow struct {
		gameID, kind, manifest string
	}
	var packages []packageRow
	for rows.Next() {
		var item packageRow
		if err := rows.Scan(&item.gameID, &item.kind, &item.manifest); err != nil {
			rows.Close()
			return err
		}
		packages = append(packages, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range packages {
		if (item.kind != "static" && item.kind != "external") || !json.Valid([]byte(item.manifest)) {
			continue
		}
		platform := resolveGamePlatform(GamePlatform{
			Kind:                item.kind,
			IdentityMode:        IdentityNone,
			StorageProvider:     StorageNone,
			StorageScope:        StorageScopePlayerGame,
			MatchmakingProtocol: "http",
		}, item.manifest)
		if _, err := tx.Exec(
			`UPDATE games SET requires_login=?,platform_storage=?,matchmaking_enabled=? WHERE id=?`,
			platform.RequiresLogin, platform.UsesPlatformStorage, platform.MatchmakingEnabled, item.gameID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seed(tx *sql.Tx, adminEmail, adminHash string, initializeGames bool) error {
	categories := []Category{
		{ID: "arcade", Name: "街机动作", Description: "短回合、快速反馈与技巧挑战", SortOrder: 10},
		{ID: "adventure", Name: "动作冒险", Description: "平台跳跃、关卡探索与即时战斗", SortOrder: 15},
		{ID: "puzzle", Name: "益智解谜", Description: "观察、推理与空间思考", SortOrder: 20},
		{ID: "rpg", Name: "角色扮演", Description: "角色成长、装备构筑与任务旅程", SortOrder: 25},
		{ID: "strategy", Name: "策略经营", Description: "规划资源并建立长期优势", SortOrder: 30},
		{ID: "simulation", Name: "模拟养成", Description: "经营建造、生活模拟与角色养成", SortOrder: 35},
		{ID: "narrative", Name: "叙事探索", Description: "由选择推动的互动故事", SortOrder: 40},
		{ID: "card", Name: "卡牌桌游", Description: "卡组构筑、回合博弈与桌面规则", SortOrder: 45},
		{ID: "rhythm", Name: "音乐节奏", Description: "音符判定、节拍挑战与音乐互动", SortOrder: 50},
		{ID: "sports-racing", Name: "体育竞速", Description: "球类竞技、赛车与运动挑战", SortOrder: 60},
		{ID: "shooter", Name: "射击竞技", Description: "精准射击、弹幕躲避与战术对抗", SortOrder: 70},
		{ID: "survival-horror", Name: "生存恐怖", Description: "资源管理、潜行求生与惊悚氛围", SortOrder: 80},
		{ID: "sandbox", Name: "沙盒创造", Description: "自由建造、开放玩法与创意表达", SortOrder: 90},
		{ID: "casual-party", Name: "休闲派对", Description: "轻量规则、聚会同乐与碎片时间", SortOrder: 100},
		{ID: "multiplayer", Name: "多人社交", Description: "在线合作、竞技对抗与社交互动", SortOrder: 110},
		{ID: "educational", Name: "教育科普", Description: "知识探索、技能训练与互动学习", SortOrder: 120},
	}
	for _, category := range categories {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO categories(id,name,description,sort_order) VALUES(?,?,?,?)`, category.ID, category.Name, category.Description, category.SortOrder); err != nil {
			return err
		}
	}

	adminID := "usr_admin"
	if _, err := tx.Exec(`INSERT OR IGNORE INTO users(id,user_number,email,password_hash,display_name,role,status,created_at) VALUES(?,?,?,?,?,?,?,strftime('%Y-%m-%dT%H:%M:%SZ','now'))`, adminID, 1, adminEmail, adminHash, "Atri 管理员", "admin", "active"); err != nil {
		return err
	}

	var seedState string
	err := tx.QueryRow(`SELECT value FROM app_meta WHERE key='initial_game_seed_v1'`).Scan(&seedState)
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	if !initializeGames {
		_, err = tx.Exec(`INSERT INTO app_meta(key,value) VALUES('initial_game_seed_v1','preserved-existing')`)
		return err
	}

	seedGames := []struct {
		id, slug, title, summary, description, author, cover, launch, engine, version, category, tags string
		featured, network, backend, plays                                                             int
	}{
		{"game_neon", "neon-relay", "霓光中继", "在不断折叠的航线上传递能量，维持整座浮空城的信号。", "控制信号艇穿越动态航道，收集脉冲节点并避开失真的数据风暴。每一轮都由实时规则重新排列。", "Mori Lab", "/covers/neon-relay.webp", "/demos/neon-relay/index.html", "Canvas", "1.3.0", "arcade", `["反应","科幻","短回合"]`, 1, 0, 0, 4821},
		{"game_circuit", "circuit-bloom", "电路花园", "让沉睡的机械花园重新连通，在有限步数中唤醒每一片区域。", "旋转与交换电路模块，把能源引向休眠节点。关卡会记录你的最短路径，并生成新的布局。", "Kite Studio", "/covers/circuit-bloom.webp", "/demos/circuit-bloom/index.html", "React", "0.9.4", "puzzle", `["逻辑","轻松","关卡"]`, 1, 0, 0, 3650},
		{"game_echo", "echo-vault", "回声档案", "与一座会遗忘的档案馆对话，从残缺记录中拼回事件全貌。", "一款文字与声音驱动的调查游戏。你的提问会改变档案馆保存的线索，并开启不同调查路径。", "North Window", "/covers/echo-vault.webp", "/demos/echo-vault/index.html", "Vue", "1.1.2", "narrative", `["叙事","推理","多结局"]`, 1, 1, 1, 2984},
		{"game_orbit", "paper-orbit", "纸上轨道", "用几笔轨道引导探测器，在墨水耗尽前抵达未知行星。", "绘制引力轨道、借助星体弹弓并控制有限燃料。支持每日挑战与本地最佳成绩。", "Tiny Comet", "/covers/paper-orbit.webp", "/demos/paper-orbit/index.html", "Phaser", "1.0.1", "puzzle", `["物理","绘画","每日挑战"]`, 0, 0, 0, 1876},
		{"game_forge", "pixel-forge", "像素铸造局", "经营一间只接受奇怪订单的像素工坊，平衡材料、时间与口碑。", "从小型订单开始扩建设备，组合材料属性并训练自动助手。每个订单都可能改变街区经济。", "Soft Hammer", "/covers/pixel-forge.webp", "/demos/pixel-forge/index.html", "Godot Web", "0.8.0", "strategy", `["经营","像素","自动化"]`, 0, 1, 1, 1442},
		{"game_tide", "memory-tide", "潮汐记忆", "在潮水抹去道路前记住岛屿的形状，带领灯火回到港湾。", "观察短暂出现的安全路径，在潮汐覆盖后凭记忆移动。难度会根据连续成功次数动态调整。", "Aster Works", "/covers/memory-tide.webp", "/demos/memory-tide/index.html", "Unity Web", "0.7.5", "arcade", `["记忆","节奏","氛围"]`, 0, 0, 0, 987},
	}
	for _, game := range seedGames {
		_, err := tx.Exec(`INSERT OR IGNORE INTO games(
			id,slug,title,summary,description,author_name,cover_url,launch_url,repository_url,engine,version,status,category_id,featured,network_required,own_backend,tags_json,play_count,published_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,strftime('%Y-%m-%dT%H:%M:%SZ','now'))`,
			game.id, game.slug, game.title, game.summary, game.description, game.author, game.cover, game.launch, "", game.engine, game.version, "published", game.category, game.featured, game.network, game.backend, game.tags, game.plays)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(`INSERT INTO app_meta(key,value) VALUES('initial_game_seed_v1','seeded')`)
	return err
}

func migrateSeedLaunchURLs(tx *sql.Tx) error {
	seedLaunches := []struct {
		id, slug string
	}{
		{"game_neon", "neon-relay"},
		{"game_circuit", "circuit-bloom"},
		{"game_echo", "echo-vault"},
		{"game_orbit", "paper-orbit"},
		{"game_forge", "pixel-forge"},
		{"game_tide", "memory-tide"},
	}
	for _, game := range seedLaunches {
		oldURL := "/demos/arcade/index.html?game=" + game.slug
		newURL := "/demos/" + game.slug + "/index.html"
		if _, err := tx.Exec(
			`UPDATE games SET launch_url=?,updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
			 WHERE id=? AND slug=? AND launch_url=?`,
			newURL, game.id, game.slug, oldURL,
		); err != nil {
			return err
		}
	}
	return nil
}

// migratePlayableURLsToGamePath rewrites static-package launch_urls from the
// legacy /playables/<slug>/[entry] form to the unified /games/<slug>/play[/entry]
// path served by the main Caddy site. The migration is idempotent: rows that
// already carry the new form are left unchanged.
func migratePlayableURLsToGamePath(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, slug, launch_url FROM games WHERE launch_url LIKE '/playables/%'`)
	if err != nil {
		return err
	}
	type row struct{ id, slug, launchURL string }
	var updates []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.slug, &r.launchURL); err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range updates {
		root := "/playables/" + r.slug
		prefix := root + "/"
		var newURL string
		if r.launchURL == root || r.launchURL == prefix || r.launchURL == prefix+"index.html" {
			newURL = "/games/" + r.slug + "/play/"
		} else if strings.HasPrefix(r.launchURL, prefix) {
			entry := strings.TrimPrefix(r.launchURL, prefix)
			if entry == "index.html" {
				newURL = "/games/" + r.slug + "/play/"
			} else {
				newURL = "/games/" + r.slug + "/play/" + entry
			}
		} else {
			// unexpected form — skip safely
			continue
		}
		if _, err := tx.Exec(
			`UPDATE games SET launch_url=?,updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
			newURL, r.id,
		); err != nil {
			return err
		}
	}
	return nil
}

func normalizeGamePlayURLs(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE games
		SET launch_url='/games/' || slug || '/play/',
			updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE launch_url='/games/' || slug || '/play'`)
	return err
}

func newID(prefix string) string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

func (s *Store) CreateUser(email, passwordHash, displayName string) (User, error) {
	user := User{ID: newID("usr"), Email: strings.ToLower(email), DisplayName: displayName, Role: "user", Status: "active"}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT next_value FROM user_number_sequence WHERE singleton=1`).Scan(&user.UserNumber); err != nil {
		return User{}, err
	}
	if user.UserNumber <= 0 || user.UserNumber == maxUserNumber {
		return User{}, errors.New("user number range is exhausted")
	}
	nextValue := user.UserNumber + 1
	result, err := tx.Exec(`UPDATE user_number_sequence SET next_value=? WHERE singleton=1 AND next_value=?`, nextValue, user.UserNumber)
	if err != nil {
		return User{}, err
	}
	if count, err := result.RowsAffected(); err != nil {
		return User{}, err
	} else if count != 1 {
		return User{}, errors.New("user number sequence changed concurrently")
	}
	if _, err := tx.Exec(`INSERT INTO users(id,user_number,email,password_hash,display_name,role,status,created_at) VALUES(?,?,?,?,?,?,?,strftime('%Y-%m-%dT%H:%M:%SZ','now'))`, user.ID, user.UserNumber, user.Email, passwordHash, user.DisplayName, user.Role, user.Status); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return s.UserByID(user.ID)
}

func (s *Store) UserByEmail(email string) (User, error) {
	return scanUser(s.db.QueryRow(`SELECT id,user_number,email,password_hash,display_name,avatar_url,bio,website_url,role,status,created_at FROM users WHERE email=?`, strings.ToLower(email)))
}

func (s *Store) UserByID(id string) (User, error) {
	return scanUser(s.db.QueryRow(`SELECT id,user_number,email,password_hash,display_name,avatar_url,bio,website_url,role,status,created_at FROM users WHERE id=?`, id))
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var user User
	if err := row.Scan(&user.ID, &user.UserNumber, &user.Email, &user.PasswordHash, &user.DisplayName, &user.AvatarURL, &user.Bio, &user.WebsiteURL, &user.Role, &user.Status, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

func (s *Store) UpdateProfile(userID, displayName, avatarURL string) (User, error) {
	current, err := s.UserByID(userID)
	if err != nil {
		return User{}, err
	}
	return s.UpdateProfileDetails(userID, displayName, avatarURL, current.Bio, current.WebsiteURL)
}

func (s *Store) UpdateProfileDetails(userID, displayName, avatarURL, bio, websiteURL string) (User, error) {
	result, err := s.db.Exec(`UPDATE users SET display_name=?,avatar_url=?,bio=?,website_url=? WHERE id=?`, displayName, avatarURL, bio, websiteURL, userID)
	if err != nil {
		return User{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return User{}, ErrNotFound
	}
	return s.UserByID(userID)
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id,user_number,email,password_hash,display_name,avatar_url,bio,website_url,role,status,created_at FROM users ORDER BY user_number DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) UpdateUserAccess(actorID, userID, role, status string) (User, error) {
	result, err := s.db.Exec(`UPDATE users SET role=?,status=? WHERE id=? AND (
		role!='admin' OR status!='active' OR
		(?='admin' AND ?='active') OR
		(SELECT COUNT(*) FROM users WHERE role='admin' AND status='active') > 1
	)`, role, status, userID, role, status)
	if err != nil {
		return User{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		var exists bool
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=?)`, userID).Scan(&exists); err != nil {
			return User{}, err
		}
		if !exists {
			return User{}, ErrNotFound
		}
		return User{}, ErrLastAdmin
	}
	_ = s.audit(actorID, "user.access.updated", "user", userID, fmt.Sprintf("role=%s status=%s", role, status))
	return s.UserByID(userID)
}

func (s *Store) Categories() ([]Category, error) {
	rows, err := s.db.Query(`SELECT c.id,c.name,c.description,c.sort_order,COUNT(g.id) FROM categories c LEFT JOIN games g ON g.category_id=c.id AND g.status='published' GROUP BY c.id ORDER BY c.sort_order,c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Category{}
	for rows.Next() {
		var item Category
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.SortOrder, &item.GameCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CategoryExists(id string) (bool, error) {
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM categories WHERE id=?)`, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Store) CreateCategory(actorID string, item Category) (Category, error) {
	_, err := s.db.Exec(`INSERT INTO categories(id,name,description,sort_order) VALUES(?,?,?,?)`, item.ID, item.Name, item.Description, item.SortOrder)
	if err != nil {
		return Category{}, err
	}
	_ = s.audit(actorID, "category.created", "category", item.ID, item.Name)
	return item, nil
}

func (s *Store) UpdateCategory(actorID, id string, item Category) (Category, error) {
	result, err := s.db.Exec(`UPDATE categories SET name=?,description=?,sort_order=? WHERE id=?`, item.Name, item.Description, item.SortOrder, id)
	if err != nil {
		return Category{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Category{}, ErrNotFound
	}
	item.ID = id
	_ = s.audit(actorID, "category.updated", "category", id, item.Name)
	return item, nil
}

func (s *Store) DeleteCategory(actorID, id string) error {
	result, err := s.db.Exec(`DELETE FROM categories WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return s.audit(actorID, "category.deleted", "category", id, "")
}

func (s *Store) Games(filter GameFilter) (GameList, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 24
	}
	conditions := []string{}
	args := []any{}
	if !filter.Admin {
		conditions = append(conditions, "g.status='published'")
	} else if filter.Status != "" {
		conditions = append(conditions, "g.status=?")
		args = append(args, filter.Status)
	}
	if filter.Query != "" {
		conditions = append(conditions, "(g.title LIKE ? OR g.summary LIKE ? OR g.author_name LIKE ? OR g.tags_json LIKE ?)")
		term := "%" + filter.Query + "%"
		args = append(args, term, term, term, term)
	}
	if filter.CategoryID != "" {
		conditions = append(conditions, "g.category_id=?")
		args = append(args, filter.CategoryID)
	}
	if filter.Featured != nil {
		conditions = append(conditions, "g.featured=?")
		args = append(args, *filter.Featured)
	}
	if filter.OwnerID != "" {
		conditions = append(conditions, "g.owner_user_id=?")
		args = append(args, filter.OwnerID)
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM games g"+where, args...).Scan(&total); err != nil {
		return GameList{}, err
	}

	selectArgs := append([]any{filter.UserID, filter.UserID, filter.UserID, filter.UserID}, args...)
	selectArgs = append(selectArgs, filter.PageSize, (filter.Page-1)*filter.PageSize)
	newestOrder := `COALESCE(NULLIF(g.published_at,''),g.created_at) DESC, g.created_at DESC, g.rowid DESC`
	orderBy := newestOrder
	switch filter.Sort {
	case "recommended":
		orderBy = `g.featured DESC, g.play_count DESC, ` + newestOrder
	case "likes":
		orderBy = `(SELECT COUNT(*) FROM game_likes l WHERE l.game_id=g.id) DESC, g.play_count DESC, ` + newestOrder
	case "plays":
		orderBy = `g.play_count DESC, (SELECT COUNT(*) FROM game_likes l WHERE l.game_id=g.id) DESC, ` + newestOrder
	}
	query := gameSelect + where + ` ORDER BY ` + orderBy + ` LIMIT ? OFFSET ?`
	rows, err := s.db.Query(query, selectArgs...)
	if err != nil {
		return GameList{}, err
	}
	defer rows.Close()
	items := []Game{}
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return GameList{}, err
		}
		items = append(items, game)
	}
	return GameList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, rows.Err()
}

const gameSelect = `SELECT
	g.id,g.slug,g.title,g.summary,g.description,g.author_name,g.cover_url,g.launch_url,g.launch_open_in,g.repository_url,
	g.engine,g.version,g.status,COALESCE(g.owner_user_id,''),COALESCE(u.display_name,''),g.category_id,COALESCE(c.name,''),g.featured,g.network_required,g.own_backend,
	g.requires_login,g.platform_storage,g.matchmaking_enabled,
	g.tags_json,g.play_count,(SELECT COUNT(*) FROM favorites f WHERE f.game_id=g.id),
	CASE WHEN ?='' THEN 0 ELSE EXISTS(SELECT 1 FROM favorites uf WHERE uf.game_id=g.id AND uf.user_id=?) END,
	(SELECT COUNT(*) FROM game_likes l WHERE l.game_id=g.id),
	CASE WHEN ?='' THEN 0 ELSE EXISTS(SELECT 1 FROM game_likes ul WHERE ul.game_id=g.id AND ul.user_id=?) END,
	(SELECT COUNT(*) FROM game_comments cm WHERE cm.game_id=g.id AND cm.status='visible'),
	(SELECT COUNT(*) FROM game_share_events sh WHERE sh.game_id=g.id),
	g.created_at,g.updated_at,COALESCE(g.published_at,'')
	FROM games g LEFT JOIN categories c ON c.id=g.category_id
	LEFT JOIN users u ON u.id=g.owner_user_id`

func (s *Store) gameBy(column, value, userID string, publishedOnly bool) (Game, error) {
	query := gameSelect + " WHERE g." + column + "=?"
	args := []any{userID, userID, userID, userID, value}
	if publishedOnly {
		query += " AND g.status='published'"
	}
	return scanGame(s.db.QueryRow(query, args...))
}

func (s *Store) GameBySlug(slug, userID string, publishedOnly bool) (Game, error) {
	return s.gameBy("slug", slug, userID, publishedOnly)
}

func (s *Store) GameByID(id, userID string) (Game, error) {
	return s.gameBy("id", id, userID, false)
}

// GameOwnedBy returns a game only when its owner_user_id matches ownerID.
// A mismatch yields ErrNotFound so a caller cannot probe whether a foreign
// game exists by id.
func (s *Store) GameOwnedBy(id, ownerID string) (Game, error) {
	query := gameSelect + " WHERE g.id=? AND g.owner_user_id=?"
	args := []any{ownerID, ownerID, ownerID, ownerID, id, ownerID}
	return scanGame(s.db.QueryRow(query, args...))
}

func scanGame(row interface{ Scan(...any) error }) (Game, error) {
	var game Game
	var tags string
	if err := row.Scan(
		&game.ID, &game.Slug, &game.Title, &game.Summary, &game.Description, &game.AuthorName,
		&game.CoverURL, &game.LaunchURL, &game.LaunchOpenIn, &game.RepositoryURL, &game.Engine, &game.Version, &game.Status,
		&game.OwnerID, &game.OwnerName, &game.CategoryID, &game.CategoryName, &game.Featured, &game.NetworkRequired, &game.OwnBackend,
		&game.RequiresLogin, &game.UsesPlatformStorage, &game.MatchmakingEnabled,
		&tags, &game.PlayCount, &game.FavoriteCount, &game.IsFavorite,
		&game.LikeCount, &game.IsLiked, &game.CommentCount, &game.ShareCount,
		&game.CreatedAt, &game.UpdatedAt, &game.PublishedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Game{}, ErrNotFound
		}
		return Game{}, err
	}
	if err := json.Unmarshal([]byte(tags), &game.Tags); err != nil {
		game.Tags = []string{}
	}
	if game.Tags == nil {
		game.Tags = []string{}
	}
	return game, nil
}

func (s *Store) CreateGame(actorID, ownerID string, input GameInput) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	return s.createGame(actorID, ownerID, input, nil)
}

func (s *Store) createGame(actorID, ownerID string, input GameInput, coverReceipt *gameCoverReceipt) (Game, error) {
	id := newID("game")
	if input.Tags == nil {
		input.Tags = []string{}
	}
	if input.LaunchOpenIn == "" {
		input.LaunchOpenIn = "same-tab"
	}
	tags, _ := json.Marshal(input.Tags)
	published := any(nil)
	if input.Status == "published" {
		published = "now"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Game{}, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO games(
		id,slug,title,summary,description,author_name,cover_url,launch_url,launch_open_in,repository_url,engine,version,status,owner_user_id,category_id,featured,network_required,own_backend,requires_login,platform_storage,matchmaking_enabled,tags_json,published_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CASE WHEN ?='now' THEN strftime('%Y-%m-%dT%H:%M:%SZ','now') ELSE NULL END)`,
		id, input.Slug, input.Title, input.Summary, input.Description, input.AuthorName, input.CoverURL, input.LaunchURL, input.LaunchOpenIn,
		input.RepositoryURL, input.Engine, input.Version, input.Status, ownerID, input.CategoryID, input.Featured, input.NetworkRequired,
		input.OwnBackend, input.RequiresLogin, input.UsesPlatformStorage, input.MatchmakingEnabled, string(tags), published)
	if err != nil {
		return Game{}, err
	}
	if err := trackGameAssetsTx(tx, id, input.Slug, input.CoverURL, input.LaunchURL); err != nil {
		return Game{}, err
	}
	if err := recordGameCoverReceiptTx(tx, id, coverReceipt); err != nil {
		return Game{}, err
	}
	if err := auditTx(tx, actorID, "game.created", "game", id, input.Title); err != nil {
		return Game{}, err
	}
	if err := tx.Commit(); err != nil {
		return Game{}, err
	}
	return s.GameByID(id, "")
}

func (s *Store) UpdateGame(actorID, id string, input GameInput) (Game, error) {
	s.gameMu.Lock()
	defer s.gameMu.Unlock()

	return s.updateGame(actorID, id, input, nil)
}

func (s *Store) updateGame(actorID, id string, input GameInput, coverReceipt *gameCoverReceipt) (Game, error) {
	if input.Tags == nil {
		input.Tags = []string{}
	}
	if input.LaunchOpenIn == "" {
		input.LaunchOpenIn = "same-tab"
	}
	tags, _ := json.Marshal(input.Tags)
	tx, err := s.db.Begin()
	if err != nil {
		return Game{}, err
	}
	defer tx.Rollback()
	// Package capabilities are declared in atri-game.json and must not be
	// accidentally cleared by an older admin client that omits the newer
	// boolean hints from PUT /admin/games/{id}.
	var packageKind string
	packageErr := tx.QueryRow(`SELECT COALESCE(kind,'') FROM game_packages WHERE game_id=?`, id).Scan(&packageKind)
	if packageErr != nil && !errors.Is(packageErr, sql.ErrNoRows) {
		return Game{}, packageErr
	}
	if packageErr == nil && packageKind != "" {
		if err := tx.QueryRow(
			`SELECT requires_login,platform_storage,matchmaking_enabled FROM games WHERE id=?`,
			id,
		).Scan(&input.RequiresLogin, &input.UsesPlatformStorage, &input.MatchmakingEnabled); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Game{}, ErrNotFound
			}
			return Game{}, err
		}
	}
	result, err := tx.Exec(`UPDATE games SET
		slug=?,title=?,summary=?,description=?,author_name=?,cover_url=?,launch_url=?,launch_open_in=?,repository_url=?,engine=?,version=?,status=?,category_id=?,featured=?,network_required=?,own_backend=?,requires_login=?,platform_storage=?,matchmaking_enabled=?,tags_json=?,
		published_at=CASE WHEN ?='published' AND published_at IS NULL THEN strftime('%Y-%m-%dT%H:%M:%SZ','now') WHEN ? IN ('draft','review') THEN NULL ELSE published_at END,
		updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
		input.Slug, input.Title, input.Summary, input.Description, input.AuthorName, input.CoverURL, input.LaunchURL, input.LaunchOpenIn,
		input.RepositoryURL, input.Engine, input.Version, input.Status, input.CategoryID, input.Featured, input.NetworkRequired,
		input.OwnBackend, input.RequiresLogin, input.UsesPlatformStorage, input.MatchmakingEnabled, string(tags), input.Status, input.Status, id)
	if err != nil {
		return Game{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Game{}, ErrNotFound
	}
	if err := trackGameAssetsTx(tx, id, input.Slug, input.CoverURL, input.LaunchURL); err != nil {
		return Game{}, err
	}
	if err := recordGameCoverReceiptTx(tx, id, coverReceipt); err != nil {
		return Game{}, err
	}
	if err := auditTx(tx, actorID, "game.updated", "game", id, input.Title+" -> "+input.Status); err != nil {
		return Game{}, err
	}
	if err := tx.Commit(); err != nil {
		return Game{}, err
	}
	return s.GameByID(id, "")
}

func (s *Store) AddFavorite(userID, gameID string) error {
	result, err := s.db.Exec(`INSERT OR IGNORE INTO favorites(user_id,game_id) SELECT ?,id FROM games WHERE id=? AND status='published'`, userID, gameID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}

	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM games WHERE id=? AND status='published')`, gameID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RemoveFavorite(userID, gameID string) error {
	_, err := s.db.Exec(`DELETE FROM favorites WHERE user_id=? AND game_id=?`, userID, gameID)
	return err
}

func (s *Store) FavoriteGames(userID string) ([]Game, error) {
	query := gameSelect + ` JOIN favorites mine ON mine.game_id=g.id AND mine.user_id=? WHERE g.status='published' ORDER BY mine.created_at DESC`
	rows, err := s.db.Query(query, userID, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Game{}
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, game)
	}
	return items, rows.Err()
}

func (s *Store) RecordLaunch(slug, userID string) (LaunchResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return LaunchResult{}, err
	}
	defer tx.Rollback()
	var result LaunchResult
	var gameID string
	if err := tx.QueryRow(`SELECT id,launch_url,launch_open_in FROM games WHERE slug=? AND status='published'`, slug).Scan(&gameID, &result.URL, &result.OpenIn); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LaunchResult{}, ErrNotFound
		}
		return LaunchResult{}, err
	}
	if _, err := tx.Exec(`UPDATE games SET play_count=play_count+1 WHERE id=?`, gameID); err != nil {
		return LaunchResult{}, err
	}
	var nullableUser any
	if userID != "" {
		nullableUser = userID
	}
	if _, err := tx.Exec(`INSERT INTO play_events(game_id,user_id) VALUES(?,?)`, gameID, nullableUser); err != nil {
		return LaunchResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return LaunchResult{}, err
	}
	return result, nil
}

func (s *Store) Dashboard() (DashboardMetrics, error) {
	var metrics DashboardMetrics
	err := s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM games WHERE status='published'),
		(SELECT COUNT(*) FROM games WHERE status='review'),
		(SELECT COUNT(*) FROM play_events WHERE created_at >= strftime('%Y-%m-%dT00:00:00Z','now')),
		(SELECT COUNT(*) FROM favorites)`).Scan(&metrics.Users, &metrics.PublishedGames, &metrics.ReviewGames, &metrics.LaunchesToday, &metrics.Favorites)
	return metrics, err
}

func (s *Store) Activity(limit int) ([]Activity, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT a.id,a.action,a.entity_type,a.entity_id,COALESCE(u.display_name,'系统'),a.detail,a.created_at FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id ORDER BY a.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Activity{}
	for rows.Next() {
		var item Activity
		if err := rows.Scan(&item.ID, &item.Action, &item.EntityType, &item.EntityID, &item.ActorName, &item.Detail, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) audit(actorID, action, entityType, entityID, detail string) error {
	_, err := s.db.Exec(`INSERT INTO audit_logs(id,actor_user_id,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?,?)`, newID("audit"), actorID, action, entityType, entityID, detail)
	return err
}

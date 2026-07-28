package data

// This file contains the storage-facing part of the low-friction game
// platform API. A package's manifest remains the source of truth for the
// provider and protocol; the catalog columns are only denormalized hints used
// by the public game list.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	IdentityNone     = "none"
	IdentityOptional = "optional"
	IdentityRequired = "required"

	StorageNone   = "none"
	StorageSQLite = "sqlite"

	StorageScopePlayerGame = "player-game"
	StorageScopePlayer     = "player"
	StorageScopeGame       = "game"

	defaultMatchTicketTTL = 2 * time.Minute
	maxGameDataBytes      = 256 * 1024
	maxGameDataKeys       = 256
	maxGameDataTotalBytes = 4 * 1024 * 1024
)

var (
	ErrPlatformUnavailable  = errors.New("built-in platform services are unavailable for this game")
	ErrGameLoginRequired    = errors.New("game login is required")
	ErrGameStorageDisabled  = errors.New("game storage is not enabled")
	ErrGameStorageQuota     = errors.New("game storage quota exceeded")
	ErrMatchmakingDisabled  = errors.New("game matchmaking is not enabled")
	ErrMatchTicketExists    = errors.New("player already has a waiting matchmaking ticket")
	ErrMatchTicketNotActive = errors.New("matchmaking ticket is no longer waiting")
	ErrInvalidGameData      = errors.New("invalid game data")
	ErrInvalidMatchmaking   = errors.New("invalid matchmaking request")
)

// GamePlatform is the effective capability set for an imported game. The
// *Declared flags distinguish static package defaults from an explicit
// developer request: static packages get SQLite as a storage fallback, but
// are not marked "requires login" until they declare a player-bound service.
type GamePlatform struct {
	GameID                string
	Slug                  string
	Status                string
	Kind                  string
	IdentityMode          string
	StorageProvider       string
	StorageScope          string
	MatchmakingEnabled    bool
	MatchmakingProtocol   string
	IdentityDeclared      bool
	StorageDeclared       bool
	MatchmakingDeclared   bool
	RequiresLogin         bool
	UsesPlatformStorage   bool
	MatchmakingPublicHint bool
}

func (p GamePlatform) IsStatic() bool { return p.Kind == "static" }

// Scopes returns only the player-facing capabilities the package explicitly
// requested. Static packages retain a SQLite metadata fallback for compatibility,
// but that fallback must never silently create a bearer credential for arbitrary
// uploaded game code.
func (p GamePlatform) Scopes() []string {
	scopes := make([]string, 0, 3)
	if p.IdentityDeclared && p.IdentityMode != IdentityNone {
		scopes = append(scopes, "identity")
	}
	if p.StorageDeclared && p.StorageProvider == StorageSQLite {
		scopes = append(scopes, "storage")
	}
	if p.MatchmakingDeclared && p.MatchmakingEnabled {
		scopes = append(scopes, "matchmaking")
	}
	return scopes
}

// SessionAllowed reports whether a logged-in player can request a game ticket.
// Only explicitly declared built-in services can produce a ticket; a static
// package's compatibility SQLite default is deliberately not enough. Anonymous
// external games never reach this method; their own authentication remains
// entirely outside Atri.
func (p GamePlatform) SessionAllowed() bool {
	return p.IsStatic() && len(p.Scopes()) > 0
}

type manifestCapabilities struct {
	Runtime struct {
		Kind string `json:"kind"`
	} `json:"runtime"`
	Services *struct {
		Identity *struct {
			Mode string `json:"mode"`
		} `json:"identity"`
		Storage *struct {
			Provider string `json:"provider"`
			Scope    string `json:"scope"`
		} `json:"storage"`
		Matchmaking *struct {
			Enabled  *bool  `json:"enabled"`
			Protocol string `json:"protocol"`
		} `json:"matchmaking"`
	} `json:"services"`
	// platform is accepted as a compatibility alias for early package
	// prototypes. The canonical schema is services.*.
	Platform *struct {
		RequiresLogin bool   `json:"requiresLogin"`
		Storage       string `json:"storage"`
		Matchmaking   string `json:"matchmaking"`
	} `json:"platform"`
}

// GamePlatformBySlug reads the package manifest associated with a catalog
// record. Manually-created catalog entries and old external packages simply
// resolve to no built-in capabilities.
func (s *Store) GamePlatformBySlug(slug string, publishedOnly bool) (GamePlatform, error) {
	query := `SELECT g.id,g.slug,g.status,COALESCE(p.kind,''),COALESCE(p.manifest_json,''),
		COALESCE(g.requires_login,0),COALESCE(g.platform_storage,0),COALESCE(g.matchmaking_enabled,0)
		FROM games g LEFT JOIN game_packages p ON p.game_id=g.id WHERE g.slug=?`
	if publishedOnly {
		query += ` AND g.status='published'`
	}
	var (
		platform                                   GamePlatform
		raw                                        string
		requiresHint, storageHint, matchmakingHint bool
	)
	if err := s.db.QueryRow(query, slug).Scan(
		&platform.GameID, &platform.Slug, &platform.Status, &platform.Kind, &raw,
		&requiresHint, &storageHint, &matchmakingHint,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GamePlatform{}, ErrNotFound
		}
		return GamePlatform{}, err
	}
	platform.IdentityMode = IdentityNone
	platform.StorageProvider = StorageNone
	platform.StorageScope = StorageScopePlayerGame
	platform.MatchmakingProtocol = "http"
	platform.RequiresLogin = requiresHint
	platform.UsesPlatformStorage = storageHint
	platform.MatchmakingEnabled = matchmakingHint
	platform.MatchmakingPublicHint = matchmakingHint

	return resolveGamePlatform(platform, raw), nil
}

func resolveGamePlatform(platform GamePlatform, raw string) GamePlatform {
	if platform.Kind == "external" {
		// External URLs never inherit catalog capability hints.
		platform.RequiresLogin = false
		platform.UsesPlatformStorage = false
		platform.MatchmakingEnabled = false
		platform.MatchmakingPublicHint = false
		return platform
	}
	if raw == "" || platform.Kind == "" {
		return platform
	}
	var manifest manifestCapabilities
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		// A corrupt optional manifest must not make the catalog unavailable.
		return platform
	}
	if manifest.Runtime.Kind != "" && manifest.Runtime.Kind != "static" {
		return platform
	}
	// Static packages default to a private SQLite namespace. This fallback
	// keeps the package format easy to adopt; it does not imply login.
	platform.IdentityMode = IdentityNone
	platform.StorageProvider = StorageSQLite
	platform.StorageScope = StorageScopePlayerGame
	platform.MatchmakingEnabled = false
	platform.MatchmakingProtocol = "http"
	platform.IdentityDeclared = false
	platform.StorageDeclared = false
	platform.MatchmakingDeclared = false
	if services := manifest.Services; services != nil {
		if services.Identity != nil {
			platform.IdentityDeclared = true
			if oneOfString(services.Identity.Mode, IdentityNone, IdentityOptional, IdentityRequired) {
				platform.IdentityMode = services.Identity.Mode
			}
		}
		if services.Storage != nil {
			platform.StorageDeclared = true
			if services.Storage.Provider == StorageNone || services.Storage.Provider == StorageSQLite {
				platform.StorageProvider = services.Storage.Provider
			}
			if oneOfString(services.Storage.Scope, StorageScopePlayerGame, StorageScopePlayer, StorageScopeGame) {
				platform.StorageScope = services.Storage.Scope
			}
		}
		if services.Matchmaking != nil {
			platform.MatchmakingDeclared = true
			if services.Matchmaking.Enabled != nil {
				platform.MatchmakingEnabled = *services.Matchmaking.Enabled
			}
			if oneOfString(services.Matchmaking.Protocol, "websocket", "sse", "http") {
				platform.MatchmakingProtocol = services.Matchmaking.Protocol
			}
		}
	}
	if manifest.Platform != nil {
		// Compatibility alias only fills fields that were not explicitly
		// declared in services.*.
		if !platform.IdentityDeclared && manifest.Platform.RequiresLogin {
			// The legacy requiresLogin flag is still an explicit opt-in to
			// identity, rather than an inherited static-package default.
			platform.IdentityDeclared = true
			platform.IdentityMode = IdentityRequired
		}
		if !platform.StorageDeclared && manifest.Platform.Storage != "" {
			platform.StorageDeclared = true
			if manifest.Platform.Storage == StorageNone || manifest.Platform.Storage == StorageSQLite {
				platform.StorageProvider = manifest.Platform.Storage
			}
		}
		if !platform.MatchmakingDeclared && manifest.Platform.Matchmaking == "builtin" {
			platform.MatchmakingDeclared = true
			platform.MatchmakingEnabled = true
		}
	}
	platform.UsesPlatformStorage = platform.StorageProvider == StorageSQLite
	platform.RequiresLogin = platform.IdentityMode == IdentityRequired ||
		(platform.StorageDeclared && platform.StorageProvider == StorageSQLite && platform.StorageScope != StorageScopeGame) ||
		(platform.MatchmakingDeclared && platform.MatchmakingEnabled)
	platform.MatchmakingPublicHint = platform.MatchmakingEnabled
	return platform
}

func oneOfString(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

type GameData struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt string          `json:"updatedAt"`
}

func normalizeGameDataKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 80 || strings.ContainsAny(key, "/\\\x00\r\n\t") {
		return "", ErrInvalidGameData
	}
	return key, nil
}

func dataScopeUser(platform GamePlatform, userID string) (string, error) {
	switch platform.StorageScope {
	case StorageScopeGame:
		// Browser game tickets are player credentials. Giving every player a
		// shared writable namespace would let any one player overwrite global
		// game state, so provider=sqlite only supports player-owned scopes.
		return "", ErrGameStorageDisabled
	case StorageScopePlayer, StorageScopePlayerGame:
		if strings.TrimSpace(userID) == "" {
			return "", ErrGameLoginRequired
		}
		return userID, nil
	default:
		return "", ErrGameStorageDisabled
	}
}

func (s *Store) GetGameData(platform GamePlatform, userID, key string) (GameData, error) {
	if platform.StorageProvider != StorageSQLite {
		return GameData{}, ErrGameStorageDisabled
	}
	key, err := normalizeGameDataKey(key)
	if err != nil {
		return GameData{}, err
	}
	namespace, err := dataScopeUser(platform, userID)
	if err != nil {
		return GameData{}, err
	}
	var item GameData
	var rawValue string
	query := `SELECT data_key,value_json,updated_at FROM game_player_data WHERE game_id=? AND user_id=? AND data_key=?`
	args := []any{platform.GameID, namespace, key}
	if err := s.db.QueryRow(query, args...).Scan(&item.Key, &rawValue, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GameData{}, ErrNotFound
		}
		return GameData{}, err
	}
	item.Value = json.RawMessage(rawValue)
	return item, nil
}

func (s *Store) PutGameData(platform GamePlatform, userID, key string, value json.RawMessage) (GameData, error) {
	if platform.StorageProvider != StorageSQLite {
		return GameData{}, ErrGameStorageDisabled
	}
	key, err := normalizeGameDataKey(key)
	if err != nil {
		return GameData{}, err
	}
	if len(value) == 0 || len(value) > maxGameDataBytes || !json.Valid(value) {
		return GameData{}, ErrInvalidGameData
	}
	namespace, err := dataScopeUser(platform, userID)
	if err != nil {
		return GameData{}, err
	}
	value = append([]byte(nil), value...)
	tx, err := s.db.Begin()
	if err != nil {
		return GameData{}, err
	}
	defer tx.Rollback()
	var (
		keyCount      int
		totalBytes    int64
		existingBytes int64
	)
	if err := tx.QueryRow(
		`SELECT COUNT(*),COALESCE(SUM(length(CAST(value_json AS BLOB))),0)
		 FROM game_player_data WHERE game_id=? AND user_id=?`,
		platform.GameID, namespace,
	).Scan(&keyCount, &totalBytes); err != nil {
		return GameData{}, err
	}
	existing := true
	if err := tx.QueryRow(
		`SELECT length(CAST(value_json AS BLOB)) FROM game_player_data
		 WHERE game_id=? AND user_id=? AND data_key=?`,
		platform.GameID, namespace, key,
	).Scan(&existingBytes); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return GameData{}, err
		}
		existing = false
		existingBytes = 0
	}
	if (!existing && keyCount >= maxGameDataKeys) ||
		totalBytes-existingBytes+int64(len(value)) > maxGameDataTotalBytes {
		return GameData{}, ErrGameStorageQuota
	}
	query := `INSERT INTO game_player_data(game_id,user_id,data_key,value_json,updated_at)
		VALUES(?,?,?,?,strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		ON CONFLICT(game_id,user_id,data_key) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at`
	args := []any{platform.GameID, namespace, key, string(value)}
	if _, err := tx.Exec(query, args...); err != nil {
		return GameData{}, err
	}
	if err := tx.Commit(); err != nil {
		return GameData{}, err
	}
	return s.GetGameData(platform, userID, key)
}

func (s *Store) DeleteGameData(platform GamePlatform, userID, key string) error {
	if platform.StorageProvider != StorageSQLite {
		return ErrGameStorageDisabled
	}
	key, err := normalizeGameDataKey(key)
	if err != nil {
		return err
	}
	namespace, err := dataScopeUser(platform, userID)
	if err != nil {
		return err
	}
	query := `DELETE FROM game_player_data WHERE game_id=? AND user_id=? AND data_key=?`
	args := []any{platform.GameID, namespace, key}
	_, err = s.db.Exec(query, args...)
	return err
}

type MatchTicket struct {
	ID        string `json:"ticketId"`
	GameID    string `json:"gameId"`
	UserID    string `json:"userId,omitempty"`
	Mode      string `json:"mode"`
	Region    string `json:"region"`
	Status    string `json:"status"`
	MatchID   string `json:"matchId,omitempty"`
	Position  int    `json:"position,omitempty"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	UpdatedAt string `json:"updatedAt"`
}

func normalizeMatchField(value, fallback string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if len(value) > max || strings.ContainsAny(value, "\x00\r\n\t/\\") {
		return "", ErrInvalidMatchmaking
	}
	return value, nil
}

func (s *Store) CreateMatchTicket(platform GamePlatform, userID, mode, region string) (MatchTicket, error) {
	if !platform.MatchmakingEnabled {
		return MatchTicket{}, ErrMatchmakingDisabled
	}
	if strings.TrimSpace(userID) == "" {
		return MatchTicket{}, ErrGameLoginRequired
	}
	mode, err := normalizeMatchField(mode, "default", 64)
	if err != nil {
		return MatchTicket{}, err
	}
	region, err = normalizeMatchField(region, "global", 64)
	if err != nil {
		return MatchTicket{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(defaultMatchTicketTTL)
	tx, err := s.db.Begin()
	if err != nil {
		return MatchTicket{}, err
	}
	defer tx.Rollback()
	_, _ = tx.Exec(`UPDATE matchmaking_tickets SET status='expired',updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE status='waiting' AND expires_at<=strftime('%Y-%m-%dT%H:%M:%SZ','now')`)
	_, _ = tx.Exec(`DELETE FROM matchmaking_tickets
		WHERE status IN ('matched','cancelled','expired')
		AND updated_at<strftime('%Y-%m-%dT%H:%M:%SZ','now','-1 day')`)
	// A player only needs the current/most recent ticket. Removing their
	// previous terminal rows prevents cancel/rejoin loops from growing the
	// SQLite database without bound; the matched partner keeps its own row.
	_, _ = tx.Exec(
		`DELETE FROM matchmaking_tickets
		 WHERE game_id=? AND user_id=? AND status IN ('matched','cancelled','expired')`,
		platform.GameID, userID,
	)
	ticketID := newID("match")
	_, err = tx.Exec(
		`INSERT INTO matchmaking_tickets(id,game_id,user_id,mode,region,status,expires_at)
		 VALUES(?,?,?,?,?,'waiting',?)`,
		ticketID, platform.GameID, userID, mode, region, expires.UTC().Format("2006-01-02T15:04:05Z"),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return MatchTicket{}, ErrMatchTicketExists
		}
		return MatchTicket{}, err
	}
	var otherID string
	err = tx.QueryRow(
		`SELECT id FROM matchmaking_tickets
		 WHERE game_id=? AND user_id<>? AND mode=? AND region=? AND status='waiting'
		 ORDER BY created_at,id LIMIT 1`,
		platform.GameID, userID, mode, region,
	).Scan(&otherID)
	if err == nil {
		matchID := newID("room")
		if _, err := tx.Exec(
			`UPDATE matchmaking_tickets SET status='matched',match_id=?,updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id IN (?,?)`,
			matchID, ticketID, otherID,
		); err != nil {
			return MatchTicket{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return MatchTicket{}, err
	}
	if err := tx.Commit(); err != nil {
		return MatchTicket{}, err
	}
	return s.MatchTicketByID(platform.GameID, userID, ticketID)
}

func (s *Store) MatchTicketByID(gameID, userID, ticketID string) (MatchTicket, error) {
	var item MatchTicket
	if err := s.db.QueryRow(
		`SELECT id,game_id,user_id,mode,region,status,match_id,created_at,expires_at,updated_at
		 FROM matchmaking_tickets WHERE id=? AND game_id=? AND user_id=?`,
		ticketID, gameID, userID,
	).Scan(&item.ID, &item.GameID, &item.UserID, &item.Mode, &item.Region, &item.Status, &item.MatchID, &item.CreatedAt, &item.ExpiresAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MatchTicket{}, ErrNotFound
		}
		return MatchTicket{}, err
	}
	if item.Status == "waiting" {
		if expires, err := time.Parse(time.RFC3339, item.ExpiresAt); err == nil && !expires.After(time.Now().UTC()) {
			_, _ = s.db.Exec(`UPDATE matchmaking_tickets SET status='expired',updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=? AND status='waiting'`, item.ID)
			item.Status = "expired"
		}
		if item.Status == "waiting" {
			_ = s.db.QueryRow(
				`SELECT COUNT(*) FROM matchmaking_tickets WHERE game_id=? AND mode=? AND region=? AND status='waiting' AND created_at<=?`,
				item.GameID, item.Mode, item.Region, item.CreatedAt,
			).Scan(&item.Position)
		}
	}
	return item, nil
}

func (s *Store) CancelMatchTicket(gameID, userID, ticketID string) error {
	result, err := s.db.Exec(
		`UPDATE matchmaking_tickets SET status='cancelled',updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id=? AND game_id=? AND user_id=? AND status='waiting'`,
		ticketID, gameID, userID,
	)
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
	var status string
	if err := s.db.QueryRow(`SELECT status FROM matchmaking_tickets WHERE id=? AND game_id=? AND user_id=?`, ticketID, gameID, userID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == "cancelled" || status == "expired" {
		return nil
	}
	return ErrMatchTicketNotActive
}

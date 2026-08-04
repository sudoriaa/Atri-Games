package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *Store) CreatorProfile(id, viewerID string) (CreatorProfile, error) {
	var profile CreatorProfile
	err := s.db.QueryRow(`SELECT u.id,u.user_number,u.display_name,u.avatar_url,u.bio,u.website_url,u.created_at,
		(SELECT COUNT(*) FROM creator_follows f WHERE f.creator_user_id=u.id),
		CASE WHEN ?='' THEN 0 ELSE EXISTS(SELECT 1 FROM creator_follows f WHERE f.follower_user_id=? AND f.creator_user_id=u.id) END,
		CASE WHEN ?='' THEN 0 ELSE EXISTS(SELECT 1 FROM user_blocks b WHERE b.blocker_user_id=? AND b.blocked_user_id=u.id) END,
		(SELECT COUNT(*) FROM games g WHERE g.owner_user_id=u.id AND g.status='published')
		FROM users u WHERE u.id=? AND u.status='active'`, viewerID, viewerID, viewerID, viewerID, id).Scan(
		&profile.ID, &profile.UserNumber, &profile.DisplayName, &profile.AvatarURL, &profile.Bio, &profile.WebsiteURL,
		&profile.JoinedAt, &profile.FollowerCount, &profile.Following, &profile.Blocked, &profile.GameCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CreatorProfile{}, ErrNotFound
	}
	return profile, err
}

func (s *Store) creatorFollowState(followerID, creatorID string) (FollowState, error) {
	var state FollowState
	err := s.db.QueryRow(`SELECT
		EXISTS(SELECT 1 FROM creator_follows WHERE follower_user_id=? AND creator_user_id=?),
		(SELECT COUNT(*) FROM creator_follows WHERE creator_user_id=?)`, followerID, creatorID, creatorID).
		Scan(&state.Following, &state.FollowerCount)
	return state, err
}

func (s *Store) FollowCreator(followerID, creatorID string) (FollowState, error) {
	if followerID == "" || creatorID == "" || followerID == creatorID {
		return FollowState{}, ErrInvalidFollow
	}
	tx, err := s.db.Begin()
	if err != nil {
		return FollowState{}, err
	}
	defer tx.Rollback()
	var creatorName, followerName string
	if err := tx.QueryRow(`SELECT display_name FROM users WHERE id=? AND status='active'`, creatorID).Scan(&creatorName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FollowState{}, ErrNotFound
		}
		return FollowState{}, err
	}
	if err := tx.QueryRow(`SELECT display_name FROM users WHERE id=? AND status='active'`, followerID).Scan(&followerName); err != nil {
		return FollowState{}, err
	}
	var blocked bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_blocks WHERE
		(blocker_user_id=? AND blocked_user_id=?) OR (blocker_user_id=? AND blocked_user_id=?))`,
		followerID, creatorID, creatorID, followerID).Scan(&blocked); err != nil {
		return FollowState{}, err
	}
	if blocked {
		return FollowState{}, ErrInvalidFollow
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO creator_follows(follower_user_id,creator_user_id) VALUES(?,?)`, followerID, creatorID)
	if err != nil {
		return FollowState{}, err
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		if _, err := tx.Exec(`INSERT INTO notifications(id,user_id,kind,actor_user_id,title,body,link) VALUES(?,?,?,?,?,?,?)`,
			newID("ntf"), creatorID, "creator.followed", followerID, "有新的关注者", followerName+" 关注了你", "/creators/"+followerID); err != nil {
			return FollowState{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FollowState{}, err
	}
	return s.creatorFollowState(followerID, creatorID)
}

func (s *Store) UnfollowCreator(followerID, creatorID string) (FollowState, error) {
	if followerID == "" || creatorID == "" || followerID == creatorID {
		return FollowState{}, ErrInvalidFollow
	}
	if _, err := s.db.Exec(`DELETE FROM creator_follows WHERE follower_user_id=? AND creator_user_id=?`, followerID, creatorID); err != nil {
		return FollowState{}, err
	}
	return s.creatorFollowState(followerID, creatorID)
}

func (s *Store) creatorBlockState(blockerID, blockedID string) (BlockState, error) {
	var state BlockState
	err := s.db.QueryRow(`SELECT
		EXISTS(SELECT 1 FROM user_blocks WHERE blocker_user_id=? AND blocked_user_id=?),
		EXISTS(SELECT 1 FROM creator_follows WHERE follower_user_id=? AND creator_user_id=?),
		(SELECT COUNT(*) FROM creator_follows WHERE creator_user_id=?)`,
		blockerID, blockedID, blockerID, blockedID, blockedID).
		Scan(&state.Blocked, &state.Following, &state.FollowerCount)
	return state, err
}

func (s *Store) BlockCreator(blockerID, blockedID string) (BlockState, error) {
	if blockerID == "" || blockedID == "" || blockerID == blockedID {
		return BlockState{}, ErrInvalidBlock
	}
	tx, err := s.db.Begin()
	if err != nil {
		return BlockState{}, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')`, blockedID).Scan(&exists); err != nil {
		return BlockState{}, err
	}
	if !exists {
		return BlockState{}, ErrNotFound
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO user_blocks(blocker_user_id,blocked_user_id) VALUES(?,?)`, blockerID, blockedID); err != nil {
		return BlockState{}, err
	}
	if _, err := tx.Exec(`DELETE FROM creator_follows WHERE
		(follower_user_id=? AND creator_user_id=?) OR (follower_user_id=? AND creator_user_id=?)`,
		blockerID, blockedID, blockedID, blockerID); err != nil {
		return BlockState{}, err
	}
	if _, err := tx.Exec(`DELETE FROM game_follows WHERE
		(user_id=? AND game_id IN (SELECT id FROM games WHERE owner_user_id=?)) OR
		(user_id=? AND game_id IN (SELECT id FROM games WHERE owner_user_id=?))`,
		blockerID, blockedID, blockedID, blockerID); err != nil {
		return BlockState{}, err
	}
	if _, err := tx.Exec(`DELETE FROM notifications WHERE user_id=? AND actor_user_id=?`, blockerID, blockedID); err != nil {
		return BlockState{}, err
	}
	if err := tx.Commit(); err != nil {
		return BlockState{}, err
	}
	return s.creatorBlockState(blockerID, blockedID)
}

func (s *Store) UnblockCreator(blockerID, blockedID string) (BlockState, error) {
	if blockerID == "" || blockedID == "" || blockerID == blockedID {
		return BlockState{}, ErrInvalidBlock
	}
	if _, err := s.db.Exec(`DELETE FROM user_blocks WHERE blocker_user_id=? AND blocked_user_id=?`, blockerID, blockedID); err != nil {
		return BlockState{}, err
	}
	return s.creatorBlockState(blockerID, blockedID)
}

func (s *Store) gameFollowState(userID, gameID string) (FollowState, error) {
	var state FollowState
	err := s.db.QueryRow(`SELECT
		EXISTS(SELECT 1 FROM game_follows WHERE user_id=? AND game_id=?),
		(SELECT COUNT(*) FROM game_follows WHERE game_id=?)`, userID, gameID, gameID).
		Scan(&state.Following, &state.FollowerCount)
	return state, err
}

func (s *Store) GameFollowState(userID, gameID string) (FollowState, error) {
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM games WHERE id=? AND status='published')`, gameID).Scan(&exists); err != nil {
		return FollowState{}, err
	}
	if !exists {
		return FollowState{}, ErrNotFound
	}
	return s.gameFollowState(userID, gameID)
}

func (s *Store) FollowGame(userID, gameID string) (FollowState, error) {
	if _, err := s.GameFollowState(userID, gameID); err != nil {
		return FollowState{}, err
	}
	var blocked bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM games g JOIN user_blocks b ON
		(b.blocker_user_id=? AND b.blocked_user_id=g.owner_user_id) OR
		(b.blocker_user_id=g.owner_user_id AND b.blocked_user_id=?) WHERE g.id=?)`, userID, userID, gameID).Scan(&blocked); err != nil {
		return FollowState{}, err
	}
	if blocked {
		return FollowState{}, ErrInvalidFollow
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO game_follows(user_id,game_id) VALUES(?,?)`, userID, gameID); err != nil {
		return FollowState{}, err
	}
	return s.gameFollowState(userID, gameID)
}

func (s *Store) UnfollowGame(userID, gameID string) (FollowState, error) {
	if _, err := s.db.Exec(`DELETE FROM game_follows WHERE user_id=? AND game_id=?`, userID, gameID); err != nil {
		return FollowState{}, err
	}
	return s.gameFollowState(userID, gameID)
}

func (s *Store) RecordGameCommunityEvent(actorID string, game Game, kind, summary string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO community_events(id,kind,actor_user_id,game_id,summary) VALUES(?,?,?,?,?)`,
		newID("evt"), kind, nullableString(actorID), game.ID, summary); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT user_id FROM (
		SELECT user_id FROM game_follows WHERE game_id=?
		UNION
		SELECT follower_user_id AS user_id FROM creator_follows WHERE creator_user_id=?
	) WHERE user_id!=? AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE
		(b.blocker_user_id=user_id AND b.blocked_user_id=?) OR
		(b.blocker_user_id=? AND b.blocked_user_id=user_id))`, game.ID, game.OwnerID, actorID, actorID, actorID)
	if err != nil {
		return err
	}
	var recipients []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	title := "关注的游戏有新动态"
	if kind == "game.published" {
		title = "关注的创作者发布了新游戏"
	}
	for _, userID := range recipients {
		if _, err := tx.Exec(`INSERT INTO notifications(id,user_id,kind,actor_user_id,game_id,title,body,link) VALUES(?,?,?,?,?,?,?,?)`,
			newID("ntf"), userID, kind, nullableString(actorID), game.ID, title, game.Title+" · "+summary, "/games/"+game.Slug); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) HasGameCommunityEvent(gameID string) bool {
	var exists bool
	return s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM community_events WHERE game_id=?)`, gameID).Scan(&exists) == nil && exists
}

func (s *Store) NotifyComment(actorID, gameID, parentID, body string) error {
	var gameTitle, gameSlug, ownerID string
	if err := s.db.QueryRow(`SELECT title,slug,COALESCE(owner_user_id,'') FROM games WHERE id=?`, gameID).Scan(&gameTitle, &gameSlug, &ownerID); err != nil {
		return err
	}
	recipients := map[string]string{}
	if ownerID != "" && ownerID != actorID {
		recipients[ownerID] = "你的游戏收到了新留言"
	}
	if parentID != "" {
		var parentAuthor string
		if err := s.db.QueryRow(`SELECT user_id FROM game_comments WHERE id=? AND game_id=?`, parentID, gameID).Scan(&parentAuthor); err == nil && parentAuthor != actorID {
			recipients[parentAuthor] = "你的留言收到了回复"
		}
	}
	preview := strings.TrimSpace(body)
	if len([]rune(preview)) > 80 {
		preview = string([]rune(preview)[:80]) + "…"
	}
	for userID, title := range recipients {
		var blocked bool
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_blocks WHERE
			(blocker_user_id=? AND blocked_user_id=?) OR (blocker_user_id=? AND blocked_user_id=?))`,
			userID, actorID, actorID, userID).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO notifications(id,user_id,kind,actor_user_id,game_id,title,body,link) VALUES(?,?,?,?,?,?,?,?)`,
			newID("ntf"), userID, "comment.created", actorID, gameID, title, gameTitle+" · "+preview, "/games/"+gameSlug+"#comments"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CommunityFeed(userID string, limit int) ([]CommunityEvent, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT e.id,e.kind,COALESCE(u.id,''),COALESCE(u.display_name,'Atri 编辑部'),COALESCE(u.avatar_url,''),
		COALESCE(g.id,''),COALESCE(g.slug,''),COALESCE(g.title,''),COALESCE(g.cover_url,''),e.summary,e.created_at
		FROM community_events e
		LEFT JOIN users u ON u.id=e.actor_user_id
		LEFT JOIN games g ON g.id=e.game_id
		WHERE (e.actor_user_id=?
		   OR EXISTS(SELECT 1 FROM creator_follows f WHERE f.follower_user_id=? AND f.creator_user_id=e.actor_user_id)
		   OR EXISTS(SELECT 1 FROM game_follows f WHERE f.user_id=? AND f.game_id=e.game_id))
		  AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE
			(b.blocker_user_id=? AND b.blocked_user_id=e.actor_user_id) OR
			(b.blocker_user_id=e.actor_user_id AND b.blocked_user_id=?))
		ORDER BY e.created_at DESC LIMIT ?`, userID, userID, userID, userID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CommunityEvent{}
	for rows.Next() {
		var item CommunityEvent
		if err := rows.Scan(&item.ID, &item.Kind, &item.ActorID, &item.ActorName, &item.ActorAvatarURL,
			&item.GameID, &item.GameSlug, &item.GameTitle, &item.GameCoverURL, &item.Summary, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Notifications(userID string, limit int) (NotificationList, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var result NotificationList
	visibility := `user_id=? AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE
		(b.blocker_user_id=? AND b.blocked_user_id=notifications.actor_user_id) OR
		(b.blocker_user_id=notifications.actor_user_id AND b.blocked_user_id=?))`
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE `+visibility+` AND read_at IS NULL`, userID, userID, userID).Scan(&result.UnreadCount); err != nil {
		return result, err
	}
	rows, err := s.db.Query(`SELECT id,kind,title,body,link,read_at IS NOT NULL,created_at FROM notifications WHERE `+visibility+` ORDER BY created_at DESC LIMIT ?`, userID, userID, userID, limit)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Items = []Notification{}
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Body, &item.Link, &item.Read, &item.CreatedAt); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (s *Store) MarkNotificationsRead(userID, id string) error {
	if id == "" {
		_, err := s.db.Exec(`UPDATE notifications SET read_at=COALESCE(read_at,strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE user_id=?`, userID)
		return err
	}
	result, err := s.db.Exec(`UPDATE notifications SET read_at=COALESCE(read_at,strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func gameSnapshot(game Game) GameInput {
	return GameInput{
		Slug: game.Slug, Title: game.Title, Summary: game.Summary, Description: game.Description,
		AuthorName: game.AuthorName, CoverURL: game.CoverURL, LaunchURL: game.LaunchURL,
		LaunchOpenIn: game.LaunchOpenIn, RepositoryURL: game.RepositoryURL, Engine: game.Engine,
		Version: game.Version, Status: game.Status, CategoryID: game.CategoryID, Featured: game.Featured,
		NetworkRequired: game.NetworkRequired, OwnBackend: game.OwnBackend, RequiresLogin: game.RequiresLogin,
		UsesPlatformStorage: game.UsesPlatformStorage, MatchmakingEnabled: game.MatchmakingEnabled, Tags: game.Tags,
	}
}

func (s *Store) RecordGameVersion(actorID string, game Game, releaseNotes string) error {
	snapshot, err := json.Marshal(gameSnapshot(game))
	if err != nil {
		return err
	}
	releaseNotes = strings.TrimSpace(releaseNotes)
	if releaseNotes == "" {
		releaseNotes = "更新至 v" + game.Version
	}
	_, err = s.db.Exec(`INSERT INTO game_versions(id,game_id,version,release_notes,snapshot_json,created_by_user_id) VALUES(?,?,?,?,?,?)`,
		newID("ver"), game.ID, game.Version, releaseNotes, string(snapshot), nullableString(actorID))
	return err
}

func versionChanges(previous, current GameInput) []string {
	changes := []string{}
	checks := []struct {
		label   string
		changed bool
	}{
		{"版本", previous.Version != current.Version}, {"标题", previous.Title != current.Title},
		{"摘要", previous.Summary != current.Summary}, {"详情", previous.Description != current.Description},
		{"作者", previous.AuthorName != current.AuthorName}, {"分类", previous.CategoryID != current.CategoryID},
		{"封面", previous.CoverURL != current.CoverURL}, {"启动地址", previous.LaunchURL != current.LaunchURL},
		{"引擎", previous.Engine != current.Engine}, {"标签", strings.Join(previous.Tags, "\x00") != strings.Join(current.Tags, "\x00")},
	}
	for _, check := range checks {
		if check.changed {
			changes = append(changes, check.label)
		}
	}
	if len(changes) == 0 {
		changes = append(changes, "内容记录")
	}
	return changes
}

func (s *Store) GameVersions(gameID, viewerID string, admin bool) ([]GameVersion, error) {
	var status, ownerID string
	if err := s.db.QueryRow(`SELECT status,COALESCE(owner_user_id,'') FROM games WHERE id=?`, gameID).Scan(&status, &ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !admin && status != "published" && ownerID != viewerID {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(`SELECT v.id,v.game_id,v.version,v.release_notes,v.snapshot_json,COALESCE(v.created_by_user_id,''),COALESCE(u.display_name,'系统'),v.created_at
		FROM game_versions v LEFT JOIN users u ON u.id=v.created_by_user_id WHERE v.game_id=? ORDER BY v.created_at DESC,v.rowid DESC`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []GameVersion{}
	snapshots := []GameInput{}
	for rows.Next() {
		var item GameVersion
		if err := rows.Scan(&item.ID, &item.GameID, &item.Version, &item.ReleaseNotes, &item.SnapshotJSON, &item.CreatedByID, &item.CreatedByName, &item.CreatedAt); err != nil {
			return nil, err
		}
		var snapshot GameInput
		if err := json.Unmarshal([]byte(item.SnapshotJSON), &snapshot); err != nil {
			return nil, err
		}
		items = append(items, item)
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		if index+1 < len(items) {
			items[index].Changes = versionChanges(snapshots[index+1], snapshots[index])
		} else {
			items[index].Changes = []string{"首次记录"}
		}
	}
	return items, nil
}

func (s *Store) VersionRollbackInput(gameID, versionID, ownerID string) (GameInput, GameVersion, error) {
	if ownerID != "" {
		var exists bool
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM games WHERE id=? AND owner_user_id=?)`, gameID, ownerID).Scan(&exists); err != nil {
			return GameInput{}, GameVersion{}, err
		}
		if !exists {
			return GameInput{}, GameVersion{}, ErrNotFound
		}
	}
	var version GameVersion
	if err := s.db.QueryRow(`SELECT id,game_id,version,release_notes,snapshot_json,COALESCE(created_by_user_id,''),created_at FROM game_versions WHERE id=? AND game_id=?`, versionID, gameID).Scan(
		&version.ID, &version.GameID, &version.Version, &version.ReleaseNotes, &version.SnapshotJSON, &version.CreatedByID, &version.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GameInput{}, GameVersion{}, ErrNotFound
		}
		return GameInput{}, GameVersion{}, err
	}
	var input GameInput
	if err := json.Unmarshal([]byte(version.SnapshotJSON), &input); err != nil {
		return GameInput{}, GameVersion{}, err
	}
	return input, version, nil
}

func backfillGameVersionsTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT g.id,g.slug,g.title,g.summary,g.description,g.author_name,g.cover_url,g.launch_url,g.launch_open_in,g.repository_url,g.engine,g.version,g.status,g.category_id,g.featured,g.network_required,g.own_backend,g.requires_login,g.platform_storage,g.matchmaking_enabled,g.tags_json
		FROM games g WHERE NOT EXISTS(SELECT 1 FROM game_versions v WHERE v.game_id=g.id)`)
	if err != nil {
		return err
	}
	type item struct {
		id    string
		input GameInput
		tags  string
	}
	items := []item{}
	for rows.Next() {
		var entry item
		if err := rows.Scan(&entry.id, &entry.input.Slug, &entry.input.Title, &entry.input.Summary, &entry.input.Description,
			&entry.input.AuthorName, &entry.input.CoverURL, &entry.input.LaunchURL, &entry.input.LaunchOpenIn, &entry.input.RepositoryURL,
			&entry.input.Engine, &entry.input.Version, &entry.input.Status, &entry.input.CategoryID, &entry.input.Featured,
			&entry.input.NetworkRequired, &entry.input.OwnBackend, &entry.input.RequiresLogin, &entry.input.UsesPlatformStorage,
			&entry.input.MatchmakingEnabled, &entry.tags); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(entry.tags), &entry.input.Tags)
		if entry.input.Tags == nil {
			entry.input.Tags = []string{}
		}
		items = append(items, entry)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, entry := range items {
		snapshot, err := json.Marshal(entry.input)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO game_versions(id,game_id,version,release_notes,snapshot_json) VALUES(?,?,?,?,?)`,
			newID("ver"), entry.id, entry.input.Version, "初始版本", string(snapshot)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateReport(reporterID, targetType, targetID, reason, detail string) (ContentReport, error) {
	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	reason = strings.TrimSpace(reason)
	detail = strings.TrimSpace(detail)
	if !oneOfString(targetType, "game", "comment", "creator") || targetID == "" || reason == "" || len([]rune(reason)) > 80 || len([]rune(detail)) > 1000 {
		return ContentReport{}, ErrInvalidReport
	}
	var exists bool
	switch targetType {
	case "game":
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM games WHERE id=? AND status='published')`, targetID).Scan(&exists); err != nil {
			return ContentReport{}, err
		}
	case "comment":
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM game_comments WHERE id=? AND status='visible')`, targetID).Scan(&exists); err != nil {
			return ContentReport{}, err
		}
	case "creator":
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')`, targetID).Scan(&exists); err != nil {
			return ContentReport{}, err
		}
	}
	if !exists {
		return ContentReport{}, ErrNotFound
	}
	id := newID("rpt")
	if _, err := s.db.Exec(`INSERT INTO content_reports(id,reporter_user_id,target_type,target_id,reason,detail) VALUES(?,?,?,?,?,?)`,
		id, reporterID, targetType, targetID, reason, detail); err != nil {
		return ContentReport{}, err
	}
	return s.ReportByID(id)
}

func reportSelect() string {
	return `SELECT r.id,r.reporter_user_id,COALESCE(reporter.display_name,'已注销用户'),r.target_type,r.target_id,
		CASE r.target_type
			WHEN 'game' THEN COALESCE((SELECT title FROM games WHERE id=r.target_id),'已删除游戏')
			WHEN 'comment' THEN COALESCE((SELECT substr(body,1,60) FROM game_comments WHERE id=r.target_id),'已删除留言')
			WHEN 'creator' THEN COALESCE((SELECT display_name FROM users WHERE id=r.target_id),'已注销创作者')
			ELSE r.target_id END,
		r.reason,r.detail,r.status,r.resolution,COALESCE(resolver.display_name,''),r.created_at,r.updated_at
		FROM content_reports r
		LEFT JOIN users reporter ON reporter.id=r.reporter_user_id
		LEFT JOIN users resolver ON resolver.id=r.resolved_by_user_id`
}

func scanReport(row interface{ Scan(...any) error }) (ContentReport, error) {
	var report ContentReport
	err := row.Scan(&report.ID, &report.ReporterID, &report.ReporterName, &report.TargetType, &report.TargetID,
		&report.TargetLabel, &report.Reason, &report.Detail, &report.Status, &report.Resolution,
		&report.ResolvedByName, &report.CreatedAt, &report.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ContentReport{}, ErrNotFound
	}
	return report, err
}

func (s *Store) ReportByID(id string) (ContentReport, error) {
	return scanReport(s.db.QueryRow(reportSelect()+` WHERE r.id=?`, id))
}

func (s *Store) Reports(status string) ([]ContentReport, error) {
	query := reportSelect()
	args := []any{}
	if status != "" {
		query += ` WHERE r.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY CASE WHEN r.status='pending' THEN 0 ELSE 1 END,r.created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ContentReport{}
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, report)
	}
	return items, rows.Err()
}

func (s *Store) MyReports(userID string) ([]ContentReport, error) {
	rows, err := s.db.Query(reportSelect()+` WHERE r.reporter_user_id=? ORDER BY r.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	items := []ContentReport{}
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, report)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		appeal, err := s.AppealByReportID(items[index].ID)
		if err == nil {
			items[index].Appeal = &appeal
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	return items, nil
}

func (s *Store) ResolveReport(actorID, id, status, resolution string) (ContentReport, error) {
	if !oneOfString(status, "resolved", "dismissed") || strings.TrimSpace(resolution) == "" || len([]rune(resolution)) > 1000 {
		return ContentReport{}, ErrInvalidReport
	}
	result, err := s.db.Exec(`UPDATE content_reports SET status=?,resolution=?,resolved_by_user_id=?,updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
		status, strings.TrimSpace(resolution), actorID, id)
	if err != nil {
		return ContentReport{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ContentReport{}, ErrNotFound
	}
	_ = s.audit(actorID, "report."+status, "report", id, strings.TrimSpace(resolution))
	report, err := s.ReportByID(id)
	if err != nil {
		return ContentReport{}, err
	}
	title := "举报已完成处置"
	if status == "dismissed" {
		title = "举报未予采纳"
	}
	if _, err := s.db.Exec(`INSERT INTO notifications(id,user_id,kind,actor_user_id,title,body,link) VALUES(?,?,?,?,?,?,?)`,
		newID("ntf"), report.ReporterID, "report."+status, actorID, title, report.TargetLabel+" · "+report.Resolution, "/safety"); err != nil {
		return ContentReport{}, err
	}
	return report, nil
}

func appealSelect() string {
	return `SELECT a.id,a.report_id,a.appellant_user_id,COALESCE(appellant.display_name,'已注销用户'),
		CASE r.target_type
			WHEN 'game' THEN COALESCE((SELECT title FROM games WHERE id=r.target_id),'已删除游戏')
			WHEN 'comment' THEN COALESCE((SELECT substr(body,1,60) FROM game_comments WHERE id=r.target_id),'已删除留言')
			WHEN 'creator' THEN COALESCE((SELECT display_name FROM users WHERE id=r.target_id),'已注销创作者')
			ELSE r.target_id END,
		a.report_status,a.reason,a.status,a.resolution,COALESCE(resolver.display_name,''),a.created_at,a.updated_at
		FROM moderation_appeals a
		JOIN content_reports r ON r.id=a.report_id
		LEFT JOIN users appellant ON appellant.id=a.appellant_user_id
		LEFT JOIN users resolver ON resolver.id=a.resolved_by_user_id`
}

func scanAppeal(row interface{ Scan(...any) error }) (ModerationAppeal, error) {
	var appeal ModerationAppeal
	err := row.Scan(&appeal.ID, &appeal.ReportID, &appeal.AppellantID, &appeal.AppellantName,
		&appeal.TargetLabel, &appeal.ReportStatus, &appeal.Reason, &appeal.Status,
		&appeal.Resolution, &appeal.ResolvedByName, &appeal.CreatedAt, &appeal.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ModerationAppeal{}, ErrNotFound
	}
	return appeal, err
}

func (s *Store) AppealByID(id string) (ModerationAppeal, error) {
	return scanAppeal(s.db.QueryRow(appealSelect()+` WHERE a.id=?`, id))
}

func (s *Store) AppealByReportID(reportID string) (ModerationAppeal, error) {
	return scanAppeal(s.db.QueryRow(appealSelect()+` WHERE a.report_id=?`, reportID))
}

func (s *Store) CreateAppeal(appellantID, reportID, reason string) (ModerationAppeal, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 1000 {
		return ModerationAppeal{}, ErrInvalidAppeal
	}
	var reporterID, reportStatus string
	if err := s.db.QueryRow(`SELECT reporter_user_id,status FROM content_reports WHERE id=?`, reportID).Scan(&reporterID, &reportStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModerationAppeal{}, ErrNotFound
		}
		return ModerationAppeal{}, err
	}
	if reporterID != appellantID {
		return ModerationAppeal{}, ErrForbidden
	}
	if reportStatus == "pending" {
		return ModerationAppeal{}, ErrInvalidAppeal
	}
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM moderation_appeals WHERE report_id=?)`, reportID).Scan(&exists); err != nil {
		return ModerationAppeal{}, err
	}
	if exists {
		return ModerationAppeal{}, ErrAppealExists
	}
	id := newID("apl")
	if _, err := s.db.Exec(`INSERT INTO moderation_appeals(id,report_id,appellant_user_id,report_status,reason) VALUES(?,?,?,?,?)`,
		id, reportID, appellantID, reportStatus, reason); err != nil {
		return ModerationAppeal{}, err
	}
	return s.AppealByID(id)
}

func (s *Store) Appeals(status string) ([]ModerationAppeal, error) {
	query := appealSelect()
	args := []any{}
	if status != "" {
		query += ` WHERE a.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY CASE WHEN a.status='pending' THEN 0 ELSE 1 END,a.created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ModerationAppeal{}
	for rows.Next() {
		appeal, err := scanAppeal(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, appeal)
	}
	return items, rows.Err()
}

func (s *Store) ResolveAppeal(actorID, id, status, resolution string) (ModerationAppeal, error) {
	resolution = strings.TrimSpace(resolution)
	if !oneOfString(status, "accepted", "rejected") || resolution == "" || len([]rune(resolution)) > 1000 {
		return ModerationAppeal{}, ErrInvalidAppeal
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ModerationAppeal{}, err
	}
	defer tx.Rollback()
	var reportID, appellantID, currentStatus string
	if err := tx.QueryRow(`SELECT report_id,appellant_user_id,status FROM moderation_appeals WHERE id=?`, id).Scan(&reportID, &appellantID, &currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModerationAppeal{}, ErrNotFound
		}
		return ModerationAppeal{}, err
	}
	if currentStatus != "pending" {
		return ModerationAppeal{}, ErrInvalidAppeal
	}
	if _, err := tx.Exec(`UPDATE moderation_appeals SET status=?,resolution=?,resolved_by_user_id=?,updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
		status, resolution, actorID, id); err != nil {
		return ModerationAppeal{}, err
	}
	if status == "accepted" {
		if _, err := tx.Exec(`UPDATE content_reports SET status='pending' WHERE id=?`, reportID); err != nil {
			return ModerationAppeal{}, err
		}
	}
	title := "申诉已通过，举报已重新进入复核"
	if status == "rejected" {
		title = "申诉复核已结束"
	}
	if _, err := tx.Exec(`INSERT INTO notifications(id,user_id,kind,actor_user_id,title,body,link) VALUES(?,?,?,?,?,?,?)`,
		newID("ntf"), appellantID, "appeal."+status, actorID, title, resolution, "/safety"); err != nil {
		return ModerationAppeal{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModerationAppeal{}, err
	}
	_ = s.audit(actorID, "appeal."+status, "appeal", id, resolution)
	return s.AppealByID(id)
}

func (s *Store) NotifyGameOwner(game Game, kind, title, body string) error {
	if game.OwnerID == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO notifications(id,user_id,kind,game_id,title,body,link) VALUES(?,?,?,?,?,?,?)`,
		newID("ntf"), game.OwnerID, kind, game.ID, title, body, "/my-games")
	return err
}

func (s *Store) CommentAuthorID(commentID string) (string, error) {
	var id string
	if err := s.db.QueryRow(`SELECT user_id FROM game_comments WHERE id=?`, commentID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return id, nil
}

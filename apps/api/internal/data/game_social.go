package data

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

// Social counters are always derived with COUNT(*) over the event tables so a
// single source of truth stays authoritative; there is no denormalised column
// that could drift from the rows it summarises.

const maxCommentBodyRunes = 1000

var (
	// ErrCommentTooDeep rejects a reply whose parent is already a reply. The
	// discussion model is deliberately one level deep.
	ErrCommentTooDeep = errors.New("comment nesting limited to one level")
	// ErrInvalidComment marks an empty or oversized message body.
	ErrInvalidComment = errors.New("invalid comment body")
	// ErrCommentForbidden marks a delete attempted by a non-author non-admin.
	ErrCommentForbidden = errors.New("comment may only be removed by its author or an administrator")
)

// LikeGame records a player's like. Repeat calls are idempotent.
func (s *Store) LikeGame(userID, gameID string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO game_likes(user_id,game_id) VALUES(?,?)`,
		userID, gameID,
	)
	return err
}

// UnlikeGame withdraws a like. Removing an absent like is not an error.
func (s *Store) UnlikeGame(userID, gameID string) error {
	_, err := s.db.Exec(`DELETE FROM game_likes WHERE user_id=? AND game_id=?`, userID, gameID)
	return err
}

// GameLikeState reports the counter and the caller's own state so the client
// can settle its optimistic update against the stored value.
func (s *Store) GameLikeState(userID, gameID string) (int64, bool, error) {
	var count int64
	var liked bool
	err := s.db.QueryRow(
		`SELECT (SELECT COUNT(*) FROM game_likes WHERE game_id=?),
		        CASE WHEN ?='' THEN 0 ELSE EXISTS(SELECT 1 FROM game_likes WHERE game_id=? AND user_id=?) END`,
		gameID, userID, gameID, userID,
	).Scan(&count, &liked)
	return count, liked, err
}

// RecordShare appends one share event. userID is empty for anonymous shares;
// channel identifies the surface the player used (link, card, native).
func (s *Store) RecordShare(gameID, userID, channel string) (int64, error) {
	var actor any
	if userID != "" {
		actor = userID
	}
	if channel == "" {
		channel = "link"
	}
	if _, err := s.db.Exec(
		`INSERT INTO game_share_events(game_id,user_id,channel) VALUES(?,?,?)`,
		gameID, actor, channel,
	); err != nil {
		return 0, err
	}
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM game_share_events WHERE game_id=?`, gameID).Scan(&count)
	return count, err
}

// CreateGameComment stores a root comment or a one-level reply and returns the
// stored row shaped for the caller.
func (s *Store) CreateGameComment(userID, gameID, parentID, body string) (GameComment, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxCommentBodyRunes {
		return GameComment{}, ErrInvalidComment
	}
	if parentID != "" {
		var parentGame string
		var parentParent sql.NullString
		err := s.db.QueryRow(
			`SELECT game_id,parent_id FROM game_comments WHERE id=? AND status='visible'`,
			parentID,
		).Scan(&parentGame, &parentParent)
		if errors.Is(err, sql.ErrNoRows) {
			return GameComment{}, ErrNotFound
		}
		if err != nil {
			return GameComment{}, err
		}
		if parentGame != gameID {
			return GameComment{}, ErrNotFound
		}
		if parentParent.Valid && parentParent.String != "" {
			return GameComment{}, ErrCommentTooDeep
		}
	}

	id := newID("cmt")
	var parent any
	if parentID != "" {
		parent = parentID
	}
	if _, err := s.db.Exec(
		`INSERT INTO game_comments(id,game_id,user_id,parent_id,body) VALUES(?,?,?,?,?)`,
		id, gameID, userID, parent, body,
	); err != nil {
		return GameComment{}, err
	}
	return s.gameComment(id, userID)
}

// DeleteGameComment soft-deletes a comment. Authors may remove their own
// message; administrators may remove any. Replies cascade with the root.
func (s *Store) DeleteGameComment(actorID, actorRole, gameID, commentID string) error {
	var ownerID, storedGame string
	err := s.db.QueryRow(
		`SELECT user_id,game_id FROM game_comments WHERE id=? AND status='visible'`,
		commentID,
	).Scan(&ownerID, &storedGame)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if storedGame != gameID {
		return ErrNotFound
	}
	if ownerID != actorID && actorRole != "admin" {
		return ErrCommentForbidden
	}
	_, err = s.db.Exec(
		`UPDATE game_comments SET status='deleted',updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id=? OR parent_id=?`,
		commentID, commentID,
	)
	return err
}

// LikeComment and UnlikeComment mirror the game-level like handling.
func (s *Store) LikeComment(userID, gameID, commentID string) (int64, error) {
	if err := s.commentBelongsToGame(commentID, gameID); err != nil {
		return 0, err
	}
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO game_comment_likes(user_id,comment_id) VALUES(?,?)`,
		userID, commentID,
	); err != nil {
		return 0, err
	}
	return s.commentLikeCount(commentID)
}

func (s *Store) UnlikeComment(userID, gameID, commentID string) (int64, error) {
	if err := s.commentBelongsToGame(commentID, gameID); err != nil {
		return 0, err
	}
	if _, err := s.db.Exec(
		`DELETE FROM game_comment_likes WHERE user_id=? AND comment_id=?`,
		userID, commentID,
	); err != nil {
		return 0, err
	}
	return s.commentLikeCount(commentID)
}

func (s *Store) commentBelongsToGame(commentID, gameID string) error {
	var storedGame string
	err := s.db.QueryRow(
		`SELECT game_id FROM game_comments WHERE id=? AND status='visible'`,
		commentID,
	).Scan(&storedGame)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && storedGame != gameID) {
		return ErrNotFound
	}
	return err
}

func (s *Store) commentLikeCount(commentID string) (int64, error) {
	var count int64
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM game_comment_likes WHERE comment_id=?`, commentID,
	).Scan(&count)
	return count, err
}

const commentSelect = `SELECT
	cm.id,cm.game_id,COALESCE(cm.parent_id,''),cm.user_id,COALESCE(u.user_number,0),COALESCE(u.display_name,'已注销'),COALESCE(u.avatar_url,''),COALESCE(u.role,'user'),
	cm.body,
	(SELECT COUNT(*) FROM game_comment_likes cl WHERE cl.comment_id=cm.id),
	CASE WHEN ?='' THEN 0 ELSE EXISTS(SELECT 1 FROM game_comment_likes ucl WHERE ucl.comment_id=cm.id AND ucl.user_id=?) END,
	(SELECT COUNT(*) FROM game_comments r WHERE r.parent_id=cm.id AND r.status='visible'),
	cm.created_at,cm.updated_at
	FROM game_comments cm LEFT JOIN users u ON u.id=cm.user_id`

func scanComment(row interface{ Scan(...any) error }) (GameComment, error) {
	var item GameComment
	if err := row.Scan(
		&item.ID, &item.GameID, &item.ParentID, &item.AuthorID, &item.AuthorUserNumber, &item.AuthorName, &item.AuthorAvatarURL, &item.AuthorRole,
		&item.Body, &item.LikeCount, &item.IsLiked, &item.ReplyCount, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GameComment{}, ErrNotFound
		}
		return GameComment{}, err
	}
	return item, nil
}

func (s *Store) gameComment(commentID, viewerID string) (GameComment, error) {
	return scanComment(s.db.QueryRow(
		commentSelect+` WHERE cm.id=? AND cm.status='visible'`,
		viewerID, viewerID, commentID,
	))
}

// GameComments returns a page of root comments with their replies nested. The
// viewer's own like state and delete permission are resolved per row.
func (s *Store) GameComments(gameID, viewerID, viewerRole string, page, pageSize int) (GameCommentList, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM game_comments WHERE game_id=? AND status='visible' AND (parent_id IS NULL OR parent_id='')`,
		gameID,
	).Scan(&total); err != nil {
		return GameCommentList{}, err
	}

	rows, err := s.db.Query(
		commentSelect+` WHERE cm.game_id=? AND cm.status='visible' AND (cm.parent_id IS NULL OR cm.parent_id='')
		 ORDER BY cm.created_at DESC LIMIT ? OFFSET ?`,
		viewerID, viewerID, gameID, pageSize, (page-1)*pageSize,
	)
	if err != nil {
		return GameCommentList{}, err
	}
	defer rows.Close()

	roots := []GameComment{}
	ids := []any{}
	for rows.Next() {
		item, scanErr := scanComment(rows)
		if scanErr != nil {
			return GameCommentList{}, scanErr
		}
		item.CanDelete = viewerID != "" && (item.AuthorID == viewerID || viewerRole == "admin")
		roots = append(roots, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return GameCommentList{}, err
	}
	if len(roots) == 0 {
		return GameCommentList{Items: roots, Total: total, Page: page, PageSize: pageSize}, nil
	}

	replies, err := s.commentReplies(ids, viewerID, viewerRole)
	if err != nil {
		return GameCommentList{}, err
	}
	for index := range roots {
		roots[index].Replies = replies[roots[index].ID]
	}
	return GameCommentList{Items: roots, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) commentReplies(parentIDs []any, viewerID, viewerRole string) (map[string][]GameComment, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(parentIDs)), ",")
	args := append([]any{viewerID, viewerID}, parentIDs...)
	rows, err := s.db.Query(
		commentSelect+` WHERE cm.status='visible' AND cm.parent_id IN (`+placeholders+`)
		 ORDER BY cm.created_at ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grouped := map[string][]GameComment{}
	for rows.Next() {
		item, scanErr := scanComment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		item.CanDelete = viewerID != "" && (item.AuthorID == viewerID || viewerRole == "admin")
		grouped[item.ParentID] = append(grouped[item.ParentID], item)
	}
	return grouped, rows.Err()
}

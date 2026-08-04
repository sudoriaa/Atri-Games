package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Social endpoints all resolve the public slug to a published game first, so a
// draft or hidden game never accumulates likes, shares or comments.
func (s *Server) publishedGameID(w http.ResponseWriter, r *http.Request) (string, bool) {
	game, err := s.store.GameBySlug(r.PathValue("slug"), "", true)
	if err != nil {
		s.writeStoreError(w, r, err)
		return "", false
	}
	return game.ID, true
}

func (s *Server) likeGame(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	if err := s.store.LikeGame(userID, gameID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.writeLikeState(w, r, userID, gameID)
}

func (s *Server) unlikeGame(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	if err := s.store.UnlikeGame(userID, gameID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.writeLikeState(w, r, userID, gameID)
}

func (s *Server) writeLikeState(w http.ResponseWriter, r *http.Request, userID, gameID string) {
	count, liked, err := s.store.GameLikeState(userID, gameID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"likeCount": count, "isLiked": liked})
}

// recordShare counts one share. The channel is a coarse label for reporting; an
// unrecognised value is normalised to "link" rather than rejected so a client
// rollout never loses the count.
func (s *Server) recordShare(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	var input struct {
		Channel string `json:"channel"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input)
	}
	channel := strings.ToLower(strings.TrimSpace(input.Channel))
	if !oneOf(channel, "link", "card", "native", "qrcode") {
		channel = "link"
	}
	count, err := s.store.RecordShare(gameID, currentUser(r).ID, channel)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"shareCount": count, "channel": channel})
}

func (s *Server) gameComments(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	user := currentUser(r)
	list, err := s.store.GameComments(gameID, user.ID, user.Role, page, pageSize)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	// The viewer's own like flags make every response player-specific.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createGameComment(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	var input struct {
		Body     string `json:"body"`
		ParentID string `json:"parentId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "请求内容无法解析")
		return
	}
	comment, err := s.store.CreateGameComment(currentUser(r).ID, gameID, strings.TrimSpace(input.ParentID), input.Body)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.store.NotifyComment(currentUser(r).ID, gameID, strings.TrimSpace(input.ParentID), input.Body); err != nil {
		s.logger.Error("notify comment recipients", "gameId", gameID, "commentId", comment.ID, "error", err)
	}
	comment.CanDelete = true
	writeJSON(w, http.StatusCreated, comment)
}

func (s *Server) deleteGameComment(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	if err := s.store.DeleteGameComment(user.ID, user.Role, gameID, r.PathValue("commentId")); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) likeGameComment(w http.ResponseWriter, r *http.Request) {
	s.writeCommentLike(w, r, true)
}

func (s *Server) unlikeGameComment(w http.ResponseWriter, r *http.Request) {
	s.writeCommentLike(w, r, false)
}

func (s *Server) writeCommentLike(w http.ResponseWriter, r *http.Request, like bool) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	commentID := r.PathValue("commentId")
	var count int64
	var err error
	if like {
		count, err = s.store.LikeComment(userID, gameID, commentID)
	} else {
		count, err = s.store.UnlikeComment(userID, gameID, commentID)
	}
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"likeCount": count, "isLiked": like})
}

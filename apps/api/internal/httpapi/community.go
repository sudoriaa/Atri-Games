package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sudoriaa/atri-games/apps/api/internal/data"
)

func (s *Server) creatorProfile(w http.ResponseWriter, r *http.Request) {
	viewer := currentUser(r)
	profile, err := s.store.CreatorProfile(r.PathValue("id"), viewer.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	games, err := s.store.Games(data.GameFilter{OwnerID: profile.ID, UserID: viewer.ID, Page: 1, PageSize: 100})
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	profile.Games = games.Items
	profile.GameCount = games.Total
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) followCreator(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.FollowCreator(currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) unfollowCreator(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.UnfollowCreator(currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) blockCreator(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.BlockCreator(currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) unblockCreator(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.UnblockCreator(currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) gameFollowState(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	state, err := s.store.GameFollowState(currentUser(r).ID, gameID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) followGame(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	state, err := s.store.FollowGame(currentUser(r).ID, gameID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) unfollowGame(w http.ResponseWriter, r *http.Request) {
	gameID, ok := s.publishedGameID(w, r)
	if !ok {
		return
	}
	state, err := s.store.UnfollowGame(currentUser(r).ID, gameID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) communityFeed(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.CommunityFeed(currentUser(r).ID, limit)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.Notifications(currentUser(r).ID, limit)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	if err := s.store.MarkNotificationsRead(currentUser(r).ID, r.PathValue("id")); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if err := s.store.MarkNotificationsRead(currentUser(r).ID, ""); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) publicGameVersions(w http.ResponseWriter, r *http.Request) {
	viewer := currentUser(r)
	game, err := s.store.GameBySlug(r.PathValue("slug"), viewer.ID, true)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	versions, err := s.store.GameVersions(game.ID, viewer.ID, false)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) myGameVersions(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if _, err := s.store.GameOwnedBy(r.PathValue("id"), user.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	versions, err := s.store.GameVersions(r.PathValue("id"), user.ID, false)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) adminGameVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.store.GameVersions(r.PathValue("id"), currentUser(r).ID, true)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) rollbackMyGame(w http.ResponseWriter, r *http.Request) {
	s.rollbackGame(w, r, currentUser(r).ID)
}

func (s *Server) rollbackAdminGame(w http.ResponseWriter, r *http.Request) {
	s.rollbackGame(w, r, "")
}

func (s *Server) rollbackGame(w http.ResponseWriter, r *http.Request, ownerID string) {
	actor := currentUser(r)
	gameID := r.PathValue("id")
	current, err := s.store.GameByID(gameID, actor.ID)
	if ownerID != "" {
		current, err = s.store.GameOwnedBy(gameID, ownerID)
	}
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	input, version, err := s.store.VersionRollbackInput(gameID, r.PathValue("versionId"), ownerID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	// Package files and managed cover assets remain on their current revision;
	// rollback restores editable metadata and submits it through review again.
	input.Slug = current.Slug
	input.CoverURL = current.CoverURL
	input.LaunchURL = current.LaunchURL
	input.LaunchOpenIn = current.LaunchOpenIn
	input.NetworkRequired = current.NetworkRequired
	input.OwnBackend = current.OwnBackend
	input.RequiresLogin = current.RequiresLogin
	input.UsesPlatformStorage = current.UsesPlatformStorage
	input.MatchmakingEnabled = current.MatchmakingEnabled
	input.Status = "review"
	input.ReleaseNotes = "回滚至 v" + version.Version
	normalizeGameInput(&input)
	if !validateGameInput(w, input) {
		return
	}
	game, err := s.store.UpdateGameWithCoverCleanup(actor.ID, gameID, input, s.config.AssetRoot)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.store.RecordGameVersion(actor.ID, game, input.ReleaseNotes); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.syncGameObjects("covers/" + game.Slug)
	writeJSON(w, http.StatusOK, game)
}

func (s *Server) createReport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Reason     string `json:"reason"`
		Detail     string `json:"detail"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	report, err := s.store.CreateReport(currentUser(r).ID, input.TargetType, input.TargetID, input.Reason, input.Detail)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, report)
}

func (s *Server) myReports(w http.ResponseWriter, r *http.Request) {
	reports, err := s.store.MyReports(currentUser(r).ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) createAppeal(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason string `json:"reason"`
	}
	if !decodeSmallJSON(w, r, &input) {
		return
	}
	appeal, err := s.store.CreateAppeal(currentUser(r).ID, r.PathValue("id"), input.Reason)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, appeal)
}

func (s *Server) adminReports(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "pending" && status != "resolved" && status != "dismissed" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_report_status", "举报状态无效")
		return
	}
	reports, err := s.store.Reports(status)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) resolveReport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status     string `json:"status"`
		Resolution string `json:"resolution"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if utf8.RuneCountInString(input.Resolution) > 1000 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_report", "处置说明不能超过 1000 字")
		return
	}
	report, err := s.store.ResolveReport(currentUser(r).ID, r.PathValue("id"), strings.TrimSpace(input.Status), input.Resolution)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) adminAppeals(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "pending" && status != "accepted" && status != "rejected" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_appeal_status", "申诉状态无效")
		return
	}
	appeals, err := s.store.Appeals(status)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, appeals)
}

func (s *Server) resolveAppeal(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status     string `json:"status"`
		Resolution string `json:"resolution"`
	}
	if !decodeSmallJSON(w, r, &input) {
		return
	}
	appeal, err := s.store.ResolveAppeal(currentUser(r).ID, r.PathValue("id"), strings.TrimSpace(input.Status), input.Resolution)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, appeal)
}

func decodeSmallJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求内容格式无效")
		return false
	}
	return true
}

func versionEventSummary(game data.Game, releaseNotes string) string {
	releaseNotes = strings.TrimSpace(releaseNotes)
	if releaseNotes == "" {
		return fmt.Sprintf("发布了 v%s", game.Version)
	}
	return fmt.Sprintf("发布了 v%s：%s", game.Version, releaseNotes)
}

func (s *Server) recordPublishedGameEvent(game data.Game, releaseNotes string) {
	kind := "game.updated"
	if !s.store.HasGameCommunityEvent(game.ID) {
		kind = "game.published"
	}
	if err := s.store.RecordGameCommunityEvent(game.OwnerID, game, kind, versionEventSummary(game, releaseNotes)); err != nil {
		s.logger.Error("record game community event", "gameId", game.ID, "error", err)
	}
}

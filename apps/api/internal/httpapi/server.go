package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sudoriaa/atri-games/apps/api/internal/config"
	"github.com/sudoriaa/atri-games/apps/api/internal/data"
	"github.com/sudoriaa/atri-games/apps/api/internal/gamepkg"
	"github.com/sudoriaa/atri-games/apps/api/internal/objectstore"
	"github.com/sudoriaa/atri-games/apps/api/internal/security"
)

type contextKey string

const (
	userContextKey contextKey = "auth-user"
	gameClaimsKey  contextKey = "game-claims"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const dummyPasswordHash = "$2a$12$1/3uL9JcfoEnAxClFho0w.x7iW5RIyKSpNa4Sx6bYd5t22pOORfou"

type Server struct {
	config           config.Config
	store            *data.Store
	tokens           *security.TokenManager
	logger           *slog.Logger
	http             *http.Server
	limiter          *authLimiter
	gameReadLimiter  *windowLimiter
	gameWriteLimiter *windowLimiter
	assets           objectstore.Store
}

func New(cfg config.Config, store *data.Store, tokens *security.TokenManager, logger *slog.Logger) *Server {
	return NewWithObjectStore(cfg, store, tokens, logger, objectstore.DisabledStore{})
}

// NewWithObjectStore wires the API to an optional managed-asset mirror. Tests
// and local development use DisabledStore; production passes the MinIO store
// initialized during process startup.
func NewWithObjectStore(cfg config.Config, store *data.Store, tokens *security.TokenManager, logger *slog.Logger, assets objectstore.Store) *Server {
	if assets == nil {
		assets = objectstore.DisabledStore{}
	}
	server := &Server{
		config:           cfg,
		store:            store,
		tokens:           tokens,
		logger:           logger,
		limiter:          newAuthLimiter(),
		gameReadLimiter:  newWindowLimiter(600, time.Minute),
		gameWriteLimiter: newWindowLimiter(120, time.Minute),
		assets:           assets,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", server.health)
	mux.Handle("POST /api/v1/auth/register", server.limiter.wrap(http.HandlerFunc(server.register)))
	mux.Handle("POST /api/v1/auth/login", server.limiter.wrap(http.HandlerFunc(server.login)))
	mux.Handle("GET /api/v1/me", server.requireUser(http.HandlerFunc(server.me)))
	mux.Handle("PATCH /api/v1/me", server.requireUser(http.HandlerFunc(server.updateMe)))
	mux.HandleFunc("GET /api/v1/categories", server.categories)
	mux.Handle("GET /api/v1/games", server.optionalUser(http.HandlerFunc(server.games)))
	mux.Handle("GET /api/v1/games/{slug}", server.optionalUser(http.HandlerFunc(server.game)))
	mux.Handle("POST /api/v1/games/{slug}/launch", server.optionalUser(http.HandlerFunc(server.launch)))
	// A game-scoped ticket is deliberately separate from the platform JWT.
	// The aliases keep older SDK previews working while /ticket is canonical.
	mux.Handle("POST /api/v1/games/{slug}/ticket", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.gameTicket))))
	mux.Handle("POST /api/v1/games/{slug}/session", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.gameTicket))))
	mux.Handle("POST /api/v1/games/{slug}/session-ticket", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.gameTicket))))
	mux.Handle("POST /api/v1/games/{slug}/ticket/refresh", server.requireGameTicket(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.refreshGameTicket))))
	mux.Handle("GET /api/v1/games/{slug}/data/{key}", server.requireGameTicket(server.limitGameRequests(server.gameReadLimiter, http.HandlerFunc(server.getGameData))))
	mux.Handle("PUT /api/v1/games/{slug}/data/{key}", server.requireGameTicket(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.putGameData))))
	mux.Handle("DELETE /api/v1/games/{slug}/data/{key}", server.requireGameTicket(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.deleteGameData))))
	mux.Handle("POST /api/v1/games/{slug}/matchmaking/tickets", server.requireGameTicket(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.createMatchTicket))))
	mux.Handle("GET /api/v1/games/{slug}/matchmaking/tickets/{ticketId}", server.requireGameTicket(server.limitGameRequests(server.gameReadLimiter, http.HandlerFunc(server.getMatchTicket))))
	mux.Handle("DELETE /api/v1/games/{slug}/matchmaking/tickets/{ticketId}", server.requireGameTicket(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.cancelMatchTicket))))
	mux.Handle("POST /api/v1/games/{slug}/likes", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.likeGame))))
	mux.Handle("DELETE /api/v1/games/{slug}/likes", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.unlikeGame))))
	mux.Handle("POST /api/v1/games/{slug}/shares", server.optionalUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.recordShare))))
	mux.Handle("GET /api/v1/games/{slug}/comments", server.optionalUser(http.HandlerFunc(server.gameComments)))
	mux.Handle("POST /api/v1/games/{slug}/comments", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.createGameComment))))
	mux.Handle("DELETE /api/v1/games/{slug}/comments/{commentId}", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.deleteGameComment))))
	mux.Handle("POST /api/v1/games/{slug}/comments/{commentId}/likes", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.likeGameComment))))
	mux.Handle("DELETE /api/v1/games/{slug}/comments/{commentId}/likes", server.requireUser(server.limitGameRequests(server.gameWriteLimiter, http.HandlerFunc(server.unlikeGameComment))))
	mux.Handle("GET /api/v1/me/favorites", server.requireUser(http.HandlerFunc(server.favorites)))
	mux.Handle("POST /api/v1/me/favorites/{gameId}", server.requireUser(http.HandlerFunc(server.addFavorite)))
	mux.Handle("DELETE /api/v1/me/favorites/{gameId}", server.requireUser(http.HandlerFunc(server.removeFavorite)))

	mux.Handle("GET /api/v1/admin/dashboard", server.requireAdmin(http.HandlerFunc(server.adminDashboard)))
	mux.Handle("GET /api/v1/admin/activity", server.requireAdmin(http.HandlerFunc(server.adminActivity)))
	mux.Handle("GET /api/v1/admin/games", server.requireAdmin(http.HandlerFunc(server.adminGames)))
	mux.Handle("POST /api/v1/admin/games", server.requireAdmin(http.HandlerFunc(server.createGame)))
	mux.Handle("POST /api/v1/admin/games/import", server.requireAdmin(http.HandlerFunc(server.importGame)))
	mux.Handle("PUT /api/v1/admin/games/{id}", server.requireAdmin(http.HandlerFunc(server.updateGame)))
	mux.Handle("POST /api/v1/admin/games/{id}/unpublish", server.requireAdmin(http.HandlerFunc(server.unpublishGame)))
	mux.Handle("DELETE /api/v1/admin/games/{id}", server.requireAdmin(http.HandlerFunc(server.deleteGame)))
	mux.Handle("GET /api/v1/admin/users", server.requireAdmin(http.HandlerFunc(server.adminUsers)))
	mux.Handle("PATCH /api/v1/admin/users/{id}", server.requireAdmin(http.HandlerFunc(server.updateUser)))
	mux.Handle("GET /api/v1/admin/categories", server.requireAdmin(http.HandlerFunc(server.adminCategories)))
	mux.Handle("POST /api/v1/admin/categories", server.requireAdmin(http.HandlerFunc(server.createCategory)))
	mux.Handle("PUT /api/v1/admin/categories/{id}", server.requireAdmin(http.HandlerFunc(server.updateCategory)))
	mux.Handle("DELETE /api/v1/admin/categories/{id}", server.requireAdmin(http.HandlerFunc(server.deleteCategory)))

	handler := server.recoverer(server.requestLog(server.securityHeaders(server.cors(mux))))
	server.http = &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       90 * time.Second,
	}
	return server
}

func (s *Server) ListenAndServe() error {
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	provider := "local"
	if s.assets != nil {
		provider = s.assets.Provider()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
		"objectStorage": map[string]string{
			"provider": provider,
			"status":   "ready",
		},
	})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if !validEmail(input.Email) || utf8.RuneCountInString(input.DisplayName) < 2 || utf8.RuneCountInString(input.DisplayName) > 40 || utf8.RuneCountInString(input.Password) < 8 || len(input.Password) > 72 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_input", "请填写有效邮箱、2-40 字昵称和至少 8 位密码")
		return
	}
	hash, err := security.HashPassword(input.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	user, err := s.store.CreateUser(input.Email, hash, input.DisplayName)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.writeAuth(w, r, user, http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user, err := s.store.UserByEmail(strings.ToLower(strings.TrimSpace(input.Email)))
	passwordHash := user.PasswordHash
	if err != nil {
		passwordHash = dummyPasswordHash
	}
	passwordMatches := security.CheckPassword(passwordHash, input.Password)
	if err != nil || !passwordMatches {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "邮箱或密码不正确")
		return
	}
	if user.Status != "active" {
		writeError(w, http.StatusForbidden, "account_suspended", "当前账户已停用")
		return
	}
	s.writeAuth(w, r, user, http.StatusOK)
}

func (s *Server) writeAuth(w http.ResponseWriter, r *http.Request, user data.User, status int) {
	token, err := s.tokens.Issue(user.ID, user.Role)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, map[string]any{"token": token, "user": user})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DisplayName string `json:"displayName"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if count := utf8.RuneCountInString(input.DisplayName); count < 2 || count > 40 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_display_name", "昵称需要 2-40 个字符")
		return
	}
	user, err := s.store.UpdateProfile(currentUser(r).ID, input.DisplayName)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Categories()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if currentUser(r).ID == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) games(w http.ResponseWriter, r *http.Request) {
	filter := parseGameFilter(r, false)
	if user := currentUser(r); user.ID != "" {
		filter.UserID = user.ID
	}
	result, err := s.store.Games(filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if filter.UserID == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) game(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r).ID
	game, err := s.store.GameBySlug(r.PathValue("slug"), userID, true)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if userID == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	writeJSON(w, http.StatusOK, game)
}

func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	platform, err := s.store.GamePlatformBySlug(slug, true)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	userID := currentUser(r).ID
	if platform.RequiresLogin && userID == "" {
		w.Header().Set("Cache-Control", "private, no-store")
		writeError(w, http.StatusUnauthorized, "authentication_required", "该游戏需要登录后才能游玩")
		return
	}
	launch, err := s.store.RecordLaunch(slug, userID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	response := map[string]any{"launchUrl": launch.URL, "openIn": launch.OpenIn}
	if userID != "" && platform.SessionAllowed() {
		ticket, expiresAt, issueErr := s.issueGameTicket(currentUser(r), platform)
		if issueErr != nil {
			s.internalError(w, r, issueErr)
			return
		}
		response["gameTicket"] = ticket
		response["expiresAt"] = expiresAt.Format(time.RFC3339)
		response["gameTicketExpiresAt"] = expiresAt.Format(time.RFC3339)
		response["apiBase"] = "/api/v1"
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) gameTicket(w http.ResponseWriter, r *http.Request) {
	platform, err := s.store.GamePlatformBySlug(r.PathValue("slug"), true)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !platform.SessionAllowed() {
		writeError(w, http.StatusConflict, "platform_services_disabled", "该游戏未启用内置玩家服务")
		return
	}
	ticket, expiresAt, err := s.issueGameTicket(currentUser(r), platform)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeGameTicket(w, currentUser(r), platform, ticket, expiresAt)
}

func (s *Server) refreshGameTicket(w http.ResponseWriter, r *http.Request) {
	platform, _, ok := s.authorizedGamePlatform(w, r, "")
	if !ok {
		return
	}
	claims, _ := gameClaims(r)
	currentScopes := platform.Scopes()
	refreshedScopes := make([]string, 0, len(currentScopes))
	for _, allowed := range currentScopes {
		for _, granted := range claims.Scopes {
			if granted == allowed {
				refreshedScopes = append(refreshedScopes, allowed)
				break
			}
		}
	}
	ticket, expiresAt, err := s.issueGameTicketScopes(currentUser(r), platform, refreshedScopes)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeGameTicket(w, currentUser(r), platform, ticket, expiresAt)
}

func (s *Server) writeGameTicket(w http.ResponseWriter, user data.User, platform data.GamePlatform, ticket string, expiresAt time.Time) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":    ticket,
		"expiresAt": expiresAt.Format(time.RFC3339),
		"game": map[string]string{
			"id":   platform.GameID,
			"slug": platform.Slug,
		},
		"user": map[string]string{
			"id":          user.ID,
			"displayName": user.DisplayName,
		},
		"scopes": platform.Scopes(),
	})
}

func (s *Server) issueGameTicket(user data.User, platform data.GamePlatform) (string, time.Time, error) {
	return s.issueGameTicketScopes(user, platform, platform.Scopes())
}

func (s *Server) issueGameTicketScopes(user data.User, platform data.GamePlatform, scopes []string) (string, time.Time, error) {
	ttl := s.config.GameTicketTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return s.tokens.IssueGameTicket(user.ID, platform.GameID, platform.Slug, scopes, ttl)
}

func (s *Server) getGameData(w http.ResponseWriter, r *http.Request) {
	platform, userID, ok := s.authorizedGamePlatform(w, r, "storage")
	if !ok {
		return
	}
	item, err := s.store.GetGameData(platform, userID, r.PathValue("key"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) putGameData(w http.ResponseWriter, r *http.Request) {
	platform, userID, ok := s.authorizedGamePlatform(w, r, "storage")
	if !ok {
		return
	}
	var input struct {
		Value json.RawMessage `json:"value"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if len(input.Value) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_game_data", "请求必须包含 value JSON 字段")
		return
	}
	item, err := s.store.PutGameData(platform, userID, r.PathValue("key"), input.Value)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteGameData(w http.ResponseWriter, r *http.Request) {
	platform, userID, ok := s.authorizedGamePlatform(w, r, "storage")
	if !ok {
		return
	}
	if err := s.store.DeleteGameData(platform, userID, r.PathValue("key")); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createMatchTicket(w http.ResponseWriter, r *http.Request) {
	platform, userID, ok := s.authorizedGamePlatform(w, r, "matchmaking")
	if !ok {
		return
	}
	var input struct {
		Mode   string `json:"mode"`
		Region string `json:"region"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	ticket, err := s.store.CreateMatchTicket(platform, userID, input.Mode, input.Region)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	ticket.UserID = ""
	writeJSON(w, http.StatusCreated, ticket)
}

func (s *Server) getMatchTicket(w http.ResponseWriter, r *http.Request) {
	platform, userID, ok := s.authorizedGamePlatform(w, r, "matchmaking")
	if !ok {
		return
	}
	ticket, err := s.store.MatchTicketByID(platform.GameID, userID, r.PathValue("ticketId"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	ticket.UserID = ""
	writeJSON(w, http.StatusOK, ticket)
}

func (s *Server) cancelMatchTicket(w http.ResponseWriter, r *http.Request) {
	platform, userID, ok := s.authorizedGamePlatform(w, r, "matchmaking")
	if !ok {
		return
	}
	if err := s.store.CancelMatchTicket(platform.GameID, userID, r.PathValue("ticketId")); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authorizedGamePlatform validates both the game-scoped JWT and the
// capability declaration. It is intentionally shared by storage and
// matchmaking handlers so a ticket issued for another game cannot be reused.
func (s *Server) authorizedGamePlatform(w http.ResponseWriter, r *http.Request, requiredScope string) (data.GamePlatform, string, bool) {
	platform, err := s.store.GamePlatformBySlug(r.PathValue("slug"), true)
	if err != nil {
		s.writeStoreError(w, r, err)
		return data.GamePlatform{}, "", false
	}
	claims, ok := gameClaims(r)
	if !ok || claims.GameID != platform.GameID || claims.GameSlug != platform.Slug {
		writeError(w, http.StatusUnauthorized, "invalid_game_ticket", "游戏票据无效或已过期")
		return data.GamePlatform{}, "", false
	}
	hasScope := requiredScope == ""
	for _, scope := range claims.Scopes {
		if scope == requiredScope {
			hasScope = true
			break
		}
	}
	if !hasScope {
		writeError(w, http.StatusForbidden, "insufficient_game_scope", "游戏票据未包含所需服务权限")
		return data.GamePlatform{}, "", false
	}
	user := currentUser(r)
	if user.ID == "" || user.Status != "active" {
		writeError(w, http.StatusUnauthorized, "invalid_account", "账户不可用")
		return data.GamePlatform{}, "", false
	}
	w.Header().Set("Cache-Control", "private, no-store")
	return platform, user.ID, true
}

func gameClaims(r *http.Request) (*security.Claims, bool) {
	claims, ok := r.Context().Value(gameClaimsKey).(*security.Claims)
	return claims, ok && claims != nil
}

func (s *Server) favorites(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.FavoriteGames(currentUser(r).ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) addFavorite(w http.ResponseWriter, r *http.Request) {
	if err := s.store.AddFavorite(currentUser(r).ID, r.PathValue("gameId")); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeFavorite(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RemoveFavorite(currentUser(r).ID, r.PathValue("gameId")); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.store.Dashboard()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) adminActivity(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Activity(30)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) adminGames(w http.ResponseWriter, r *http.Request) {
	filter := parseGameFilter(r, true)
	result, err := s.store.Games(filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createGame(w http.ResponseWriter, r *http.Request) {
	input, cover, ok := s.decodeGameMutation(w, r)
	if !ok {
		return
	}
	defer cover.cleanup()
	normalizeGameInput(&input)
	if cover != nil && slugPattern.MatchString(input.Slug) {
		coverURL, err := data.ManagedGameCoverURL(input.Slug, cover.digest, cover.extension)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		input.CoverURL = coverURL
	}
	if !validateGameInput(w, input) {
		return
	}
	var game data.Game
	var err error
	if cover == nil {
		game, err = s.store.CreateGame(currentUser(r).ID, input)
	} else {
		game, err = s.store.CreateGameWithCover(currentUser(r).ID, input, cover.upload(), s.config.AssetRoot)
	}
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.syncGameObjects("covers/" + game.Slug)
	writeJSON(w, http.StatusCreated, game)
}

func (s *Server) importGame(w http.ResponseWriter, r *http.Request) {
	maxBytes := s.config.GamePackageMaxBytes
	if maxBytes <= 0 {
		maxBytes = gamepkg.DefaultMaxArchive
	}
	// The archive limit applies to the decrypted ZIP. Reserve bounded space for
	// the multipart envelope and the authenticated encrypted-container header,
	// while gamepkg still enforces MaxArchiveBytes after decryption.
	const (
		multipartOverhead       = int64(1024 * 1024)
		encryptedContainerSlack = int64(1024 * 1024)
	)
	maxUploadBytes := maxBytes + encryptedContainerSlack
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+multipartOverhead)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "请使用 multipart/form-data 上传 .atri 游戏包")
		return
	}
	importsRoot := filepath.Join(s.config.AssetRoot, ".atri-imports")
	if err := os.MkdirAll(importsRoot, 0o700); err != nil {
		s.internalError(w, r, err)
		return
	}
	archive, err := os.CreateTemp(importsRoot, "upload-*.atri")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	defer archive.Close()

	fields := map[string]string{}
	var packageSeen bool
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			writeError(w, http.StatusBadRequest, "invalid_package", "游戏包上传不完整")
			return
		}
		name := part.FormName()
		if name == "package" {
			if packageSeen {
				part.Close()
				writeError(w, http.StatusBadRequest, "invalid_package", "一次只能上传一个游戏包")
				return
			}
			packageSeen = true
			written, copyErr := io.Copy(archive, io.LimitReader(part, maxUploadBytes+1))
			closeErr := part.Close()
			if copyErr != nil || closeErr != nil {
				writeError(w, http.StatusBadRequest, "invalid_package", "游戏包上传不完整")
				return
			}
			if written > maxUploadBytes {
				writeError(w, http.StatusRequestEntityTooLarge, "package_too_large", "游戏包超过服务器配置的大小上限")
				return
			}
			continue
		}
		if name == "categoryId" || name == "status" || name == "replace" {
			value, readErr := io.ReadAll(io.LimitReader(part, 4097))
			part.Close()
			if readErr != nil || len(value) > 4096 {
				writeError(w, http.StatusBadRequest, "invalid_package_options", "游戏包选项无效")
				return
			}
			fields[name] = strings.TrimSpace(string(value))
			continue
		}
		part.Close()
	}
	if !packageSeen {
		writeError(w, http.StatusBadRequest, "missing_package", "请选择 .atri 游戏包")
		return
	}
	if err := archive.Sync(); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := archive.Close(); err != nil {
		s.internalError(w, r, err)
		return
	}

	limits := gamepkg.Limits{
		MaxArchiveBytes:  maxBytes,
		MaxUnpackedBytes: s.config.GamePackageMaxUnpackedBytes,
		MaxFiles:         s.config.GamePackageMaxFiles,
	}
	prepared, err := gamepkg.ExtractWithPrivateKey(archivePath, s.config.AssetRoot, limits, s.config.PackageDecryptionPrivateKey)
	if err != nil {
		s.writePackageImportError(w, err)
		return
	}
	defer prepared.Cleanup()

	categoryID := fields["categoryId"]
	status := fields["status"]
	if status == "" {
		status = "draft"
	}
	if !slugPattern.MatchString(categoryID) || !oneOf(status, "draft", "review", "published", "hidden") {
		writeError(w, http.StatusUnprocessableEntity, "invalid_package_options", "请选择有效分类和发布状态")
		return
	}
	categoryExists, err := s.store.CategoryExists(categoryID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !categoryExists {
		writeError(w, http.StatusUnprocessableEntity, "invalid_package_options", "所选游戏分类不存在")
		return
	}
	replace := false
	if fields["replace"] != "" {
		replace, err = strconv.ParseBool(fields["replace"])
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_package_options", "覆盖选项无效")
			return
		}
	}

	manifest := prepared.Manifest
	engine := manifest.Engine.Name
	if manifest.Engine.Framework != "" && !strings.EqualFold(manifest.Engine.Framework, engine) {
		engine += " / " + manifest.Engine.Framework
	}
	coverExtension := strings.ToLower(filepath.Ext(manifest.Media.Cover))
	coverURL := "/covers/" + manifest.ID + "/cover" + coverExtension
	launchURL := manifest.Runtime.URL
	if manifest.Runtime.Kind == "static" {
		entry := manifest.Runtime.Entry
		if entry == "index.html" || entry == "" {
			launchURL = "/games/" + manifest.ID + "/play"
		} else {
			launchURL = "/games/" + manifest.ID + "/play/" + entry
		}
	}
	hints := manifest.CapabilityHints()
	input := data.GameInput{
		Slug:                manifest.ID,
		Title:               manifest.Title,
		Summary:             manifest.Summary,
		Description:         manifest.Description,
		AuthorName:          manifest.Authors[0].Name,
		CoverURL:            coverURL,
		LaunchURL:           launchURL,
		LaunchOpenIn:        manifest.Runtime.OpenIn,
		RepositoryURL:       manifest.Repository,
		Engine:              engine,
		Version:             manifest.Version,
		Status:              status,
		CategoryID:          categoryID,
		NetworkRequired:     boolValue(manifest.Services.NetworkRequired),
		OwnBackend:          boolValue(manifest.Services.OwnBackend),
		RequiresLogin:       hints.RequiresLogin,
		UsesPlatformStorage: hints.UsesPlatformStorage,
		MatchmakingEnabled:  hints.MatchmakingEnabled,
		Tags:                manifest.Tags,
	}
	normalizeGameInput(&input)
	if !validateGameInput(w, input) {
		return
	}
	_, existingErr := s.store.GameBySlug(input.Slug, "", false)
	existed := existingErr == nil
	if existingErr != nil && !errors.Is(existingErr, data.ErrNotFound) {
		s.internalError(w, r, existingErr)
		return
	}
	if existed && !replace {
		writeError(w, http.StatusConflict, "game_exists", "该游戏标识已经存在；确认版本无误后可勾选覆盖")
		return
	}
	manifestRaw, err := os.ReadFile(prepared.ManifestPath)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	game, err := s.store.ImportGame(currentUser(r).ID, data.ImportedGame{
		Input:        input,
		Kind:         manifest.Runtime.Kind,
		ManifestJSON: string(manifestRaw),
		CoverSource:  prepared.CoverPath,
		BundleSource: prepared.BundlePath,
	}, s.config.AssetRoot, replace)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	statusCode := http.StatusCreated
	if existed {
		statusCode = http.StatusOK
	}
	s.syncGameObjects("covers/"+game.Slug, "playables/"+game.Slug, "demos/"+game.Slug)
	writeJSON(w, statusCode, game)
}

func (s *Server) writePackageImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gamepkg.ErrEncryptedPackagePrivateKeyRequired):
		if s.config.PackageDecryptionPrivateKeyError != "" {
			writeError(w, http.StatusUnprocessableEntity, "package_decryption_key_configuration_invalid", s.config.PackageDecryptionPrivateKeyError)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "package_decryption_key_required", "该 .atri 游戏包已加密；请配置 ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64 后重试导入")
	case errors.Is(err, gamepkg.ErrEncryptedPackagePrivateKeyInvalid):
		writeError(w, http.StatusUnprocessableEntity, "package_decryption_key_invalid", "已配置的 .atri 解密私钥格式无效")
	case errors.Is(err, gamepkg.ErrEncryptedPackageUnsupported):
		writeError(w, http.StatusUnprocessableEntity, "encrypted_package_unsupported", "该 .atri 游戏包使用了当前平台不支持的加密版本或算法")
	case errors.Is(err, gamepkg.ErrInvalidEncryptedPackage):
		writeError(w, http.StatusUnprocessableEntity, "invalid_encrypted_package", "加密 .atri 游戏包格式无效、内容已损坏或认证校验失败")
	default:
		var validation *gamepkg.ValidationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_manifest", validation.Error())
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_package", err.Error())
	}
}

func (s *Server) updateGame(w http.ResponseWriter, r *http.Request) {
	input, cover, ok := s.decodeGameMutation(w, r)
	if !ok {
		return
	}
	defer cover.cleanup()
	normalizeGameInput(&input)
	if cover != nil && slugPattern.MatchString(input.Slug) {
		coverURL, err := data.ManagedGameCoverURL(input.Slug, cover.digest, cover.extension)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		input.CoverURL = coverURL
	}
	if !validateGameInput(w, input) {
		return
	}
	var game data.Game
	var err error
	if cover == nil {
		game, err = s.store.UpdateGameWithCoverCleanup(currentUser(r).ID, r.PathValue("id"), input, s.config.AssetRoot)
	} else {
		game, err = s.store.UpdateGameWithCover(currentUser(r).ID, r.PathValue("id"), input, cover.upload(), s.config.AssetRoot)
	}
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.syncGameObjects("covers/" + game.Slug)
	writeJSON(w, http.StatusOK, game)
}

func (s *Server) deleteGame(w http.ResponseWriter, r *http.Request) {
	game, err := s.store.GameByID(r.PathValue("id"), "")
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.store.DeleteGame(currentUser(r).ID, r.PathValue("id"), s.config.AssetRoot); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.syncGameObjects("covers/"+game.Slug, "playables/"+game.Slug, "demos/"+game.Slug)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unpublishGame(w http.ResponseWriter, r *http.Request) {
	game, err := s.store.UnpublishGame(currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, game)
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !oneOf(input.Role, "user", "admin") || !oneOf(input.Status, "active", "suspended") {
		writeError(w, http.StatusUnprocessableEntity, "invalid_access", "角色或账户状态无效")
		return
	}
	if r.PathValue("id") == currentUser(r).ID && (input.Status != "active" || input.Role != "admin") {
		writeError(w, http.StatusConflict, "self_lockout", "不能移除自己的管理员权限或停用当前账户")
		return
	}
	user, err := s.store.UpdateUserAccess(currentUser(r).ID, r.PathValue("id"), input.Role, input.Status)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) adminCategories(w http.ResponseWriter, r *http.Request) {
	s.categories(w, r)
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var input data.Category
	if !decodeJSON(w, r, &input) {
		return
	}
	normalizeCategory(&input)
	if !validateCategory(w, input, true) {
		return
	}
	item, err := s.store.CreateCategory(currentUser(r).ID, input)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) {
	var input data.Category
	if !decodeJSON(w, r, &input) {
		return
	}
	normalizeCategory(&input)
	if !validateCategory(w, input, false) {
		return
	}
	item, err := s.store.UpdateCategory(currentUser(r).ID, r.PathValue("id"), input)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCategory(currentUser(r).ID, r.PathValue("id")); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseGameFilter(r *http.Request, admin bool) data.GameFilter {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	pageSize, _ := strconv.Atoi(query.Get("pageSize"))
	filter := data.GameFilter{
		Query: strings.TrimSpace(query.Get("query")), CategoryID: query.Get("category"), Status: query.Get("status"),
		Page: page, PageSize: pageSize, Admin: admin,
	}
	if raw := query.Get("featured"); raw != "" {
		value := raw == "true"
		filter.Featured = &value
	}
	return filter
}

func validateGameInput(w http.ResponseWriter, input data.GameInput) bool {
	if !slugPattern.MatchString(input.Slug) || len(input.Slug) > 64 || strings.TrimSpace(input.Title) == "" || utf8.RuneCountInString(input.Title) > 80 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_game", "游戏标识或标题无效")
		return false
	}
	if utf8.RuneCountInString(input.Summary) < 10 || utf8.RuneCountInString(input.Summary) > 240 || input.Description == "" || utf8.RuneCountInString(input.Description) > 4000 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_description", "摘要需要 10-240 字，详情不能超过 4000 字")
		return false
	}
	if !validWebURL(input.LaunchURL, true) || !validWebURL(input.CoverURL, true) || (input.RepositoryURL != "" && !validWebURL(input.RepositoryURL, false)) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_url", "启动、封面或仓库地址无效")
		return false
	}
	if !oneOf(input.Status, "draft", "review", "published", "hidden") ||
		!oneOf(input.LaunchOpenIn, "same-tab", "new-tab") ||
		!slugPattern.MatchString(input.CategoryID) || len(input.CategoryID) > 64 ||
		input.Engine == "" || utf8.RuneCountInString(input.Engine) > 80 ||
		input.Version == "" || utf8.RuneCountInString(input.Version) > 40 ||
		input.AuthorName == "" || utf8.RuneCountInString(input.AuthorName) > 80 ||
		len(input.Tags) > 10 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_game", "游戏字段不完整或状态无效")
		return false
	}
	for _, tag := range input.Tags {
		if tag == "" || utf8.RuneCountInString(tag) > 40 {
			writeError(w, http.StatusUnprocessableEntity, "invalid_game", "游戏标签无效")
			return false
		}
	}
	return true
}

func normalizeGameInput(input *data.GameInput) {
	input.Slug = strings.TrimSpace(input.Slug)
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Description = strings.TrimSpace(input.Description)
	input.AuthorName = strings.TrimSpace(input.AuthorName)
	input.CoverURL = strings.TrimSpace(input.CoverURL)
	input.LaunchURL = strings.TrimSpace(input.LaunchURL)
	input.LaunchOpenIn = strings.TrimSpace(input.LaunchOpenIn)
	if input.LaunchOpenIn == "" {
		input.LaunchOpenIn = "same-tab"
	}
	input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	input.Engine = strings.TrimSpace(input.Engine)
	input.Version = strings.TrimSpace(input.Version)
	input.Status = strings.TrimSpace(input.Status)
	input.CategoryID = strings.TrimSpace(input.CategoryID)

	tags := make([]string, 0, len(input.Tags))
	seen := make(map[string]struct{}, len(input.Tags))
	for _, raw := range input.Tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	input.Tags = tags
}

func validateCategory(w http.ResponseWriter, input data.Category, requireID bool) bool {
	if (requireID && !slugPattern.MatchString(input.ID)) || strings.TrimSpace(input.Name) == "" || utf8.RuneCountInString(input.Name) > 40 || input.SortOrder < 0 || input.SortOrder > 9999 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_category", "分类标识、名称或排序值无效")
		return false
	}
	return true
}

func normalizeCategory(input *data.Category) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value) && len(value) <= 254
}

func validWebURL(value string, allowRelative bool) bool {
	if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\\\r\n\t") {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || strings.Contains(parsed.Path, "\\") {
		return false
	}
	if allowRelative && parsed.Scheme == "" && parsed.Host == "" &&
		strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") &&
		strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//") {
		return true
	}
	return parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return s.authenticate(true, false, next)
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return s.authenticate(true, true, next)
}

func (s *Server) optionalUser(next http.Handler) http.Handler {
	return s.authenticate(false, false, next)
}

func (s *Server) authenticate(required, admin bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Authorization")
		header := r.Header.Get("Authorization")
		if header == "" {
			if required {
				w.Header().Set("Cache-Control", "private, no-store")
				writeError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "invalid_token", "登录状态无效")
			return
		}
		claims, err := s.tokens.Parse(parts[1])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_token", "登录状态已过期，请重新登录")
			return
		}
		if claims.TokenType == "game" || claims.Kind == "game" {
			// A game ticket is deliberately narrower than a platform session.
			// Never let it reach /me, favorites or admin handlers.
			writeError(w, http.StatusUnauthorized, "invalid_token", "请使用平台登录状态")
			return
		}
		user, err := s.store.UserByID(claims.Subject)
		if err != nil || user.Status != "active" {
			writeError(w, http.StatusUnauthorized, "invalid_account", "账户不可用")
			return
		}
		if admin && user.Role != "admin" {
			writeError(w, http.StatusForbidden, "admin_required", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)))
	})
}

func (s *Server) requireGameTicket(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Authorization")
		header := r.Header.Get("Authorization")
		if header == "" {
			w.Header().Set("Cache-Control", "private, no-store")
			writeError(w, http.StatusUnauthorized, "authentication_required", "请先登录并获取游戏票据")
			return
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "invalid_game_ticket", "游戏票据格式无效")
			return
		}
		platform, err := s.store.GamePlatformBySlug(r.PathValue("slug"), true)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		claims, err := s.tokens.ParseGameTicket(parts[1], platform.GameID, platform.Slug)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_game_ticket", "游戏票据无效或已过期")
			return
		}
		user, err := s.store.UserByID(claims.Subject)
		if err != nil || user.Status != "active" {
			writeError(w, http.StatusUnauthorized, "invalid_account", "账户不可用")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, gameClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) data.User {
	user, _ := r.Context().Value(userContextKey).(data.User)
	return user
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求内容格式无效")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *Server) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, data.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "请求的记录不存在")
	case errors.Is(err, data.ErrLastAdmin):
		writeError(w, http.StatusConflict, "last_admin", "系统必须保留至少一名可用的管理员")
	case errors.Is(err, data.ErrGameAlreadyExists):
		writeError(w, http.StatusConflict, "game_exists", "相同游戏标识已经存在")
	case errors.Is(err, data.ErrAssetShared):
		writeError(w, http.StatusConflict, "asset_shared", "该游戏目录仍被其他游戏引用，未覆盖本地文件")
	case errors.Is(err, data.ErrPlatformUnavailable):
		writeError(w, http.StatusConflict, "platform_services_disabled", "该游戏未启用内置平台服务")
	case errors.Is(err, data.ErrGameLoginRequired):
		writeError(w, http.StatusUnauthorized, "authentication_required", "该游戏需要登录后才能使用此服务")
	case errors.Is(err, data.ErrGameStorageDisabled):
		writeError(w, http.StatusConflict, "storage_disabled", "该游戏未启用内置数据服务")
	case errors.Is(err, data.ErrGameStorageQuota):
		writeError(w, http.StatusRequestEntityTooLarge, "game_storage_quota_exceeded", "该玩家在此游戏中的内置数据已达到配额")
	case errors.Is(err, data.ErrMatchmakingDisabled):
		writeError(w, http.StatusConflict, "matchmaking_disabled", "该游戏未启用内置匹配服务")
	case errors.Is(err, data.ErrMatchTicketExists):
		writeError(w, http.StatusConflict, "match_ticket_exists", "你已经在该游戏的匹配队列中")
	case errors.Is(err, data.ErrMatchTicketNotActive):
		writeError(w, http.StatusConflict, "match_ticket_not_active", "匹配票据已经结束或取消")
	case errors.Is(err, data.ErrInvalidGameData):
		writeError(w, http.StatusUnprocessableEntity, "invalid_game_data", "游戏数据格式或大小无效")
	case errors.Is(err, data.ErrInvalidComment):
		writeError(w, http.StatusUnprocessableEntity, "invalid_comment", "留言内容不能为空，且不超过 1000 字")
	case errors.Is(err, data.ErrCommentTooDeep):
		writeError(w, http.StatusUnprocessableEntity, "comment_too_deep", "只支持对主留言回复一层")
	case errors.Is(err, data.ErrCommentForbidden):
		writeError(w, http.StatusForbidden, "comment_forbidden", "只能删除自己的留言")
	case errors.Is(err, data.ErrInvalidMatchmaking):
		writeError(w, http.StatusUnprocessableEntity, "invalid_matchmaking", "匹配参数格式无效")
	case strings.Contains(strings.ToLower(err.Error()), "unique"):
		writeError(w, http.StatusConflict, "already_exists", "相同标识或邮箱已经存在")
	case strings.Contains(strings.ToLower(err.Error()), "foreign key"):
		writeError(w, http.StatusConflict, "resource_in_use", "该记录仍被其他内容使用")
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "服务器暂时没有完成请求")
}

func (s *Server) cors(next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(s.config.CORSOrigins))
	for _, origin := range s.config.CORSOrigins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := allowed[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered", "value", fmt.Sprint(recovered), "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "服务器暂时没有完成请求")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type limitEntry struct {
	count int
	reset time.Time
}

type windowLimiter struct {
	mu          sync.Mutex
	entries     map[string]limitEntry
	limit       int
	window      time.Duration
	lastCleanup time.Time
}

func newWindowLimiter(limit int, window time.Duration) *windowLimiter {
	return &windowLimiter{
		entries: make(map[string]limitEntry),
		limit:   limit,
		window:  window,
	}
}

func (l *windowLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= l.window {
		for existingKey, entry := range l.entries {
			if !entry.reset.After(now) {
				delete(l.entries, existingKey)
			}
		}
		l.lastCleanup = now
	}
	entry := l.entries[key]
	if !entry.reset.After(now) {
		entry = limitEntry{reset: now.Add(l.window)}
	}
	entry.count++
	l.entries[key] = entry
	return entry.count <= l.limit, entry.reset.Sub(now)
}

func (s *Server) limitGameRequests(limiter *windowLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := currentUser(r).ID
		if userID == "" {
			userID = clientHost(r)
		}
		gameKey := r.PathValue("slug")
		if claims, ok := gameClaims(r); ok && claims.GameID != "" {
			gameKey = claims.GameID
		}
		allowed, retryAfter := limiter.allow(userID+"|"+gameKey, time.Now())
		if !allowed {
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "游戏服务请求过于频繁，请稍后重试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type authLimiter struct {
	mu          sync.Mutex
	entries     map[string]limitEntry
	lastCleanup time.Time
}

func newAuthLimiter() *authLimiter { return &authLimiter{entries: map[string]limitEntry{}} }

func (l *authLimiter) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := clientHost(r)
		now := time.Now()
		l.mu.Lock()
		if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= time.Minute {
			for key, existing := range l.entries {
				if !existing.reset.After(now) {
					delete(l.entries, key)
				}
			}
			l.lastCleanup = now
		}
		entry := l.entries[host]
		if entry.reset.Before(now) {
			entry = limitEntry{reset: now.Add(10 * time.Minute)}
		}
		entry.count++
		l.entries[host] = entry
		l.mu.Unlock()
		if entry.count > 30 {
			w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(entry.reset).Seconds())))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "尝试次数过多，请稍后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientHost(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		candidate := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if net.ParseIP(candidate) != nil {
			return candidate
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

package security

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const issuer = "atri-games"

type Claims struct {
	// TokenType distinguishes the long-lived platform session from a
	// short-lived game-scoped ticket. Older platform tokens have an empty
	// value and remain valid through Parse; game endpoints require "game".
	TokenType string `json:"tokenType,omitempty"`
	// Kind is kept as a compact public claim for game runtimes. It mirrors
	// TokenType so clients that inspect the ticket can use either convention.
	Kind     string   `json:"kind,omitempty"`
	Role     string   `json:"role,omitempty"`
	UserID   string   `json:"userId,omitempty"`
	GameID   string   `json:"gameId,omitempty"`
	GameSlug string   `json:"gameSlug,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

func (m *TokenManager) Issue(userID, role string) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		TokenType: "user",
		Kind:      "user",
		Role:      role,
		UserID:    userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *TokenManager) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithIssuer(issuer), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.Subject == "" {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// IssueGameTicket creates a short-lived bearer credential scoped to one
// published game. The caller decides the TTL (the API uses a 15 minute
// default); keeping this separate from Issue prevents a platform session from
// accidentally gaining game API privileges.
func (m *TokenManager) IssueGameTicket(userID, gameID, gameSlug string, scopes []string, ttl time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(gameID) == "" || strings.TrimSpace(gameSlug) == "" {
		return "", time.Time{}, errors.New("game ticket subject and game are required")
	}
	if ttl <= 0 {
		return "", time.Time{}, errors.New("game ticket ttl must be positive")
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	normalizedScopes := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, exists := seen[scope]; exists {
			continue
		}
		seen[scope] = struct{}{}
		normalizedScopes = append(normalizedScopes, scope)
	}
	claims := Claims{
		TokenType: "game",
		Kind:      "game",
		UserID:    userID,
		GameID:    gameID,
		GameSlug:  gameSlug,
		Scopes:    normalizedScopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID,
			Audience:  jwt.ClaimStrings{"game:" + gameSlug, gameSlug},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	return raw, expires, err
}

// ParseGameTicket validates the token type, game audience and game identity.
// It intentionally does not accept a regular platform token.
func (m *TokenManager) ParseGameTicket(raw, gameID, gameSlug string) (*Claims, error) {
	claims, err := m.parse(raw)
	if err != nil {
		return nil, err
	}
	if (claims.TokenType != "game" && claims.Kind != "game") || claims.Subject == "" ||
		(claims.UserID != "" && claims.UserID != claims.Subject) ||
		claims.GameID != gameID || claims.GameSlug != gameSlug {
		return nil, errors.New("invalid game ticket")
	}
	expectedAudience := "game:" + gameSlug
	matched := false
	for _, audience := range claims.Audience {
		if audience == expectedAudience || audience == gameSlug {
			matched = true
			break
		}
	}
	if !matched {
		return nil, errors.New("invalid game ticket audience")
	}
	return claims, nil
}

func (m *TokenManager) parse(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithIssuer(issuer), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.Subject == "" {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

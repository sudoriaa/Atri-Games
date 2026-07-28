package security

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password was not hashed")
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password was rejected")
	}
	if CheckPassword(hash, "incorrect") {
		t.Fatal("incorrect password was accepted")
	}
	if _, err := HashPassword(strings.Repeat("x", 73)); err == nil {
		t.Fatal("bcrypt accepted a password beyond its 72-byte limit")
	}
}

func TestTokenManagerIssueAndParse(t *testing.T) {
	manager := NewTokenManager("a-test-secret", time.Hour)
	raw, err := manager.Issue("usr_test", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := manager.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != "usr_test" || claims.UserID != "usr_test" || claims.Role != "admin" || claims.Issuer != issuer {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		t.Fatalf("token timestamps are invalid: %+v", claims)
	}

	if _, err := NewTokenManager("different-secret", time.Hour).Parse(raw); err == nil {
		t.Fatal("token signed by a different secret was accepted")
	}
	if _, err := manager.Parse(raw + "tampered"); err == nil {
		t.Fatal("tampered token was accepted")
	}
}

func TestTokenManagerRejectsExpiredAndUnexpectedAlgorithm(t *testing.T) {
	expiredManager := NewTokenManager("a-test-secret", -time.Second)
	expired, err := expiredManager.Issue("usr_test", "user")
	if err != nil {
		t.Fatalf("Issue expired token: %v", err)
	}
	if _, err := expiredManager.Parse(expired); err == nil {
		t.Fatal("expired token was accepted")
	}

	claims := Claims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "usr_test",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	wrongAlgorithm, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte("a-test-secret"))
	if err != nil {
		t.Fatalf("sign wrong-algorithm token: %v", err)
	}
	if _, err := NewTokenManager("a-test-secret", time.Hour).Parse(wrongAlgorithm); err == nil {
		t.Fatal("token with an unexpected signing algorithm was accepted")
	}
}

func TestGameTicketIsScopedAndCannotBeUsedAsPlatformSession(t *testing.T) {
	manager := NewTokenManager("a-test-secret", time.Hour)
	raw, expires, err := manager.IssueGameTicket("usr_test", "game_test", "test-game", []string{"identity", "storage"}, 10*time.Minute)
	if err != nil {
		t.Fatalf("IssueGameTicket: %v", err)
	}
	if expires.Before(time.Now()) {
		t.Fatalf("game ticket expires in the past: %v", expires)
	}
	claims, err := manager.ParseGameTicket(raw, "game_test", "test-game")
	if err != nil {
		t.Fatalf("ParseGameTicket: %v", err)
	}
	if claims.Kind != "game" || claims.TokenType != "game" || claims.GameID != "game_test" || claims.GameSlug != "test-game" || len(claims.Scopes) != 2 {
		t.Fatalf("unexpected game claims: %+v", claims)
	}
	if _, err := manager.ParseGameTicket(raw, "other-game", "test-game"); err == nil {
		t.Fatal("game ticket was accepted for another game")
	}
	if _, err := manager.ParseGameTicket(raw, "game_test", "other-slug"); err == nil {
		t.Fatal("game ticket was accepted for another slug")
	}
	if parsed, err := manager.Parse(raw); err != nil || parsed.Kind != "game" {
		t.Fatalf("generic Parse should still verify signature for middleware: %v %+v", err, parsed)
	}
}

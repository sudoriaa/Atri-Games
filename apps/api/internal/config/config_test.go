package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"ATRI_ADDR",
		"ATRI_DB_PATH",
		"ATRI_ASSET_ROOT",
		"ATRI_AVATAR_MAX_BYTES",
		"ATRI_GAME_COVER_MAX_BYTES",
		"ATRI_GAME_PACKAGE_MAX_BYTES",
		"ATRI_GAME_PACKAGE_MAX_UNPACKED_BYTES",
		"ATRI_GAME_PACKAGE_MAX_FILES",
		"ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64",
		"ATRI_GAME_TICKET_TTL_SECONDS",
		"ATRI_JWT_SECRET",
		"ATRI_CORS_ORIGINS",
		"ATRI_ADMIN_EMAIL",
		"ATRI_ADMIN_PASSWORD",
		"ATRI_OBJECT_STORAGE_PROVIDER",
		"ATRI_OBJECT_STORAGE_ENDPOINT",
		"ATRI_OBJECT_STORAGE_ACCESS_KEY",
		"ATRI_OBJECT_STORAGE_SECRET_KEY",
		"ATRI_OBJECT_STORAGE_BUCKET",
		"ATRI_OBJECT_STORAGE_PREFIX",
		"ATRI_OBJECT_STORAGE_USE_SSL",
		"ATRI_OBJECT_STORAGE_REGION",
		"ATRI_OBJECT_STORAGE_SYNC_TIMEOUT_SECONDS",
		"ATRI_LOG_LEVEL",
	} {
		t.Setenv(key, "")
	}

	cfg := Load()
	if cfg.Address != ":8080" || cfg.DatabasePath != "./data/atri-games.db" || cfg.AssetRoot != "./assets" ||
		cfg.AvatarMaxBytes != 2*1024*1024 ||
		cfg.GameCoverMaxBytes != 10*1024*1024 ||
		cfg.GamePackageMaxBytes != 512*1024*1024 || cfg.GamePackageMaxUnpackedBytes != 2*1024*1024*1024 ||
		cfg.GamePackageMaxFiles != 20000 || cfg.GameTicketTTL != 15*time.Minute {
		t.Fatalf("unexpected default address/database: %+v", cfg)
	}
	if cfg.JWTSecret == "" || cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		t.Fatalf("missing authentication defaults: %+v", cfg)
	}
	if len(cfg.PackageDecryptionPrivateKey) != 0 || cfg.PackageDecryptionPrivateKeyError != "" {
		t.Fatalf("unexpected package decryption key defaults")
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("default log level = %v, want info", cfg.LogLevel)
	}
	if cfg.ObjectStorageProvider != "local" || cfg.ObjectStorageBucket != "atri-games" || cfg.ObjectStoragePrefix != "assets" || cfg.ObjectStorageSyncTimeout != 10*time.Minute {
		t.Fatalf("unexpected object storage defaults: %+v", cfg)
	}
	if want := []string{"http://localhost:5173", "http://localhost:5174"}; !reflect.DeepEqual(cfg.CORSOrigins, want) {
		t.Fatalf("default CORS origins = %#v, want %#v", cfg.CORSOrigins, want)
	}
}

func TestLoadEnvironmentOverrides(t *testing.T) {
	t.Setenv("ATRI_ADDR", " 127.0.0.1:9090 ")
	t.Setenv("ATRI_DB_PATH", " custom.db ")
	t.Setenv("ATRI_ASSET_ROOT", " /srv/atri/assets ")
	t.Setenv("ATRI_AVATAR_MAX_BYTES", "123456")
	t.Setenv("ATRI_GAME_COVER_MAX_BYTES", "234567")
	t.Setenv("ATRI_GAME_PACKAGE_MAX_BYTES", "123456")
	t.Setenv("ATRI_GAME_PACKAGE_MAX_UNPACKED_BYTES", "654321")
	t.Setenv("ATRI_GAME_PACKAGE_MAX_FILES", "42")
	t.Setenv("ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64", packageDecryptionKeyBase64(t))
	t.Setenv("ATRI_GAME_TICKET_TTL_SECONDS", "600")
	t.Setenv("ATRI_JWT_SECRET", " custom-secret ")
	t.Setenv("ATRI_CORS_ORIGINS", " https://one.example, ,https://two.example ")
	t.Setenv("ATRI_ADMIN_EMAIL", " ADMIN@EXAMPLE.TEST ")
	t.Setenv("ATRI_ADMIN_PASSWORD", " custom-password ")
	t.Setenv("ATRI_OBJECT_STORAGE_PROVIDER", " MINIO ")
	t.Setenv("ATRI_OBJECT_STORAGE_ENDPOINT", " minio.internal:9000 ")
	t.Setenv("ATRI_OBJECT_STORAGE_ACCESS_KEY", " access-key ")
	t.Setenv("ATRI_OBJECT_STORAGE_SECRET_KEY", " secret-key ")
	t.Setenv("ATRI_OBJECT_STORAGE_BUCKET", " atri-assets ")
	t.Setenv("ATRI_OBJECT_STORAGE_PREFIX", " /game-assets/ ")
	t.Setenv("ATRI_OBJECT_STORAGE_USE_SSL", "true")
	t.Setenv("ATRI_OBJECT_STORAGE_REGION", " cn-east-1 ")
	t.Setenv("ATRI_OBJECT_STORAGE_SYNC_TIMEOUT_SECONDS", "123")
	t.Setenv("ATRI_LOG_LEVEL", "DEBUG")

	cfg := Load()
	if cfg.Address != "127.0.0.1:9090" || cfg.DatabasePath != "custom.db" || cfg.AssetRoot != "/srv/atri/assets" || cfg.JWTSecret != "custom-secret" ||
		cfg.AvatarMaxBytes != 123456 ||
		cfg.GameCoverMaxBytes != 234567 ||
		cfg.GamePackageMaxBytes != 123456 || cfg.GamePackageMaxUnpackedBytes != 654321 || cfg.GamePackageMaxFiles != 42 ||
		cfg.GameTicketTTL != 10*time.Minute {
		t.Fatalf("environment overrides were not trimmed: %+v", cfg)
	}
	if cfg.AdminEmail != "admin@example.test" || cfg.AdminPassword != "custom-password" {
		t.Fatalf("admin overrides = %q/%q", cfg.AdminEmail, cfg.AdminPassword)
	}
	if len(cfg.PackageDecryptionPrivateKey) == 0 || cfg.PackageDecryptionPrivateKeyError != "" {
		t.Fatalf("package decryption key was not loaded")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("log level = %v, want debug", cfg.LogLevel)
	}
	if cfg.ObjectStorageProvider != "minio" || cfg.ObjectStorageEndpoint != "minio.internal:9000" ||
		cfg.ObjectStorageAccessKey != "access-key" || cfg.ObjectStorageSecretKey != "secret-key" ||
		cfg.ObjectStorageBucket != "atri-assets" || cfg.ObjectStoragePrefix != "game-assets" ||
		!cfg.ObjectStorageUseSSL || cfg.ObjectStorageRegion != "cn-east-1" || cfg.ObjectStorageSyncTimeout != 123*time.Second {
		t.Fatalf("object storage overrides were not normalized: %+v", cfg)
	}
	if err := cfg.ValidateObjectStorage(); err != nil {
		t.Fatalf("ValidateObjectStorage: %v", err)
	}
	if want := []string{"https://one.example", "https://two.example"}; !reflect.DeepEqual(cfg.CORSOrigins, want) {
		t.Fatalf("CORS origins = %#v, want %#v", cfg.CORSOrigins, want)
	}
}

func TestLoadDefersInvalidPackageDecryptionKeyToEncryptedImport(t *testing.T) {
	t.Run("invalid Base64", func(t *testing.T) {
		t.Setenv("ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64", "not base64")
		cfg := Load()
		if len(cfg.PackageDecryptionPrivateKey) != 0 {
			t.Fatal("invalid package decryption key was retained")
		}
		if !strings.Contains(cfg.PackageDecryptionPrivateKeyError, "Base64-encoded PEM") {
			t.Fatalf("package decryption key error = %q", cfg.PackageDecryptionPrivateKeyError)
		}
	})

	t.Run("non-PEM Base64", func(t *testing.T) {
		t.Setenv("ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("not a PEM key")))
		cfg := Load()
		if len(cfg.PackageDecryptionPrivateKey) != 0 {
			t.Fatal("non-PEM package decryption key was retained")
		}
		if !strings.Contains(cfg.PackageDecryptionPrivateKeyError, "exactly one PEM") {
			t.Fatalf("package decryption key error = %q", cfg.PackageDecryptionPrivateKeyError)
		}
	})
}

func packageDecryptionKeyBase64(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal RSA key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

package config

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address                     string
	DatabasePath                string
	AssetRoot                   string
	GameCoverMaxBytes           int64
	GamePackageMaxBytes         int64
	GamePackageMaxUnpackedBytes int64
	GamePackageMaxFiles         int
	// PackageDecryptionPrivateKey contains the PEM bytes decoded from
	// ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64. It is intentionally kept out
	// of logs and is only passed to the encrypted-package extractor at import
	// time. An absent key leaves ordinary ZIP-based .atri packages unchanged.
	PackageDecryptionPrivateKey      []byte
	PackageDecryptionPrivateKeyError string
	GameTicketTTL                    time.Duration
	JWTSecret                        string
	CORSOrigins                      []string
	AdminEmail                       string
	AdminPassword                    string
	ObjectStorageProvider            string
	ObjectStorageEndpoint            string
	ObjectStorageAccessKey           string
	ObjectStorageSecretKey           string
	ObjectStorageBucket              string
	ObjectStoragePrefix              string
	ObjectStorageUseSSL              bool
	ObjectStorageRegion              string
	ObjectStorageSyncTimeout         time.Duration
	LogLevel                         slog.Level
}

func Load() Config {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("ATRI_LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	packageDecryptionPrivateKey, packageDecryptionPrivateKeyError := packageDecryptionKeyFromEnv(os.Getenv("ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64"))

	return Config{
		Address:                          envOr("ATRI_ADDR", ":8080"),
		DatabasePath:                     envOr("ATRI_DB_PATH", "./data/atri-games.db"),
		AssetRoot:                        envOr("ATRI_ASSET_ROOT", "./assets"),
		GameCoverMaxBytes:                envInt64("ATRI_GAME_COVER_MAX_BYTES", 10*1024*1024),
		GamePackageMaxBytes:              envInt64("ATRI_GAME_PACKAGE_MAX_BYTES", 512*1024*1024),
		GamePackageMaxUnpackedBytes:      envInt64("ATRI_GAME_PACKAGE_MAX_UNPACKED_BYTES", 2*1024*1024*1024),
		GamePackageMaxFiles:              envInt("ATRI_GAME_PACKAGE_MAX_FILES", 20000),
		PackageDecryptionPrivateKey:      packageDecryptionPrivateKey,
		PackageDecryptionPrivateKeyError: packageDecryptionPrivateKeyError,
		GameTicketTTL:                    time.Duration(envInt("ATRI_GAME_TICKET_TTL_SECONDS", 15*60)) * time.Second,
		JWTSecret:                        envOr("ATRI_JWT_SECRET", "atri-local-development-secret-change-me"),
		CORSOrigins:                      splitCSV(envOr("ATRI_CORS_ORIGINS", "http://localhost:5173,http://localhost:5174")),
		AdminEmail:                       strings.ToLower(envOr("ATRI_ADMIN_EMAIL", "admin@atri.games")),
		AdminPassword:                    envOr("ATRI_ADMIN_PASSWORD", "AtriAdmin123!"),
		ObjectStorageProvider:            strings.ToLower(envOr("ATRI_OBJECT_STORAGE_PROVIDER", "local")),
		ObjectStorageEndpoint:            envOr("ATRI_OBJECT_STORAGE_ENDPOINT", ""),
		ObjectStorageAccessKey:           envOr("ATRI_OBJECT_STORAGE_ACCESS_KEY", ""),
		ObjectStorageSecretKey:           envOr("ATRI_OBJECT_STORAGE_SECRET_KEY", ""),
		ObjectStorageBucket:              envOr("ATRI_OBJECT_STORAGE_BUCKET", "atri-games"),
		ObjectStoragePrefix:              strings.Trim(envOr("ATRI_OBJECT_STORAGE_PREFIX", "assets"), "/"),
		ObjectStorageUseSSL:              envBool("ATRI_OBJECT_STORAGE_USE_SSL", false),
		ObjectStorageRegion:              envOr("ATRI_OBJECT_STORAGE_REGION", "us-east-1"),
		ObjectStorageSyncTimeout:         time.Duration(envInt("ATRI_OBJECT_STORAGE_SYNC_TIMEOUT_SECONDS", 600)) * time.Second,
		LogLevel:                         level,
	}
}

// packageDecryptionKeyFromEnv parses the optional RSA private key without
// failing process startup. This is deliberate: legacy ZIP .atri packages do
// not require a key, while an encrypted import can surface a precise, local
// validation error to the administrator.
func packageDecryptionKeyFromEnv(raw string) ([]byte, string) {
	encoded := strings.TrimSpace(raw)
	if encoded == "" {
		return nil, ""
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64 must be Base64-encoded PEM"
	}
	block, rest := pem.Decode(decoded)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, "ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64 must contain exactly one PEM private key"
	}
	privateKey, err := parseRSAPrivateKeyPEMBlock(block)
	if err != nil {
		return nil, "ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64 must contain an unencrypted RSA private-key PEM"
	}
	if err := privateKey.Validate(); err != nil {
		return nil, "ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64 must contain an unencrypted RSA private-key PEM"
	}
	return decoded, ""
}

func parseRSAPrivateKeyPEMBlock(block *pem.Block) (*rsa.PrivateKey, error) {
	if block == nil {
		return nil, fmt.Errorf("missing PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		value, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key, ok := value.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PEM key is not RSA")
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

// ValidateObjectStorage keeps the optional object-storage configuration
// explicit. Local development can leave the provider at "local"; a MinIO
// deployment must supply all connection details before the API starts.
func (c Config) ValidateObjectStorage() error {
	switch c.ObjectStorageProvider {
	case "", "local":
		return nil
	case "minio":
		if strings.TrimSpace(c.ObjectStorageEndpoint) == "" ||
			strings.TrimSpace(c.ObjectStorageAccessKey) == "" ||
			strings.TrimSpace(c.ObjectStorageSecretKey) == "" ||
			strings.TrimSpace(c.ObjectStorageBucket) == "" {
			return fmt.Errorf("MinIO object storage requires endpoint, access key, secret key, and bucket")
		}
		if strings.Contains(c.ObjectStorageBucket, "/") || strings.Contains(c.ObjectStoragePrefix, "\\") {
			return fmt.Errorf("MinIO bucket or prefix is invalid")
		}
		return nil
	default:
		return fmt.Errorf("unsupported ATRI_OBJECT_STORAGE_PROVIDER %q", c.ObjectStorageProvider)
	}
}

func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if clean := strings.TrimSpace(part); clean != "" {
			result = append(result, clean)
		}
	}
	return result
}

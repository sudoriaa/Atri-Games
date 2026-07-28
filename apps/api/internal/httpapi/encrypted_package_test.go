package httpapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/sudoriaa/atri-games/apps/api/internal/config"
	"github.com/sudoriaa/atri-games/apps/api/internal/gamepkg"
)

func TestLegacyPackageImportIgnoresInvalidOptionalDecryptionConfiguration(t *testing.T) {
	api := newTestAPIWithConfig(t, func(cfg *config.Config) {
		cfg.PackageDecryptionPrivateKeyError = "ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64 must be Base64-encoded PEM"
	})
	admin := loginAdmin(t, api)

	response := api.importPackage(t, plainPackageArchive(t, "legacy-zip-fixture"), "arcade", "published", false, admin.Token)
	requireStatus(t, response, http.StatusCreated)
}

func TestEncryptedPackageImportReportsMissingOrInvalidKeyConfiguration(t *testing.T) {
	privateKey := encryptedPackageTestPrivateKey(t)
	archive := encryptAtriPackage(t, plainPackageArchive(t, "encrypted-key-error"), &privateKey.PublicKey)

	t.Run("missing key", func(t *testing.T) {
		api := newTestAPI(t)
		admin := loginAdmin(t, api)
		response := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
		requireError(t, response, http.StatusUnprocessableEntity, "package_decryption_key_required")
	})

	t.Run("invalid environment configuration", func(t *testing.T) {
		api := newTestAPIWithConfig(t, func(cfg *config.Config) {
			cfg.PackageDecryptionPrivateKeyError = "ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64 must be Base64-encoded PEM"
		})
		admin := loginAdmin(t, api)
		response := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
		requireError(t, response, http.StatusUnprocessableEntity, "package_decryption_key_configuration_invalid")
		payload := decodeResponse[errorResponse](t, response)
		if !strings.Contains(payload.Error.Message, "ATRI_PACKAGE_DECRYPTION_PRIVATE_KEY_BASE64") {
			t.Fatalf("configuration error did not identify the environment variable: %q", payload.Error.Message)
		}
	})
}

func TestEncryptedPackageImportUsesConfiguredPrivateKey(t *testing.T) {
	privateKey := encryptedPackageTestPrivateKey(t)
	archive := encryptAtriPackage(t, plainPackageArchive(t, "encrypted-package-fixture"), &privateKey.PublicKey)

	t.Run("matching key imports", func(t *testing.T) {
		api := newTestAPIWithConfig(t, func(cfg *config.Config) {
			cfg.PackageDecryptionPrivateKey = rsaPrivateKeyPEM(t, privateKey)
		})
		admin := loginAdmin(t, api)
		response := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
		requireStatus(t, response, http.StatusCreated)
	})

	t.Run("other valid key fails authenticated decryption", func(t *testing.T) {
		wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate wrong RSA key: %v", err)
		}
		api := newTestAPIWithConfig(t, func(cfg *config.Config) {
			cfg.PackageDecryptionPrivateKey = rsaPrivateKeyPEM(t, wrongKey)
		})
		admin := loginAdmin(t, api)
		response := api.importPackage(t, archive, "arcade", "published", false, admin.Token)
		requireError(t, response, http.StatusUnprocessableEntity, "invalid_encrypted_package")
	})

	t.Run("container overhead does not count against decrypted ZIP limit", func(t *testing.T) {
		plainArchive := plainPackageArchive(t, "encrypted-limit-fixture")
		encryptedArchive := encryptAtriPackage(t, plainArchive, &privateKey.PublicKey)
		if len(encryptedArchive) <= len(plainArchive) {
			t.Fatalf("encrypted package did not add container overhead: %d <= %d", len(encryptedArchive), len(plainArchive))
		}
		api := newTestAPIWithConfig(t, func(cfg *config.Config) {
			defaults := gamepkg.DefaultLimits()
			cfg.PackageDecryptionPrivateKey = rsaPrivateKeyPEM(t, privateKey)
			cfg.GamePackageMaxBytes = int64(len(plainArchive))
			cfg.GamePackageMaxUnpackedBytes = defaults.MaxUnpackedBytes
			cfg.GamePackageMaxFiles = defaults.MaxFiles
		})
		admin := loginAdmin(t, api)
		response := api.importPackage(t, encryptedArchive, "arcade", "published", false, admin.Token)
		requireStatus(t, response, http.StatusCreated)
	})
}

func plainPackageArchive(t *testing.T, id string) []byte {
	t.Helper()
	manifest := fmt.Sprintf(`{
		"schemaVersion":2,
		"id":%q,
		"version":"1.0.0",
		"title":"Encrypted Package Fixture",
		"summary":"A complete fixture that validates encrypted package imports.",
		"description":"This static fixture exists to exercise authenticated package decryption through the admin API.",
		"authors":[{"name":"Atri Test"}],
		"license":"MIT",
		"engine":{"name":"HTML"},
		"runtime":{"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"},
		"services":{"networkRequired":false,"ownBackend":false},
		"privacy":{"collectsPersonalData":false,"dataSummary":"This fixture collects no personal data from players."},
		"media":{"cover":"cover.webp"},
		"compatibility":{"devices":["desktop"],"inputs":["keyboard"],"orientation":"any"},
		"tags":["encrypted","fixture"]
	}`, id)
	return buildGamePackage(t, map[string]string{
		"atri-game.json":  manifest,
		"cover.webp":      "cover fixture",
		"game/index.html": "<!doctype html><title>Encrypted package fixture</title>",
	})
}

func encryptAtriPackage(t *testing.T, archive []byte, publicKey *rsa.PublicKey) []byte {
	t.Helper()
	contentKey := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err := rand.Read(contentKey); err != nil {
		t.Fatalf("generate content key: %v", err)
	}
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("generate nonce: %v", err)
	}
	wrapped, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, contentKey, nil)
	if err != nil {
		t.Fatalf("wrap content key: %v", err)
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		t.Fatalf("create AES cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("create GCM: %v", err)
	}

	const (
		headerLength = 8 + 1 + 4 + 1 + 1 + 4
		chunkSize    = 1024 * 1024
	)
	header := make([]byte, headerLength, headerLength+len(wrapped)+len(nonce))
	copy(header, "ATRIENC1")
	header[8] = 1
	binary.BigEndian.PutUint32(header[9:13], uint32(len(wrapped)))
	header[13] = byte(len(nonce))
	header[14] = byte(gcm.Overhead())
	binary.BigEndian.PutUint32(header[15:19], chunkSize)
	header = append(header, wrapped...)
	header = append(header, nonce...)

	result := append([]byte(nil), header...)
	chunkIndex := uint64(0)
	for offset := 0; offset < len(archive); {
		end := offset + chunkSize
		if end > len(archive) {
			end = len(archive)
		}
		sealed := gcm.Seal(nil, encryptedPackageChunkNonce(nonce, chunkIndex), archive[offset:end], encryptedPackageAAD(header, chunkIndex))
		result = appendEncryptedPackageFrame(result, sealed)
		offset = end
		chunkIndex++
	}
	// The terminal authenticated frame detects truncation at a chunk boundary.
	terminal := gcm.Seal(nil, encryptedPackageChunkNonce(nonce, chunkIndex), nil, encryptedPackageAAD(header, chunkIndex))
	return appendEncryptedPackageFrame(result, terminal)
}

func appendEncryptedPackageFrame(target, frame []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(frame)))
	target = append(target, length[:]...)
	return append(target, frame...)
}

func encryptedPackageChunkNonce(base []byte, index uint64) []byte {
	nonce := append([]byte(nil), base...)
	binary.BigEndian.PutUint64(nonce[4:], index)
	return nonce
}

func encryptedPackageAAD(header []byte, index uint64) []byte {
	aad := make([]byte, len(header)+8)
	copy(aad, header)
	binary.BigEndian.PutUint64(aad[len(header):], index)
	return aad
}

func rsaPrivateKeyPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal RSA private key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

var encryptedPackageKeyCache struct {
	sync.Once
	key *rsa.PrivateKey
	err error
}

func encryptedPackageTestPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	encryptedPackageKeyCache.Do(func() {
		encryptedPackageKeyCache.key, encryptedPackageKeyCache.err = rsa.GenerateKey(rand.Reader, 2048)
	})
	if encryptedPackageKeyCache.err != nil {
		t.Fatalf("generate encrypted package RSA key: %v", encryptedPackageKeyCache.err)
	}
	return encryptedPackageKeyCache.key
}

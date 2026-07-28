package gamepkg

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractWithPrivateKeyDecryptsATRIENC1PKCS1AndPKCS8(t *testing.T) {
	largeAsset := make([]byte, encryptedPackageChunkSize+257)
	if _, err := cryptorand.Read(largeAsset); err != nil {
		t.Fatalf("random fixture: %v", err)
	}
	zipPath := writePackage(t, map[string]string{
		"atri-game.json":  validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`),
		"cover.webp":      "cover",
		"game/index.html": "<!doctype html><script src='./main.js'></script>",
		"game/main.js":    "console.log('ok')",
		"game/large.bin":  string(largeAsset),
	})
	zipPayload, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read fixture ZIP: %v", err)
	}
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	keyEncodings := map[string][]byte{
		"pkcs1": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}),
		"pkcs8": mustPKCS8PEM(t, privateKey),
	}
	for name, privateKeyPEM := range keyEncodings {
		t.Run(name, func(t *testing.T) {
			encryptedPath := filepath.Join(t.TempDir(), "fixture.atri")
			if err := os.WriteFile(encryptedPath, encryptFixtureContainer(t, zipPayload, &privateKey.PublicKey, nil, true), 0o600); err != nil {
				t.Fatalf("write encrypted package: %v", err)
			}
			assetRoot := t.TempDir()
			prepared, err := ExtractWithPrivateKey(encryptedPath, assetRoot, DefaultLimits(), privateKeyPEM)
			if err != nil {
				t.Fatalf("ExtractWithPrivateKey: %v", err)
			}
			t.Cleanup(func() { _ = prepared.Cleanup() })
			if prepared.Manifest.ID != "fixture-game" || prepared.ArchivePath != encryptedPath {
				t.Fatalf("prepared encrypted package = %+v", prepared)
			}
			if _, err := os.Stat(filepath.Join(prepared.BundlePath, "large.bin")); err != nil {
				t.Fatalf("streamed package asset is missing: %v", err)
			}
			assertNoDecryptedArchives(t, assetRoot)
		})
	}
}

func TestExtractWithPrivateKeyLeavesLegacyZIPCompatibilityUntouched(t *testing.T) {
	zipPath := writePackage(t, map[string]string{
		"atri-game.json":  validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`),
		"cover.webp":      "cover",
		"game/index.html": "<!doctype html><title>Legacy ZIP</title>",
	})
	prepared, err := ExtractWithPrivateKey(zipPath, t.TempDir(), DefaultLimits(), []byte("not a PEM private key"))
	if err != nil {
		t.Fatalf("legacy ZIP import failed because of an unused invalid key: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Cleanup() })
}

func TestExtractWithPrivateKeyRequiresATRIENC1KeyAndRemovesTemporaryZIP(t *testing.T) {
	zipPath := writePackage(t, map[string]string{
		"atri-game.json":  validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`),
		"cover.webp":      "cover",
		"game/index.html": "<!doctype html><title>Encrypted Fixture</title>",
	})
	zipPayload, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read fixture ZIP: %v", err)
	}
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	encryptedPath := filepath.Join(t.TempDir(), "fixture.atri")
	if err := os.WriteFile(encryptedPath, encryptFixtureContainer(t, zipPayload, &privateKey.PublicKey, nil, true), 0o600); err != nil {
		t.Fatalf("write encrypted package: %v", err)
	}

	assetRoot := t.TempDir()
	if _, err := Extract(encryptedPath, assetRoot, DefaultLimits()); !errors.Is(err, ErrEncryptedPackagePrivateKeyRequired) {
		t.Fatalf("Extract without a private key error = %v, want ErrEncryptedPackagePrivateKeyRequired", err)
	}
	if _, err := ExtractWithPrivateKey(encryptedPath, assetRoot, DefaultLimits(), []byte("invalid PEM")); !errors.Is(err, ErrEncryptedPackagePrivateKeyInvalid) {
		t.Fatalf("invalid private key error = %v, want ErrEncryptedPackagePrivateKeyInvalid", err)
	}
	assertNoDecryptedArchives(t, assetRoot)

	privateKeyPEM := mustPKCS8PEM(t, privateKey)
	temporaryPath, cleanup, err := prepareArchiveForExtraction(encryptedPath, assetRoot, DefaultLimits(), privateKeyPEM)
	if err != nil {
		t.Fatalf("prepareArchiveForExtraction: %v", err)
	}
	info, err := os.Stat(temporaryPath)
	if err != nil {
		t.Fatalf("stat decrypted ZIP: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("decrypted ZIP permissions = %o, want owner-only", info.Mode().Perm())
	}
	if err := cleanup(); err != nil {
		t.Fatalf("remove decrypted ZIP: %v", err)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("decrypted ZIP still exists after cleanup: %v", err)
	}
}

func TestExtractWithPrivateKeyRejectsTamperedOrMalformedATRIENC1(t *testing.T) {
	zipPath := writePackage(t, map[string]string{
		"atri-game.json":  validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`),
		"cover.webp":      "cover",
		"game/index.html": "<!doctype html><title>Encrypted Fixture</title>",
	})
	zipPayload, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read fixture ZIP: %v", err)
	}
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	privateKeyPEM := mustPKCS8PEM(t, privateKey)
	valid := encryptFixtureContainer(t, zipPayload, &privateKey.PublicKey, nil, true)

	cases := map[string][]byte{
		"authenticated prefix": tamperFixturePrefix(t, valid),
		"authenticated frame":  tamperFixtureFrame(t, valid),
		"missing terminal":     valid[:len(valid)-(4+encryptedPackageTagSize)],
		"trailing bytes":       append(append([]byte(nil), valid...), 0x01),
		"short before data": encryptFixtureContainer(t, zipPayload, &privateKey.PublicKey, [][]byte{
			[]byte("short"), []byte("later"),
		}, true),
	}
	for name, container := range cases {
		t.Run(name, func(t *testing.T) {
			encryptedPath := filepath.Join(t.TempDir(), "fixture.atri")
			if err := os.WriteFile(encryptedPath, container, 0o600); err != nil {
				t.Fatalf("write encrypted package: %v", err)
			}
			assetRoot := t.TempDir()
			if _, err := ExtractWithPrivateKey(encryptedPath, assetRoot, DefaultLimits(), privateKeyPEM); !errors.Is(err, ErrInvalidEncryptedPackage) {
				t.Fatalf("ExtractWithPrivateKey error = %v, want ErrInvalidEncryptedPackage", err)
			}
			assertNoDecryptedArchives(t, assetRoot)
		})
	}
}

func mustPKCS8PEM(t *testing.T, privateKey *rsa.PrivateKey) []byte {
	t.Helper()
	raw, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal PKCS#8 private key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw})
}

// encryptFixtureContainer intentionally constructs ATRIENC1 independently of
// the production parser so the tests exercise its framing and authentication
// contract. Passing chunks overrides the normal 1 MiB ZIP chunking for tests
// that assert strict malformed-sequence rejection.
func encryptFixtureContainer(t *testing.T, payload []byte, publicKey *rsa.PublicKey, chunks [][]byte, includeTerminal bool) []byte {
	t.Helper()
	contentKey := make([]byte, 32)
	baseNonce := make([]byte, encryptedPackageNonceSize)
	if _, err := cryptorand.Read(contentKey); err != nil {
		t.Fatalf("generate AES key: %v", err)
	}
	if _, err := cryptorand.Read(baseNonce); err != nil {
		t.Fatalf("generate base nonce: %v", err)
	}
	wrappedKey, err := rsa.EncryptOAEP(sha256.New(), cryptorand.Reader, publicKey, contentKey, nil)
	if err != nil {
		t.Fatalf("wrap AES key: %v", err)
	}
	prefix := make([]byte, encryptedPackageFixedHeaderLen+len(wrappedKey)+len(baseNonce))
	copy(prefix, []byte(encryptedPackageMagic))
	prefix[len(encryptedPackageMagic)] = encryptedPackageVersion
	binary.BigEndian.PutUint32(prefix[len(encryptedPackageMagic)+1:], uint32(len(wrappedKey)))
	prefix[len(encryptedPackageMagic)+5] = encryptedPackageNonceSize
	prefix[len(encryptedPackageMagic)+6] = encryptedPackageTagSize
	binary.BigEndian.PutUint32(prefix[len(encryptedPackageMagic)+7:], encryptedPackageChunkSize)
	copy(prefix[encryptedPackageFixedHeaderLen:], wrappedKey)
	copy(prefix[encryptedPackageFixedHeaderLen+len(wrappedKey):], baseNonce)

	block, err := aes.NewCipher(contentKey)
	if err != nil {
		t.Fatalf("new AES cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new GCM cipher: %v", err)
	}
	if chunks == nil {
		for offset := 0; offset < len(payload); offset += encryptedPackageChunkSize {
			end := offset + encryptedPackageChunkSize
			if end > len(payload) {
				end = len(payload)
			}
			chunks = append(chunks, payload[offset:end])
		}
	}
	container := append([]byte(nil), prefix...)
	for index, chunk := range chunks {
		container = appendEncryptedFixtureFrame(t, container, gcm, baseNonce, prefix, uint64(index), chunk)
	}
	if includeTerminal {
		container = appendEncryptedFixtureFrame(t, container, gcm, baseNonce, prefix, uint64(len(chunks)), nil)
	}
	return container
}

func appendEncryptedFixtureFrame(t *testing.T, container []byte, gcm cipher.AEAD, baseNonce, prefix []byte, index uint64, plaintext []byte) []byte {
	t.Helper()
	nonce := make([]byte, encryptedPackageNonceSize)
	copy(nonce, baseNonce[:4])
	binary.BigEndian.PutUint64(nonce[4:], index)
	aad := make([]byte, len(prefix)+8)
	copy(aad, prefix)
	binary.BigEndian.PutUint64(aad[len(prefix):], index)
	sealed := gcm.Seal(nil, nonce, plaintext, aad)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(sealed)))
	container = append(container, length[:]...)
	return append(container, sealed...)
}

func tamperFixturePrefix(t *testing.T, value []byte) []byte {
	t.Helper()
	tampered := append([]byte(nil), value...)
	wrappedKeyLength := int(binary.BigEndian.Uint32(tampered[len(encryptedPackageMagic)+1:]))
	prefixLength := encryptedPackageFixedHeaderLen + wrappedKeyLength + int(tampered[len(encryptedPackageMagic)+5])
	tampered[prefixLength-1] ^= 1
	return tampered
}

func tamperFixtureFrame(t *testing.T, value []byte) []byte {
	t.Helper()
	tampered := append([]byte(nil), value...)
	tampered[len(tampered)-1] ^= 1
	return tampered
}

func assertNoDecryptedArchives(t *testing.T, assetRoot string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(assetRoot, ".atri-imports", ".atri-decrypted-*.zip"))
	if err != nil {
		t.Fatalf("list decrypted temporary ZIPs: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("decrypted temporary ZIPs remain: %v", paths)
	}
}

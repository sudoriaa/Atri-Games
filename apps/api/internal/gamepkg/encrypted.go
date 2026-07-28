package gamepkg

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	encryptedPackageMagic               = "ATRIENC1"
	encryptedPackageVersion        byte = 1
	encryptedPackageNonceSize           = 12
	encryptedPackageTagSize             = 16
	encryptedPackageChunkSize           = 1024 * 1024
	encryptedPackageFixedHeaderLen      = len(encryptedPackageMagic) + 1 + 4 + 1 + 1 + 4
	maxWrappedKeyBytes                  = 16 * 1024
)

var (
	// ErrEncryptedPackagePrivateKeyRequired means an ATRIENC1 package was
	// supplied without the server-side RSA key needed to unwrap it.
	ErrEncryptedPackagePrivateKeyRequired = errors.New("encrypted package private key is required")
	// ErrEncryptedPackagePrivateKeyInvalid means the configured PEM is not a
	// usable PKCS#1 or PKCS#8 RSA private key. It is only returned after an
	// ATRIENC1 magic value has been detected, so legacy ZIP imports are never
	// affected by a misconfigured optional key.
	ErrEncryptedPackagePrivateKeyInvalid = errors.New("encrypted package private key is invalid")
	// ErrInvalidEncryptedPackage covers malformed ATRIENC1 framing, a failed
	// OAEP unwrap, or failed AES-GCM authentication.
	ErrInvalidEncryptedPackage = errors.New("invalid encrypted package")
	// ErrEncryptedPackageUnsupported covers a recognized container whose
	// version or fixed cryptographic parameters are not supported by this API.
	ErrEncryptedPackageUnsupported = errors.New("unsupported encrypted package")
)

type encryptedPackageHeader struct {
	raw        []byte
	wrappedKey []byte
	baseNonce  []byte
	chunkSize  int
	tagSize    int
}

// prepareArchiveForExtraction leaves ordinary ZIP files untouched. For an
// ATRIENC1 container it verifies and decrypts the framed ZIP into a 0600
// temporary file under the private import workspace. The caller must invoke
// the returned cleanup function after ZIP validation/extraction completes.
func prepareArchiveForExtraction(archivePath, assetRoot string, limits Limits, privateKeyPEM []byte) (string, func() error, error) {
	encrypted, err := isEncryptedPackage(archivePath)
	if err != nil {
		return "", nil, err
	}
	if !encrypted {
		return archivePath, func() error { return nil }, nil
	}
	return decryptEncryptedPackage(archivePath, assetRoot, limits, privateKeyPEM)
}

func isEncryptedPackage(archivePath string) (bool, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return false, fmt.Errorf("inspect package: %w", err)
	}
	defer file.Close()

	magic := make([]byte, len(encryptedPackageMagic))
	if _, err := io.ReadFull(file, magic); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, fmt.Errorf("inspect package: %w", err)
	}
	return bytes.Equal(magic, []byte(encryptedPackageMagic)), nil
}

func decryptEncryptedPackage(archivePath, assetRoot string, limits Limits, privateKeyPEM []byte) (string, func() error, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return "", nil, fmt.Errorf("inspect package: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("inspect package: encrypted package must be a regular file")
	}
	if info.Size() < int64(encryptedPackageFixedHeaderLen) {
		return "", nil, fmt.Errorf("%w: header is truncated", ErrInvalidEncryptedPackage)
	}
	if info.Size() > maxEncryptedPackageBytes(limits.MaxArchiveBytes, maxWrappedKeyBytes) {
		return "", nil, fmt.Errorf("package exceeds %d byte limit", limits.MaxArchiveBytes)
	}

	source, err := os.Open(archivePath)
	if err != nil {
		return "", nil, fmt.Errorf("open encrypted package: %w", err)
	}
	defer source.Close()

	header, err := readEncryptedPackageHeader(source, info.Size())
	if err != nil {
		return "", nil, err
	}
	if info.Size() > maxEncryptedPackageBytes(limits.MaxArchiveBytes, len(header.wrappedKey)) {
		return "", nil, fmt.Errorf("package exceeds %d byte limit", limits.MaxArchiveBytes)
	}
	privateKey, err := parseEncryptedPackagePrivateKey(privateKeyPEM)
	if err != nil {
		return "", nil, err
	}
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, header.wrappedKey, nil)
	if err != nil || len(aesKey) != 32 {
		return "", nil, fmt.Errorf("%w: cannot unwrap AES key", ErrInvalidEncryptedPackage)
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", nil, fmt.Errorf("%w: cannot initialize AES cipher", ErrInvalidEncryptedPackage)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || gcm.NonceSize() != encryptedPackageNonceSize || gcm.Overhead() != header.tagSize {
		return "", nil, fmt.Errorf("%w: unsupported AES-GCM parameters", ErrEncryptedPackageUnsupported)
	}

	temporary, temporaryPath, cleanup, err := createDecryptedArchive(assetRoot)
	if err != nil {
		return "", nil, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = temporary.Close()
			_ = cleanup()
		}
	}()

	if err := decryptPackageFrames(source, temporary, gcm, header, limits.MaxArchiveBytes); err != nil {
		return "", nil, err
	}
	if err := temporary.Sync(); err != nil {
		return "", nil, fmt.Errorf("sync decrypted package: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", nil, fmt.Errorf("close decrypted package: %w", err)
	}
	succeeded = true
	return temporaryPath, cleanup, nil
}

func readEncryptedPackageHeader(source io.Reader, packageSize int64) (encryptedPackageHeader, error) {
	fixed := make([]byte, encryptedPackageFixedHeaderLen)
	if _, err := io.ReadFull(source, fixed); err != nil {
		return encryptedPackageHeader{}, fmt.Errorf("%w: header is truncated", ErrInvalidEncryptedPackage)
	}
	if !bytes.Equal(fixed[:len(encryptedPackageMagic)], []byte(encryptedPackageMagic)) {
		return encryptedPackageHeader{}, fmt.Errorf("%w: invalid magic", ErrInvalidEncryptedPackage)
	}
	if fixed[len(encryptedPackageMagic)] != encryptedPackageVersion {
		return encryptedPackageHeader{}, fmt.Errorf("%w: version %d", ErrEncryptedPackageUnsupported, fixed[len(encryptedPackageMagic)])
	}
	wrappedKeyLength := int(binary.BigEndian.Uint32(fixed[len(encryptedPackageMagic)+1 : len(encryptedPackageMagic)+5]))
	nonceLength := int(fixed[len(encryptedPackageMagic)+5])
	tagLength := int(fixed[len(encryptedPackageMagic)+6])
	chunkSize := int(binary.BigEndian.Uint32(fixed[len(encryptedPackageMagic)+7 : encryptedPackageFixedHeaderLen]))
	if wrappedKeyLength <= 0 || wrappedKeyLength > maxWrappedKeyBytes {
		return encryptedPackageHeader{}, fmt.Errorf("%w: wrapped key length is invalid", ErrInvalidEncryptedPackage)
	}
	if nonceLength != encryptedPackageNonceSize || tagLength != encryptedPackageTagSize || chunkSize != encryptedPackageChunkSize {
		return encryptedPackageHeader{}, fmt.Errorf("%w: unsupported encryption parameters", ErrEncryptedPackageUnsupported)
	}
	headerLength := encryptedPackageFixedHeaderLen + wrappedKeyLength + nonceLength
	if int64(headerLength) > packageSize {
		return encryptedPackageHeader{}, fmt.Errorf("%w: header is truncated", ErrInvalidEncryptedPackage)
	}
	header := make([]byte, headerLength)
	copy(header, fixed)
	if _, err := io.ReadFull(source, header[encryptedPackageFixedHeaderLen:]); err != nil {
		return encryptedPackageHeader{}, fmt.Errorf("%w: header is truncated", ErrInvalidEncryptedPackage)
	}
	return encryptedPackageHeader{
		raw:        header,
		wrappedKey: header[encryptedPackageFixedHeaderLen : encryptedPackageFixedHeaderLen+wrappedKeyLength],
		baseNonce:  header[encryptedPackageFixedHeaderLen+wrappedKeyLength:],
		chunkSize:  chunkSize,
		tagSize:    tagLength,
	}, nil
}

func parseEncryptedPackagePrivateKey(value []byte) (*rsa.PrivateKey, error) {
	if len(bytes.TrimSpace(value)) == 0 {
		return nil, ErrEncryptedPackagePrivateKeyRequired
	}
	block, rest := pem.Decode(value)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("%w: expected exactly one PEM block", ErrEncryptedPackagePrivateKeyInvalid)
	}
	if privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		if err := privateKey.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEncryptedPackagePrivateKeyInvalid, err)
		}
		return privateKey, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: expected a PKCS#1 or PKCS#8 RSA private key", ErrEncryptedPackagePrivateKeyInvalid)
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: PEM does not contain an RSA private key", ErrEncryptedPackagePrivateKeyInvalid)
	}
	if err := privateKey.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptedPackagePrivateKeyInvalid, err)
	}
	return privateKey, nil
}

func decryptPackageFrames(source io.Reader, destination *os.File, gcm cipher.AEAD, header encryptedPackageHeader, maxPlaintext int64) error {
	var (
		chunkIndex    uint64
		total         int64
		sawShortChunk bool
	)
	for {
		var sizeRaw [4]byte
		if _, err := io.ReadFull(source, sizeRaw[:]); err != nil {
			return fmt.Errorf("%w: missing encrypted terminal frame", ErrInvalidEncryptedPackage)
		}
		frameLength := int(binary.BigEndian.Uint32(sizeRaw[:]))
		if frameLength < header.tagSize || frameLength > header.chunkSize+header.tagSize {
			return fmt.Errorf("%w: frame length is invalid", ErrInvalidEncryptedPackage)
		}
		frame := make([]byte, frameLength)
		if _, err := io.ReadFull(source, frame); err != nil {
			return fmt.Errorf("%w: frame is truncated", ErrInvalidEncryptedPackage)
		}
		nonce := frameNonce(header.baseNonce, chunkIndex)
		aad := frameAAD(header.raw, chunkIndex)
		plaintext, err := gcm.Open(nil, nonce, frame, aad)
		if err != nil {
			return fmt.Errorf("%w: frame authentication failed", ErrInvalidEncryptedPackage)
		}
		if len(plaintext) == 0 {
			if frameLength != header.tagSize {
				return fmt.Errorf("%w: terminal frame is invalid", ErrInvalidEncryptedPackage)
			}
			trailing, err := io.ReadAll(io.LimitReader(source, 1))
			if err != nil {
				return fmt.Errorf("%w: inspect trailing bytes", ErrInvalidEncryptedPackage)
			}
			if len(trailing) != 0 {
				return fmt.Errorf("%w: trailing bytes after terminal frame", ErrInvalidEncryptedPackage)
			}
			return nil
		}
		if sawShortChunk || len(plaintext) > header.chunkSize {
			return fmt.Errorf("%w: invalid chunk sequence", ErrInvalidEncryptedPackage)
		}
		if int64(len(plaintext)) > maxPlaintext-total {
			return fmt.Errorf("unpacked package exceeds %d byte limit", maxPlaintext)
		}
		if written, err := destination.Write(plaintext); err != nil || written != len(plaintext) {
			if err == nil {
				err = io.ErrShortWrite
			}
			return fmt.Errorf("write decrypted package: %w", err)
		}
		total += int64(len(plaintext))
		sawShortChunk = len(plaintext) < header.chunkSize
		if chunkIndex == ^uint64(0) {
			return fmt.Errorf("%w: too many encrypted frames", ErrInvalidEncryptedPackage)
		}
		chunkIndex++
	}
}

func frameNonce(baseNonce []byte, chunkIndex uint64) []byte {
	nonce := make([]byte, encryptedPackageNonceSize)
	copy(nonce, baseNonce[:4])
	binary.BigEndian.PutUint64(nonce[4:], chunkIndex)
	return nonce
}

func frameAAD(header []byte, chunkIndex uint64) []byte {
	aad := make([]byte, len(header)+8)
	copy(aad, header)
	binary.BigEndian.PutUint64(aad[len(header):], chunkIndex)
	return aad
}

func createDecryptedArchive(assetRoot string) (*os.File, string, func() error, error) {
	importsRoot := filepath.Join(assetRoot, ".atri-imports")
	if info, err := os.Lstat(importsRoot); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, "", nil, fmt.Errorf("import workspace is not a private directory: %s", importsRoot)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", nil, fmt.Errorf("inspect import workspace: %w", err)
	}
	if err := os.MkdirAll(importsRoot, 0o700); err != nil {
		return nil, "", nil, fmt.Errorf("create import workspace: %w", err)
	}
	temporary, err := os.CreateTemp(importsRoot, ".atri-decrypted-*.zip")
	if err != nil {
		return nil, "", nil, fmt.Errorf("create decrypted package workspace: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() error { return os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		_ = cleanup()
		return nil, "", nil, fmt.Errorf("protect decrypted package workspace: %w", err)
	}
	return temporary, temporaryPath, cleanup, nil
}

func maxEncryptedPackageBytes(maxPlaintext int64, wrappedKeyLength int) int64 {
	if maxPlaintext <= 0 {
		return 0
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	chunkCount := maxPlaintext / encryptedPackageChunkSize
	if maxPlaintext%encryptedPackageChunkSize != 0 {
		chunkCount++
	}
	headerLength := int64(encryptedPackageFixedHeaderLen + wrappedKeyLength + encryptedPackageNonceSize)
	frameOverhead := int64(4 + encryptedPackageTagSize)
	if headerLength < 0 || maxPlaintext > maxInt64-headerLength {
		return maxInt64
	}
	total := headerLength + maxPlaintext
	frameCount := chunkCount + 1 // Include the authenticated empty terminal frame.
	if frameCount > (maxInt64-total)/frameOverhead {
		return maxInt64
	}
	return total + frameCount*frameOverhead
}

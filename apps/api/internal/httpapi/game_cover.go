package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/sudoriaa/atri-games/apps/api/internal/data"
)

const (
	defaultGameCoverMaxBytes = int64(10 * 1024 * 1024)
	gameMutationJSONMaxBytes = int64(1 * 1024 * 1024)
	gameCoverMultipartSlack  = int64(1 * 1024 * 1024)
	maxGameCoverDimension    = uint64(16_384)
	maxGameCoverPixels       = uint64(40_000_000)
)

var (
	errGameCoverTooLarge = errors.New("game cover is too large")
	errInvalidGameCover  = errors.New("game cover is not a supported image")
)

type pendingGameCover struct {
	sourcePath string
	extension  string
	digest     string
}

func (cover *pendingGameCover) cleanup() {
	if cover != nil && cover.sourcePath != "" {
		_ = os.Remove(cover.sourcePath)
	}
}

func (cover *pendingGameCover) upload() data.GameCoverUpload {
	return data.GameCoverUpload{
		SourcePath: cover.sourcePath,
		Extension:  cover.extension,
		SHA256:     cover.digest,
	}
}

func (s *Server) decodeGameMutation(w http.ResponseWriter, r *http.Request) (input data.GameInput, cover *pendingGameCover, ok bool) {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, mediaErr := mime.ParseMediaType(contentType)
	if mediaErr != nil || mediaType != "multipart/form-data" {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/") {
			writeError(w, http.StatusBadRequest, "invalid_multipart", "封面上传格式无效")
			return input, nil, false
		}
		return input, nil, decodeJSON(w, r, &input)
	}

	maxBytes := s.config.GameCoverMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultGameCoverMaxBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+gameMutationJSONMaxBytes+gameCoverMultipartSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "请使用 multipart/form-data 上传游戏信息和封面")
		return input, nil, false
	}

	var gameJSON []byte
	defer func() {
		if !ok {
			cover.cleanup()
		}
	}()
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			if isBodyTooLarge(nextErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "cover_too_large", "封面图片超过服务器允许的大小")
			} else {
				writeError(w, http.StatusBadRequest, "invalid_multipart", "封面上传不完整")
			}
			return input, cover, false
		}

		switch part.FormName() {
		case "game":
			if gameJSON != nil {
				part.Close()
				writeError(w, http.StatusBadRequest, "invalid_multipart", "游戏信息只能提交一次")
				return input, cover, false
			}
			value, readErr := io.ReadAll(io.LimitReader(part, gameMutationJSONMaxBytes+1))
			closeErr := part.Close()
			if readErr != nil || closeErr != nil || int64(len(value)) > gameMutationJSONMaxBytes {
				writeError(w, http.StatusBadRequest, "invalid_json", "游戏信息格式无效")
				return input, cover, false
			}
			gameJSON = value
		case "cover":
			if cover != nil {
				part.Close()
				writeError(w, http.StatusBadRequest, "invalid_cover", "一次只能上传一张游戏封面")
				return input, cover, false
			}
			cover, err = s.readGameCover(part, maxBytes)
			part.Close()
			if err != nil {
				switch {
				case errors.Is(err, errGameCoverTooLarge):
					writeError(w, http.StatusRequestEntityTooLarge, "cover_too_large", "封面图片不能超过 10 MB")
				case errors.Is(err, errInvalidGameCover):
					writeError(w, http.StatusUnprocessableEntity, "invalid_cover", "封面仅支持真实的 AVIF、JPG、PNG 或 WebP 图片")
				default:
					s.internalError(w, r, err)
				}
				return input, cover, false
			}
		default:
			part.Close()
			writeError(w, http.StatusBadRequest, "invalid_multipart", "封面上传包含未知字段")
			return input, cover, false
		}
	}

	if len(gameJSON) == 0 || cover == nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "请同时提交游戏信息和封面图片")
		return input, cover, false
	}
	decoder := json.NewDecoder(bytes.NewReader(gameJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "游戏信息格式无效")
		return input, cover, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "游戏信息只能包含一个 JSON 对象")
		return input, cover, false
	}
	ok = true
	return input, cover, true
}

func (s *Server) readGameCover(part io.Reader, maxBytes int64) (*pendingGameCover, error) {
	uploadRoot, err := data.GameCoverUploadRoot(s.config.AssetRoot)
	if err != nil {
		return nil, fmt.Errorf("create cover upload workspace: %w", err)
	}
	file, err := os.CreateTemp(uploadRoot, "cover-*")
	if err != nil {
		return nil, fmt.Errorf("create cover upload: %w", err)
	}
	sourcePath := file.Name()
	cleanup := func(cause error) (*pendingGameCover, error) {
		_ = file.Close()
		_ = os.Remove(sourcePath)
		return nil, cause
	}

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(part, maxBytes+1))
	if copyErr != nil {
		if isBodyTooLarge(copyErr) {
			return cleanup(errGameCoverTooLarge)
		}
		return cleanup(fmt.Errorf("write cover upload: %w", copyErr))
	}
	if written > maxBytes {
		return cleanup(errGameCoverTooLarge)
	}
	if written == 0 {
		return cleanup(errInvalidGameCover)
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync cover upload: %w", err))
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(sourcePath)
		return nil, fmt.Errorf("close cover upload: %w", err)
	}
	extension, err := validateGameCoverFile(sourcePath)
	if err != nil {
		_ = os.Remove(sourcePath)
		if errors.Is(err, errInvalidGameCover) {
			return nil, err
		}
		return nil, fmt.Errorf("inspect cover upload: %w", err)
	}
	return &pendingGameCover{
		sourcePath: sourcePath,
		extension:  extension,
		digest:     hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func validateGameCoverFile(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	extension, valid := detectGameCoverExtension(content)
	if !valid {
		return "", errInvalidGameCover
	}

	var width, height uint64
	switch extension {
	case ".jpg", ".png":
		config, format, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil ||
			(extension == ".jpg" && format != "jpeg") ||
			(extension == ".png" && format != "png") {
			return "", errInvalidGameCover
		}
		width, height = uint64(config.Width), uint64(config.Height)
	case ".webp":
		width, height, valid = webPDimensions(content)
		if !valid {
			return "", errInvalidGameCover
		}
	case ".avif":
		width, height, valid = avifDimensions(content)
		if !valid {
			return "", errInvalidGameCover
		}
	}
	if !validGameCoverDimensions(width, height) {
		return "", errInvalidGameCover
	}
	return extension, nil
}

func detectGameCoverExtension(header []byte) (string, bool) {
	switch {
	case len(header) >= 8 && bytes.Equal(header[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return ".png", true
	case len(header) >= 3 && header[0] == 0xff && header[1] == 0xd8 && header[2] == 0xff:
		return ".jpg", true
	case len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WEBP")):
		return ".webp", true
	case isAVIFHeader(header):
		return ".avif", true
	default:
		return "", false
	}
}

func validGameCoverDimensions(width, height uint64) bool {
	return width > 0 && height > 0 &&
		width <= maxGameCoverDimension &&
		height <= maxGameCoverDimension &&
		width <= maxGameCoverPixels/height
}

func webPDimensions(content []byte) (uint64, uint64, bool) {
	if len(content) < 20 ||
		!bytes.Equal(content[:4], []byte("RIFF")) ||
		!bytes.Equal(content[8:12], []byte("WEBP")) ||
		uint64(binary.LittleEndian.Uint32(content[4:8]))+8 != uint64(len(content)) {
		return 0, 0, false
	}
	var canvasWidth, canvasHeight uint64
	for offset := 12; offset+8 <= len(content); {
		chunkType := string(content[offset : offset+4])
		chunkSize := uint64(binary.LittleEndian.Uint32(content[offset+4 : offset+8]))
		payloadStart := uint64(offset + 8)
		payloadEnd := payloadStart + chunkSize
		if payloadEnd > uint64(len(content)) {
			return 0, 0, false
		}
		payload := content[payloadStart:payloadEnd]
		switch chunkType {
		case "VP8X":
			if len(payload) < 10 {
				return 0, 0, false
			}
			canvasWidth = 1 + littleEndianUint24(payload[4:7])
			canvasHeight = 1 + littleEndianUint24(payload[7:10])
		case "VP8 ":
			if len(payload) < 10 || !bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, false
			}
			width := uint64(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff)
			height := uint64(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff)
			if canvasWidth != 0 && (canvasWidth != width || canvasHeight != height) {
				return 0, 0, false
			}
			return width, height, true
		case "VP8L":
			if len(payload) < 5 || payload[0] != 0x2f {
				return 0, 0, false
			}
			bits := binary.LittleEndian.Uint32(payload[1:5])
			width := uint64(bits&0x3fff) + 1
			height := uint64((bits>>14)&0x3fff) + 1
			if canvasWidth != 0 && (canvasWidth != width || canvasHeight != height) {
				return 0, 0, false
			}
			return width, height, true
		}
		next := payloadEnd + chunkSize%2
		if next <= uint64(offset) || next > uint64(len(content)) {
			return 0, 0, false
		}
		offset = int(next)
	}
	return 0, 0, false
}

func littleEndianUint24(value []byte) uint64 {
	return uint64(value[0]) | uint64(value[1])<<8 | uint64(value[2])<<16
}

func avifDimensions(content []byte) (uint64, uint64, bool) {
	if len(content) < 16 {
		return 0, 0, false
	}
	size, headerSize, ok := isoBoxSize(content)
	if !ok || size > uint64(len(content)) || string(content[4:8]) != "ftyp" {
		return 0, 0, false
	}
	ftyp := content[headerSize:size]
	if len(ftyp) < 8 || !containsAVIFBrand(ftyp) {
		return 0, 0, false
	}
	return findAVIFDimensions(content, 0)
}

func containsAVIFBrand(ftyp []byte) bool {
	if bytes.Equal(ftyp[:4], []byte("avif")) || bytes.Equal(ftyp[:4], []byte("avis")) {
		return true
	}
	for offset := 8; offset+4 <= len(ftyp); offset += 4 {
		if bytes.Equal(ftyp[offset:offset+4], []byte("avif")) ||
			bytes.Equal(ftyp[offset:offset+4], []byte("avis")) {
			return true
		}
	}
	return false
}

func findAVIFDimensions(content []byte, depth int) (uint64, uint64, bool) {
	if depth > 8 {
		return 0, 0, false
	}
	for offset := 0; offset+8 <= len(content); {
		size, headerSize, ok := isoBoxSize(content[offset:])
		if !ok || size > uint64(len(content)-offset) {
			return 0, 0, false
		}
		boxType := string(content[offset+4 : offset+8])
		payloadStart := offset + headerSize
		payloadEnd := offset + int(size)
		payload := content[payloadStart:payloadEnd]
		if boxType == "ispe" {
			if len(payload) < 12 {
				return 0, 0, false
			}
			return uint64(binary.BigEndian.Uint32(payload[4:8])),
				uint64(binary.BigEndian.Uint32(payload[8:12])),
				true
		}
		switch boxType {
		case "meta":
			if len(payload) < 4 {
				return 0, 0, false
			}
			if width, height, found := findAVIFDimensions(payload[4:], depth+1); found {
				return width, height, true
			}
		case "iprp", "ipco", "moov", "trak", "mdia", "minf", "stbl":
			if width, height, found := findAVIFDimensions(payload, depth+1); found {
				return width, height, true
			}
		}
		offset = payloadEnd
	}
	return 0, 0, false
}

func isoBoxSize(content []byte) (uint64, int, bool) {
	if len(content) < 8 {
		return 0, 0, false
	}
	size := uint64(binary.BigEndian.Uint32(content[:4]))
	headerSize := 8
	switch size {
	case 0:
		size = uint64(len(content))
	case 1:
		if len(content) < 16 {
			return 0, 0, false
		}
		size = binary.BigEndian.Uint64(content[8:16])
		headerSize = 16
	}
	return size, headerSize, size >= uint64(headerSize)
}

func isAVIFHeader(header []byte) bool {
	if len(header) < 12 || !bytes.Equal(header[4:8], []byte("ftyp")) {
		return false
	}
	if bytes.Equal(header[8:12], []byte("avif")) || bytes.Equal(header[8:12], []byte("avis")) {
		return true
	}
	for offset := 16; offset+4 <= len(header); offset += 4 {
		if bytes.Equal(header[offset:offset+4], []byte("avif")) || bytes.Equal(header[offset:offset+4], []byte("avis")) {
			return true
		}
	}
	return false
}

func isBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError) || strings.Contains(strings.ToLower(err.Error()), "request body too large")
}

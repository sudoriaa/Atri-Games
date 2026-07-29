package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/sudoriaa/atri-games/apps/api/internal/data"
)

const (
	defaultAvatarMaxBytes = int64(2 * 1024 * 1024)
	avatarMultipartSlack  = int64(256 * 1024)
)

var (
	errAvatarTooLarge = errors.New("avatar is too large")
	errInvalidAvatar  = errors.New("avatar is not a supported image")
)

type pendingAvatar struct {
	sourcePath string
	extension  string
	digest     string
}

func (avatar *pendingAvatar) cleanup() {
	if avatar != nil && avatar.sourcePath != "" {
		_ = os.Remove(avatar.sourcePath)
	}
}

func (avatar *pendingAvatar) upload() data.AvatarUpload {
	return data.AvatarUpload{
		SourcePath: avatar.sourcePath,
		Extension:  avatar.extension,
		SHA256:     avatar.digest,
	}
}

func (s *Server) updateAvatar(w http.ResponseWriter, r *http.Request) {
	avatar, ok := s.decodeAvatarUpload(w, r)
	if !ok {
		return
	}
	defer avatar.cleanup()

	current := currentUser(r)
	s.avatarMu.Lock()
	defer s.avatarMu.Unlock()

	avatarURL, created, err := data.InstallAvatar(s.config.AssetRoot, current.ID, avatar.upload())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	user, err := s.store.UpdateProfile(current.ID, current.DisplayName, avatarURL)
	if err != nil {
		if created {
			if cleanupErr := data.RemoveManagedAvatar(s.config.AssetRoot, current.ID, avatarURL); cleanupErr != nil {
				s.logger.Error("remove uncommitted avatar", "userId", current.ID, "error", cleanupErr)
			}
		}
		s.writeStoreError(w, r, err)
		return
	}
	if current.AvatarURL != avatarURL {
		if err := data.RemoveManagedAvatar(s.config.AssetRoot, current.ID, current.AvatarURL); err != nil {
			s.logger.Error("remove replaced avatar", "userId", current.ID, "error", err)
		}
		s.syncGameObjects("avatars/" + current.ID)
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) decodeAvatarUpload(w http.ResponseWriter, r *http.Request) (*pendingAvatar, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "请使用 multipart/form-data 上传头像")
		return nil, false
	}

	maxBytes := s.config.AvatarMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultAvatarMaxBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+avatarMultipartSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "头像上传格式无效")
		return nil, false
	}

	var avatar *pendingAvatar
	defer func() {
		if avatar != nil {
			avatar.cleanup()
		}
	}()
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			if isBodyTooLarge(nextErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "头像图片超过大小限制")
			} else {
				writeError(w, http.StatusBadRequest, "invalid_multipart", "头像上传不完整")
			}
			return nil, false
		}
		if part.FormName() != "avatar" || avatar != nil {
			part.Close()
			writeError(w, http.StatusBadRequest, "invalid_multipart", "一次只能上传一张头像图片")
			return nil, false
		}
		avatar, err = s.readAvatar(part, maxBytes)
		part.Close()
		if err != nil {
			switch {
			case errors.Is(err, errAvatarTooLarge):
				writeError(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "头像图片超过大小限制")
			case errors.Is(err, errInvalidAvatar):
				writeError(w, http.StatusUnprocessableEntity, "invalid_avatar", "头像仅支持真实的 AVIF、JPG、PNG 或 WebP 图片")
			default:
				s.internalError(w, r, err)
			}
			return nil, false
		}
	}
	if avatar == nil {
		writeError(w, http.StatusBadRequest, "missing_avatar", "请选择一张头像图片")
		return nil, false
	}
	result := avatar
	avatar = nil
	return result, true
}

func (s *Server) readAvatar(part io.Reader, maxBytes int64) (*pendingAvatar, error) {
	uploadRoot, err := data.AvatarUploadRoot(s.config.AssetRoot)
	if err != nil {
		return nil, fmt.Errorf("create avatar upload workspace: %w", err)
	}
	file, err := os.CreateTemp(uploadRoot, "avatar-*")
	if err != nil {
		return nil, fmt.Errorf("create avatar upload: %w", err)
	}
	sourcePath := file.Name()
	cleanup := func(cause error) (*pendingAvatar, error) {
		_ = file.Close()
		_ = os.Remove(sourcePath)
		return nil, cause
	}

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(part, maxBytes+1))
	if copyErr != nil {
		if isBodyTooLarge(copyErr) {
			return cleanup(errAvatarTooLarge)
		}
		return cleanup(fmt.Errorf("write avatar upload: %w", copyErr))
	}
	if written > maxBytes {
		return cleanup(errAvatarTooLarge)
	}
	if written == 0 {
		return cleanup(errInvalidAvatar)
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync avatar upload: %w", err))
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(sourcePath)
		return nil, fmt.Errorf("close avatar upload: %w", err)
	}
	extension, err := validateGameCoverFile(sourcePath)
	if err != nil {
		_ = os.Remove(sourcePath)
		if errors.Is(err, errInvalidGameCover) {
			return nil, errInvalidAvatar
		}
		return nil, fmt.Errorf("inspect avatar upload: %w", err)
	}
	return &pendingAvatar{
		sourcePath: sourcePath,
		extension:  extension,
		digest:     hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

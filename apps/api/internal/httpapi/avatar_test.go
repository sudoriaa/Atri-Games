package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sudoriaa/atri-games/apps/api/internal/data"
)

func TestAvatarUploadAndProfileURLValidation(t *testing.T) {
	api := newTestAPI(t)
	player := registerUser(t, api, "avatar-player@example.test")

	requireError(t, api.avatarUpload(t, avatarPNG(t, color.RGBA{R: 1, A: 255}), "avatar.png", ""), http.StatusUnauthorized, "authentication_required")
	requireError(t, api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"avatarUrl": "/covers/neon-relay.webp",
	}, player.Token), http.StatusUnprocessableEntity, "invalid_avatar_url")
	requireError(t, api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"avatarUrl": "/avatars/usr_other/avatar-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png",
	}, player.Token), http.StatusUnprocessableEntity, "invalid_avatar_url")

	external := api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"avatarUrl": "https://images.example.test/player.png",
	}, player.Token)
	requireStatus(t, external, http.StatusOK)
	if user := decodeResponse[data.User](t, external); user.AvatarURL != "https://images.example.test/player.png" {
		t.Fatalf("HTTPS avatar URL = %q", user.AvatarURL)
	}

	requireError(t, api.avatarUpload(t, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "avatar.svg", player.Token), http.StatusUnprocessableEntity, "invalid_avatar")

	firstResponse := api.avatarUpload(t, avatarPNG(t, color.RGBA{R: 10, G: 20, B: 30, A: 255}), "first.png", player.Token)
	requireStatus(t, firstResponse, http.StatusOK)
	first := decodeResponse[data.User](t, firstResponse)
	assertManagedAvatarURL(t, player.User.ID, first.AvatarURL)
	firstPath := managedAvatarPathOnDisk(api.assetRoot, first.AvatarURL)
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("first uploaded avatar missing: %v", err)
	}

	secondResponse := api.avatarUpload(t, avatarPNG(t, color.RGBA{R: 40, G: 50, B: 60, A: 255}), "second.png", player.Token)
	requireStatus(t, secondResponse, http.StatusOK)
	second := decodeResponse[data.User](t, secondResponse)
	if second.AvatarURL == first.AvatarURL {
		t.Fatal("replacing an avatar retained the first content-addressed URL")
	}
	secondPath := managedAvatarPathOnDisk(api.assetRoot, second.AvatarURL)
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("replaced avatar remains on disk: %v", err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("replacement avatar missing: %v", err)
	}
	ownedURL := api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"avatarUrl": second.AvatarURL,
	}, player.Token)
	requireStatus(t, ownedURL, http.StatusOK)

	other := registerUser(t, api, "avatar-other@example.test")
	requireError(t, api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"avatarUrl": second.AvatarURL,
	}, other.Token), http.StatusUnprocessableEntity, "invalid_avatar_url")

	clearResponse := api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{"avatarUrl": ""}, player.Token)
	requireStatus(t, clearResponse, http.StatusOK)
	if cleared := decodeResponse[data.User](t, clearResponse); cleared.AvatarURL != "" {
		t.Fatalf("cleared avatar URL = %q, want empty", cleared.AvatarURL)
	}
	if _, err := os.Stat(secondPath); !os.IsNotExist(err) {
		t.Fatalf("cleared avatar remains on disk: %v", err)
	}
}

func (api *testAPI) avatarUpload(t *testing.T, content []byte, filename, token string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("avatar", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/me/avatar", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	return response
}

func avatarPNG(t *testing.T, pixel color.RGBA) []byte {
	t.Helper()

	imageValue := image.NewRGBA(image.Rect(0, 0, 1, 1))
	imageValue.SetRGBA(0, 0, pixel)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageValue); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func assertManagedAvatarURL(t *testing.T, userID, value string) {
	t.Helper()
	if !data.IsManagedAvatarURL(userID, value) || !strings.HasPrefix(value, "/avatars/"+userID+"/avatar-") {
		t.Fatalf("avatar URL = %q, want managed URL for %s", value, userID)
	}
}

func managedAvatarPathOnDisk(assetRoot, value string) string {
	return filepath.Join(assetRoot, filepath.FromSlash(strings.TrimPrefix(value, "/")))
}

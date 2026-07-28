package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sudoriaa/atri-games/apps/api/internal/data"
)

func TestAdminCreatesUpdatesAndDeletesGameWithUploadedCover(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	input := coverTestGameInput("cover-upload-game")
	png := tinyPNG(t)

	createResponse := api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		input,
		"..\\..\\user-selected.png",
		png,
		admin.Token,
	)
	requireStatus(t, createResponse, http.StatusCreated)
	game := decodeResponse[data.Game](t, createResponse)
	if !strings.HasPrefix(game.CoverURL, "/covers/cover-upload-game/cover-") ||
		!strings.HasSuffix(game.CoverURL, ".png") ||
		len(strings.TrimSuffix(strings.TrimPrefix(game.CoverURL, "/covers/cover-upload-game/cover-"), ".png")) != 64 {
		t.Fatalf("uploaded cover URL = %q", game.CoverURL)
	}
	coverPath := filepath.Join(api.assetRoot, filepath.FromSlash(strings.TrimPrefix(game.CoverURL, "/")))
	if content, err := os.ReadFile(coverPath); err != nil || !bytes.Equal(content, png) {
		t.Fatalf("installed cover mismatch: err=%v bytes=%d", err, len(content))
	}

	// JSON updates remain compatible and preserve the current cover when the
	// administrator does not select a replacement image.
	input.Title = "Cover Upload Game Updated"
	input.CoverURL = game.CoverURL
	updateResponse := api.request(t, http.MethodPut, "/api/v1/admin/games/"+game.ID, input, admin.Token)
	requireStatus(t, updateResponse, http.StatusOK)
	updated := decodeResponse[data.Game](t, updateResponse)
	if updated.CoverURL != game.CoverURL {
		t.Fatalf("cover changed without a replacement upload: %q -> %q", game.CoverURL, updated.CoverURL)
	}
	if _, err := os.Stat(coverPath); err != nil {
		t.Fatalf("preserved cover missing: %v", err)
	}

	webp := tinyWebP(t)
	replaceResponse := api.gameMutationWithCover(
		t,
		http.MethodPut,
		"/api/v1/admin/games/"+game.ID,
		input,
		"replacement.jpg",
		webp,
		admin.Token,
	)
	requireStatus(t, replaceResponse, http.StatusOK)
	replaced := decodeResponse[data.Game](t, replaceResponse)
	if !strings.HasSuffix(replaced.CoverURL, ".webp") || replaced.CoverURL == game.CoverURL {
		t.Fatalf("replacement cover URL = %q", replaced.CoverURL)
	}
	replacementPath := filepath.Join(api.assetRoot, filepath.FromSlash(strings.TrimPrefix(replaced.CoverURL, "/")))
	if content, err := os.ReadFile(replacementPath); err != nil || !bytes.Equal(content, webp) {
		t.Fatalf("replacement cover mismatch: err=%v bytes=%d", err, len(content))
	}
	if _, err := os.Stat(coverPath); !os.IsNotExist(err) {
		t.Fatalf("replacement retained the old content-hash cover: %v", err)
	}

	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/admin/games/"+game.ID+"/unpublish", nil, admin.Token), http.StatusOK)
	if _, err := os.Stat(replacementPath); err != nil {
		t.Fatalf("unpublishing removed the cover: %v", err)
	}
	requireStatus(t, api.request(t, http.MethodDelete, "/api/v1/admin/games/"+game.ID, nil, admin.Token), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(api.assetRoot, "covers", input.Slug)); !os.IsNotExist(err) {
		t.Fatalf("permanent deletion retained managed cover directory: %v", err)
	}
}

func TestUploadedGameCoverRejectsDisguisedFilesAndCleansFailedMutations(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)

	disguised := coverTestGameInput("fake-cover-game")
	response := api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		disguised,
		"not-really.png",
		[]byte("<svg><script>alert(1)</script></svg>"),
		admin.Token,
	)
	requireError(t, response, http.StatusUnprocessableEntity, "invalid_cover")
	listResponse := api.request(t, http.MethodGet, "/api/v1/admin/games?query=fake-cover-game", nil, admin.Token)
	requireStatus(t, listResponse, http.StatusOK)
	if list := decodeResponse[data.GameList](t, listResponse); list.Total != 0 {
		t.Fatalf("disguised cover created a game: %+v", list.Items)
	}

	truncated := coverTestGameInput("truncated-cover-game")
	response = api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		truncated,
		"truncated.png",
		[]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		admin.Token,
	)
	requireError(t, response, http.StatusUnprocessableEntity, "invalid_cover")

	invalid := coverTestGameInput("invalid-cover-game")
	invalid.Summary = "short"
	response = api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		invalid,
		"valid.png",
		tinyPNG(t),
		admin.Token,
	)
	requireError(t, response, http.StatusUnprocessableEntity, "invalid_description")
	if _, err := os.Stat(filepath.Join(api.assetRoot, "covers", invalid.Slug)); !os.IsNotExist(err) {
		t.Fatalf("validation failure created a managed cover directory: %v", err)
	}
	assertCoverUploadWorkspaceEmpty(t, api.assetRoot)

	conflict := coverTestGameInput("conflicting-cover-game")
	conflict.CoverURL = "/covers/existing-conflict.webp"
	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/admin/games", conflict, admin.Token), http.StatusCreated)
	conflict.CoverURL = ""
	response = api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		conflict,
		"valid.png",
		tinyPNG(t),
		admin.Token,
	)
	requireError(t, response, http.StatusConflict, "already_exists")
	if _, err := os.Stat(filepath.Join(api.assetRoot, "covers", conflict.Slug)); !os.IsNotExist(err) {
		t.Fatalf("database conflict retained an uncommitted cover directory: %v", err)
	}
	assertCoverUploadWorkspaceEmpty(t, api.assetRoot)
}

func TestDuplicateCreateWithSameUploadedCoverReturnsConflict(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	input := coverTestGameInput("duplicate-same-cover")
	cover := tinyPNG(t)

	first := api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		input,
		"cover.png",
		cover,
		admin.Token,
	)
	requireStatus(t, first, http.StatusCreated)
	created := decodeResponse[data.Game](t, first)

	second := api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		input,
		"same-cover.png",
		cover,
		admin.Token,
	)
	requireError(t, second, http.StatusConflict, "already_exists")
	persisted, err := api.store.GameBySlug(input.Slug, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ID != created.ID {
		t.Fatalf("duplicate create replaced game %q with %q", created.ID, persisted.ID)
	}
	coverPath := filepath.Join(api.assetRoot, filepath.FromSlash(strings.TrimPrefix(created.CoverURL, "/")))
	if content, err := os.ReadFile(coverPath); err != nil || !bytes.Equal(content, cover) {
		t.Fatalf("original cover changed: err=%v bytes=%d", err, len(content))
	}
	assertCoverUploadWorkspaceEmpty(t, api.assetRoot)
}

func TestSameCoverUpdateWithInvalidCategoryReturnsConflict(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	input := coverTestGameInput("same-cover-invalid-update")
	cover := tinyPNG(t)
	createResponse := api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		input,
		"cover.png",
		cover,
		admin.Token,
	)
	requireStatus(t, createResponse, http.StatusCreated)
	created := decodeResponse[data.Game](t, createResponse)

	input.Title = "This title must not be committed"
	input.CategoryID = "missing-category"
	updateResponse := api.gameMutationWithCover(
		t,
		http.MethodPut,
		"/api/v1/admin/games/"+created.ID,
		input,
		"same-cover.png",
		cover,
		admin.Token,
	)
	requireError(t, updateResponse, http.StatusConflict, "resource_in_use")
	persisted, err := api.store.GameByID(created.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Title != created.Title || persisted.CategoryID != created.CategoryID || persisted.CoverURL != created.CoverURL {
		t.Fatalf("failed update changed game: before=%+v after=%+v", created, persisted)
	}
	coverPath := filepath.Join(api.assetRoot, filepath.FromSlash(strings.TrimPrefix(created.CoverURL, "/")))
	if _, err := os.Stat(coverPath); err != nil {
		t.Fatalf("failed update removed existing cover: %v", err)
	}
	assertCoverUploadWorkspaceEmpty(t, api.assetRoot)
}

func TestJSONUpdateReclaimsUnreferencedUploadedCover(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	input := coverTestGameInput("uploaded-to-external-cover")
	createResponse := api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		input,
		"cover.png",
		tinyPNG(t),
		admin.Token,
	)
	requireStatus(t, createResponse, http.StatusCreated)
	created := decodeResponse[data.Game](t, createResponse)
	uploadedPath := filepath.Join(api.assetRoot, filepath.FromSlash(strings.TrimPrefix(created.CoverURL, "/")))

	input.Title = "External Cover"
	input.CoverURL = "https://images.example.test/external-cover.webp"
	updateResponse := api.request(t, http.MethodPut, "/api/v1/admin/games/"+created.ID, input, admin.Token)
	requireStatus(t, updateResponse, http.StatusOK)
	updated := decodeResponse[data.Game](t, updateResponse)
	if updated.CoverURL != input.CoverURL {
		t.Fatalf("updated cover URL = %q", updated.CoverURL)
	}
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("JSON update retained the old uploaded cover: %v", err)
	}
}

func TestUploadedGameCoverRejectsFilesOverTheConfiguredLimit(t *testing.T) {
	api := newTestAPI(t)
	admin := loginAdmin(t, api)
	tooLarge := make([]byte, defaultGameCoverMaxBytes+1)
	copy(tooLarge, tinyPNG(t))
	response := api.gameMutationWithCover(
		t,
		http.MethodPost,
		"/api/v1/admin/games",
		coverTestGameInput("oversized-cover"),
		"oversized.png",
		tooLarge,
		admin.Token,
	)
	requireError(t, response, http.StatusRequestEntityTooLarge, "cover_too_large")
	assertCoverUploadWorkspaceEmpty(t, api.assetRoot)
}

func TestGameCoverMagicDetection(t *testing.T) {
	tests := []struct {
		name      string
		header    []byte
		extension string
		valid     bool
	}{
		{name: "png", header: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, extension: ".png", valid: true},
		{name: "jpeg", header: []byte{0xff, 0xd8, 0xff, 0xe0}, extension: ".jpg", valid: true},
		{name: "webp", header: []byte("RIFF\x10\x00\x00\x00WEBP"), extension: ".webp", valid: true},
		{name: "avif major brand", header: []byte("\x00\x00\x00\x18ftypavif\x00\x00\x00\x00"), extension: ".avif", valid: true},
		{name: "avif compatible brand", header: []byte("\x00\x00\x00\x18ftypmif1\x00\x00\x00\x00avif"), extension: ".avif", valid: true},
		{name: "gif excluded", header: []byte("GIF89a"), valid: false},
		{name: "svg excluded", header: []byte("<svg xmlns="), valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extension, valid := detectGameCoverExtension(test.header)
			if extension != test.extension || valid != test.valid {
				t.Fatalf("detectGameCoverExtension() = %q, %v; want %q, %v", extension, valid, test.extension, test.valid)
			}
		})
	}
}

func (api *testAPI) gameMutationWithCover(
	t *testing.T,
	method, target string,
	input data.GameInput,
	filename string,
	cover []byte,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	gameJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("game", string(gameJSON)); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("cover", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(cover); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	return response
}

func coverTestGameInput(slug string) data.GameInput {
	return data.GameInput{
		Slug:          slug,
		Title:         "Cover Upload Game",
		Summary:       "A complete summary for testing a directly uploaded game cover.",
		Description:   "A complete description for the uploaded game cover integration test.",
		AuthorName:    "Cover Test Studio",
		CoverURL:      "",
		LaunchURL:     "https://example.test/games/" + slug,
		LaunchOpenIn:  "new-tab",
		RepositoryURL: "",
		Engine:        "Canvas",
		Version:       "1.0.0",
		Status:        "draft",
		CategoryID:    "arcade",
		Tags:          []string{"cover", "upload"},
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	value, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func tinyWebP(t *testing.T) []byte {
	t.Helper()
	value, err := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAgA0JaACdLoB+AADsAD+8Oj3/yC5YXXI1/8gP+QH/ID/+PIAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertCoverUploadWorkspaceEmpty(t *testing.T, assetRoot string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(assetRoot, ".atri-cover-uploads"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read cover upload workspace: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cover upload workspace contains %d orphan(s)", len(entries))
	}
}

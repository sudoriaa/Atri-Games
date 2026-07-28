// Package gamepkg validates and unpacks the technology-neutral .atri package.
// It intentionally knows nothing about a game engine or server language: the
// only executable content it accepts is a browser entry page and static files.
package gamepkg

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ManifestName       = "atri-game.json"
	DefaultMaxArchive  = int64(512 * 1024 * 1024)
	DefaultMaxUnpacked = int64(2 * 1024 * 1024 * 1024)
	DefaultMaxFiles    = 20000
	maxManifestBytes   = 2 * 1024 * 1024
	// Static entry pages should be small HTML launch documents. Keeping this
	// bounded prevents an import or startup backfill from allocating an entire
	// package-sized file merely to add one script tag.
	maxRuntimeEntryBytes = int64(8 * 1024 * 1024)
	runtimeBootstrapSrc  = "/sdk/atri-game-runtime-bootstrap.js"
)

var (
	slugPattern             = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	semverPattern           = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	imagePattern            = regexp.MustCompile(`(?i)\.(avif|jpe?g|png|webp)$`)
	packageSegmentPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	scriptStartPattern      = regexp.MustCompile(`(?i)<script\b`)
	runtimeBootstrapPattern = regexp.MustCompile(`(?is)<script\b[^>]*(?:\bdata-atri-runtime-bootstrap\b|\bsrc\s*=\s*["'][^"']*/sdk/atri-game-runtime-bootstrap\.js(?:[?#][^"']*)?["'])[^>]*>`)
)

type Limits struct {
	MaxArchiveBytes  int64
	MaxUnpackedBytes int64
	MaxFiles         int
}

func DefaultLimits() Limits {
	return Limits{
		MaxArchiveBytes:  DefaultMaxArchive,
		MaxUnpackedBytes: DefaultMaxUnpacked,
		MaxFiles:         DefaultMaxFiles,
	}
}

type Manifest struct {
	Schema        string        `json:"$schema"`
	SchemaVersion int           `json:"schemaVersion"`
	ID            string        `json:"id"`
	Version       string        `json:"version"`
	Title         string        `json:"title"`
	Summary       string        `json:"summary"`
	Description   string        `json:"description"`
	Authors       []Author      `json:"authors"`
	License       string        `json:"license"`
	Repository    string        `json:"repository"`
	Homepage      string        `json:"homepage"`
	Engine        Engine        `json:"engine"`
	Runtime       Runtime       `json:"runtime"`
	Services      *Services     `json:"services"`
	Privacy       *Privacy      `json:"privacy"`
	Media         Media         `json:"media"`
	Compatibility Compatibility `json:"compatibility"`
	Tags          []string      `json:"tags"`
	AI            *AI           `json:"ai"`
}

type Author struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Engine struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Framework string `json:"framework"`
}

type Runtime struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Entry  string `json:"entry"`
	OpenIn string `json:"openIn"`
	Bridge string `json:"bridge"`
}

type Services struct {
	NetworkRequired      *bool               `json:"networkRequired"`
	OwnBackend           *bool               `json:"ownBackend"`
	Realtime             []string            `json:"realtime"`
	PlatformIntegrations []string            `json:"platformIntegrations"`
	Identity             *IdentityService    `json:"identity"`
	Storage              *StorageService     `json:"storage"`
	Matchmaking          *MatchmakingService `json:"matchmaking"`
}

type IdentityService struct {
	Mode string `json:"mode"`
}

type StorageService struct {
	Provider string `json:"provider"`
	Scope    string `json:"scope"`
}

type MatchmakingService struct {
	Enabled  *bool  `json:"enabled"`
	Protocol string `json:"protocol"`
}

type Privacy struct {
	CollectsPersonalData *bool  `json:"collectsPersonalData"`
	PolicyURL            string `json:"policyUrl"`
	DataSummary          string `json:"dataSummary"`
}

type AI struct {
	Tools      *[]string `json:"tools"`
	Disclosure string    `json:"disclosure"`
}

type Media struct {
	Cover       string   `json:"cover"`
	Screenshots []string `json:"screenshots"`
}

type Compatibility struct {
	Devices         []string  `json:"devices"`
	Inputs          []string  `json:"inputs"`
	Orientation     string    `json:"orientation"`
	MinimumViewport *Viewport `json:"minimumViewport"`
}

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ValidationError struct {
	Messages []string
}

// CapabilityHints are the small denormalized values exposed in the catalog.
// Detailed storage scope and matchmaking protocol remain in the manifest.
type CapabilityHints struct {
	RequiresLogin       bool
	UsesPlatformStorage bool
	MatchmakingEnabled  bool
}

// CapabilityHints returns effective low-friction defaults for a package.
// Static packages get SQLite as a fallback, while login is only required
// when a developer explicitly requests a player-bound service.
func (manifest Manifest) CapabilityHints() CapabilityHints {
	hints := CapabilityHints{}
	if manifest.Runtime.Kind != "static" {
		return hints
	}
	hints.UsesPlatformStorage = true
	if manifest.Services == nil {
		return hints
	}
	if manifest.Services.Storage != nil {
		hints.UsesPlatformStorage = manifest.Services.Storage.Provider == "sqlite"
		scope := manifest.Services.Storage.Scope
		if scope == "" {
			scope = "player-game"
		}
		if hints.UsesPlatformStorage && scope != "game" {
			hints.RequiresLogin = true
		}
	}
	if manifest.Services.Identity != nil && manifest.Services.Identity.Mode == "required" {
		hints.RequiresLogin = true
	}
	if manifest.Services.Matchmaking != nil && manifest.Services.Matchmaking.Enabled != nil && *manifest.Services.Matchmaking.Enabled {
		hints.MatchmakingEnabled = true
		hints.RequiresLogin = true
	}
	return hints
}

func (e *ValidationError) Error() string {
	return "invalid game package: " + strings.Join(e.Messages, "; ")
}

type Prepared struct {
	Manifest     Manifest
	Root         string
	ManifestPath string
	CoverPath    string
	BundlePath   string
	Entry        string
	ArchivePath  string
}

func (p *Prepared) Cleanup() error {
	if p == nil || p.Root == "" {
		return nil
	}
	return os.RemoveAll(p.Root)
}

func ReadManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", ManifestName, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Manifest{}, errors.New("manifest contains trailing JSON")
	}
	var envelope struct {
		Services map[string]json.RawMessage `json:"services"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		var nullFields []string
		for _, name := range []string{"identity", "storage", "matchmaking"} {
			if value, exists := envelope.Services[name]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				nullFields = append(nullFields, "services."+name+" must be an object when present")
			}
		}
		if len(nullFields) > 0 {
			return Manifest{}, &ValidationError{Messages: nullFields}
		}
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) error {
	var messages []string
	add := func(ok bool, message string) {
		if !ok {
			messages = append(messages, message)
		}
	}
	add(manifest.SchemaVersion == 2, "schemaVersion must be 2")
	add(slugPattern.MatchString(manifest.ID) && len(manifest.ID) >= 3 && len(manifest.ID) <= 64, "id must be 3-64 lowercase kebab-case characters")
	add(semverPattern.MatchString(manifest.Version), "version must be semantic versioning")
	add(strings.TrimSpace(manifest.Title) != "" && len([]rune(manifest.Title)) <= 80, "title is required and must be at most 80 characters")
	add(len([]rune(manifest.Summary)) >= 10 && len([]rune(manifest.Summary)) <= 240, "summary must be 10-240 characters")
	add(strings.TrimSpace(manifest.Description) != "" && len([]rune(manifest.Description)) <= 4000, "description is required and must be at most 4000 characters")
	add(len(manifest.Authors) > 0 && len(manifest.Authors) <= 20, "authors must contain 1-20 authors")
	for index, author := range manifest.Authors {
		add(strings.TrimSpace(author.Name) != "" && len([]rune(author.Name)) <= 80, fmt.Sprintf("authors[%d].name is invalid", index))
		if author.URL != "" {
			add(isHTTPS(author.URL), fmt.Sprintf("authors[%d].url must be HTTPS", index))
		}
	}
	add(strings.TrimSpace(manifest.License) != "" && len([]rune(manifest.License)) <= 80, "license is required")
	if manifest.Repository != "" {
		add(isHTTPS(manifest.Repository), "repository must be HTTPS")
	}
	if manifest.Homepage != "" {
		add(isHTTPS(manifest.Homepage), "homepage must be HTTPS")
	}
	add(strings.TrimSpace(manifest.Engine.Name) != "" && len([]rune(manifest.Engine.Name)) <= 80, "engine.name is required")
	add(len([]rune(manifest.Engine.Version)) <= 40, "engine.version is too long")
	add(len([]rune(manifest.Engine.Framework)) <= 80, "engine.framework is too long")

	switch manifest.Runtime.Kind {
	case "external":
		add(isHTTPS(manifest.Runtime.URL), "runtime.url must be HTTPS")
		add(manifest.Runtime.OpenIn == "same-tab" || manifest.Runtime.OpenIn == "new-tab", "runtime.openIn is invalid")
		add(manifest.Runtime.Entry == "", "external runtime.entry is not allowed")
		add(manifest.Runtime.Bridge == "", "external runtime.bridge is not allowed")
		if manifest.Services != nil {
			// Explicit no-op declarations keep the generated manifest
			// portable between static and external runtimes. Any active
			// built-in capability remains forbidden for an external URL.
			if manifest.Services.Identity != nil {
				add(manifest.Services.Identity.Mode == "none", "external games cannot use built-in identity")
			}
			if manifest.Services.Storage != nil {
				add(manifest.Services.Storage.Provider == "none" && (manifest.Services.Storage.Scope == "" || manifest.Services.Storage.Scope == "game"), "external games cannot use built-in storage")
			}
			if manifest.Services.Matchmaking != nil {
				add(manifest.Services.Matchmaking.Enabled == nil || !*manifest.Services.Matchmaking.Enabled, "external games cannot use built-in matchmaking")
			}
		}
	case "static":
		add(safePackagePath(manifest.Runtime.Entry) && strings.HasSuffix(strings.ToLower(manifest.Runtime.Entry), ".html"), "runtime.entry must be a safe HTML path")
		add(manifest.Runtime.OpenIn == "same-tab" || manifest.Runtime.OpenIn == "new-tab", "runtime.openIn is invalid")
		add(manifest.Runtime.Bridge == "" || oneOf(manifest.Runtime.Bridge, "none", "optional", "required"), "runtime.bridge is invalid")
		add(manifest.Runtime.URL == "", "static runtime.url is not allowed")
	default:
		messages = append(messages, "runtime.kind must be external or static")
	}

	add(manifest.Media.Cover != "" && safePackagePath(manifest.Media.Cover) && imagePattern.MatchString(manifest.Media.Cover), "media.cover must be a packaged image")
	add(len(manifest.Media.Screenshots) <= 8, "media.screenshots must contain at most 8 images")
	for index, screenshot := range manifest.Media.Screenshots {
		add(safePackagePath(screenshot) && imagePattern.MatchString(screenshot), fmt.Sprintf("media.screenshots[%d] is invalid", index))
	}
	add(manifest.Services != nil, "services is required")
	if manifest.Services != nil {
		add(manifest.Services.NetworkRequired != nil, "services.networkRequired is required")
		add(manifest.Services.OwnBackend != nil, "services.ownBackend is required")
		add(validEnumList(manifest.Services.Realtime, []string{"websocket", "server-sent-events", "webrtc", "other"}, 4), "services.realtime is invalid")
		add(validEnumList(manifest.Services.PlatformIntegrations, []string{"lifecycle", "identity", "cloud-save", "achievements", "leaderboards", "webhooks"}, 6), "services.platformIntegrations is invalid")
		if manifest.Services.Identity != nil {
			add(oneOf(manifest.Services.Identity.Mode, "none", "optional", "required"), "services.identity.mode is invalid")
		}
		if manifest.Services.Storage != nil {
			add(oneOf(manifest.Services.Storage.Provider, "none", "sqlite"), "services.storage.provider is invalid")
			add(oneOf(manifest.Services.Storage.Scope, "player-game", "player", "game"), "services.storage.scope is invalid")
			if manifest.Services.Storage.Provider == "none" {
				add(manifest.Services.Storage.Scope == "game", "services.storage.scope must be game when provider is none")
			} else if manifest.Services.Storage.Provider == "sqlite" {
				add(oneOf(manifest.Services.Storage.Scope, "player-game", "player"), "services.storage.scope must be player-game or player when provider is sqlite")
			}
		}
		if manifest.Services.Matchmaking != nil {
			add(manifest.Services.Matchmaking.Enabled != nil, "services.matchmaking.enabled is required")
			add(oneOf(manifest.Services.Matchmaking.Protocol, "websocket", "sse", "http"), "services.matchmaking.protocol is invalid")
		}
	}
	add(manifest.Privacy != nil, "privacy is required")
	if manifest.Privacy != nil {
		add(manifest.Privacy.CollectsPersonalData != nil, "privacy.collectsPersonalData is required")
		add(manifest.Privacy.DataSummary != "" && len([]rune(manifest.Privacy.DataSummary)) >= 10 && len([]rune(manifest.Privacy.DataSummary)) <= 800, "privacy.dataSummary must be 10-800 characters")
		if manifest.Privacy.CollectsPersonalData != nil && *manifest.Privacy.CollectsPersonalData {
			add(isHTTPS(manifest.Privacy.PolicyURL), "privacy.policyUrl is required and must be HTTPS when personal data is collected")
		}
	}
	add(len(manifest.Compatibility.Devices) > 0 && validEnumList(manifest.Compatibility.Devices, []string{"desktop", "mobile", "tablet"}, 3), "compatibility.devices is invalid")
	add(len(manifest.Compatibility.Inputs) > 0 && validEnumList(manifest.Compatibility.Inputs, []string{"keyboard", "mouse", "touch", "gamepad"}, 4), "compatibility.inputs is invalid")
	add(oneOf(manifest.Compatibility.Orientation, "any", "landscape", "portrait"), "compatibility.orientation is invalid")
	add(len(manifest.Tags) > 0 && len(manifest.Tags) <= 10, "tags must contain 1-10 values")
	for index, tag := range manifest.Tags {
		add(strings.TrimSpace(tag) != "" && len([]rune(tag)) <= 40, fmt.Sprintf("tags[%d] is invalid", index))
	}
	add(uniqueStrings(manifest.Tags), "tags must be unique")
	if manifest.Compatibility.MinimumViewport != nil {
		add(manifest.Compatibility.MinimumViewport.Width >= 240 && manifest.Compatibility.MinimumViewport.Width <= 7680, "compatibility.minimumViewport.width is invalid")
		add(manifest.Compatibility.MinimumViewport.Height >= 240 && manifest.Compatibility.MinimumViewport.Height <= 4320, "compatibility.minimumViewport.height is invalid")
	}
	if manifest.AI != nil {
		add(manifest.AI.Tools != nil, "ai.tools is required")
		if manifest.AI.Tools != nil {
			add(len(*manifest.AI.Tools) <= 20, "ai.tools must contain at most 20 values")
			add(uniqueStrings(*manifest.AI.Tools), "ai.tools must be unique")
			for index, tool := range *manifest.AI.Tools {
				add(strings.TrimSpace(tool) != "" && len([]rune(tool)) <= 80, fmt.Sprintf("ai.tools[%d] is invalid", index))
			}
		}
		add(strings.TrimSpace(manifest.AI.Disclosure) != "" && len([]rune(manifest.AI.Disclosure)) <= 1000, "ai.disclosure is invalid")
	}
	if len(messages) > 0 {
		return &ValidationError{Messages: messages}
	}
	return nil
}

func Extract(archivePath, assetRoot string, limits Limits) (*Prepared, error) {
	return ExtractWithPrivateKey(archivePath, assetRoot, limits, nil)
}

// ExtractWithPrivateKey accepts both legacy ZIP .atri packages and ATRIENC1
// encrypted containers. A private key is only parsed after ATRIENC1 is
// detected, preserving ordinary ZIP imports even when an optional key is
// misconfigured.
func ExtractWithPrivateKey(archivePath, assetRoot string, limits Limits, privateKeyPEM []byte) (*Prepared, error) {
	if limits.MaxArchiveBytes <= 0 || limits.MaxUnpackedBytes <= 0 || limits.MaxFiles <= 0 {
		limits = DefaultLimits()
	}
	zipPath, cleanup, err := prepareArchiveForExtraction(archivePath, assetRoot, limits, privateKeyPEM)
	if err != nil {
		return nil, err
	}
	prepared, extractErr := extractZIP(zipPath, assetRoot, limits)
	cleanupErr := cleanup()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("remove temporary decrypted package: %w", cleanupErr)
	}
	if extractErr != nil {
		if cleanupErr != nil {
			return nil, errors.Join(extractErr, cleanupErr)
		}
		return nil, extractErr
	}
	if cleanupErr != nil {
		_ = prepared.Cleanup()
		return nil, cleanupErr
	}
	// The temporary decrypted ZIP is intentionally removed before a Prepared
	// value is returned, so callers always retain the original upload path.
	prepared.ArchivePath = archivePath
	return prepared, nil
}

func extractZIP(archivePath, assetRoot string, limits Limits) (*Prepared, error) {
	if limits.MaxArchiveBytes <= 0 || limits.MaxUnpackedBytes <= 0 || limits.MaxFiles <= 0 {
		limits = DefaultLimits()
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("inspect package: %w", err)
	}
	if info.Size() > limits.MaxArchiveBytes {
		return nil, fmt.Errorf("package exceeds %d byte limit", limits.MaxArchiveBytes)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	defer reader.Close()

	names := make(map[string]struct{}, len(reader.File))
	var total uint64
	var manifestFile *zip.File
	for _, file := range reader.File {
		name, err := normalizeArchivePath(file.Name)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(name)
		if _, exists := names[key]; exists {
			return nil, fmt.Errorf("duplicate package path: %s", name)
		}
		names[key] = struct{}{}
		if len(names) > limits.MaxFiles {
			return nil, fmt.Errorf("package contains more than %d files", limits.MaxFiles)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symbolic links are not allowed: %s", name)
		}
		if file.UncompressedSize64 > uint64(limits.MaxUnpackedBytes) || total > uint64(limits.MaxUnpackedBytes)-file.UncompressedSize64 {
			return nil, fmt.Errorf("unpacked package exceeds %d byte limit", limits.MaxUnpackedBytes)
		}
		total += file.UncompressedSize64
		if name == ManifestName {
			if manifestFile != nil {
				return nil, errors.New("package contains more than one manifest")
			}
			manifestFile = file
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("package must contain %s", ManifestName)
	}
	manifestRaw, err := readZipFile(manifestFile, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifest, err := ReadManifest(manifestRaw)
	if err != nil {
		return nil, err
	}

	importsRoot := filepath.Join(assetRoot, ".atri-imports")
	if info, err := os.Lstat(importsRoot); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("import workspace is not a private directory: %s", importsRoot)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect import workspace: %w", err)
	}
	if err := os.MkdirAll(importsRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create import workspace: %w", err)
	}
	root, err := os.MkdirTemp(importsRoot, "package-")
	if err != nil {
		return nil, fmt.Errorf("create package workspace: %w", err)
	}
	prepared := &Prepared{
		Manifest:     manifest,
		Root:         root,
		ManifestPath: filepath.Join(root, ManifestName),
		ArchivePath:  archivePath,
	}
	cleanupOnError := func(cause error) (*Prepared, error) {
		_ = prepared.Cleanup()
		return nil, cause
	}
	if err := os.WriteFile(prepared.ManifestPath, manifestRaw, 0o600); err != nil {
		return cleanupOnError(fmt.Errorf("write package manifest: %w", err))
	}
	for _, file := range reader.File {
		name, _ := normalizeArchivePath(file.Name)
		if file.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if !pathWithin(root, target) {
			return cleanupOnError(fmt.Errorf("package path escaped workspace: %s", name))
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return cleanupOnError(fmt.Errorf("create package directory: %w", err))
		}
		source, err := file.Open()
		if err != nil {
			return cleanupOnError(fmt.Errorf("read package file %s: %w", name, err))
		}
		destination, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			source.Close()
			return cleanupOnError(fmt.Errorf("create package file %s: %w", name, err))
		}
		written, copyErr := io.Copy(destination, io.LimitReader(source, int64(file.UncompressedSize64)+1))
		closeErr := errors.Join(source.Close(), destination.Close())
		if copyErr != nil || closeErr != nil || written != int64(file.UncompressedSize64) {
			return cleanupOnError(fmt.Errorf("extract package file %s: %w", name, errors.Join(copyErr, closeErr)))
		}
	}
	prepared.CoverPath = filepath.Join(root, filepath.FromSlash(manifest.Media.Cover))
	if manifest.Runtime.Kind == "static" {
		prepared.Entry = manifest.Runtime.Entry
		prepared.BundlePath = filepath.Join(root, "game")
		entryPath := filepath.Join(prepared.BundlePath, filepath.FromSlash(prepared.Entry))
		if !pathWithin(prepared.BundlePath, entryPath) {
			return cleanupOnError(errors.New("static entry escaped game directory"))
		}
		if info, err := os.Stat(entryPath); err != nil || !info.Mode().IsRegular() {
			return cleanupOnError(fmt.Errorf("static entry game/%s is missing", prepared.Entry))
		}
		if _, err := InjectRuntimeBootstrap(entryPath); err != nil {
			return cleanupOnError(fmt.Errorf("inject static runtime bootstrap: %w", err))
		}
	}
	if info, err := os.Stat(prepared.CoverPath); err != nil || !info.Mode().IsRegular() {
		return cleanupOnError(fmt.Errorf("packaged cover %s is missing", manifest.Media.Cover))
	}
	for _, screenshot := range manifest.Media.Screenshots {
		screenshotPath := filepath.Join(root, filepath.FromSlash(screenshot))
		if info, err := os.Stat(screenshotPath); err != nil || !info.Mode().IsRegular() {
			return cleanupOnError(fmt.Errorf("packaged screenshot %s is missing", screenshot))
		}
	}
	return prepared, nil
}

// InjectRuntimeBootstrap adds the platform's parser-blocking launch bridge to
// a static game entry page. It returns true only when it changed the file, so
// callers can safely backfill already imported packages at startup.
func InjectRuntimeBootstrap(entryPath string) (bool, error) {
	info, err := os.Lstat(entryPath)
	if err != nil {
		return false, fmt.Errorf("inspect entry page: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("entry page must be a regular file")
	}
	if info.Size() > maxRuntimeEntryBytes {
		return false, fmt.Errorf("entry page exceeds %d byte bootstrap limit", maxRuntimeEntryBytes)
	}
	file, err := os.Open(entryPath)
	if err != nil {
		return false, fmt.Errorf("read entry page: %w", err)
	}
	content, readErr := io.ReadAll(io.LimitReader(file, maxRuntimeEntryBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return false, fmt.Errorf("read entry page: %w", errors.Join(readErr, closeErr))
	}
	if int64(len(content)) > maxRuntimeEntryBytes {
		return false, fmt.Errorf("entry page exceeds %d byte bootstrap limit", maxRuntimeEntryBytes)
	}
	if runtimeBootstrapPattern.Match(content) {
		return false, nil
	}

	tag := []byte(`<script src="` + runtimeBootstrapSrc + `" data-atri-runtime-bootstrap></script>` + "\n")
	lower := bytes.ToLower(content)
	insertAt := -1
	if match := scriptStartPattern.FindIndex(content); match != nil {
		insertAt = match[0]
	} else if index := bytes.Index(lower, []byte("</head>")); index >= 0 {
		insertAt = index
	} else if index := bytes.Index(lower, []byte("<body")); index >= 0 {
		if close := bytes.IndexByte(content[index:], '>'); close >= 0 {
			insertAt = index + close + 1
		}
	}
	if insertAt < 0 {
		insertAt = 0
	}

	injected := make([]byte, 0, len(content)+len(tag)+1)
	injected = append(injected, content[:insertAt]...)
	injected = append(injected, tag...)
	injected = append(injected, content[insertAt:]...)
	if err := replaceEntryAtomically(entryPath, injected, info.Mode().Perm()); err != nil {
		return false, fmt.Errorf("write entry page: %w", err)
	}
	return true, nil
}

func replaceEntryAtomically(entryPath string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(entryPath), ".atri-runtime-bootstrap-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, entryPath)
}

func readZipFile(file *zip.File, limit int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file.Name, err)
	}
	defer reader.Close()
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > limit {
		return nil, fmt.Errorf("%s exceeds %d byte limit", file.Name, limit)
	}
	return value, nil
}

func normalizeArchivePath(raw string) (string, error) {
	if raw == "" || strings.ContainsAny(raw, "\\\x00") || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("unsafe package path: %q", raw)
	}
	trimmed := strings.TrimSuffix(raw, "/")
	if trimmed == "" {
		return "", errors.New("package contains an empty path")
	}
	clean := path.Clean(trimmed)
	if clean != trimmed || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe package path: %q", raw)
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." || !safePathSegment(segment) {
			return "", fmt.Errorf("unsafe package path: %q", raw)
		}
	}
	return clean, nil
}

func safePackagePath(value string) bool {
	if value == "" || len(value) > 240 || strings.ContainsAny(value, "\\\x00") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if !packageSegmentPattern.MatchString(segment) {
			return false
		}
	}
	return true
}

func safePathSegment(value string) bool {
	return value != "" && value != "." && value != ".."
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isHTTPS(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func validEnumList(values []string, allowed []string, max int) bool {
	if len(values) == 0 {
		return true
	}
	if len(values) > max {
		return false
	}
	lookup := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		lookup[item] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := lookup[value]; !ok {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func uniqueStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

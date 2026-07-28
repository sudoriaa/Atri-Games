package gamepkg

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractStaticPackage(t *testing.T) {
	archivePath := writePackage(t, map[string]string{
		"atri-game.json":  validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`),
		"cover.webp":      "cover",
		"game/index.html": "<!doctype html><script src='./main.js'></script>",
		"game/main.js":    "console.log('ok')",
	})
	prepared, err := Extract(archivePath, t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Cleanup() })
	if prepared.Manifest.ID != "fixture-game" || prepared.Entry != "index.html" {
		t.Fatalf("prepared package = %+v", prepared)
	}
	entry, err := os.ReadFile(filepath.Join(prepared.BundlePath, prepared.Entry))
	if err != nil {
		t.Fatalf("read injected entry: %v", err)
	}
	bootstrapAt := strings.Index(string(entry), `/sdk/atri-game-runtime-bootstrap.js`)
	gameScriptAt := strings.Index(string(entry), `src='./main.js'`)
	if bootstrapAt < 0 || gameScriptAt < 0 || bootstrapAt > gameScriptAt {
		t.Fatalf("runtime bootstrap was not inserted before the game script: %s", entry)
	}
}

func TestExtractStaticPackageKeepsExistingRuntimeBootstrap(t *testing.T) {
	archivePath := writePackage(t, map[string]string{
		"atri-game.json":  validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`),
		"cover.webp":      "cover",
		"game/index.html": `<!doctype html><head><script data-atri-runtime-bootstrap src="/sdk/atri-game-runtime-bootstrap.js"></script></head><body><script src="./main.js"></script></body>`,
		"game/main.js":    "console.log('ok')",
	})
	prepared, err := Extract(archivePath, t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Cleanup() })
	entry, err := os.ReadFile(filepath.Join(prepared.BundlePath, prepared.Entry))
	if err != nil {
		t.Fatalf("read injected entry: %v", err)
	}
	if count := strings.Count(string(entry), `/sdk/atri-game-runtime-bootstrap.js`); count != 1 {
		t.Fatalf("runtime bootstrap count = %d, want 1: %s", count, entry)
	}
	changed, err := InjectRuntimeBootstrap(filepath.Join(prepared.BundlePath, prepared.Entry))
	if err != nil {
		t.Fatalf("InjectRuntimeBootstrap: %v", err)
	}
	if changed {
		t.Fatal("InjectRuntimeBootstrap changed an entry that already had the bootstrap")
	}
}

func TestInjectRuntimeBootstrapRejectsOversizedEntry(t *testing.T) {
	entryPath := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(entryPath, bytes.Repeat([]byte("x"), int(maxRuntimeEntryBytes)+1), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := InjectRuntimeBootstrap(entryPath)
	if err == nil {
		t.Fatal("InjectRuntimeBootstrap accepted an oversized entry")
	}
	if changed {
		t.Fatal("InjectRuntimeBootstrap reported a change for an oversized entry")
	}
}

func TestExtractRejectsTraversalAndSymlink(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "unsafe.atri")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{"atri-game.json", "../escape.txt"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if name == "atri-game.json" {
			_, _ = entry.Write([]byte(validManifest(`"kind":"external","url":"https://example.test/play","openIn":"same-tab"`)))
		}
	}
	_ = writer.Close()
	_ = file.Close()
	if _, err := Extract(archivePath, t.TempDir(), DefaultLimits()); err == nil {
		t.Fatal("unsafe archive was accepted")
	}
}

func TestValidateManifestNeedsHTTPSForExternalRuntime(t *testing.T) {
	manifest, err := ReadManifest([]byte(validManifest(`"kind":"external","url":"http://example.test/play","openIn":"same-tab"`)))
	if err == nil {
		t.Fatalf("ReadManifest accepted %+v", manifest)
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
}

func TestReadManifestAcceptsSchemaAndAIMetadata(t *testing.T) {
	raw := validManifest(`"kind":"external","url":"https://example.test/play","openIn":"same-tab"`)
	raw = strings.Replace(raw, "{", "{\n\t\t\"$schema\":\"https://atri.games/schemas/game-manifest.schema.json\",", 1)
	raw = strings.Replace(raw, `"tags":["fixture"]`, `"tags":["fixture"],"ai":{"tools":["image-generation"],"disclosure":"Generated assets are disclosed in the game credits."}`, 1)
	manifest, err := ReadManifest([]byte(raw))
	if err != nil {
		t.Fatalf("ReadManifest rejected valid optional metadata: %v", err)
	}
	if manifest.Schema == "" || manifest.AI == nil || manifest.AI.Tools == nil || len(*manifest.AI.Tools) != 1 {
		t.Fatalf("optional metadata was not decoded: %+v", manifest)
	}
}

func TestReadManifestPlatformServices(t *testing.T) {
	raw := validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`)
	raw = strings.Replace(raw, `"services":{"networkRequired":false,"ownBackend":false}`, `"services":{"networkRequired":true,"ownBackend":false,"identity":{"mode":"required"},"storage":{"provider":"sqlite","scope":"player-game"},"matchmaking":{"enabled":true,"protocol":"websocket"}}`, 1)
	manifest, err := ReadManifest([]byte(raw))
	if err != nil {
		t.Fatalf("ReadManifest platform services: %v", err)
	}
	hints := manifest.CapabilityHints()
	if !hints.RequiresLogin || !hints.UsesPlatformStorage || !hints.MatchmakingEnabled {
		t.Fatalf("unexpected platform hints: %+v", hints)
	}
}

func TestReadManifestRejectsGloballyWritableSQLiteScope(t *testing.T) {
	raw := validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`)
	raw = strings.Replace(raw, `"services":{"networkRequired":false,"ownBackend":false}`, `"services":{"networkRequired":false,"ownBackend":false,"storage":{"provider":"sqlite","scope":"game"}}`, 1)
	if _, err := ReadManifest([]byte(raw)); err == nil {
		t.Fatal("manifest with globally writable SQLite scope was accepted")
	}
}

func TestReadManifestRejectsNullPlatformService(t *testing.T) {
	raw := validManifest(`"kind":"static","entry":"index.html","openIn":"same-tab","bridge":"optional"`)
	raw = strings.Replace(raw, `"services":{"networkRequired":false,"ownBackend":false}`, `"services":{"networkRequired":false,"ownBackend":false,"storage":null}`, 1)
	if _, err := ReadManifest([]byte(raw)); err == nil {
		t.Fatal("manifest with a null platform service was accepted")
	}
}

func TestReadManifestRejectsPlatformServicesForExternalRuntime(t *testing.T) {
	raw := validManifest(`"kind":"external","url":"https://example.test/game","openIn":"same-tab"`)
	raw = strings.Replace(raw, `"services":{"networkRequired":false,"ownBackend":false}`, `"services":{"networkRequired":true,"ownBackend":false,"identity":{"mode":"required"}}`, 1)
	if _, err := ReadManifest([]byte(raw)); err == nil {
		t.Fatal("external manifest with identity service was accepted")
	}
}

func TestReadManifestAcceptsExplicitNoopServicesForExternalRuntime(t *testing.T) {
	raw := validManifest(`"kind":"external","url":"https://example.test/game","openIn":"same-tab"`)
	raw = strings.Replace(
		raw,
		`"services":{"networkRequired":false,"ownBackend":false}`,
		`"services":{"networkRequired":true,"ownBackend":true,"identity":{"mode":"none"},"storage":{"provider":"none","scope":"game"},"matchmaking":{"enabled":false,"protocol":"http"}}`,
		1,
	)
	if _, err := ReadManifest([]byte(raw)); err != nil {
		t.Fatalf("external manifest with explicit no-op services was rejected: %v", err)
	}
}

func TestValidateManifestRequiresPrivacyBoolean(t *testing.T) {
	raw := validManifest(`"kind":"external","url":"https://example.test/play","openIn":"same-tab"`)
	raw = strings.Replace(raw, `"collectsPersonalData":false,`, "", 1)
	if _, err := ReadManifest([]byte(raw)); err == nil {
		t.Fatal("manifest without privacy.collectsPersonalData was accepted")
	}
}

func validManifest(runtime string) string {
	return `{
		"schemaVersion":2,
		"id":"fixture-game",
		"version":"1.0.0",
		"title":"Fixture Game",
		"summary":"A fixture game used for package tests.",
		"description":"A small fixture package for validating the universal game contract.",
		"authors":[{"name":"Fixture Team"}],
		"license":"MIT",
		"engine":{"name":"custom"},
		"runtime":{` + runtime + `},
		"services":{"networkRequired":false,"ownBackend":false},
		"privacy":{"collectsPersonalData":false,"dataSummary":"No personal data is collected."},
		"media":{"cover":"cover.webp"},
		"compatibility":{"devices":["desktop"],"inputs":["keyboard"],"orientation":"any"},
		"tags":["fixture"]
	}`
}

func writePackage(t *testing.T, files map[string]string) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "fixture.atri")
	output, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

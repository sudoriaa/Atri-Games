package data

import "testing"

func TestStaticPackageEntriesReturnsManifestRuntimeEntries(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`,
		"game_neon", "runtime-static", "static", `{"runtime":{"entry":"dist/index.html"}}`); err != nil {
		t.Fatalf("insert static package: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO game_packages(game_id,receipt_token,kind,manifest_json) VALUES(?,?,?,?)`,
		"game_circuit", "runtime-external", "external", `{"runtime":{"url":"https://example.test/game"}}`); err != nil {
		t.Fatalf("insert external package: %v", err)
	}

	entries, err := store.StaticPackageEntries()
	if err != nil {
		t.Fatalf("StaticPackageEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "neon-relay" || entries[0].Entry != "dist/index.html" {
		t.Fatalf("static package entries = %+v", entries)
	}
}

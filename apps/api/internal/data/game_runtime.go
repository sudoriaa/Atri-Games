package data

import (
	"encoding/json"
)

// StaticPackageEntry identifies the installed HTML entry of an imported static
// package. It is used at process startup to apply idempotent platform-runtime
// compatibility updates to packages that were imported before the update.
type StaticPackageEntry struct {
	Slug  string
	Entry string
}

// StaticPackageEntries returns only manifest-backed static packages with a
// usable runtime entry. Malformed historical manifests are skipped here: they
// remain visible to normal repair tooling and must not prevent the API from
// starting solely because a compatibility backfill cannot inspect them.
func (s *Store) StaticPackageEntries() ([]StaticPackageEntry, error) {
	rows, err := s.db.Query(`SELECT g.slug,p.manifest_json
		FROM games g JOIN game_packages p ON p.game_id=g.id
		WHERE p.kind='static'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]StaticPackageEntry, 0)
	for rows.Next() {
		var item StaticPackageEntry
		var raw string
		if err := rows.Scan(&item.Slug, &raw); err != nil {
			return nil, err
		}
		var manifest struct {
			Runtime struct {
				Entry string `json:"entry"`
			} `json:"runtime"`
		}
		if err := json.Unmarshal([]byte(raw), &manifest); err != nil || manifest.Runtime.Entry == "" {
			continue
		}
		item.Entry = manifest.Runtime.Entry
		entries = append(entries, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

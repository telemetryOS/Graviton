// Package jsontracking serializes the applied-migrations tracking list as a
// single JSON document. The store-like drivers (fs, s3, redis) have no table or
// collection model, so each keeps the whole list in one document (a file, an
// object, or a key) named after the same graviton-migrations convention the
// database drivers use.
package jsontracking

import (
	"encoding/json"
	"time"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

type record struct {
	Filename  string    `json:"filename"`
	Source    string    `json:"source"`
	AppliedAt time.Time `json:"applied_at"`
}

// Marshal encodes the tracking list. An empty list encodes as an empty JSON
// array rather than null so the stored document is always a valid list.
func Marshal(migrationsMetadata []*migrationsmeta.MigrationMetadata) ([]byte, error) {
	records := make([]record, 0, len(migrationsMetadata))
	for _, m := range migrationsMetadata {
		records = append(records, record{
			Filename:  m.Filename,
			Source:    m.Source,
			AppliedAt: m.AppliedAt,
		})
	}
	return json.MarshalIndent(records, "", "  ")
}

// Unmarshal decodes a stored tracking document. Empty input decodes to an
// empty list, matching a store where no migration has been applied yet.
func Unmarshal(data []byte) ([]*migrationsmeta.MigrationMetadata, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var records []record
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	migrationsMetadata := make([]*migrationsmeta.MigrationMetadata, 0, len(records))
	for _, r := range records {
		migrationsMetadata = append(migrationsMetadata, &migrationsmeta.MigrationMetadata{
			Filename:  r.Filename,
			Source:    r.Source,
			AppliedAt: r.AppliedAt,
		})
	}
	return migrationsMetadata, nil
}

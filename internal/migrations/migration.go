package migrations

import (
	_ "embed"

	migrationsmeta "graviton/internal/migrations-meta"
)

type Migration struct {
	*migrationsmeta.MigrationMetadata
	Script *Script
}

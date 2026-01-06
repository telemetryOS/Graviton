package migrations

import (
	_ "embed"

	migrationsmeta "graviton/migrations-meta"
)

type Migration struct {
	*migrationsmeta.MigrationMetadata
	Script *Script
}

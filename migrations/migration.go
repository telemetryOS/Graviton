package migrations

import (
	_ "embed"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

type Migration struct {
	*migrationsmeta.MigrationMetadata
	Script *Script
}

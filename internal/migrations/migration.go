package migrations

import (
	_ "embed"

	migrationsmeta "github.com/telemetrytv/graviton-cli/internal/migrations-meta"
)

type Migration struct {
	*migrationsmeta.MigrationMetadata
	Script *Script
}

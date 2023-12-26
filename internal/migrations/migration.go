package migrations

import (
	_ "embed"

	"github.com/telemetrytv/graviton-cli/internal/driver"
)

type Migration struct {
	*driver.MigrationMetadata
	Script *Script
}

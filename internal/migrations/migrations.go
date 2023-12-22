package migrations

import (
	"github.com/telemetrytv/graviton-cli/internal/driver"
)

type Migration struct {
	*driver.MigrationMetadata
	Script *Script
}

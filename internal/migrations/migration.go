package migrations

import (
	_ "embed"

	"github.com/telemetrytv/graviton-cli/internal/driver"
)

//go:embed template.ts
var Template []byte

type Migration struct {
	*driver.MigrationMetadata
	Script *Script
}

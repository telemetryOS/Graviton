package mongodb

import (
	_ "embed"
)

//go:embed migration.ts
var MigrationTemplate []byte

//go:embed migration.d.ts
var MigrationTypeDefTemplate []byte

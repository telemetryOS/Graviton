package migrations

import (
	_ "embed"
)

//go:embed tsconfig.json
var TSConfigTemplate []byte

// MigrationTemplate is the skeleton `graviton create` writes for a new
// migration file.
//
//go:embed migration.ts
var MigrationTemplate []byte

// MigrationTypeDefTemplate is the migration.d.ts `graviton create` seeds a
// project's migrations directory with. It types the unified root handle across
// every database kind; per-kind examples in example/ trim it to their surface.
//
//go:embed migration.d.ts
var MigrationTypeDefTemplate []byte

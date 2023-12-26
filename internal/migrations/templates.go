package migrations

import (
	_ "embed"
)

//go:embed tsconfig.json
var TSConfigTemplate []byte

package assets

import (
	_ "embed"
)

//go:embed description.txt
var Description string

//go:embed splash.txt
var Splash string

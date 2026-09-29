// Package commanddata exposes Poolboy's embedded command-help catalog.
package commanddata

import _ "embed"

//go:embed commands.json
var commands string

// Commands returns the canonical command-help catalog bundled with the CLI.
func Commands() string { return commands }

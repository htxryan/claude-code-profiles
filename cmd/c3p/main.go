// c3p — protocol module, configuration division.
//
// Top-level binary entry. Routes argv → hand-rolled parser → dispatcher →
// exit code, including:
//
//   - --cwd= override (works for every subcommand without per-handler wiring)
//   - --non-interactive flag and CI=true env auto-detection
//   - exit-code mapping via internal/cli/exit.go
//
// The parser is hand-rolled (not cobra) for byte-equivalence with the
// pre-1.0 TS implementation — see internal/cli/parse.go for the rationale.
package main

import (
	"os"

	"github.com/htxryan/claude-code-config-profiles/internal/cli"
)

// version is overridden at build time via -ldflags for release builds; the
// "0.0.0-dev" default keeps `c3p --version` informative under `go run` and
// `go test -c` without requiring ldflags wiring in the test harness.
var version = "0.0.0-dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version, os.Stdout, os.Stderr))
}

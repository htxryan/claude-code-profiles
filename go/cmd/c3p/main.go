// c3p — protocol module, configuration division.
//
// Top-level binary entry. F1 lands the cobra root + dispatch skeleton; verb
// logic is filled in by D7 (CLI) and the per-domain epics. The skeleton
// routes argv → cobra → exit code, including:
//
//   - --cwd= override (works for every subcommand without per-handler wiring)
//   - --non-interactive flag and CI=true env auto-detection
//   - exit-code mapping via internal/errors
//
// Subcommand handlers print a "not implemented" notice and return a sentinel
// exit code (3 — internal error) so any test that accidentally exercises a
// real verb fails loudly instead of silently passing.
package main

import (
	"os"

	"github.com/htxryan/c3p/internal/cli"
)

// version is overridden at build time via -ldflags for release builds; the
// "0.0.0-dev" default keeps `c3p --version` informative under `go run` and
// `go test -c` without requiring ldflags wiring in the test harness.
var version = "0.0.0-dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version, os.Stdout, os.Stderr))
}

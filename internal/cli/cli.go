// Package cli wires argv → parse → dispatch → exit code. Mirrors src/cli/bin.ts.
//
// Cobra is intentionally NOT used here. The TS reference is hand-rolled, and
// the dual-suite IV harness compares parse/dispatch behaviour byte-for-byte
// across both binaries — keeping the parser surface tight to the TS shape
// avoids two parity-test surfaces.
package cli

import (
	"errors"
	"io"
	"os"
)

// Run is the testable entrypoint: argv (without the binary name), version,
// and writers for stdout/stderr. Returns the exit code so main() can exit
// and tests can assert directly.
//
// Mirrors src/cli/bin.ts:main.
func Run(argv []string, version string, stdout, stderr io.Writer) int {
	defaultCwd, err := os.Getwd()
	if err != nil {
		// os.Getwd failure is rare (current dir was unlinked); fall back
		// to "." so the subsequent path math still produces a usable
		// project root for tests that don't pass --cwd=.
		defaultCwd = "."
	}

	parseRes := ParseArgs(argv, defaultCwd)

	output := NewOutput(OutputOptions{
		JSON:   parseRes.Ok && parseRes.Invocation.Global.JSON,
		Quiet:  parseRes.Ok && parseRes.Invocation.Global.Quiet,
		IsTTY:  isStdoutTTY(stdout),
		Stdout: stdout,
		Stderr: stderr,
	})

	if !parseRes.Ok {
		// Argv error class is always user error (exit 1) per TS bin.
		output.Error("c3p: " + parseRes.Err.Message)
		return ExitUser
	}

	code, err := Dispatch(parseRes.Invocation, DispatchContext{
		Output:         output,
		Version:        version,
		SignalHandlers: true,
	})
	if err != nil {
		// Suppress duplicate emission of CliUserError messages — the
		// orchestrator/handler may have already written user-facing text.
		// Pipeline errors from the resolver/merge layer print via the
		// generic branch above (CliUserError doesn't wrap them).
		var cue *CliUserError
		if !errors.As(err, &cue) {
			output.Error("c3p: " + err.Error())
		} else if cue.Message != "" {
			output.Error("c3p: " + cue.Message)
		}
		return ExitCodeFor(err)
	}
	return code
}

// isStdoutTTY reports whether stdout is a terminal. Defaults to false when
// the caller supplies a custom writer (tests, pipes).
func isStdoutTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

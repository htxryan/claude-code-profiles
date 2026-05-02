// Package cli wires argv → cobra → exit code. F1 lands a dispatch skeleton
// only: every verb is a stub that returns ErrNotImplemented, so tests that
// accidentally invoke real verbs fail loudly. Verb logic is filled in by
// D7 (CLI) and the per-domain epics.
//
// What lives here in F1:
//   - the cobra root + global flags (--cwd, --non-interactive, --json, --quiet)
//   - the 13 subcommand stubs (init, use, sync, list, status, drift, diff,
//     new, validate, hook, doctor, completions, hello)
//   - exit-code routing (internal errors → 3; user errors → 1; usage → 2)
//
// What does NOT live here in F1:
//   - any verb behaviour (E1–E6, R-numbered requirements)
//   - JSON output (PR3 — D7 deliverable)
//   - help template byte-equality with TS bin (R3 risk — D7 deliverable)
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/spf13/cobra"
)

// ErrNotImplemented is returned by every F1 stub handler. D7 swaps each
// stub for its real implementation as the verb lands; the sentinel makes
// "I forgot to wire this verb" a loud failure instead of a silent zero
// exit.
var ErrNotImplemented = errors.New("not implemented in F1 — see epic claude-code-profiles-248")

// Exit codes mirror the TS bin (cli/exit.ts) so the dual-suite parity tests
// can assert the same numeric surface across both binaries.
const (
	ExitOK            = 0
	ExitUserError     = 1
	ExitUsageError    = 2
	ExitInternalError = 3
)

// globalOpts captures the cross-cutting flags every subcommand inherits.
// Stored on the cobra command's Annotations is awkward; persistent flags
// bound to a struct read more cleanly.
type globalOpts struct {
	cwd            string
	nonInteractive bool
	json           bool
	quiet          bool
}

// Run is the testable entrypoint: argv (without the binary name), version
// string, and writers for stdout/stderr. Returns the exit code so main()
// can pass it to os.Exit and tests can assert against it directly.
func Run(argv []string, version string, stdout, stderr io.Writer) int {
	opts := &globalOpts{}

	root := newRootCmd(opts, version, stdout, stderr)
	root.SetArgs(argv)
	root.SetOut(stdout)
	root.SetErr(stderr)
	// cobra's default behaviour prints usage on every error; we'd rather
	// route to exitCodeFor and let the formatter decide. SilenceErrors
	// stops the auto-print; SilenceUsage keeps usage out of the output for
	// runtime failures (only argv-shape errors print usage).
	root.SilenceErrors = true
	root.SilenceUsage = true

	if err := root.Execute(); err != nil {
		fmt.Fprintf(stderr, "c3p: %s\n", err.Error())
		return exitCodeFor(err)
	}
	return ExitOK
}

func newRootCmd(opts *globalOpts, version string, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:     "c3p",
		Short:   "claude-code-config-profiles — swappable .claude/ configurations",
		Long:    "c3p manages swappable .claude/ profile bundles for Claude Code projects.",
		Version: version,
	}

	// --cwd works everywhere because cobra propagates persistent flags down
	// the subcommand tree. Resolved at runtime by handlers (F1: stubs ignore
	// it, D7 wires it into the per-verb service layer).
	root.PersistentFlags().StringVar(&opts.cwd, "cwd", "", "Override working directory (default: process.cwd())")
	root.PersistentFlags().BoolVar(&opts.nonInteractive, "non-interactive", false, "Disable interactive prompts (also auto-detected from CI=true)")
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "Emit machine-readable JSON output (PR3 — D7)")
	root.PersistentFlags().BoolVar(&opts.quiet, "quiet", false, "Suppress informational output")

	// CI=true auto-detection: PreRun is the right hook because cobra has
	// already merged flags by the time PreRun fires, but the handler hasn't
	// run. If the flag is unset and CI=true is in the env, flip it on.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if !opts.nonInteractive && os.Getenv("CI") == "true" {
			opts.nonInteractive = true
		}
		return nil
	}

	for _, cmd := range subcommands(opts, stdout, stderr) {
		root.AddCommand(cmd)
	}
	return root
}

// subcommands returns the 13 verb stubs. Each carries a one-line Short doc
// stolen from the TS surface so `c3p --help` shows something coherent even
// before D7 lands the real handlers.
func subcommands(opts *globalOpts, stdout, stderr io.Writer) []*cobra.Command {
	stub := func(name, short string) *cobra.Command {
		return &cobra.Command{
			Use:   name,
			Short: short,
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("%w: verb %q", ErrNotImplemented, name)
			},
		}
	}
	return []*cobra.Command{
		stub("init", "Initialize a c3p project (writes managed-block markers)"),
		stub("use", "Switch to a profile (resolve, merge, materialize)"),
		stub("sync", "Re-materialize the active profile from disk sources"),
		stub("list", "List available profiles"),
		stub("status", "Show active profile and drift summary"),
		stub("drift", "Detect drift between materialized state and sources"),
		stub("diff", "Diff two profiles or one profile vs the materialized state"),
		stub("new", "Scaffold a new profile"),
		stub("validate", "Validate a profile manifest without materializing"),
		stub("hook", "Manage the git pre-commit drift gate"),
		stub("doctor", "Diagnose common configuration problems"),
		stub("completions", "Generate shell completions"),
		stub("hello", "Print a friendly hello (smoke-test verb)"),
	}
}

// exitCodeFor maps the error chain to a numeric exit code. PipelineError
// variants get distinct codes; cobra's argv-shape errors map to usage; any
// other error is treated as an internal fault.
func exitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	if pe := pipelineerrors.AsPipelineError(err); pe != nil {
		// Most pipeline errors are user-facing config faults (1). The
		// merge-phase IO faults map to internal-error (3) because they
		// signal disk drift, not a bad manifest. F1 only ships the
		// resolver-phase variants, so the merge case is dead until D2.
		switch pe.Phase() {
		case pipelineerrors.PhaseMerge:
			if pe.ErrorCode() == pipelineerrors.CodeMergeReadFailed {
				return ExitInternalError
			}
		}
		return ExitUserError
	}
	// cobra returns this kind of error for "unknown flag" / "missing arg".
	// They're argv-shape problems → usage error (2), not user error (1).
	if isUsageError(err) {
		return ExitUsageError
	}
	return ExitInternalError
}

// isUsageError sniffs the cobra error string for the patterns it produces
// on argv-shape failures. Cobra doesn't expose a typed sentinel for these,
// so we match on the prefix it stamps on the error message. If cobra
// changes its phrasing we'll discover it via the json_roundtrip test in IV.
func isUsageError(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"required flag",
		"flag needs an argument",
		"invalid argument",
	} {
		if len(msg) >= len(prefix) && msg[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

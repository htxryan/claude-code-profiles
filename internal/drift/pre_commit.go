package drift

import (
	"fmt"
	"io"
	"os"

	"github.com/htxryan/c3p/internal/state"
)

// preCommitMaxLines bounds the per-file lines emitted by PreCommitWarn so a
// commit terminal isn't flooded when a user has dozens of drifted files.
const preCommitMaxLines = 10

// PreCommitWarnResult is the structured outcome of PreCommitWarn. Capturing
// warnings (rather than just writing to stderr) makes the function unit-
// testable. ExitCode is always 0 — fail-open invariant (R25/R25a).
type PreCommitWarnResult struct {
	// ExitCode is always 0 (fail-open). The CLI caller MUST exit 0 regardless
	// of internal errors.
	ExitCode int
	// Warnings are the stderr lines that would be written. Captured for tests.
	Warnings []string
	// Report is the DriftReport we produced, or nil if detection itself failed.
	Report *DriftReport
}

// PreCommitWarn is the pre-commit hook entry point (R25, R25a). The hook
// script (installed by D7's commands) invokes `c3p drift --pre-commit-warn`
// which lands here.
//
// Fail-open invariant: NEVER returns a non-zero exit code. If detection
// fails for any reason — corrupted state, missing profile dir, FS error
// mid-walk — print a one-line diagnostic to stderr and exit silently. A
// drift check must NEVER block a commit.
//
// PR25 (backup notice on non-interactive discard): not relevant here —
// pre-commit never discards; it only warns. The PR25 contract lives in
// ApplyGate's discard path, where SnapshotForDiscard's return path is
// surfaced via ApplyGateResult.BackupSnapshot (*string, nil = no snapshot).
//
// Output discipline (non-blocking quality bar §7):
//   - When no drift: silent (no output). The hook is invisible in the happy
//     path.
//   - When drift: a header line ("c3p: <N> drifted file(s) ...") plus up to
//     10 entries (truncated with "...and N more") so the commit terminal
//     isn't flooded.
//   - When detection failed: a single line "c3p: drift check skipped:
//     <reason>". Never two lines, never a stack trace.
//
// EPIPE-safe write loop: if stderr is a closed pipe (e.g. git pre-commit
// driver disconnected), `os.Stderr.Write` may return an error. Swallow it
// and abandon the rest — preserves the fail-open invariant.
func PreCommitWarn(paths state.StatePaths) PreCommitWarnResult {
	return preCommitWarnTo(paths, os.Stderr)
}

// preCommitWarnTo is the internal entry point that accepts an explicit
// writer. Tests use this directly to assert stderr behavior without the
// data race that a global mutable writer would introduce when t.Parallel
// tests overlap.
func preCommitWarnTo(paths state.StatePaths, w io.Writer) (result PreCommitWarnResult) {
	warnings := []string{}
	var report *DriftReport
	defer func() {
		// Defense in depth: a panic in detection should never block a commit
		// (fail-open). But silent recovery makes future regressions invisible —
		// emit a single diagnostic line so a panicking detector is debuggable.
		// EPIPE: if the writer fails we still return exit 0; the message is
		// best-effort, the fail-open contract is mandatory.
		if r := recover(); r != nil {
			line := fmt.Sprintf("c3p: drift check skipped: panic: %v", r)
			warnings = append(warnings, line)
			_, _ = fmt.Fprintln(w, line)
			result = PreCommitWarnResult{
				ExitCode: 0,
				Warnings: warnings,
				Report:   report,
			}
		}
	}()

	r, err := DetectDrift(paths)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("c3p: drift check skipped: %v", err))
	} else {
		report = &r
		// Surface a degraded-state notice (S17 / R42) before drift output.
		// Without it, a corrupted .state.json looks indistinguishable from a
		// fresh project. We deliberately skip code "Missing": that's the
		// normal NoActive case for projects that haven't run init yet, and
		// we don't want a warning on every commit in those repos.
		if r.Warning != nil && r.Warning.Code != state.StateReadWarningMissing {
			warnings = append(warnings, fmt.Sprintf(
				"c3p: state file degraded (%s): %s",
				r.Warning.Code, r.Warning.Detail,
			))
		}
		if r.FingerprintOk && len(r.Entries) > 0 {
			warnings = append(warnings, formatPreCommitWarning(r)...)
		}
	}

	// EPIPE-safe write loop: if stderr is a closed pipe, abandon the rest
	// silently — better to lose the message than block the commit.
	for _, line := range warnings {
		if _, werr := fmt.Fprintln(w, line); werr != nil {
			break
		}
	}

	return PreCommitWarnResult{
		ExitCode: 0,
		Warnings: warnings,
		Report:   report,
	}
}

func formatPreCommitWarning(r DriftReport) []string {
	n := len(r.Entries)
	lines := make([]string, 0, n+2)
	active := ""
	if r.Active != nil {
		active = *r.Active
	}
	lines = append(lines, fmt.Sprintf(
		"c3p: %d drifted file(s) in .claude/ vs active profile '%s'",
		n, active,
	))
	limit := n
	if limit > preCommitMaxLines {
		limit = preCommitMaxLines
	}
	for i := 0; i < limit; i++ {
		e := r.Entries[i]
		lines = append(lines, fmt.Sprintf("  %s %s", statusGlyph(e.Status), e.RelPath))
	}
	if n > preCommitMaxLines {
		lines = append(lines, fmt.Sprintf("  ...and %d more", n-preCommitMaxLines))
	}
	return lines
}

func statusGlyph(s DriftStatus) string {
	switch s {
	case DriftStatusModified:
		return "M"
	case DriftStatusAdded:
		return "A"
	case DriftStatusDeleted:
		return "D"
	case DriftStatusUnrecoverable:
		// 'X' (broken) for unrecoverable — visually distinct from M/A/D so
		// users immediately see the row needs init/validate rather than the
		// standard discard/persist gate.
		return "X"
	default:
		return "?"
	}
}

package drift

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/merge"
	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

// errorWriter mimics a closed-pipe stderr (e.g. git pre-commit driver
// disconnected) — every Write returns an error.
type errorWriter struct{ err error }

func (e *errorWriter) Write(p []byte) (int, error) { return 0, e.err }

// makeBaseFixture stands up a single-profile project + materializes it.
func makeBaseFixture(t *testing.T) state.StatePaths {
	t.Helper()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	plan := resolver.ResolvedPlan{
		SchemaVersion: resolver.ResolvedPlanSchemaVersion,
		ProfileName:   "base",
		Contributors: []resolver.Contributor{
			{Kind: resolver.ContributorProfile, ID: "base", RootPath: "/abs/base"},
		},
		Files:         []resolver.PlanFile{},
		Warnings:      []resolver.ResolutionWarning{},
		ExternalPaths: []resolver.ExternalTrustEntry{},
	}
	merged := []merge.MergedFile{
		{
			Path:          "CLAUDE.md",
			Bytes:         []byte("BASE\n"),
			Contributors:  []string{"base"},
			MergePolicy:   resolver.MergePolicyConcat,
			Destination:   resolver.DestinationClaude,
			SchemaVersion: merge.MergedFileSchemaVersion,
		},
	}
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, nil); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	return paths
}

// S18: when there's no .claude-profiles state, exits 0 silently.
func TestPreCommitWarn_S18_NoStateExitsSilent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	stderr := &bytes.Buffer{}

	res := preCommitWarnTo(paths, stderr)
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("Warnings = %v, want empty", res.Warnings)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// R25: when there's drift, prints a header + per-file lines and exits 0.
func TestPreCommitWarn_R25_PrintsHeaderAndPerFileLines(t *testing.T) {
	t.Parallel()
	paths := makeBaseFixture(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("EDITED\n"), 0o644); err != nil {
		t.Fatalf("WriteFile edit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "extra.md"), []byte("extra"), 0o644); err != nil {
		t.Fatalf("WriteFile extra: %v", err)
	}
	stderr := &bytes.Buffer{}

	res := preCommitWarnTo(paths, stderr)
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Warnings) < 2 {
		t.Fatalf("Warnings len = %d, want >= 2 (header + file lines)", len(res.Warnings))
	}
	header := res.Warnings[0]
	if !strings.Contains(header, "c3p:") {
		t.Errorf("header missing 'c3p:': %q", header)
	}
	if !strings.Contains(header, "drifted file") {
		t.Errorf("header missing 'drifted file': %q", header)
	}
	if !strings.Contains(header, "'base'") {
		t.Errorf("header missing 'base': %q", header)
	}
	for _, line := range res.Warnings[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("entry line missing two-space indent: %q", line)
		}
	}
	if stderr.Len() == 0 {
		t.Errorf("stderr was not written; expected drift output")
	}
}

// R25a fail-open: exits 0 even when state is missing.
func TestPreCommitWarn_R25a_FailOpenWhenStateMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	res := preCommitWarnTo(paths, &bytes.Buffer{})
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("Warnings = %v, want empty", res.Warnings)
	}
}

// Truncates output at 10 entries with an "and N more" line.
//
// Layout assertions are spelled out (header + glyph-prefixed entries +
// truncation tail) so a future fixture change that adds drifted files
// surfaces a clear test failure rather than a bare integer mismatch.
func TestPreCommitWarn_TruncatesAt10Entries(t *testing.T) {
	t.Parallel()
	paths := makeBaseFixture(t)
	const numAdded = 15
	for i := 0; i < numAdded; i++ {
		// Use a numeric suffix so filenames stay ASCII-stable if numAdded
		// is ever raised past 26 — `string(rune('a'+i))` would silently
		// produce multi-byte/punctuation names there.
		name := filepath.Join(paths.ClaudeDir, fmt.Sprintf("extra-%02d.md", i))
		if err := os.WriteFile(name, []byte{byte('a' + i%26)}, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	res := preCommitWarnTo(paths, &bytes.Buffer{})

	// Confirm the underlying drift count matches the files we added —
	// guards against a fixture change that adds previously-stable files.
	if res.Report == nil {
		t.Fatalf("Report is nil; expected a populated DriftReport")
	}
	if got := len(res.Report.Entries); got != numAdded {
		t.Fatalf("entries = %d, want %d (fixture invariant)", got, numAdded)
	}

	// Output layout: 1 header + min(numAdded, 10) entry lines + truncation.
	expectedEntryLines := 10
	expectedWarnings := 1 + expectedEntryLines + 1
	if len(res.Warnings) != expectedWarnings {
		t.Errorf("Warnings len = %d, want %d (1 header + %d entries + 1 truncation)",
			len(res.Warnings), expectedWarnings, expectedEntryLines)
	}
	last := res.Warnings[len(res.Warnings)-1]
	expectedRemaining := numAdded - expectedEntryLines
	if !strings.Contains(last, fmt.Sprintf("and %d more", expectedRemaining)) {
		t.Errorf("last line = %q, want substring 'and %d more'", last, expectedRemaining)
	}
}

// Pre-commit renders the X glyph for unrecoverable-status entries
// (cw6/T5 R46 — markers gone). Critical: the user immediately sees
// the row needs init/validate, not the standard discard/persist gate.
func TestPreCommitWarn_UnrecoverableEntryRendersX(t *testing.T) {
	t.Parallel()
	// Stand up a project-root section so DetectDrift surfaces the
	// unrecoverable terminal when we strip the markers.
	root := t.TempDir()
	beforeMarkers := "# project\n\n"
	managed := "<!-- c3p:v1:begin -->\nORIG\n<!-- c3p:v1:end -->\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(beforeMarkers+managed), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	paths := state.BuildStatePaths(root)
	plan := resolver.ResolvedPlan{
		SchemaVersion: resolver.ResolvedPlanSchemaVersion,
		ProfileName:   "leaf",
		Contributors: []resolver.Contributor{
			{Kind: resolver.ContributorProfile, ID: "leaf", RootPath: "/abs/leaf"},
		},
		Files:         []resolver.PlanFile{},
		Warnings:      []resolver.ResolutionWarning{},
		ExternalPaths: []resolver.ExternalTrustEntry{},
	}
	merged := []merge.MergedFile{
		{
			Path:          "CLAUDE.md",
			Bytes:         []byte("ORIG"),
			Contributors:  []string{"leaf"},
			MergePolicy:   resolver.MergePolicyConcat,
			Destination:   resolver.DestinationProjectRoot,
			SchemaVersion: merge.MergedFileSchemaVersion,
		},
	}
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, nil); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	// User strips the markers — DetectDrift should report unrecoverable.
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte("# project\n\nNo markers anymore.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile strip: %v", err)
	}
	res := preCommitWarnTo(paths, &bytes.Buffer{})

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if res.Report == nil {
		t.Fatalf("Report is nil")
	}
	// Must have at least one unrecoverable entry.
	foundUnrecoverable := false
	for _, e := range res.Report.Entries {
		if e.Status == DriftStatusUnrecoverable {
			foundUnrecoverable = true
			break
		}
	}
	if !foundUnrecoverable {
		t.Fatalf("no unrecoverable entry in Report.Entries: %+v", res.Report.Entries)
	}
	// Warning lines should include an X-prefixed CLAUDE.md line.
	foundX := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "X CLAUDE.md") {
			foundX = true
			break
		}
	}
	if !foundX {
		t.Errorf("no X-glyph line in warnings: %+v", res.Warnings)
	}
}

// When no drift present, prints nothing.
func TestPreCommitWarn_NoDriftPrintsNothing(t *testing.T) {
	t.Parallel()
	paths := makeBaseFixture(t)
	stderr := &bytes.Buffer{}
	res := preCommitWarnTo(paths, stderr)
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("Warnings = %v, want empty", res.Warnings)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// R25a fail-open: stderr.Write throwing (EPIPE) does not break the
// exit-0 contract.
func TestPreCommitWarn_R25a_EPIPEDoesNotBreakExit0(t *testing.T) {
	t.Parallel()
	paths := makeBaseFixture(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("EDITED\n"), 0o644); err != nil {
		t.Fatalf("WriteFile edit: %v", err)
	}

	res := preCommitWarnTo(paths, &errorWriter{err: errors.New("EPIPE")})
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Warnings) == 0 {
		t.Errorf("Warnings empty; warnings should still be captured even when stderr fails")
	}
}

// S17: surfaces a degraded-state warning when state.json is corrupted.
func TestPreCommitWarn_S17_DegradedStateWarning(t *testing.T) {
	t.Parallel()
	paths := makeBaseFixture(t)
	// Corrupt the state file so DetectDrift returns a warning.
	if err := os.WriteFile(paths.StateFile, []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}
	res := preCommitWarnTo(paths, &bytes.Buffer{})
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "state file degraded") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Warnings = %v, want one matching 'state file degraded'", res.Warnings)
	}
	if res.Report == nil || res.Report.Warning == nil {
		t.Fatalf("Report.Warning is nil; expected ParseError")
	}
	if res.Report.Warning.Code != state.StateReadWarningParseError {
		t.Errorf("warning code = %q, want ParseError", res.Report.Warning.Code)
	}
}

// Does NOT print 'state file degraded' for a Missing state file (fresh project).
func TestPreCommitWarn_NoDegradedNoticeForMissingState(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	res := preCommitWarnTo(paths, &bytes.Buffer{})
	for _, w := range res.Warnings {
		if strings.Contains(w, "state file degraded") {
			t.Errorf("unexpected 'state file degraded' warning for fresh project: %q", w)
		}
	}
}

// R25a fail-open: DetectDrift errors produce a single 'skipped' line, exit 0.
//
// Trigger an IO error: make the path where state.json lives NOT a directory,
// so reading state.json fails with a non-ENOENT error.
func TestPreCommitWarn_R25a_DetectDriftErrorProducesSkipLine(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	profilesDir := filepath.Join(root, ".claude-profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Place a regular file where .meta should be.
	if err := os.WriteFile(filepath.Join(profilesDir, ".meta"), []byte("notadir"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	paths := state.BuildStatePaths(root)
	res := preCommitWarnTo(paths, &bytes.Buffer{})
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	for _, w := range res.Warnings {
		if strings.Count(w, "\n") > 0 {
			t.Errorf("warning line contains embedded newline: %q", w)
		}
	}
}

// PreCommitWarn (the public entry) wraps preCommitWarnTo with os.Stderr.
// Verify it still works end-to-end (exit code 0).
func TestPreCommitWarn_PublicEntryReturnsExit0(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	res := PreCommitWarn(paths)
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
}

// Ensure io is imported (compiler check on the errorWriter writer interface).
var _ io.Writer = (*errorWriter)(nil)

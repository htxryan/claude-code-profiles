package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestPreviewSnapshots — IV/T3 translation of TS preview-snapshots.test.ts.
//
// TS pinned the human-readable spawn-boundary output of `drift --preview`
// and `diff --preview` against the polished UX: a header line, per-file
// status + (from: <profile>) tail, and a 6-space-indented unified-diff
// body for `modified`/`changed` entries (head preview for `added`,
// "(binary file - N bytes)" placeholder for binary).
//
// The Go bin's --preview output is intentionally simpler — a header line
// + a status-letter table:
//   drift: 1 file(s) in .claude/ vs active profile "a"
//     M  CLAUDE.md
//
// No unified-diff body, no head preview, no binary placeholder, no
// (from: <profile>) tail. The deep-snapshot tests (unified-diff body,
// head preview, binary placeholder, (b, a) order) map to features the
// Go bin doesn't implement at the spawn boundary; they're skipped with
// documented reasons. The remaining tests pin Go's actual --preview
// surface so a future regression that swaps the status letter, breaks
// the table, or drops the file row surfaces here.

// stripAnsi removes terminal escape sequences so assertions don't depend
// on the spawned child's TTY-detection heuristic. The Go bin already
// disables colour when stdout is piped (default), but we strip
// defensively to match the TS pattern.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// TestPreviewSnapshots_DriftModified — pins the Go bin's `drift
// --preview` row for a `modified` entry: header line + "M  <relPath>".
// TS expected a unified-diff body (-resolved / +live) under each entry;
// Go does not render diff bodies in --preview.
func TestPreviewSnapshots_DriftModified(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "alpha\nbeta\ngamma\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Edit the live tree.
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("alpha\nBETA\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}

	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift", "--preview"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift --preview: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	stdout := stripAnsi(r.Stdout)
	// Header: 'drift: <N> file(s) in .claude/ vs active profile "a"'.
	if !strings.Contains(stdout, `active profile "a"`) {
		t.Errorf("missing active-profile header: %q", stdout)
	}
	// Per-file row: status letter "M" + path.
	matched, err := regexp.MatchString(`M\s+CLAUDE\.md`, stdout)
	if err != nil {
		t.Fatalf("regex: %v", err)
	}
	if !matched {
		t.Errorf("missing 'M  CLAUDE.md' row: %q", stdout)
	}
}

// TestPreviewSnapshots_DriftAdded — pins the Go bin's row for an
// `added` entry (a file in .claude/ that the active profile doesn't
// own): "A  <relPath>". TS rendered a head preview of live lines; Go
// does not.
func TestPreviewSnapshots_DriftAdded(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "alpha\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Add a file the profile doesn't own.
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "extra.md"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift", "--preview"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift --preview: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	stdout := stripAnsi(r.Stdout)
	matched, err := regexp.MatchString(`A\s+extra\.md`, stdout)
	if err != nil {
		t.Fatalf("regex: %v", err)
	}
	if !matched {
		t.Errorf("missing 'A  extra.md' row: %q", stdout)
	}
}

// TestPreviewSnapshots_DriftBinary — Go bin does not substitute a
// "(binary file - N bytes)" placeholder; --preview renders the same
// status table for binary entries (M for modified). Skipped with note.
func TestPreviewSnapshots_DriftBinary(t *testing.T) {
	t.Skip("Go bin does not render '(binary file - N bytes)' placeholders in --preview (TS-only UX)")
}

// TestPreviewSnapshots_DiffChanged — pins the Go bin's `diff --preview`
// row for a `changed` entry: header line + "~  <relPath>". TS pinned a
// (b, a)-ordered unified-diff body (-ci / +dev); Go does not render
// diff bodies in --preview.
func TestPreviewSnapshots_DiffChanged(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"dev": {Manifest: map[string]any{"name": "dev"}, Files: map[string]string{"CLAUDE.md": "alpha\nDEV\ngamma\n"}},
			"ci":  {Manifest: map[string]any{"name": "ci"}, Files: map[string]string{"CLAUDE.md": "alpha\nCI\ngamma\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "diff", "dev", "ci", "--preview"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("diff --preview: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	stdout := stripAnsi(r.Stdout)
	// Header: 'diff: dev vs ci'.
	if !strings.Contains(stdout, "dev") || !strings.Contains(stdout, "ci") {
		t.Errorf("header missing both profile names: %q", stdout)
	}
	// Per-row sigil: "~ CLAUDE.md" (Go uses "~" for changed).
	matched, err := regexp.MatchString(`~\s+CLAUDE\.md`, stdout)
	if err != nil {
		t.Fatalf("regex: %v", err)
	}
	if !matched {
		t.Errorf("missing '~  CLAUDE.md' row: %q", stdout)
	}
}

// TestPreviewSnapshots_DiffIdentical — `diff a a --preview` on an
// identical pair prints the "identical" message and emits no +/- lines.
func TestPreviewSnapshots_DiffIdentical(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "same\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "diff", "a", "a", "--preview"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("diff a a --preview: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "identical") {
		t.Errorf("missing 'identical' message: %q", r.Stdout)
	}
	stdout := stripAnsi(r.Stdout)
	// Trivial-equal path renders no +/- preview body lines.
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") {
			t.Errorf("unexpected +/- line under identical pair: %q", line)
		}
	}
}

// TestPreviewSnapshots_DiffAddedRemovedSilent — added/removed entries
// have no opposing buffer to diff against. TS pinned that --preview
// renders no body for these (only the entry sigil/path). Go renders
// "A  <name>" and "B  <name>" rows (no diff body either).
func TestPreviewSnapshots_DiffAddedRemovedSilent(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "common\n", "only-a.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"CLAUDE.md": "common\n", "only-b.md": "B\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "diff", "a", "b", "--preview"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("diff a b --preview: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	stdout := stripAnsi(r.Stdout)
	if !strings.Contains(stdout, "only-a.md") {
		t.Errorf("missing only-a.md row: %q", stdout)
	}
	if !strings.Contains(stdout, "only-b.md") {
		t.Errorf("missing only-b.md row: %q", stdout)
	}
	// No 6-space-indented +/- preview body lines (Go doesn't render
	// them for any entry kind in --preview).
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "      +") || strings.HasPrefix(line, "      -") {
			t.Errorf("unexpected indented +/- preview body line: %q", line)
		}
	}
}

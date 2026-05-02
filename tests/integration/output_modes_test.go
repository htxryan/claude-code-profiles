package integration_test

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// TestOutputModes_* — IV/T7 translation of TS output-modes.test.ts
// (PR6 #10, F2). Output-mode combinatorics:
//   { NO_COLOR env, --no-color flag, --quiet, --json } × { TTY, non-TTY }
// The Go bin runs non-TTY (pipes) by default — TTY=true cells are
// represented via FORCE_COLOR=1 env hint where applicable. Go's renderer
// never emits ANSI to a non-TTY; tests assert the property each flag
// controls (escapes / silence / JSON-shape) without snapshotting bytes.

var ansiEsc = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ── Default (non-TTY) → no ANSI ─────────────────────────────────────────

func TestOutputModes_NonTTYDefaultVersionNoAnsi(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--version"}})
	if r.ExitCode != 0 {
		t.Fatalf("--version: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if ansiEsc.MatchString(r.Stdout) {
		t.Errorf("--version stdout contains ANSI: %q", r.Stdout)
	}
}

func TestOutputModes_NonTTYDefaultInitNoAnsi(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-seed", "--no-hook"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if ansiEsc.MatchString(r.Stdout) {
		t.Errorf("init stdout contains ANSI: %q", r.Stdout)
	}
}

// ── --no-color flag (additive with NO_COLOR env) ────────────────────────

func TestOutputModes_NoColorFlagBeatsForceColor(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--no-color", "init", "--no-seed", "--no-hook"},
		Env:  map[string]string{"FORCE_COLOR": "1"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if ansiEsc.MatchString(r.Stdout) {
		t.Errorf("--no-color stdout contains ANSI: %q", r.Stdout)
	}
}

// ── NO_COLOR env (per https://no-color.org) ─────────────────────────────

func TestOutputModes_NoColorEnvDisablesAnsi(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-seed", "--no-hook"},
		Env:  map[string]string{"NO_COLOR": "1", "FORCE_COLOR": "1"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if ansiEsc.MatchString(r.Stdout) {
		t.Errorf("NO_COLOR=1 stdout contains ANSI: %q", r.Stdout)
	}
}

func TestOutputModes_NoColorEmptyStringDisablesAnsi(t *testing.T) {
	if isWindows() {
		t.Skip("empty-string env values dropped by Win32 process-creation API")
	}
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-seed", "--no-hook"},
		Env:  map[string]string{"NO_COLOR": "", "FORCE_COLOR": "1"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if ansiEsc.MatchString(r.Stdout) {
		t.Errorf("NO_COLOR='' stdout contains ANSI: %q", r.Stdout)
	}
}

// ── --quiet silences print()/warn(); preserves error()+exit codes ───────

func TestOutputModes_QuietSuccessEmptyStdout(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--quiet", "init", "--no-seed", "--no-hook"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("--quiet init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if r.Stdout != "" {
		t.Errorf("--quiet stdout not empty: %q", r.Stdout)
	}
}

func TestOutputModes_ShortQuietList(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "-q", "list"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("-q list: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if r.Stdout != "" {
		t.Errorf("-q list stdout not empty: %q", r.Stdout)
	}
}

func TestOutputModes_QuietPreservesErrors(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--quiet", "use", "nonexistent"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("--quiet use missing: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if len(r.Stderr) == 0 {
		t.Errorf("--quiet error path: stderr empty (errors must NOT be silenced)")
	}
}

// ── --json mode ─────────────────────────────────────────────────────────

func TestOutputModes_JsonStatusOneObject(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "status"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("--json status: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	if parsed == nil {
		t.Errorf("parsed is nil: %q", r.Stdout)
	}
}

func TestOutputModes_JsonListOneObject(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "x\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "list"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("--json list: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var parsed struct {
		Profiles []any `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	if parsed.Profiles == nil {
		t.Errorf("missing 'profiles' array: %q", r.Stdout)
	}
}

func TestOutputModes_JsonVersion(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--json", "--version"}})
	if r.ExitCode != 0 {
		t.Fatalf("--json --version: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var parsed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	if parsed.Version == "" {
		t.Errorf("--json --version: missing 'version' field: %q", r.Stdout)
	}
}

func TestOutputModes_JsonErrorPathStdoutClean(t *testing.T) {
	// Under --json, error envelope (if any) goes to stderr. Stdout must
	// stay valid-JSON OR empty so consumers parsing stdout never see
	// mixed content. Go bin emits empty stdout on the error path.
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "use", "nonexistent"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("--json use missing: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if r.Stdout != "" {
		// If the impl chose to emit an error-envelope, it must parse.
		var parsed any
		if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
			t.Errorf("--json error path stdout not parseable: %v\nstdout=%q", err, r.Stdout)
		}
	}
	if len(r.Stderr) == 0 {
		t.Errorf("--json error path: stderr empty")
	}
}

// ── Combinatoric edge: stacking is harmless ─────────────────────────────

func TestOutputModes_StackingJsonNoColorEnvParseable(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "--no-color", "status"},
		Env:  map[string]string{"NO_COLOR": "1"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("stacking: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var parsed any
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
}

func TestOutputModes_StackingQuietNoColorEnvByteEmpty(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--quiet", "--no-color", "init", "--no-seed", "--no-hook"},
		Env:  map[string]string{"NO_COLOR": "1"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("stacking quiet: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if r.Stdout != "" {
		t.Errorf("stacking quiet: stdout not empty: %q", r.Stdout)
	}
}

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// TestNonInteractive_* — IV/T7 translation of TS non-interactive.test.ts
// (PR6 #1, F2). True PTY-driven interactive tests deferred per PR6a; the
// pre-1.0 contract is that the gate auto-aborts (or honours --on-drift=)
// whenever the bin is run with non-TTY stdin/stdout (the default for
// `os/exec`).

// useA materialises profile `a` via the CLI (no internal Go imports).
func useA(t *testing.T, fx *helpers.Fixture) {
	t.Helper()
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

func TestNonInteractive_DriftNoFlagSingleLineNamesAllValues(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "B\n"}},
		},
	})
	useA(t, fx)
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "x.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("use b drift no-flag: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--on-drift=") {
		t.Errorf("stderr missing --on-drift= guidance: %q", r.Stderr)
	}
	for _, v := range []string{"discard", "persist", "abort"} {
		if !strings.Contains(r.Stderr, v) {
			t.Errorf("stderr missing %q: %q", v, r.Stderr)
		}
	}
}

func TestNonInteractive_InvalidOnDriftValue(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=ignore", "use", "a"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("--on-drift=ignore: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--on-drift") {
		t.Errorf("stderr missing --on-drift: %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "ignore") {
		t.Errorf("stderr missing offending value 'ignore': %q", r.Stderr)
	}
	for _, v := range []string{"discard", "persist", "abort"} {
		if !strings.Contains(r.Stderr, v) {
			t.Errorf("stderr missing alternative %q: %q", v, r.Stderr)
		}
	}
}

func TestNonInteractive_UseWithoutDrift(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "B\n"}},
		},
	})
	useA(t, fx)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b no-drift: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go bin lowercases the verb in human output ("switched to b"); TS
	// bin used "Switched to b". Pin the Go-side text.
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("stdout missing 'switched to b': %q", r.Stdout)
	}
}

func TestNonInteractive_SyncWithoutDrift(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
		},
	})
	useA(t, fx)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "sync"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("sync no-drift: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go bin emits "synced a"; TS bin used "Synced". Match Go-side text.
	if !strings.Contains(strings.ToLower(r.Stdout), "synced") {
		t.Errorf("stdout missing 'synced': %q", r.Stdout)
	}
}

func TestNonInteractive_AbortDiscriminableFromNoFlag(t *testing.T) {
	// The explicit-abort wording MUST differ from the no-flag auto-abort
	// wording — CI scripts differentiate the two paths on stderr text.
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "B\n"}},
		},
	})
	useA(t, fx)
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "x.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}

	noFlag := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b"},
	})
	if noFlag.ExitCode != 1 {
		t.Fatalf("no-flag abort: want 1, got %d (stderr=%q)", noFlag.ExitCode, noFlag.Stderr)
	}
	if len(noFlag.Stderr) == 0 {
		t.Fatal("no-flag abort: stderr empty")
	}

	// Re-stage drift for the second invocation.
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "x.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("re-write drift: %v", err)
	}

	explicit := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=abort", "use", "b"},
	})
	if explicit.ExitCode != 1 {
		t.Fatalf("--on-drift=abort: want 1, got %d (stderr=%q)", explicit.ExitCode, explicit.Stderr)
	}
	if !strings.Contains(strings.ToLower(explicit.Stderr), "abort") {
		t.Errorf("explicit-abort stderr missing 'abort': %q", explicit.Stderr)
	}
	if explicit.Stderr == noFlag.Stderr {
		t.Errorf("discriminability: explicit-abort and no-flag stderr are identical: %q", explicit.Stderr)
	}
}

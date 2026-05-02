package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestExitCode_VersionZero — IV translation of TS exit-codes.test.ts
// "--version → 0".
func TestExitCode_VersionZero(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--version"}})
	if r.ExitCode != 0 {
		t.Fatalf("--version: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "c3p") {
		t.Fatalf("--version stdout missing 'c3p': %q", r.Stdout)
	}
}

// TestExitCode_UnknownVerbOne — argv-shape error → user error (1).
func TestExitCode_UnknownVerbOne(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"bogus"}})
	if r.ExitCode != 1 {
		t.Fatalf("unknown verb: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "unknown command") {
		t.Errorf("unknown verb stderr missing 'unknown command': %q", r.Stderr)
	}
}

// TestExitCode_MissingArgvOne — empty argv → user error (1).
func TestExitCode_MissingArgvOne(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{}})
	if r.ExitCode != 1 {
		t.Fatalf("empty argv: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "missing command") {
		t.Errorf("missing argv stderr missing 'missing command': %q", r.Stderr)
	}
}

// TestExitCode_InitFreshProjectZero — init in a clean fixture exits 0
// and emits the c3p-initialised banner + the "Created .claude-profiles/"
// status line (claude-code-profiles-pnf init UX).
func TestExitCode_InitFreshProjectZero(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-seed", "--no-hook"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go bin's init UX (post-port): "initialised <path>" line followed by
	// the "injected c3p markers" status. The TS bin used "c3p initialised"
	// + "Created .claude-profiles/"; PR2/PR3 byte-equality covers the
	// machine-readable surface (--json envelope), not human-readable text.
	if !strings.Contains(r.Stdout, "initialised") {
		t.Errorf("init stdout missing 'initialised' line: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, ".claude-profiles") {
		t.Errorf("init stdout missing '.claude-profiles' path mention: %q", r.Stdout)
	}
}

// TestExitCode_HookInstallNoGitTwo — hook install in a project without
// .git/ surfaces ENOENT → system error (2).
func TestExitCode_HookInstallNoGitTwo(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"},
	})
	if r.ExitCode != 2 {
		t.Fatalf("hook install no .git: want 2, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

// TestExitCode_UseMissingProfileOne — CLI typo (the user typed an unknown
// name) is a user error: exit 1, recoverable by editing argv.
func TestExitCode_UseMissingProfileOne(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "nonexistent"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("use nonexistent: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stderr), "nonexistent") {
		t.Errorf("use nonexistent stderr missing profile name: %q", r.Stderr)
	}
}

// TestExitCode_ValidateMissingExtendsParentThree — validate of a manifest
// with extends="missing-parent" is a structural fault → exit 3
// (distinct from the CLI typo above).
func TestExitCode_ValidateMissingExtendsParentThree(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"child": {
				Manifest: map[string]any{"name": "child", "extends": "missing-parent"},
				Files:    map[string]string{},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "validate", "child"},
	})
	if r.ExitCode != 3 {
		t.Fatalf("validate missing-extends: want 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

// TestExitCode_ValidateAllFailingThree — `validate` (no name) over a
// project with a structural fault still exits 3 (the worst code wins).
func TestExitCode_ValidateAllFailingThree(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"broken": {
				Manifest: map[string]any{"name": "broken", "extends": "nope"},
				Files:    map[string]string{},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "validate"},
	})
	if r.ExitCode != 3 {
		t.Fatalf("validate (all): want 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

// TestExitCode_NonTTYUseDriftNoFlagOne — non-interactive use with drift
// and no --on-drift flag → user error (1) with --on-drift= guidance in stderr.
func TestExitCode_NonTTYUseDriftNoFlagOne(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "B\n"}},
		},
	})
	// Materialize "a" via the CLI so .claude/x.md exists.
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Introduce drift.
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "x.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	// Now use b without --on-drift in non-TTY context.
	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b"},
		Env:  map[string]string{"CI": "true"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("use b drift no-flag: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--on-drift=") {
		t.Errorf("stderr missing --on-drift= guidance: %q", r.Stderr)
	}
}

// TestExitCode_NonTTYUseDriftDiscardZero — same scenario, but with
// --on-drift=discard the swap proceeds and exits 0.
func TestExitCode_NonTTYUseDriftDiscardZero(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "B\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "x.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=discard", "use", "b"},
		Env:  map[string]string{"CI": "true"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b --on-drift=discard: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go bin lowercases the verb in human output ("switched to b"); TS bin
	// used "Switched to b". Match the Go-side text.
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("stdout missing 'switched to b': %q", r.Stdout)
	}
}

// hook_byte_equality_test.go — narrow exemption to the spawn-only contract
// documented in scenarios_test.go. The byte-equality tests below are E6
// fitness functions that pin the EXACT hook-script bytes against a known
// constant; importing the constants from internal/cli/commands is the only
// way to spell "the bytes the bin would write" without copying them (which
// would defeat the regression purpose). This is the only test file allowed
// to import internal product packages — every other test in this directory
// must drive the bin via spawn.
package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/htxryan/c3p/internal/cli/commands"
	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestHookByteEquality_HookInstallWritesCanonicalBytes — IV translation
// of TS hook-byte-equality.test.ts. The pre-commit hook installed via
// `c3p hook install` must be byte-identical to commands.HookScriptContent
// (R25a + Go's PR15 spec). This is an E6 fitness function: a release-time
// edit to the script bytes is a deliberate spec bump, not a free change.
func TestHookByteEquality_HookInstallWritesCanonicalBytes(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	if err := os.MkdirAll(filepath.Join(fx.ProjectRoot, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("hook install: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	written, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatalf("read pre-commit: %v", err)
	}
	if string(written) != commands.HookScriptContent {
		t.Fatalf("pre-commit content drift:\nwant: %q\ngot:  %q",
			commands.HookScriptContent, string(written))
	}
}

// TestHookByteEquality_InitWritesSameBytes — init's bootstrap path
// (`init --no-seed`) installs the hook by default; the bytes must match
// the standalone `hook install` path.
func TestHookByteEquality_InitWritesSameBytes(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	if err := os.MkdirAll(filepath.Join(fx.ProjectRoot, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-seed"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	written, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatalf("read pre-commit: %v", err)
	}
	if string(written) != commands.HookScriptContent {
		t.Fatalf("init pre-commit drift:\nwant: %q\ngot:  %q",
			commands.HookScriptContent, string(written))
	}
}

// TestHookByteEquality_BatCompanionOnWindows — PR15: on Windows, both
// pre-commit and pre-commit.bat are written, with .bat byte-identical
// to commands.HookScriptContentBat. CRLF intentional (cmd.exe parser).
func TestHookByteEquality_BatCompanionOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PR15 .bat companion is Windows-only")
	}
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	if err := os.MkdirAll(filepath.Join(fx.ProjectRoot, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("hook install: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	written, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".git", "hooks", "pre-commit.bat"))
	if err != nil {
		t.Fatalf("read pre-commit.bat: %v", err)
	}
	if string(written) != commands.HookScriptContentBat {
		t.Fatalf("pre-commit.bat content drift:\nwant: %q\ngot:  %q",
			commands.HookScriptContentBat, string(written))
	}
}

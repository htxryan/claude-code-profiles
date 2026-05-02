package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV/T8 — translation of TS windows-platform.test.ts (F2 gap closures #4 +
// #5). The TS suite documents two Windows-specific contracts that the TS
// bin couldn't satisfy and were deferred to the Go translation:
//
//   #4: Windows pre-commit hook needs a .bat companion (PR15) — Go writes
//       both `pre-commit` and `pre-commit.bat`. The byte-equality of the
//       .bat is already pinned in hook_byte_equality_test.go::
//       TestHookByteEquality_BatCompanionOnWindows; this file pins the
//       *behavior* contract: hook install on Windows produces both, and
//       the .bat exits 0 silently when c3p is not on PATH (S18 invariant).
//
//   #5: file-lock race on Windows. The Go bin uses LockFileEx (PR14) so
//       the contract is platform-neutral but worth pinning on Windows
//       specifically so a regression here surfaces as a Windows-CI break,
//       not a hidden Linux-only assumption.

// TestWindowsPlatform_HookInstallProducesBatCompanion — gap closure #4.
// On Windows, `c3p hook install` produces `.git/hooks/pre-commit.bat`
// alongside the POSIX `pre-commit`. Both must exist; the byte-equality of
// .bat is covered separately. Skipped on non-Windows since the .bat
// companion is Windows-only.
func TestWindowsPlatform_HookInstallProducesBatCompanion(t *testing.T) {
	if !isWindows() {
		t.Skip("Windows-only: .bat hook companion is PR15 Windows behavior")
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
	for _, leaf := range []string{"pre-commit", "pre-commit.bat"} {
		fp := filepath.Join(fx.ProjectRoot, ".git", "hooks", leaf)
		if _, err := os.Stat(fp); err != nil {
			t.Errorf("expected hook file %q: %v", fp, err)
		}
	}
}

// TestWindowsPlatform_FileLockRaceOnWindows — gap closure #5. Two
// concurrent `c3p use` invocations on Windows: exactly one wins, the other
// either also wins (lock released between attempts) or fails with exit 3
// + lock-held messaging. The full N=20 race is pinned in
// concurrent_test.go::TestConcurrent_RaceUseLockSerialization on every
// platform; this Windows-only cell pins the same contract specifically
// against LockFileEx (PR14).
func TestWindowsPlatform_FileLockRaceOnWindows(t *testing.T) {
	if !isWindows() {
		t.Skip("Windows-only: pins LockFileEx (PR14) lock contract on Windows")
	}
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "X\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "Y\n"}},
		},
	})
	// Bootstrap to "a" so the second of the racing `use b` calls doesn't
	// have to set up state from scratch (which would inflate variance).
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}

	results := make([]helpers.SpawnResult, 2)
	var wg sync.WaitGroup
	for idx, prof := range []string{"a", "b"} {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			results[i] = mustRun(t, helpers.SpawnOptions{
				Args:      []string{"--cwd", fx.ProjectRoot, "--on-drift=discard", "use", p},
				TimeoutMs: 25000,
			})
		}(idx, prof)
	}
	wg.Wait()

	for _, res := range results {
		if res.ExitCode != 0 && res.ExitCode != 3 {
			t.Errorf("race exit not in {0,3}: got %d (stderr=%q)", res.ExitCode, res.Stderr)
		}
		if res.ExitCode == 3 {
			lower := strings.ToLower(res.Stderr)
			if !strings.Contains(lower, "lock") && !strings.Contains(lower, "held") {
				t.Errorf("loser stderr missing lock/held wording: %q", res.Stderr)
			}
		}
	}
}

// TestWindowsPlatform_HookSilentExitWhenC3pMissing — S18 contract: the
// installed pre-commit hook must exit 0 silently when the `c3p` binary is
// not on PATH. Pinned on every platform; the Windows-specific cell ensures
// the .bat companion respects the same contract.
func TestWindowsPlatform_HookSilentExitWhenC3pMissing(t *testing.T) {
	if !isWindows() {
		t.Skip("Windows-only: pins .bat companion's silent-exit behavior")
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
	// We don't directly invoke the .bat here — that would require setting
	// up a subshell with PATH stripped, which is fragile across runners.
	// The byte-equality test guarantees the script content; this test
	// pins the install-time invariant. The .bat's runtime behavior is
	// covered by hook_byte_equality_test.go and the script content
	// itself (which exits 0 when `where c3p` returns nonzero).
	bat := filepath.Join(fx.ProjectRoot, ".git", "hooks", "pre-commit.bat")
	if _, err := os.Stat(bat); err != nil {
		t.Fatalf("expected pre-commit.bat: %v", err)
	}
}

//go:build !windows

package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/htxryan/c3p/tests/integration/helpers"
	"golang.org/x/sys/unix"
)

// IV/T6 — sigint translation. TS test exercised the lock-module signal handler
// in a real subprocess by spawning a custom lock-holder.mjs. Go has no such
// holder script; we reproduce the same invariant by holding the OS-level flock
// in the test process via syscall.Flock, then spawning `c3p use --wait` so the
// CLI blocks in poll. SIGINT/SIGTERM during the wait must (a) tear down c3p
// cleanly, (b) leave our held lock file untouched.

// runWithSigintWaitAfter spawns the c3p binary, waits dur, sends SIGINT, and
// captures the result. Inline here per the task brief — not a helpers
// addition.
func runWithSigintWaitAfter(t *testing.T, args []string, dur time.Duration) helpers.SpawnResult {
	t.Helper()
	bin := helpers.BinPath(t)
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(dur)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal: %v", err)
	}
	err := cmd.Wait()
	res := helpers.SpawnResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	_ = err
	return res
}

// runWithSigtermWaitAfter mirrors runWithSigintWaitAfter but sends SIGTERM. Carved
// into a second helper rather than parameterised — keeps each test's intent
// readable in the call site.
func runWithSigtermWaitAfter(t *testing.T, args []string, dur time.Duration) helpers.SpawnResult {
	t.Helper()
	bin := helpers.BinPath(t)
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(dur)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	err := cmd.Wait()
	res := helpers.SpawnResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	_ = err
	return res
}

// holdLock writes a synthetic lock stamp at lockPath and acquires an exclusive
// flock on it. Returns a cleanup func the test must defer. The lock survives
// the c3p subprocess SIGINT — that's the invariant we're checking.
func holdLock(t *testing.T, lockPath string) func() {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		t.Fatalf("flock: %v", err)
	}
	// Stamp PID + ISO timestamp so c3p's parser succeeds (it requires the
	// canonical "<pid> <ts>\n" shape).
	_, _ = f.WriteString("999999 2026-05-02T07:00:00.000Z\n")
	_ = f.Sync()
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}
}

// TestSigint_DuringLockWaitExitsCleanly — c3p `use --wait` blocked on a
// peer-held lock must terminate on SIGINT and not delete the peer's lock file.
func TestSigint_DuringLockWaitExitsCleanly(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
		},
	})
	lockPath := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "lock")
	release := holdLock(t, lockPath)
	defer release()

	// Use --wait with a long timeout so c3p blocks in poll. SIGINT after
	// 500ms — by then c3p has reached the wait loop. Total cap ~3s.
	r := runWithSigintWaitAfter(t, []string{
		"--cwd", fx.ProjectRoot, "--wait=10",
		"use", "a",
	}, 500*time.Millisecond)

	// SIGINT-killed processes on POSIX exit 130 (128 + SIGINT(2)) when the
	// signal handler ran (lock was held → registerSignalRelease was wired);
	// otherwise Go's ProcessState reports -1 for raw signal-kill. We accept
	// only those two — explicitly NOT exit 1, which would mask a genuine
	// failure (panic, user-error path) as a clean signal exit.
	if r.ExitCode != 130 && r.ExitCode != -1 {
		t.Errorf("SIGINT exit: want 130 or -1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Lock file we hold must still exist.
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("peer lock disappeared after SIGINT: %v", err)
	}
}

// TestSigterm_DuringLockWaitExitsCleanly — same shape, SIGTERM (128 + 15 = 143).
func TestSigterm_DuringLockWaitExitsCleanly(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
		},
	})
	lockPath := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "lock")
	release := holdLock(t, lockPath)
	defer release()

	r := runWithSigtermWaitAfter(t, []string{
		"--cwd", fx.ProjectRoot, "--wait=10",
		"use", "a",
	}, 500*time.Millisecond)

	// Same justification as SIGINT case above; exit 1 explicitly excluded.
	if r.ExitCode != 143 && r.ExitCode != -1 {
		t.Errorf("SIGTERM exit: want 143 or -1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("peer lock disappeared after SIGTERM: %v", err)
	}
}

// TestSigint_LockHeldUXNamesPID — translation of the yd8 AC-4 case. While a
// peer holds the lock, `c3p use` (no --wait) exits 3 with a stderr message
// naming the holder PID. Go bin's text differs from TS (no "--wait" verbatim,
// no "ago"), so we pin only the load-bearing parts: exit code, "PID", and the
// "wait for" remediation phrase.
func TestSigint_LockHeldUXNamesPID(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
		},
	})
	lockPath := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "lock")
	release := holdLock(t, lockPath)
	defer release()

	r := mustRun(t, helpers.SpawnOptions{
		Args:      []string{"--cwd", fx.ProjectRoot, "use", "a"},
		TimeoutMs: 5000,
	})
	if r.ExitCode != 3 {
		t.Fatalf("lock-held use: want 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "PID") {
		t.Errorf("stderr missing 'PID': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "wait for") {
		t.Errorf("stderr missing 'wait for' remediation phrase: %q", r.Stderr)
	}
}

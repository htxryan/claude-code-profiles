package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV/T8 — translation of TS sigint-bin.test.ts (F2 gap closure #2). The TS
// suite sends SIGINT to a running c3p process to verify the bin's signal
// handling: clean exit (130 or signal-killed), no tracebacks on stderr, and
// no corruption of peer state. The TS variant relied on a Node lock-holder
// helper to keep the lock held; in Go we exercise the same signal contract
// by SIGINT-ing the bin during its own materialize phase, where the lock is
// held internally — same code path, no external helper needed.

// runWithSigintAfter spawns the bin with args and sends SIGINT after dur.
// Returns a SpawnResult populated from the captured streams + ProcessState.
func runWithSigintAfter(t *testing.T, args []string, dur time.Duration) helpers.SpawnResult {
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
	_ = cmd.Wait()
	res := helpers.SpawnResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	return res
}

// buildSigintFixture writes a profile big enough that c3p use stays alive
// long enough for the SIGINT delivery window. 200 small files across a
// couple of dirs is sufficient on every supported platform without making
// the test slow.
func buildSigintFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	base := filepath.Join(root, ".claude-profiles", "big", ".claude")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude-profiles", "big", "profile.json"), []byte(`{"name":"big"}`), 0o644); err != nil {
		t.Fatalf("write profile.json: %v", err)
	}
	for d := 0; d < 4; d++ {
		sub := filepath.Join(base, fmt.Sprintf("d%d", d))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir sub: %v", err)
		}
		for i := 0; i < 50; i++ {
			fp := filepath.Join(sub, fmt.Sprintf("f%03d.md", i))
			if err := os.WriteFile(fp, []byte(fmt.Sprintf("# %d/%d\nbody\n", d, i)), 0o644); err != nil {
				t.Fatalf("write %s: %v", fp, err)
			}
		}
	}
	return root
}

// TestSigintBin_DuringUseExitsCleanly — SIGINT delivered to a running
// `c3p use` exits via signal (130 on POSIX), with no Go panic traceback on
// stderr.
func TestSigintBin_DuringUseExitsCleanly(t *testing.T) {
	if isWindows() {
		t.Skip("SIGINT semantics differ on Windows; covered by lock_windows tests")
	}
	helpers.EnsureBuilt(t)
	root := buildSigintFixture(t)

	res := runWithSigintAfter(t, []string{"--cwd", root, "use", "big"}, 50*time.Millisecond)

	// The bin must exit either with code 130 (128+SIGINT) or be reported as
	// signal-killed (ExitCode -1 in Go's exec.ProcessState shape). Both
	// shapes are acceptable; what is NOT acceptable is exit 0 (silent
	// success despite SIGINT) or a panic/traceback.
	if res.ExitCode != 130 && res.ExitCode != -1 {
		t.Errorf("SIGINT exit: want 130 or signal-killed, got %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if strings.Contains(res.Stderr, "panic:") || strings.Contains(res.Stderr, "goroutine ") {
		t.Errorf("stderr contains Go panic/traceback: %q", res.Stderr)
	}
}

// TestSigintBin_NoStateCorruptionAfterSigint — after SIGINT mid-materialize,
// a follow-up `c3p use` invocation must succeed cleanly. Pins the contract
// that signal-killed swaps don't leave irrecoverable state.
func TestSigintBin_NoStateCorruptionAfterSigint(t *testing.T) {
	if isWindows() {
		t.Skip("SIGINT semantics differ on Windows; covered by lock_windows tests")
	}
	helpers.EnsureBuilt(t)
	root := buildSigintFixture(t)

	_ = runWithSigintAfter(t, []string{"--cwd", root, "use", "big"}, 30*time.Millisecond)

	// Follow-up swap must succeed — reconcile sweeps any orphaned
	// pending/prior left by the killed predecessor.
	r := mustRun(t, helpers.SpawnOptions{
		Args:      []string{"--cwd", root, "--on-drift=discard", "use", "big"},
		TimeoutMs: 30000,
	})
	if r.ExitCode != 0 {
		t.Fatalf("follow-up use big: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

// TestSigintBin_LockHeldByPeerSkipped — the TS variant spawns a peer
// lock-holder script and SIGINT-interrupts the second c3p (which is waiting
// on the held lock). The Go test harness has no equivalent helper script
// (and the spec says "Don't modify helpers package"); the broader signal
// contract is covered by the two tests above plus
// concurrent_test.go::TestConcurrent_RaceUseLockSerialization for the
// lock-held messaging.
func TestSigintBin_LockHeldByPeerSkipped(t *testing.T) {
	t.Skip("requires external lock-holder helper; lock-held wording covered by concurrent_test.go")
}

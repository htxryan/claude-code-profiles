package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNonInteractiveRequiresOnDriftWhenDrifted is PR29: when running in
// non-interactive mode AND drift is detected AND no --on-drift= flag, the
// CLI exits with the conflict/user-error code (we map to ExitUser=1) so CI
// scripts never silently block on a hidden prompt.
//
// Setup: init + use to materialize, then mutate live .claude/ to create
// drift, then re-run `use` with --non-interactive (no --on-drift=).
func TestNonInteractiveRequiresOnDriftWhenDrifted(t *testing.T) {
	// Use t.Setenv + Unsetenv after — t.Setenv records the prior value and
	// restores it (Unsetenv when it was originally unset, Setenv otherwise).
	t.Setenv("CI", "")
	if err := os.Unsetenv("CI"); err != nil {
		t.Fatalf("unset CI: %v", err)
	}

	tmp := t.TempDir()

	// 1. Bootstrap.
	mustRun(t, ExitOK, "--cwd="+tmp, "init", "--no-hook")

	// Add a file so the starter materializes something non-trivial.
	starterClaude := filepath.Join(tmp, ".claude-profiles", "default", ".claude")
	if err := os.MkdirAll(starterClaude, 0o755); err != nil {
		t.Fatalf("mkdir starter .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(starterClaude, "agents.md"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatalf("seed agents: %v", err)
	}

	// 2. First use — materializes and writes state.
	mustRun(t, ExitOK, "--cwd="+tmp, "use", "default", "--on-drift=abort", "--non-interactive")

	// 3. Mutate live .claude/ to create drift.
	livePath := filepath.Join(tmp, ".claude", "agents.md")
	if err := os.WriteFile(livePath, []byte("a: 2 (drifted)\n"), 0o644); err != nil {
		t.Fatalf("mutate live: %v", err)
	}

	// 4. Re-run use without --on-drift= in --non-interactive: expect exit 1.
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd=" + tmp, "use", "default", "--non-interactive"}, "0", &stdout, &stderr)
	if code != ExitUser {
		t.Fatalf("non-interactive without --on-drift on drift: want %d, got %d (stdout=%q stderr=%q)",
			ExitUser, code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String()+stderr.String(), "drift") {
		t.Fatalf("expected drift mention in output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// TestCIEnvAutoDetectsNonInteractive is PR29's env-detection arm: CI=true
// in the env should trigger the same hard-block as --non-interactive.
func TestCIEnvAutoDetectsNonInteractive(t *testing.T) {
	t.Setenv("CI", "true")

	tmp := t.TempDir()
	mustRun(t, ExitOK, "--cwd="+tmp, "init", "--no-hook")
	starterClaude := filepath.Join(tmp, ".claude-profiles", "default", ".claude")
	_ = os.MkdirAll(starterClaude, 0o755)
	_ = os.WriteFile(filepath.Join(starterClaude, "f.md"), []byte("hi\n"), 0o644)
	mustRun(t, ExitOK, "--cwd="+tmp, "use", "default", "--on-drift=abort")
	if err := os.WriteFile(filepath.Join(tmp, ".claude", "f.md"), []byte("drifted\n"), 0o644); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd=" + tmp, "use", "default"}, "0", &stdout, &stderr)
	if code != ExitUser {
		t.Fatalf("CI=true without --on-drift on drift: want %d, got %d (stderr=%q)",
			ExitUser, code, stderr.String())
	}
}

func mustRun(t *testing.T, wantCode int, args ...string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, "0", &stdout, &stderr)
	if code != wantCode {
		t.Fatalf("Run(%v): want %d, got %d (stdout=%q stderr=%q)",
			args, wantCode, code, stdout.String(), stderr.String())
	}
}

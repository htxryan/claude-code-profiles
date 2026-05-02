package commands

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// captureOutput is a minimal OutputChannel double for command tests. It
// records each call so assertions can inspect what the command emitted
// without coupling to the shape of the json/print streams.
type captureOutput struct {
	prints []string
	jsons  []interface{}
	warns  []string
	errs   []string
	phases []string
	json   bool
}

func (c *captureOutput) Print(text string)        { c.prints = append(c.prints, text) }
func (c *captureOutput) JSON(payload interface{}) { c.jsons = append(c.jsons, payload) }
func (c *captureOutput) Warn(text string)         { c.warns = append(c.warns, text) }
func (c *captureOutput) Error(text string)        { c.errs = append(c.errs, text) }
func (c *captureOutput) Phase(text string)        { c.phases = append(c.phases, text) }
func (c *captureOutput) JSONMode() bool           { return c.json }
func (c *captureOutput) IsTTY() bool              { return false }

// makeGitRepo seeds a tempdir with a real .git directory so resolveHookPath
// finds .git/hooks/. We don't shell out to `git init` — just create the
// minimum on-disk shape (a directory named .git) the hook resolver needs.
func makeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	return dir
}

// withBatCompanionEnabled forces the .bat companion code path on regardless
// of the host OS so the install/uninstall paths can be exercised on POSIX
// CI runners. Restores the original predicate on cleanup.
func withBatCompanionEnabled(t *testing.T, enabled bool) {
	t.Helper()
	prev := shouldInstallBatCompanion
	shouldInstallBatCompanion = func() bool { return enabled }
	t.Cleanup(func() { shouldInstallBatCompanion = prev })
}

// TestRunHook_Install_WritesPosixOnly verifies the default POSIX path: no
// .bat companion is created when the host predicate says so.
//
// Cannot t.Parallel: shouldInstallBatCompanion is package-global; the
// matching companion-on tests would race against this one's predicate.
func TestRunHook_Install_WritesPosixOnly(t *testing.T) {
	withBatCompanionEnabled(t, false)
	repo := makeGitRepo(t)
	out := &captureOutput{}
	code, err := RunHook(HookOptions{Cwd: repo, Output: out, Action: "install"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	got, rerr := os.ReadFile(hook)
	if rerr != nil {
		t.Fatalf("read hook: %v", rerr)
	}
	if string(got) != HookScriptContent {
		t.Errorf("hook content drift")
	}
	bat := filepath.Join(repo, ".git", "hooks", "pre-commit.bat")
	if _, statErr := os.Stat(bat); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf(".bat companion materialised on POSIX path: %v", statErr)
	}
}

// TestRunHook_Install_WritesBatOnWindowsHost verifies PR15: when the host
// predicate is true, install writes both the POSIX hook AND the .bat
// companion with byte-frozen content.
func TestRunHook_Install_WritesBatOnWindowsHost(t *testing.T) {
	withBatCompanionEnabled(t, true)
	repo := makeGitRepo(t)
	out := &captureOutput{}
	code, err := RunHook(HookOptions{Cwd: repo, Output: out, Action: "install"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	bat := filepath.Join(repo, ".git", "hooks", "pre-commit.bat")
	got, rerr := os.ReadFile(bat)
	if rerr != nil {
		t.Fatalf("read bat: %v", rerr)
	}
	if string(got) != HookScriptContentBat {
		t.Errorf(".bat companion content drift")
	}
}

// TestRunHook_Install_BatNoopWhenAlreadyOurs verifies the install path is
// idempotent for the .bat companion.
func TestRunHook_Install_BatNoopWhenAlreadyOurs(t *testing.T) {
	withBatCompanionEnabled(t, true)
	repo := makeGitRepo(t)
	out := &captureOutput{}
	if _, err := RunHook(HookOptions{Cwd: repo, Output: out, Action: "install"}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	out2 := &captureOutput{}
	code, err := RunHook(HookOptions{Cwd: repo, Output: out2, Action: "install"})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

// TestRunHook_Install_BatRefusesForeignBytesWithoutForce verifies the .bat
// companion respects --force semantics: a non-matching pre-existing .bat
// causes the install to refuse without --force.
func TestRunHook_Install_BatRefusesForeignBytesWithoutForce(t *testing.T) {
	withBatCompanionEnabled(t, true)
	repo := makeGitRepo(t)
	bat := filepath.Join(repo, ".git", "hooks", "pre-commit.bat")
	if err := os.WriteFile(bat, []byte("@echo foreign\r\n"), 0o755); err != nil {
		t.Fatalf("seed foreign bat: %v", err)
	}
	out := &captureOutput{}
	code, err := RunHook(HookOptions{Cwd: repo, Output: out, Action: "install"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("want UserError, got %v", err)
	}
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(ue.Message, "pre-commit.bat") {
		t.Errorf("UserError message %q should mention pre-commit.bat", ue.Message)
	}

	// With --force the install proceeds and overwrites both files.
	out2 := &captureOutput{}
	if _, err := RunHook(HookOptions{Cwd: repo, Output: out2, Action: "install", Force: true}); err != nil {
		t.Fatalf("install --force: %v", err)
	}
	got, _ := os.ReadFile(bat)
	if string(got) != HookScriptContentBat {
		t.Errorf("--force did not overwrite .bat content")
	}
}

// TestRunHook_Uninstall_RemovesBat verifies PR15 uninstall path: the .bat
// companion is removed when its bytes match the frozen content.
func TestRunHook_Uninstall_RemovesBat(t *testing.T) {
	withBatCompanionEnabled(t, true)
	repo := makeGitRepo(t)
	if _, err := RunHook(HookOptions{Cwd: repo, Output: &captureOutput{}, Action: "install"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := RunHook(HookOptions{Cwd: repo, Output: &captureOutput{}, Action: "uninstall"}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	bat := filepath.Join(repo, ".git", "hooks", "pre-commit.bat")
	if _, statErr := os.Stat(bat); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf(".bat companion still present after uninstall: %v", statErr)
	}
}

// TestRunHook_Uninstall_LeavesForeignBatAlone verifies a user-edited .bat
// is NOT removed by uninstall (mirrors the POSIX rule).
func TestRunHook_Uninstall_LeavesForeignBatAlone(t *testing.T) {
	withBatCompanionEnabled(t, true)
	repo := makeGitRepo(t)
	bat := filepath.Join(repo, ".git", "hooks", "pre-commit.bat")
	foreign := []byte("@echo edited by user\r\n")
	if err := os.WriteFile(bat, foreign, 0o755); err != nil {
		t.Fatalf("seed foreign: %v", err)
	}
	if _, err := RunHook(HookOptions{Cwd: repo, Output: &captureOutput{}, Action: "uninstall"}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	got, rerr := os.ReadFile(bat)
	if rerr != nil {
		t.Fatalf("read bat after uninstall: %v", rerr)
	}
	if string(got) != string(foreign) {
		t.Errorf("uninstall mutated foreign .bat: got %q want %q", got, foreign)
	}
}

// TestRunHook_Install_AtomicWhenBatBlocksMissingPosix is the D8 regression:
// when POSIX is missing AND a foreign .bat exists without --force, the
// install must refuse without writing the POSIX hook to disk. Earlier
// (8ac93b8) the POSIX write happened before the bat pre-flight, leaving a
// half-installed state.
func TestRunHook_Install_AtomicWhenBatBlocksMissingPosix(t *testing.T) {
	withBatCompanionEnabled(t, true)
	repo := makeGitRepo(t)
	bat := filepath.Join(repo, ".git", "hooks", "pre-commit.bat")
	if err := os.WriteFile(bat, []byte("@echo foreign\r\n"), 0o755); err != nil {
		t.Fatalf("seed foreign bat: %v", err)
	}
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	out := &captureOutput{}
	code, err := RunHook(HookOptions{Cwd: repo, Output: out, Action: "install"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("want UserError, got %v", err)
	}
	if _, statErr := os.Stat(hook); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("POSIX hook written despite bat refusal — install was non-atomic: %v", statErr)
	}
}

// TestRunHook_Uninstall_AtomicWhenBatReadFails mirrors the install
// atomicity guarantee: if the bat read fails with a non-ErrNotExist error,
// neither file is removed. We simulate by chmod-ing the bat to be
// unreadable; on POSIX a directory the test owns can be made unreadable
// without root.
func TestRunHook_Uninstall_AtomicWhenBatReadFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based unreadable simulation does not apply on Windows")
	}
	withBatCompanionEnabled(t, true)
	repo := makeGitRepo(t)
	if _, err := RunHook(HookOptions{Cwd: repo, Output: &captureOutput{}, Action: "install"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	// Make the hooks directory non-readable so opening the bat returns
	// EACCES instead of ErrNotExist. Restore in cleanup so t.TempDir can
	// clean up.
	hooksDir := filepath.Dir(hook)
	if err := os.Chmod(hooksDir, 0o111); err != nil {
		t.Fatalf("chmod hooks dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(hooksDir, 0o755) })

	out := &captureOutput{}
	code, _ := RunHook(HookOptions{Cwd: repo, Output: out, Action: "uninstall"})
	// Restore so we can stat — the hook should still be present.
	if err := os.Chmod(hooksDir, 0o755); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	if code == 0 {
		t.Fatalf("expected non-zero exit on bat read failure, got 0")
	}
	if _, err := os.Stat(hook); err != nil {
		t.Errorf("POSIX hook removed despite bat read failure — uninstall was non-atomic: %v", err)
	}
}

// TestEmitHookResultWithBat_FullNoopSuppressesBatLine pins the human-mode
// formatting decision: when the install path concludes with both POSIX and
// bat at noop, the note line summarises the full state and the bat line is
// suppressed.
func TestEmitHookResultWithBat_FullNoopSuppressesBatLine(t *testing.T) {
	t.Parallel()
	out := &captureOutput{}
	emitHookResultWithBat(
		HookOptions{Output: out},
		"noop", "/path/pre-commit",
		"noop", "/path/pre-commit.bat",
		"hook is already installed",
	)
	if len(out.prints) != 1 {
		t.Fatalf("want 1 line, got %d: %v", len(out.prints), out.prints)
	}
	if !strings.Contains(out.prints[0], "hook is already installed") {
		t.Errorf("note missing from line: %q", out.prints[0])
	}
}

// TestEmitHookResultWithBat_AsymmetricKeepsBatLine pins the contrasting
// case: when posix is noop and bat is "installed" (or vice-versa), keep
// the bat line so the user sees the asymmetry.
func TestEmitHookResultWithBat_AsymmetricKeepsBatLine(t *testing.T) {
	t.Parallel()
	out := &captureOutput{}
	emitHookResultWithBat(
		HookOptions{Output: out},
		"noop", "/path/pre-commit",
		"installed", "/path/pre-commit.bat",
		"",
	)
	if len(out.prints) != 2 {
		t.Fatalf("want 2 lines, got %d: %v", len(out.prints), out.prints)
	}
	if !strings.Contains(out.prints[1], "installed: /path/pre-commit.bat") {
		t.Errorf("bat line missing or malformed: %q", out.prints[1])
	}
}

// TestRunHook_HostDefaultMatchesGOOS confirms the default predicate keys
// off runtime.GOOS == "windows" so the production path is correct without
// ever calling withBatCompanionEnabled.
func TestRunHook_HostDefaultMatchesGOOS(t *testing.T) {
	want := runtime.GOOS == "windows"
	got := shouldInstallBatCompanion()
	if got != want {
		t.Fatalf("shouldInstallBatCompanion() = %v, runtime.GOOS = %q (want %v)", got, runtime.GOOS, want)
	}
}

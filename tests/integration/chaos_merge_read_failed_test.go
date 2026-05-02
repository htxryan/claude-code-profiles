package integration_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// IV translation of tests/cli/integration/chaos-merge-read-failed.test.ts.
// Triggers MergeReadFailedError by chmod 000 on a contributor file that the
// resolver enumerates (readdir doesn't need read perms) but merge can't
// read. Pins:
//   1. Exit code 2 (system error).
//   2. No partial writes (.pending/.prior absent; live .claude/ byte-identical
//      pre/post; even with no prior state, .claude/ is not created mid-failure).
//   3. The lock is recoverable: a follow-up `use` succeeds without manual
//      cleanup (the Go bin keeps a lock-record file but it does not block
//      subsequent runs — equivalent semantics to TS R41c at the user level).
//
// chmod 000 is a no-op on Windows; the tests skip there.

// chmodAllReadable best-effort restores rwx so t.TempDir cleanup can rm
// the chmod-000 file. Mirrors the TS afterEach.
func chmodAllReadable(root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = os.Chmod(p, 0o755)
		} else {
			_ = os.Chmod(p, 0o644)
		}
		return nil
	})
}

// chaosReadTree mirrors readTree from root_claude_md_preflight_test.go but
// is duplicated here to keep the file self-contained (the other helper lives
// in a different test source).
func chaosReadTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readTree %q: %v", root, err)
	}
	return out
}

func chaosAssertNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err == nil {
		t.Errorf("expected %q absent, got present", p)
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %q: %v", p, err)
	}
}

// TestChaosMergeReadFailed_AbortsCleanly — `use b` with a chmod-000
// contributor file → exit 2; .pending/.prior absent; live .claude/
// byte-identical pre/post; follow-up `use b` succeeds after restore.
func TestChaosMergeReadFailed_AbortsCleanly(t *testing.T) {
	if isWindows() {
		t.Skip("chmod 000 is a no-op on Windows; chaos path is POSIX-only")
	}
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {
				Manifest: map[string]any{"name": "a"},
				Files:    map[string]string{"CLAUDE.md": "A\n"},
			},
			"b": {
				Manifest: map[string]any{"name": "b"},
				Files:    map[string]string{"CLAUDE.md": "B\n", "agents/x.md": "X\n"},
			},
		},
	})
	t.Cleanup(func() { chmodAllReadable(filepath.Join(fx.ProjectRoot, ".claude-profiles")) })

	// Materialize `a` so .claude/ has a defined pre-state.
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "a"}})
	if r.ExitCode != 0 {
		t.Fatalf("use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}

	claudeDir := filepath.Join(fx.ProjectRoot, ".claude")
	preClaude := chaosReadTree(t, claudeDir)

	targetFile := filepath.Join(fx.ProjectRoot, ".claude-profiles", "b", ".claude", "agents", "x.md")
	if err := os.Chmod(targetFile, 0o000); err != nil {
		t.Fatalf("chmod 000: %v", err)
	}

	r = mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r.ExitCode != 2 {
		t.Fatalf("use b chaos: want 2, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Error message names the relPath so user can find offender.
	if !(strings.Contains(r.Stderr, "agents/x.md") || strings.Contains(r.Stderr, "x.md")) {
		t.Errorf("stderr missing 'x.md': %q", r.Stderr)
	}

	// Atomic abort: no partial writes.
	metaDir := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta")
	chaosAssertNotExist(t, filepath.Join(metaDir, "pending"))
	chaosAssertNotExist(t, filepath.Join(metaDir, "prior"))

	if post := chaosReadTree(t, claudeDir); !reflect.DeepEqual(post, preClaude) {
		t.Errorf("live .claude/ mutated on abort: pre=%v post=%v", preClaude, post)
	}

	// Restore so follow-up doesn't trip the same chaos.
	if err := os.Chmod(targetFile, 0o644); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}

	// Lock-record file may persist (Go bin keeps it as a stale-holder record);
	// what matters is the next `use` succeeds without manual cleanup (R41c
	// observable behavior).
	r2 := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r2.ExitCode != 0 {
		t.Fatalf("retry use b: want 0, got %d (stderr=%q)", r2.ExitCode, r2.Stderr)
	}
	if !strings.Contains(strings.ToLower(r2.Stdout), "switched to b") {
		t.Errorf("retry stdout missing 'switched to b': %q", r2.Stdout)
	}
	got, err := os.ReadFile(filepath.Join(claudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read .claude/CLAUDE.md: %v", err)
	}
	if string(got) != "B\n" {
		t.Errorf(".claude/CLAUDE.md = %q, want %q", string(got), "B\n")
	}
}

// TestChaosMergeReadFailed_NoPriorState — chaos before any active
// materialize: live .claude/ must not be created mid-failure; no state.json
// or .pending in .meta/.
func TestChaosMergeReadFailed_NoPriorState(t *testing.T) {
	if isWindows() {
		t.Skip("chmod 000 is a no-op on Windows; chaos path is POSIX-only")
	}
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"b": {
				Manifest: map[string]any{"name": "b"},
				Files:    map[string]string{"CLAUDE.md": "B\n"},
			},
		},
	})
	t.Cleanup(func() { chmodAllReadable(filepath.Join(fx.ProjectRoot, ".claude-profiles")) })

	claudeDir := filepath.Join(fx.ProjectRoot, ".claude")
	chaosAssertNotExist(t, claudeDir)

	targetFile := filepath.Join(fx.ProjectRoot, ".claude-profiles", "b", ".claude", "CLAUDE.md")
	if err := os.Chmod(targetFile, 0o000); err != nil {
		t.Fatalf("chmod 000: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r.ExitCode != 2 {
		t.Fatalf("use b chaos no-prior: want 2, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// .claude/ must not have been created mid-failure.
	chaosAssertNotExist(t, claudeDir)

	metaDir := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta")
	chaosAssertNotExist(t, filepath.Join(metaDir, "pending"))
	// No state.json — the swap aborted before the post-swap state-write.
	chaosAssertNotExist(t, filepath.Join(metaDir, "state.json"))

	// Restore so cleanup works.
	if err := os.Chmod(targetFile, 0o644); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

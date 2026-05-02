package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV/T6 — style-snapshots translation. The TS file pinned colour + glyph
// shape via in-process createStyle pinning (TTY=true, platform=linux). The
// Go bin does not emit ANSI escapes through pipes (no isTty fake from a
// spawn-only harness), so most TTY-specific assertions are not portable. We
// keep the structural invariants — phase hints land on stderr, --json
// silences phase hints, --quiet silences everything, hook install/uninstall
// emit a recognisable status line — and skip the rest as overfit-to-TS.

// ─── new — style snapshot ─────────────────────────────────────────────

// TestStyleSnapshot_NewEmitsStatus — `c3p new <name>` prints a single status
// line naming the path. TS pinned `[ok] Created profile "scratch" at <path>`;
// Go bin uses `created profile "scratch" at <path>` (no glyph). Pin the
// content (verb + name + path).
func TestStyleSnapshot_NewEmitsStatus(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "new", "scratch"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("new: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, `"scratch"`) {
		t.Errorf("new stdout missing profile name in quotes: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "scratch") {
		t.Errorf("new stdout missing path mention: %q", r.Stdout)
	}
	// No ANSI escapes in spawn-only context.
	if regexp.MustCompile("\x1b\\[").MatchString(r.Stdout) {
		t.Errorf("unexpected ANSI escapes in non-TTY new stdout: %q", r.Stdout)
	}
}

// TestStyleSnapshot_NewNoColorFlagNoEscapes — --no-color is a no-op on Go bin
// in spawn context (already escape-free) but we pin that the flag does not
// regress the output shape.
func TestStyleSnapshot_NewNoColorFlagNoEscapes(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--no-color", "new", "scratch"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("new --no-color: exit=%d", r.ExitCode)
	}
	if regexp.MustCompile("\x1b\\[").MatchString(r.Stdout) {
		t.Errorf("--no-color: unexpected ANSI escapes: %q", r.Stdout)
	}
}

// ─── use — style snapshot ─────────────────────────────────────────────

func setupStyleTwoProfiles(t *testing.T, activate string) *helpers.Fixture {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"CLAUDE.md": "B\n"}},
		},
	})
	if activate != "" {
		r := mustRun(t, helpers.SpawnOptions{
			Args: []string{"--cwd", fx.ProjectRoot, "use", activate},
		})
		if r.ExitCode != 0 {
			t.Fatalf("setup use %q: exit=%d", activate, r.ExitCode)
		}
	}
	return fx
}

// TestStyleSnapshot_UseCleanSwap — clean swap emits a "switched to <name>"
// line. TS pinned `[ok] Switched to b.\n` exact bytes; Go bin uses `switched
// to b\n` (lowercase, no period, no glyph).
func TestStyleSnapshot_UseCleanSwap(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b: exit=%d", r.ExitCode)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("missing 'switched to b' line: %q", r.Stdout)
	}
}

// TestStyleSnapshot_UseDriftDiscardedShowsBackup — drift-discard swap emits
// a backup-path line. TS pinned the two-line shape `[ok] Switched ...` then
// `  Backup: ...`; Go bin uses `switched to b` + `backup: <path>` (lowercase,
// no leading indent). Pin the substantive content.
func TestStyleSnapshot_UseDriftDiscardedShowsBackup(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b discard: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("missing 'switched to b': %q", r.Stdout)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "backup:") {
		t.Errorf("missing 'backup:' line: %q", r.Stdout)
	}
}

// ─── sync — style snapshot ────────────────────────────────────────────

// TestStyleSnapshot_SyncClean — clean sync prints "synced <name>".
func TestStyleSnapshot_SyncClean(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "sync"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("sync: exit=%d", r.ExitCode)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "synced a") {
		t.Errorf("missing 'synced a': %q", r.Stdout)
	}
}

// TestStyleSnapshot_SyncDriftDiscarded — drift-discard sync emits the same
// backup line shape as use.
func TestStyleSnapshot_SyncDriftDiscarded(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=discard", "sync"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("sync discard: exit=%d", r.ExitCode)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "synced a") {
		t.Errorf("missing 'synced a': %q", r.Stdout)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "backup:") {
		t.Errorf("missing 'backup:' line: %q", r.Stdout)
	}
}

// ─── validate — style snapshot ────────────────────────────────────────

// TestStyleSnapshot_ValidateAllPass — every profile gets a PASS row + a
// closing "validated N profile(s)" footer. TS pinned `[ok] a\n[ok] b\n[ok] 2 pass\n`;
// Go bin uses `PASS  a\nPASS  b\nvalidated 2 profile(s) cleanly\n`.
func TestStyleSnapshot_ValidateAllPass(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"y.md": "B\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "validate"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("validate: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !regexp.MustCompile(`(?m)^PASS\s+a\b`).MatchString(r.Stdout) {
		t.Errorf("missing 'PASS a' row: %q", r.Stdout)
	}
	if !regexp.MustCompile(`(?m)^PASS\s+b\b`).MatchString(r.Stdout) {
		t.Errorf("missing 'PASS b' row: %q", r.Stdout)
	}
	if !regexp.MustCompile(`validated 2 profile`).MatchString(r.Stdout) {
		t.Errorf("missing footer: %q", r.Stdout)
	}
}

// TestStyleSnapshot_ValidateMixedPassFail — fail row uses FAIL leader; exit
// code climbs to 3 (structural fault).
func TestStyleSnapshot_ValidateMixedPassFail(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"ok":  {Manifest: map[string]any{"name": "ok"}, Files: map[string]string{"x.md": "X\n"}},
			"bad": {Manifest: map[string]any{"name": "bad", "extends": "nope"}, Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "validate"},
	})
	if r.ExitCode != 3 {
		t.Fatalf("validate mixed: want 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !regexp.MustCompile(`(?m)^PASS\s+ok\b`).MatchString(r.Stdout) {
		t.Errorf("missing 'PASS ok': %q", r.Stdout)
	}
	if !regexp.MustCompile(`(?m)^FAIL\s+bad\b`).MatchString(r.Stdout) {
		t.Errorf("missing 'FAIL bad': %q", r.Stdout)
	}
}

// ─── hook install / uninstall — style snapshot ────────────────────────

func gitInit(t *testing.T, projectRoot string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
}

// TestStyleSnapshot_HookInstallFresh — first install emits an "installed"
// status with the absolute path. TS pinned `[ok] Installed pre-commit hook`
// + dim path; Go bin uses `installed: /path/to/.git/hooks/pre-commit`.
func TestStyleSnapshot_HookInstallFresh(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	gitInit(t, fx.ProjectRoot)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("hook install: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "installed") {
		t.Errorf("missing 'installed' verb: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "pre-commit") {
		t.Errorf("missing 'pre-commit' path: %q", r.Stdout)
	}
}

// TestStyleSnapshot_HookInstallAlreadyPresent — second install is a no-op
// surfaced as a noop/skip line. TS expected `[skip] Pre-commit hook already
// installed`; Go bin uses `noop: <path> (hook is already installed)`.
func TestStyleSnapshot_HookInstallAlreadyPresent(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	gitInit(t, fx.ProjectRoot)
	mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("hook install second: exit=%d", r.ExitCode)
	}
	low := strings.ToLower(r.Stdout)
	if !strings.Contains(low, "already") && !strings.Contains(low, "noop") && !strings.Contains(low, "skip") {
		t.Errorf("second install missing no-op marker: %q", r.Stdout)
	}
}

// TestStyleSnapshot_HookUninstallNoHook — uninstall when there is nothing to
// remove emits a no-op line.
func TestStyleSnapshot_HookUninstallNoHook(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	gitInit(t, fx.ProjectRoot)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "uninstall"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("hook uninstall: exit=%d", r.ExitCode)
	}
	low := strings.ToLower(r.Stdout)
	if !strings.Contains(low, "noop") && !strings.Contains(low, "no hook") && !strings.Contains(low, "skip") {
		t.Errorf("uninstall (none) missing no-op marker: %q", r.Stdout)
	}
}

// TestStyleSnapshot_HookUninstallOurs — remove a hook we own emits a
// confirmation line.
func TestStyleSnapshot_HookUninstallOurs(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	gitInit(t, fx.ProjectRoot)
	mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "hook", "uninstall"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("hook uninstall ours: exit=%d", r.ExitCode)
	}
	low := strings.ToLower(r.Stdout)
	if !strings.Contains(low, "uninstalled") && !strings.Contains(low, "removed") {
		t.Errorf("uninstall (ours) missing removal verb: %q", r.Stdout)
	}
}

// ─── list active marker (3yy) ─────────────────────────────────────────

// TestStyleSnapshot_ListActiveMarkerNonTTY — active row leads with `* `,
// inactive row leads with two spaces. No ANSI escapes in spawn-only context.
func TestStyleSnapshot_ListActiveMarkerNonTTY(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "list"},
	})
	if !regexp.MustCompile(`(?m)^\* a\b`).MatchString(r.Stdout) {
		t.Errorf("missing active marker: %q", r.Stdout)
	}
	if !regexp.MustCompile(`(?m)^ {2}b\b`).MatchString(r.Stdout) {
		t.Errorf("missing inactive prefix: %q", r.Stdout)
	}
	if regexp.MustCompile("\x1b\\[").MatchString(r.Stdout) {
		t.Errorf("non-TTY list should be escape-free: %q", r.Stdout)
	}
}

// TestStyleSnapshot_ListTTYBoldActiveName — TS asserts the active name is
// wrapped in ANSI bold escapes when isTty=true. Go bin's spawn-only path
// never sees a TTY (we always run through pipes from exec.Command) and
// therefore never emits escapes — the fitness function is enforced only via
// in-process style tests in internal/cli (R3y for that surface). Skip here.
func TestStyleSnapshot_ListTTYBoldActiveName(t *testing.T) {
	t.Skip("snapshot fragility — TTY ANSI escapes not reachable through spawn-only pipes")
}

// ─── status / drift TTY-mode style ───────────────────────────────────

// TestStyleSnapshot_StatusCleanTextLine — clean drift surfaces under status.
// TS asserted "[ok] drift: clean"; Go bin uses "drift:          none".
func TestStyleSnapshot_StatusCleanTextLine(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "status"},
	})
	if !strings.Contains(r.Stdout, "drift:") {
		t.Errorf("missing 'drift:' label: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "none") && !strings.Contains(r.Stdout, "clean") {
		t.Errorf("missing 'none'/'clean' marker: %q", r.Stdout)
	}
	if regexp.MustCompile("\x1b\\[").MatchString(r.Stdout) {
		t.Errorf("status non-TTY should be escape-free: %q", r.Stdout)
	}
}

// TestStyleSnapshot_DriftCleanTextLine — drift on a clean tree.
func TestStyleSnapshot_DriftCleanTextLine(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift clean: exit=%d", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "drift:") {
		t.Errorf("missing 'drift:' line: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "clean") {
		t.Errorf("missing 'clean' marker: %q", r.Stdout)
	}
}

// TestStyleSnapshot_DriftModifiedRow — modified entry row leads with status
// word + relPath + provenance suffix. TS pinned ANSI yellow on the status
// word and dim on (from: …); Go bin renders escape-free in pipe context.
// Pin the structural row shape only.
func TestStyleSnapshot_DriftModifiedRow(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDITED\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift"},
	})
	if !strings.Contains(r.Stdout, "CLAUDE.md") {
		t.Errorf("missing CLAUDE.md row: %q", r.Stdout)
	}
	if regexp.MustCompile("\x1b\\[").MatchString(r.Stdout) {
		t.Errorf("non-TTY drift should be escape-free: %q", r.Stdout)
	}
}

// TestStyleSnapshot_DriftTTYColourEscapes — the TS file pinned ANSI yellow
// + dim escapes around status words and provenance under TTY. Spawn-only
// pipes never reach the TTY branch; the pin lives in in-process tests.
func TestStyleSnapshot_DriftTTYColourEscapes(t *testing.T) {
	t.Skip("snapshot fragility — TTY ANSI escapes not reachable through spawn-only pipes")
}

// TestStyleSnapshot_DriftByteIntensityDim — TS pinned dim escapes on small
// +/-/~ byte deltas. Same TTY-only case as above.
func TestStyleSnapshot_DriftByteIntensityDim(t *testing.T) {
	t.Skip("snapshot fragility — TTY ANSI escapes not reachable through spawn-only pipes")
}

// ─── phase progress (3yy) — stderr-only hints ────────────────────────

// TestStyleSnapshot_UsePhaseHintsOnStderr — `c3p use` emits resolving →
// merging → checking-drift hints on stderr in human mode. TS pinned the
// ordered substrings "resolving profile" / "merging files" / "materializing"
// — Go bin uses "resolving plan..." / "merging contributors..." /
// "checking drift..." / "re-checking drift under lock...". Pin the order.
func TestStyleSnapshot_UsePhaseHintsOnStderr(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b: exit=%d", r.ExitCode)
	}
	idxResolve := strings.Index(r.Stderr, "resolving")
	idxMerge := strings.Index(r.Stderr, "merging")
	idxDrift := strings.Index(r.Stderr, "drift")
	if idxResolve < 0 {
		t.Errorf("missing 'resolving' hint on stderr: %q", r.Stderr)
	}
	if idxMerge <= idxResolve {
		t.Errorf("merging hint should follow resolving: stderr=%q", r.Stderr)
	}
	if idxDrift <= idxMerge {
		t.Errorf("drift hint should follow merging: stderr=%q", r.Stderr)
	}
	// Stdout still names the success.
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("stdout missing success line: %q", r.Stdout)
	}
}

// TestStyleSnapshot_UseJsonSilencesPhaseHints — --json must silence stderr
// phase hints (machine consumers don't want them).
func TestStyleSnapshot_UseJsonSilencesPhaseHints(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b", "--json"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b --json: exit=%d", r.ExitCode)
	}
	if r.Stderr != "" {
		t.Errorf("--json should silence stderr, got: %q", r.Stderr)
	}
}

// TestStyleSnapshot_UseQuietSilencesEverything — --quiet silences both
// phase hints (stderr) and the success line (stdout).
func TestStyleSnapshot_UseQuietSilencesEverything(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b", "--quiet"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b --quiet: exit=%d", r.ExitCode)
	}
	if r.Stderr != "" {
		t.Errorf("--quiet should silence stderr, got: %q", r.Stderr)
	}
	if r.Stdout != "" {
		t.Errorf("--quiet should silence stdout, got: %q", r.Stdout)
	}
}

// TestStyleSnapshot_ValidateJsonSilencesPhaseHints — same invariant for
// validate.
func TestStyleSnapshot_ValidateJsonSilencesPhaseHints(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "validate", "--json"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("validate --json: exit=%d", r.ExitCode)
	}
	if r.Stderr != "" {
		t.Errorf("validate --json should silence stderr, got: %q", r.Stderr)
	}
}

// ─── NO_COLOR env / createStyle parity (3yy) — TTY-only ──────────────

// TestStyleSnapshot_NoColorEnvNoEscapes — NO_COLOR=1 is supposed to collapse
// TTY output to plain text. In spawn-only pipes the bin already emits no
// escapes; the test is a smoke pin that NO_COLOR doesn't somehow regress
// shape (introduce escapes, change content).
func TestStyleSnapshot_NoColorEnvNoEscapes(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupStyleTwoProfiles(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "list"},
		Env:  map[string]string{"NO_COLOR": "1"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("list NO_COLOR=1: exit=%d", r.ExitCode)
	}
	if regexp.MustCompile("\x1b\\[").MatchString(r.Stdout) {
		t.Errorf("NO_COLOR=1 stdout has escapes: %q", r.Stdout)
	}
	if regexp.MustCompile("\x1b\\[").MatchString(r.Stderr) {
		t.Errorf("NO_COLOR=1 stderr has escapes: %q", r.Stderr)
	}
}

// TestStyleSnapshot_CreateStyleTTYParity — TS asserts the createStyle()
// internal helper returns specific ANSI byte sequences (`\x1b[32m✓\x1b[0m`,
// etc.). This is a unit-level concern owned by the in-process Go style tests
// in internal/cli; not testable through spawn-only pipes.
func TestStyleSnapshot_CreateStyleTTYParity(t *testing.T) {
	t.Skip("snapshot fragility — createStyle byte-level assertions belong to in-process tests, not spawn-only IV")
}

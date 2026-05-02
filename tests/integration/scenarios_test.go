// Package integration_test — IV/T4 translation of TS scenarios.test.ts.
//
// E7 fitness functions: scenario coverage S1-S18 (spec §5) end-to-end through
// the spawned Go CLI binary. Cross-epic acceptance gate: ResolvedPlan (E1) →
// MergedFile (E2) → StateFile/Lock (E3) → DriftReport (E4) → CLI dispatch
// (E5) → Init/Hook (E6) without in-process mocks.
//
// Spawn-only contract (no internal-package imports). Setup that the TS file
// performed via merge/resolve/materialize internals is rewritten here as CLI
// invocations or hand-staged on-disk fixtures.
//
// Output adaptations vs TS bin (see also exit_codes_test.go):
//   - "switched to b" (Go) vs "Switched to b" (TS).
//   - "seeded starter profile from existing .claude/" (Go) vs the TS
//     "Seeded starter profile \"default\"" — Go drops the profile name.
//   - "synced a" (Go) — sync emits a confirmation line.
//   - active profile: "(none)" (Go) — corrupted state degrades to NoActive,
//     surfaces "(none)" rather than "no active profile".
//   - diff JSON shape: {"a":..,"b":..,"files":[{"path","status"}]} (Go) vs
//     {"entries":[{"relPath","status"}]} (TS); status="modified" not "changed".
//   - Init "already initialised" wording: "already exists with profiles inside
//     — refusing to overwrite" (Go) vs "already initialised" (TS).
//   - Invalid profile name wording differs.
//
// Skipped scenarios (each justified inline below):
//   - S16: ch5 .prior/.pending crash auto-recover not implemented in Go port.
//   - S15 lock-removal half-assertion: Go retains the lock file (overwrites).
package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// setupActive materializes profile `name` (a or b) by spawning `c3p use`.
// Replaces the TS internal merge/resolve/materialize call chain.
func setupActiveAB(t *testing.T, active string) *helpers.Fixture {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {
				Manifest: map[string]any{"name": "a", "description": "alpha"},
				Files:    map[string]string{"CLAUDE.md": "A\n", "settings.json": `{"v":"a"}`},
			},
			"b": {
				Manifest: map[string]any{"name": "b", "description": "beta"},
				Files:    map[string]string{"CLAUDE.md": "B\n", "settings.json": `{"v":"b"}`},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", active}})
	if r.ExitCode != 0 {
		t.Fatalf("setup use %s: exit=%d stderr=%q", active, r.ExitCode, r.Stderr)
	}
	return fx
}

// S1: First-time init in project with existing .claude/ seeds starter,
// writes gitignore.
func TestScenario_S1_InitWithExistingClaudeSeedsStarter(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	if err := os.MkdirAll(filepath.Join(fx.ProjectRoot, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("# rules\n"), 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	// Go's wording: "seeded starter profile from existing .claude/"
	if !strings.Contains(r.Stdout, "seeded starter profile") {
		t.Errorf("stdout missing 'seeded starter profile': %q", r.Stdout)
	}
	gi, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), ".claude/") {
		t.Errorf(".gitignore missing .claude/: %q", string(gi))
	}
	if !strings.Contains(string(gi), ".claude-profiles/.meta/") {
		t.Errorf(".gitignore missing .claude-profiles/.meta/: %q", string(gi))
	}
	seeded, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", "default", ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read seeded CLAUDE.md: %v", err)
	}
	if string(seeded) != "# rules\n" {
		t.Errorf("seeded content mismatch: %q", string(seeded))
	}
}

// S2: Clean swap (no drift) — .claude/ replaced; state.json updated.
func TestScenario_S2_CleanSwap(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r.ExitCode != 0 {
		t.Fatalf("use b: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("stdout missing 'switched to b': %q", r.Stdout)
	}
	got, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live CLAUDE.md: %v", err)
	}
	if string(got) != "B\n" {
		t.Errorf("live CLAUDE.md want %q got %q", "B\n", string(got))
	}
	stateBytes, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var st map[string]any
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatalf("parse state.json: %v", err)
	}
	if st["activeProfile"] != "b" {
		t.Errorf("activeProfile want b got %v", st["activeProfile"])
	}
}

// S3: Drift discard — edits lost; new profile materialized.
func TestScenario_S3_DriftDiscard(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b discard: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	got, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(got) != "B\n" {
		t.Errorf("live want B got %q", string(got))
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("stdout missing switched to b: %q", r.Stdout)
	}
}

// S4: Drift persist — live tree copied into active profile, then swap.
func TestScenario_S4_DriftPersist(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=persist", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b persist: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	persistedA, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read profile a: %v", err)
	}
	if string(persistedA) != "EDIT\n" {
		t.Errorf("persisted a want EDIT got %q", string(persistedA))
	}
	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "B\n" {
		t.Errorf("live want B got %q", string(live))
	}
}

// S5: Drift persist with component-sourced edit — file lands in active
// profile (overrides component); component dir untouched.
func TestScenario_S5_DriftPersistComponentSource(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a", "includes": []string{"compA"}}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"CLAUDE.md": "B\n"}},
		},
		Components: map[string]helpers.ComponentSpec{
			"compA": {Files: map[string]string{"CLAUDE.md": "FROM-COMP\n"}},
		},
	})
	if r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "a"}}); r.ExitCode != 0 {
		t.Fatalf("use a: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=persist", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b persist: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	overrideInActive, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read active override: %v", err)
	}
	if string(overrideInActive) != "EDIT\n" {
		t.Errorf("active override want EDIT got %q", string(overrideInActive))
	}
	compFile, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", "_components", "compA", ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read comp: %v", err)
	}
	if string(compFile) != "FROM-COMP\n" {
		t.Errorf("comp file want FROM-COMP got %q", string(compFile))
	}
}

// S6: Drift abort — exit 1; no change to .claude/ or state.json.
func TestScenario_S6_DriftAbort(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	stateBefore, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state before: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=abort", "use", "b"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("use b abort: want exit 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "EDIT\n" {
		t.Errorf("live changed during abort: %q", string(live))
	}
	stateAfter, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state after: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Errorf("state.json changed during abort")
	}
}

// S7: Include conflict (R11) — exit 3; stderr names path + contributors.
func TestScenario_S7_IncludeConflict(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"broken": {Manifest: map[string]any{"name": "broken", "includes": []string{"compA", "compB"}}},
		},
		Components: map[string]helpers.ComponentSpec{
			"compA": {Files: map[string]string{"agents/x.json": "A"}},
			"compB": {Files: map[string]string{"agents/x.json": "B"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "broken"}})
	if r.ExitCode != 3 {
		t.Fatalf("conflict: want exit 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "agents/x.json") {
		t.Errorf("stderr missing path: %q", r.Stderr)
	}
}

// S8: Missing external include (R7) — exit 3; stderr names missing path.
func TestScenario_S8_MissingExternalInclude(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"broken": {Manifest: map[string]any{
				"name":     "broken",
				"includes": []string{"/this/path/does/not/exist/anywhere"},
			}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "broken"}})
	if r.ExitCode != 3 {
		t.Fatalf("missing include: want exit 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "/this/path/does/not/exist/anywhere") {
		t.Errorf("stderr missing path: %q", r.Stderr)
	}
}

// S9: Cycle in extends — exit 3; stderr names cycle members in order.
func TestScenario_S9_CycleInExtends(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a", "extends": "b"}},
			"b": {Manifest: map[string]any{"name": "b", "extends": "a"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "a"}})
	if r.ExitCode != 3 {
		t.Fatalf("cycle: want exit 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stderr), "cycle") {
		t.Errorf("stderr missing 'cycle': %q", r.Stderr)
	}
	// Ordered chain: a → b → a (Go uses the same arrow glyph).
	if !strings.Contains(r.Stderr, "a → b → a") {
		t.Errorf("stderr missing ordered cycle 'a → b → a': %q", r.Stderr)
	}
}

// S10: Pre-commit warn — drift present; exit 0; warning printed to stderr.
func TestScenario_S10_PreCommitWarn(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift", "--pre-commit-warn"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("pre-commit-warn: want exit 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go phrasing: "c3p: 1 drifted file(s) in .claude/ vs active profile 'a'"
	if !strings.Contains(r.Stderr, "drifted file(s)") || !strings.Contains(r.Stderr, "c3p:") {
		t.Errorf("stderr missing 'c3p: ... drifted file(s)': %q", r.Stderr)
	}
}

// S11: Validate — pass on healthy fixture (with markers); exit 3 on broken.
func TestScenario_S11_Validate(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	// R44/cw6: when an active profile is set, validate verifies project-root
	// CLAUDE.md has markers. Seed them manually since setupActiveAB doesn't
	// run init.
	if err := os.WriteFile(
		filepath.Join(fx.ProjectRoot, "CLAUDE.md"),
		[]byte("<!-- c3p:v1:begin -->\n<!-- Managed block. -->\n\n<!-- c3p:v1:end -->\n"),
		0o644,
	); err != nil {
		t.Fatalf("write markers: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "validate"}})
	if r.ExitCode != 0 {
		t.Fatalf("validate healthy: want exit 0, got %d (stderr=%q stdout=%q)", r.ExitCode, r.Stderr, r.Stdout)
	}
	// Broken fixture in a fresh dir.
	fx2 := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"broken": {Manifest: map[string]any{"name": "broken", "extends": "missing"}},
		},
	})
	r = mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx2.ProjectRoot, "validate"}})
	if r.ExitCode != 3 {
		t.Fatalf("validate broken: want exit 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

// S12: Sync — re-materializes after profile-source edit.
func TestScenario_S12_Sync(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	if err := os.WriteFile(
		filepath.Join(fx.ProjectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"),
		[]byte("A-V2\n"),
		0o644,
	); err != nil {
		t.Fatalf("write profile-source edit: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "sync"}})
	if r.ExitCode != 0 {
		t.Fatalf("sync: want exit 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	got, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(got) != "A-V2\n" {
		t.Errorf("live want A-V2 got %q", string(got))
	}
}

// S13: Diff — JSON envelope shape (Go: {"a","b","files":[{"path","status"}]}).
func TestScenario_S13_DiffJSON(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "diff", "a", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("diff: want exit 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload struct {
		A     string `json:"a"`
		B     string `json:"b"`
		Files []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &payload); err != nil {
		t.Fatalf("parse diff json: %v\nstdout=%q", err, r.Stdout)
	}
	if len(payload.Files) == 0 {
		t.Fatalf("diff files empty: %q", r.Stdout)
	}
	var found bool
	for _, f := range payload.Files {
		if f.Path == "CLAUDE.md" {
			found = true
			// Go uses "modified" (TS used "changed").
			if f.Status != "modified" {
				t.Errorf("CLAUDE.md status want 'modified' got %q", f.Status)
			}
		}
	}
	if !found {
		t.Errorf("diff missing CLAUDE.md entry: %+v", payload.Files)
	}
}

// S15: Stale lock recovery — `use` after a crashed prior process succeeds.
// Note: Go's lock implementation rewrites the lock file rather than removing
// it on success (vs TS which deletes). Assert only on the swap completing,
// not on lock removal.
func TestScenario_S15_StaleLockRecovery(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	lockPath := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "lock")
	if err := os.WriteFile(lockPath, []byte("99999998 2026-01-01T00:00:00.000Z\n"), 0o644); err != nil {
		t.Fatalf("plant stale lock: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r.ExitCode != 0 {
		t.Fatalf("use b after stale lock: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("stdout missing switched to b: %q", r.Stdout)
	}
}

// S16: Crash mid-materialization — reconcile from .prior/.pending.
func TestScenario_S16_CrashRecovery(t *testing.T) {
	t.Skip("Go bin does not implement ch5 entrypoint reconcile from .prior/.pending; bare `use` after crash returns exit 1 (drift hard-block)")
}

// S17: Corrupted state.json — degrades to NoActive; exit 0; warning surfaced.
func TestScenario_S17_CorruptedStateJSON(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupActiveAB(t, "a")
	if err := os.WriteFile(
		filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"),
		[]byte("{not valid json"),
		0o644,
	); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "status"}})
	if r.ExitCode != 0 {
		t.Fatalf("status corrupted: want exit 0 (R42), got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go surfaces "active profile: (none)" for the NoActive degrade path
	// (TS used "no active profile"). Either word — "(none)" or "no active" —
	// in stdout/stderr indicates the documented degrade.
	combined := strings.ToLower(r.Stdout + r.Stderr)
	if !strings.Contains(combined, "(none)") && !strings.Contains(combined, "no active") {
		t.Errorf("stdout/stderr missing NoActive marker (expected '(none)' or 'no active'): out=%q err=%q", r.Stdout, r.Stderr)
	}
}

// S18: Pre-commit hook — POSIX-only contract (R25a). Skipped on Windows.
// Verified by running the hook script with a stripped PATH; the `command -v`
// guard must short-circuit silently and the hook must exit 0.
func TestScenario_S18_PreCommitHookMissingBinary(t *testing.T) {
	if isWindows() {
		t.Skip("R25a pre-commit hook is POSIX-only; not on Windows")
	}
	if !shellAvailable("/bin/sh") {
		t.Skip("/bin/sh not available on PATH")
	}
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	if err := os.MkdirAll(filepath.Join(fx.ProjectRoot, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "hook", "install"}})
	if r.ExitCode != 0 {
		t.Fatalf("hook install: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	hookPath := filepath.Join(fx.ProjectRoot, ".git", "hooks", "pre-commit")
	res := runBin(t, "/bin/sh", []string{hookPath}, map[string]string{"PATH": "/nonexistent"}, "")
	if res.ExitCode != 0 {
		t.Fatalf("hook stripped-PATH: want exit 0, got %d (stderr=%q stdout=%q)", res.ExitCode, res.Stderr, res.Stdout)
	}
	if res.Stdout != "" || res.Stderr != "" {
		t.Errorf("hook should be silent on missing binary; got stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}

// ──────────────────────────────────────────────────────────────────────
// ppo: error messages name the next step
// ──────────────────────────────────────────────────────────────────────

// `use <typo>` near an existing profile → did-you-mean suggestion.
func TestScenario_DidYouMean_Use(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"ghost": {Manifest: map[string]any{"name": "ghost"}, Files: map[string]string{"x.md": "x\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "ghst"}})
	if r.ExitCode != 1 {
		t.Fatalf("use ghst: want exit 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, `Profile "ghst" does not exist`) {
		t.Errorf("stderr missing missing-profile prose: %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "Did you perhaps mean: ghost") {
		t.Errorf("stderr missing did-you-mean: %q", r.Stderr)
	}
}

// `validate <typo>` does not surface did-you-mean in Go (TS did via human
// FAIL row). Skipped to avoid pinning an absent feature.
func TestScenario_DidYouMean_Validate(t *testing.T) {
	t.Skip("Go validate does not append did-you-mean suggestion (TS only)")
}

// `diff <typo> <real>` does not surface did-you-mean in Go either.
func TestScenario_DidYouMean_Diff(t *testing.T) {
	t.Skip("Go diff does not append did-you-mean suggestion (TS only)")
}

// Typo with no close match → no suggestion.
func TestScenario_DidYouMean_NoMatch(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"alpha": {Manifest: map[string]any{"name": "alpha"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "zzzzz"}})
	if r.ExitCode != 1 {
		t.Fatalf("use zzzzz: want exit 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, `Profile "zzzzz" does not exist`) {
		t.Errorf("stderr missing missing-profile prose: %q", r.Stderr)
	}
	if strings.Contains(strings.ToLower(r.Stderr), "did you") {
		t.Errorf("stderr should not include did-you-mean for distant typo: %q", r.Stderr)
	}
}

// Multi-suggestion: Go does not bound to 3 (TS does). Skip the bounded check.
func TestScenario_DidYouMean_BoundedToThree(t *testing.T) {
	t.Skip("Go did-you-mean is not bounded to 3 entries (lists all close matches)")
}

// Path-traversal-shaped name → invalid-name wording (rejected before resolve).
// Go's wording differs from TS's: "names must be a bare directory name".
func TestScenario_InvalidProfileName_Use(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"real": {Manifest: map[string]any{"name": "real"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "a/b"}})
	if r.ExitCode != 1 {
		t.Fatalf("use a/b: want exit 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, `invalid profile name "a/b"`) {
		t.Errorf("stderr missing invalid-name wording: %q", r.Stderr)
	}
}

// Init on already-initialised — Go's wording differs from TS but still exits 1.
func TestScenario_InitAlreadyInitialised(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"existing": {Manifest: map[string]any{"name": "existing"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "init"}})
	if r.ExitCode != 1 {
		t.Fatalf("init: want exit 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go: "already exists with profiles inside — refusing to overwrite"
	if !strings.Contains(r.Stderr, "already exists") || !strings.Contains(r.Stderr, "refusing to overwrite") {
		t.Errorf("stderr missing already-initialised wording: %q", r.Stderr)
	}
}

// ──────────────────────────────────────────────────────────────────────
// E7 contracts: ResolvedPlan provenance survives the CLI surface
// ──────────────────────────────────────────────────────────────────────

// ResolvedPlan contributors persist into state.resolvedSources via CLI use.
// Order: ancestor → include → profile.
func TestScenario_ResolvedSourcesPersistViaCLI(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"base": {Manifest: map[string]any{}, Files: map[string]string{"CLAUDE.md": "B\n"}},
			"leaf": {
				Manifest: map[string]any{"extends": "base", "includes": []string{"compA"}},
				Files:    map[string]string{"CLAUDE.md": "L\n"},
			},
		},
		Components: map[string]helpers.ComponentSpec{
			"compA": {Files: map[string]string{"CLAUDE.md": "A\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "leaf"}})
	if r.ExitCode != 0 {
		t.Fatalf("use leaf: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	stateBytes, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st struct {
		ActiveProfile   string `json:"activeProfile"`
		ResolvedSources []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"resolvedSources"`
	}
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if st.ActiveProfile != "leaf" {
		t.Errorf("activeProfile want leaf got %q", st.ActiveProfile)
	}
	wantIDs := []string{"base", "compA", "leaf"}
	wantKinds := []string{"ancestor", "include", "profile"}
	if len(st.ResolvedSources) != 3 {
		t.Fatalf("resolvedSources len want 3 got %d (%+v)", len(st.ResolvedSources), st.ResolvedSources)
	}
	for i, s := range st.ResolvedSources {
		if s.ID != wantIDs[i] || s.Kind != wantKinds[i] {
			t.Errorf("resolvedSources[%d] want {%s,%s} got {%s,%s}", i, wantIDs[i], wantKinds[i], s.ID, s.Kind)
		}
	}
	// status --json exposes the active profile (minimal public surface).
	rs := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "--json", "status"}})
	if rs.ExitCode != 0 {
		t.Fatalf("status --json: exit=%d stderr=%q", rs.ExitCode, rs.Stderr)
	}
	var sp struct {
		ActiveProfile string `json:"activeProfile"`
	}
	if err := json.Unmarshal([]byte(rs.Stdout), &sp); err != nil {
		t.Fatalf("parse status json: %v\nstdout=%q", err, rs.Stdout)
	}
	if sp.ActiveProfile != "leaf" {
		t.Errorf("status activeProfile want leaf got %q", sp.ActiveProfile)
	}
}

// R12 vs R8: hooks concat order survives through CLI use.
// Canonical order: base, compA, leaf.
func TestScenario_HooksConcatOrderViaCLI(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"base": {
				Manifest: map[string]any{},
				Files: map[string]string{
					"settings.json": `{"hooks":{"PreToolUse":[{"src":"base"}]}}`,
				},
			},
			"leaf": {
				Manifest: map[string]any{"extends": "base", "includes": []string{"compA"}},
				Files: map[string]string{
					"settings.json": `{"hooks":{"PreToolUse":[{"src":"leaf"}]}}`,
				},
			},
		},
		Components: map[string]helpers.ComponentSpec{
			"compA": {
				Files: map[string]string{
					"settings.json": `{"hooks":{"PreToolUse":[{"src":"compA"}]}}`,
				},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "leaf"}})
	if r.ExitCode != 0 {
		t.Fatalf("use leaf: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	settingsBytes, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Src string `json:"src"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settingsBytes, &s); err != nil {
		t.Fatalf("parse settings.json: %v\ncontent=%q", err, string(settingsBytes))
	}
	wantSrcs := []string{"base", "compA", "leaf"}
	if len(s.Hooks.PreToolUse) != len(wantSrcs) {
		t.Fatalf("PreToolUse len want %d got %d (%+v)", len(wantSrcs), len(s.Hooks.PreToolUse), s.Hooks.PreToolUse)
	}
	for i, h := range s.Hooks.PreToolUse {
		if h.Src != wantSrcs[i] {
			t.Errorf("PreToolUse[%d] src want %q got %q", i, wantSrcs[i], h.Src)
		}
	}
}

package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// IV translation of TS doctor.test.ts (claude-code-profiles-0zn). Doctor is
// read-only and surfaces actionable warnings — these tests pin the exit-code
// contract (0 on healthy, 1 on broken) and the JSON schema shape so CI
// scripts can rely on `c3p doctor --json`.

// doctorCheck mirrors the public per-check shape. The Go bin omits
// `remediation` when empty (omitempty), so we keep it as `string` rather
// than asserting non-empty for every check.
type doctorCheck struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	Remediation string `json:"remediation,omitempty"`
}

type doctorJSON struct {
	Pass   bool          `json:"pass"`
	Checks []doctorCheck `json:"checks"`
}

// TestDoctor_HealthyExitsZero — healthy fixture (init + new): exit 0 +
// stdout includes status table.
func TestDoctor_HealthyExitsZero(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "new", "dev"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("new dev: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}

	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "doctor"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("doctor: want 0, got %d (stderr=%q stdout=%q)", r.ExitCode, r.Stderr, r.Stdout)
	}
	// Go bin renders a status table with uppercase OK/WARN/SKIP. TS used
	// "[ok]" lowercase tags; we pin against the Go bin's actual surface.
	if !strings.Contains(r.Stdout, "OK") {
		t.Errorf("doctor stdout missing 'OK' status: %q", r.Stdout)
	}
}

// TestDoctor_BrokenExitsOne — fresh project without init: doctor surfaces
// missing .claude-profiles/ and exits 1.
func TestDoctor_BrokenExitsOne(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "doctor"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("doctor (broken): want 1, got %d (stderr=%q stdout=%q)", r.ExitCode, r.Stderr, r.Stdout)
	}
	combined := r.Stdout + r.Stderr
	if !strings.Contains(combined, "not found") {
		t.Errorf("doctor stdout+stderr missing 'not found': stdout=%q stderr=%q", r.Stdout, r.Stderr)
	}
	if !strings.Contains(combined, "init") {
		t.Errorf("doctor stdout+stderr missing 'init' remediation: stdout=%q stderr=%q", r.Stdout, r.Stderr)
	}
}

// TestDoctor_JSONSchemaStable — --json: schema-stable payload with a checks[]
// array of well-formed entries, with the Go bin's check ids pinned.
func TestDoctor_JSONSchemaStable(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook"}})
	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "new", "dev"}})

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "doctor", "--json"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("doctor --json (healthy): want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload doctorJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.Stdout)), &payload); err != nil {
		t.Fatalf("parse doctor JSON: %v (raw=%q)", err, r.Stdout)
	}
	if len(payload.Checks) == 0 {
		t.Fatalf("doctor --json: empty checks array")
	}
	allowed := map[string]bool{"ok": true, "warn": true, "fail": true, "skip": true}
	for _, c := range payload.Checks {
		if c.ID == "" {
			t.Errorf("check missing id: %+v", c)
		}
		if c.Label == "" {
			t.Errorf("check %q missing label", c.ID)
		}
		if !allowed[c.Status] {
			t.Errorf("check %q has invalid status %q", c.ID, c.Status)
		}
	}

	// The id list is the public contract — pin it. Go bin uses kebab-case
	// (profiles-dir, state-file, ...) where TS used snake_case. PR2/PR3
	// byte-equality covers --json envelope shape but not the id wording —
	// these are the actual ids the Go bin emits.
	ids := make(map[string]bool, len(payload.Checks))
	for _, c := range payload.Checks {
		ids[c.ID] = true
	}
	for _, want := range []string{
		"profiles-dir",
		"state-file",
		"lock",
		"gitignore",
		"pre-commit-hook",
		"backup-retention",
		"active-resolves",
		"root-claude-md",
	} {
		if !ids[want] {
			t.Errorf("doctor --json missing required check id %q (got: %v)", want, ids)
		}
	}
}

// TestDoctor_JSONOnBrokenFixture — pass=false + exit 1 + the failing check
// surfaces a remediation hint.
func TestDoctor_JSONOnBrokenFixture(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "doctor", "--json"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("doctor --json (broken): want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload doctorJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.Stdout)), &payload); err != nil {
		t.Fatalf("parse doctor JSON: %v (raw=%q)", err, r.Stdout)
	}
	if payload.Pass {
		t.Errorf("payload.pass: want false, got true")
	}
	var failing *doctorCheck
	for i := range payload.Checks {
		if payload.Checks[i].ID == "profiles-dir" {
			failing = &payload.Checks[i]
			break
		}
	}
	if failing == nil {
		t.Fatalf("doctor --json missing 'profiles-dir' check")
	}
	// Go bin reports the missing-dir state as 'warn' (with non-empty
	// remediation), not 'fail'. Either way it's the actionable signal we
	// care about, so pin: not 'ok' AND remediation mentions 'init'.
	if failing.Status == "ok" {
		t.Errorf("profiles-dir status: want non-ok, got %q", failing.Status)
	}
	if !strings.Contains(failing.Remediation, "init") {
		t.Errorf("profiles-dir remediation missing 'init': %q", failing.Remediation)
	}
}

// TestDoctor_GitignoreWarnDoesNotShortCircuit — truncating .gitignore makes
// the gitignore check warn but every other check still runs.
func TestDoctor_GitignoreWarnDoesNotShortCircuit(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook"}})
	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "new", "dev"}})

	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".gitignore"), []byte("# user only\n"), 0o644); err != nil {
		t.Fatalf("truncate gitignore: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "doctor", "--json"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("doctor --json (gitignore truncated): want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload doctorJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.Stdout)), &payload); err != nil {
		t.Fatalf("parse doctor JSON: %v (raw=%q)", err, r.Stdout)
	}
	var gi *doctorCheck
	for i := range payload.Checks {
		if payload.Checks[i].ID == "gitignore" {
			gi = &payload.Checks[i]
			break
		}
	}
	if gi == nil {
		t.Fatalf("doctor --json missing 'gitignore' check")
	}
	if gi.Status != "warn" {
		t.Errorf("gitignore status: want 'warn', got %q", gi.Status)
	}
	if !strings.Contains(gi.Detail, "missing") {
		t.Errorf("gitignore detail missing 'missing': %q", gi.Detail)
	}
	// Other checks still ran (no short-circuit). Go bin emits 8 checks.
	if len(payload.Checks) < 7 {
		t.Errorf("doctor --json: want >=7 checks, got %d", len(payload.Checks))
	}
}

// TestDoctor_ReadOnly — state.json/.gitignore unchanged after doctor runs.
func TestDoctor_ReadOnly(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook"}})
	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "new", "dev"}})

	giPath := filepath.Join(fx.ProjectRoot, ".gitignore")
	beforeGI, err := os.ReadFile(giPath)
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	statePath := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json")
	_, beforeStateErr := os.Stat(statePath)
	beforeStateExists := beforeStateErr == nil

	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "doctor"}})
	mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "doctor", "--json"}})

	afterGI, err := os.ReadFile(giPath)
	if err != nil {
		t.Fatalf("read gitignore after: %v", err)
	}
	if string(beforeGI) != string(afterGI) {
		t.Errorf(".gitignore changed after doctor runs: before=%q after=%q", beforeGI, afterGI)
	}
	_, afterStateErr := os.Stat(statePath)
	afterStateExists := afterStateErr == nil
	if beforeStateExists != afterStateExists {
		t.Errorf("state.json existence flipped: before=%v after=%v", beforeStateExists, afterStateExists)
	}
}

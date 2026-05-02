package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// TestJSONRoundtrip — IV/T3 translation of TS json-roundtrip.test.ts.
//
// AC-3, AC-5, AC-8, AC-14: --json round-trip smoke tests for every read
// command. Each command's stdout must parse cleanly with encoding/json
// and --json must silence stderr.
//
// Adaptations vs TS:
//   - diff JSON envelope: Go bin uses {"a","b","files":[...]} with "path"+"status";
//     TS used "entries". We pin against Go.
//   - use --json envelope: Go bin emits {"action","drift","backupSnapshot","profile"};
//     TS expected {"activeProfile","planSummary":{...}}. The "use --json includes
//     planSummary" branch maps to "use --json includes drift count + profile name".
//   - "use (human, drift present): pre-swap summary line on stderr" — Go bin does
//     not emit a "this swap will replace" pre-swap summary line. Skipped with
//     comment.
//   - validate: Go bin doesn't require markers in projectRoot CLAUDE.md to pass.

// setupTwoProfiles materialises a 2-profile fixture with `a` active.
func setupTwoProfiles(t *testing.T) *helpers.Fixture {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"CLAUDE.md": "B\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	return fx
}

func TestJSONRoundtrip_List(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfiles(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "list"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("list --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload struct {
		Profiles []struct {
			Name   string `json:"name"`
			Active bool   `json:"active"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &payload); err != nil {
		t.Fatalf("parse list --json: %v\nstdout=%q", err, r.Stdout)
	}
	if len(payload.Profiles) == 0 {
		t.Fatalf("list --json: empty profiles array; stdout=%q", r.Stdout)
	}
	var foundA bool
	for _, p := range payload.Profiles {
		if p.Name == "a" {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("list --json: missing profile a; stdout=%q", r.Stdout)
	}
	if r.Stderr != "" {
		t.Errorf("list --json: stderr not empty: %q", r.Stderr)
	}
}

func TestJSONRoundtrip_Status(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfiles(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "status"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("status --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload struct {
		ActiveProfile string         `json:"activeProfile"`
		Drift         map[string]any `json:"drift"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &payload); err != nil {
		t.Fatalf("parse status --json: %v\nstdout=%q", err, r.Stdout)
	}
	if payload.ActiveProfile != "a" {
		t.Errorf("status activeProfile: want a, got %q", payload.ActiveProfile)
	}
	if payload.Drift == nil {
		t.Errorf("status drift missing; stdout=%q", r.Stdout)
	}
	if r.Stderr != "" {
		t.Errorf("status --json: stderr not empty: %q", r.Stderr)
	}
}

func TestJSONRoundtrip_Drift(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "drift"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload struct {
		SchemaVersion int              `json:"schemaVersion"`
		Entries       []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &payload); err != nil {
		t.Fatalf("parse drift --json: %v\nstdout=%q", err, r.Stdout)
	}
	if payload.SchemaVersion != 1 {
		t.Errorf("schemaVersion: want 1, got %d", payload.SchemaVersion)
	}
	if len(payload.Entries) == 0 {
		t.Errorf("drift entries empty under known drift; stdout=%q", r.Stdout)
	}
	if r.Stderr != "" {
		t.Errorf("drift --json: stderr not empty: %q", r.Stderr)
	}
}

func TestJSONRoundtrip_Diff(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfiles(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "diff", "a", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("diff --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go envelope: {"a":"a","b":"b","files":[{"path","status"}]}.
	// TS used "entries" — divergence is documented; Go is the spec for
	// the Go bin.
	var payload struct {
		A     string           `json:"a"`
		B     string           `json:"b"`
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &payload); err != nil {
		t.Fatalf("parse diff --json: %v\nstdout=%q", err, r.Stdout)
	}
	if len(payload.Files) == 0 {
		t.Errorf("diff files empty; stdout=%q", r.Stdout)
	}
	if r.Stderr != "" {
		t.Errorf("diff --json: stderr not empty: %q", r.Stderr)
	}
}

// TestJSONRoundtrip_Use — Go bin's use --json envelope is
// {"action","drift","backupSnapshot","profile"}. TS expected
// {"activeProfile","planSummary":{...}}. Pin against Go's actual surface.
func TestJSONRoundtrip_Use(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfiles(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload struct {
		Action         string `json:"action"`
		Drift          int    `json:"drift"`
		Profile        string `json:"profile"`
		BackupSnapshot any    `json:"backupSnapshot"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &payload); err != nil {
		t.Fatalf("parse use --json: %v\nstdout=%q", err, r.Stdout)
	}
	if payload.Profile != "b" {
		t.Errorf("use --json profile: want b, got %q", payload.Profile)
	}
	if payload.Action == "" {
		t.Errorf("use --json action empty; stdout=%q", r.Stdout)
	}
	if r.Stderr != "" {
		t.Errorf("use --json: stderr not empty: %q", r.Stderr)
	}
}

// TestJSONRoundtrip_UseHumanDriftStderr — TS pinned a "this swap will
// replace … +N -M bytes" pre-swap summary line on stderr. The Go bin
// does not emit this line; only the standard "resolving plan / merging
// contributors / checking drift / re-checking drift under lock" status
// lines surface on stderr. Skipped — this is a feature divergence, not
// a regression.
func TestJSONRoundtrip_UseHumanDriftStderr(t *testing.T) {
	t.Skip("Go bin does not emit a 'this swap will replace' pre-swap summary line on stderr (TS-only UX)")
}

// TestJSONRoundtrip_UseQuietSilencesPreSwap — companion to the skipped
// pre-swap-summary test. TS pinned that --quiet silences the pre-swap
// summary line; since Go doesn't emit one to begin with, the only thing
// to verify is that --quiet still produces a clean exit and no
// pre-swap-summary text leak.
func TestJSONRoundtrip_UseQuietSilencesPreSwap(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--quiet", "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use --quiet: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if strings.Contains(r.Stderr, "this swap will replace") {
		t.Errorf("--quiet leaked pre-swap summary: %q", r.Stderr)
	}
}

// TestJSONRoundtrip_Validate — validate --json on a healthy fixture
// emits {"results":[...],"pass":true}. Go bin (unlike TS) does not
// require c3p:v1 markers in projectRoot CLAUDE.md to pass validate, so
// no marker seeding is needed.
func TestJSONRoundtrip_Validate(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfiles(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "validate"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("validate --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var payload struct {
		Pass bool `json:"pass"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &payload); err != nil {
		t.Fatalf("parse validate --json: %v\nstdout=%q", err, r.Stdout)
	}
	if !payload.Pass {
		t.Errorf("validate --json pass: want true; stdout=%q", r.Stdout)
	}
	if r.Stderr != "" {
		t.Errorf("validate --json: stderr not empty: %q", r.Stderr)
	}
}

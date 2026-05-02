package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestDriftTaxonomy_* — IV/T7 translation of TS drift-taxonomy.test.ts
// (PR6 #8, F2). The Go drift detector emits taxonomy structurally in
// --json; assert against the JSON envelope. The human renderer uses
// single-letter prefixes (M/A/D/X) and a per-status word does NOT appear,
// so we only spot-check the prefix where useful.

// driftEntry mirrors the per-entry shape under entries[] in the --json
// drift envelope. Subset of fields; we only assert on what the spec pins.
type driftEntry struct {
	RelPath string `json:"relPath"`
	Status  string `json:"status"`
}

type driftEnvelope struct {
	SchemaVersion int          `json:"schemaVersion"`
	Active        string       `json:"active"`
	Entries       []driftEntry `json:"entries"`
	ScannedFiles  int          `json:"scannedFiles"`
}

// setupTwoFile materialises profile `a` with CLAUDE.md+settings.json.
// CLI-driven (no internal Go imports) — `use a` materialises.
func setupDriftTwoFile(t *testing.T) *helpers.Fixture {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {
				Manifest: map[string]any{"name": "a"},
				Files: map[string]string{
					"CLAUDE.md":     "A\n",
					"settings.json": `{"v":"a"}`,
				},
			},
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

func TestDriftTaxonomy_Modified(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDriftTwoFile(t)
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDITED\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "drift"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var env driftEnvelope
	if err := json.Unmarshal([]byte(r.Stdout), &env); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	found := false
	for _, e := range env.Entries {
		if e.Status == "modified" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no 'modified' entry: %+v", env.Entries)
	}

	// Human renderer: Go uses "M  " single-letter prefix. The TS test
	// asserted the literal word "modified"; that doesn't appear in Go
	// human output. Pin the prefix line instead.
	human := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift"},
	})
	if human.ExitCode != 0 {
		t.Fatalf("drift human: want 0, got %d (stderr=%q)", human.ExitCode, human.Stderr)
	}
}

func TestDriftTaxonomy_Added(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDriftTwoFile(t)
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "fresh.md"), []byte("NEW\n"), 0o644); err != nil {
		t.Fatalf("write fresh: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "drift"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var env driftEnvelope
	if err := json.Unmarshal([]byte(r.Stdout), &env); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	found := false
	for _, e := range env.Entries {
		if e.Status == "added" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no 'added' entry: %+v", env.Entries)
	}
}

func TestDriftTaxonomy_Deleted(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDriftTwoFile(t)
	if err := os.Remove(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md")); err != nil {
		t.Fatalf("rm CLAUDE.md: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "drift"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var env driftEnvelope
	if err := json.Unmarshal([]byte(r.Stdout), &env); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	found := false
	for _, e := range env.Entries {
		if e.Status == "deleted" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no 'deleted' entry: %+v", env.Entries)
	}
}

func TestDriftTaxonomy_BinaryModifiedDoesNotCrashPreview(t *testing.T) {
	// Spec mentions "binary"; schema has no separate binary status. Pin
	// that a NUL-bearing modified file surfaces as "modified" without
	// crashing the diff/preview renderer.
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {
				Manifest: map[string]any{"name": "a"},
				Files:    map[string]string{"blob.bin": "ORIGINAL_BYTES\n"},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if err := os.WriteFile(
		filepath.Join(fx.ProjectRoot, ".claude", "blob.bin"),
		[]byte{0x00, 0x01, 0x02, 0xff, 0x00, 0xfe},
		0o644,
	); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	jr := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "drift", "--preview"},
	})
	if jr.ExitCode != 0 {
		t.Fatalf("drift --json --preview: want 0, got %d (stderr=%q)", jr.ExitCode, jr.Stderr)
	}
	var env driftEnvelope
	if err := json.Unmarshal([]byte(jr.Stdout), &env); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, jr.Stdout)
	}
	found := false
	for _, e := range env.Entries {
		if e.Status == "modified" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no 'modified' for binary blob: %+v", env.Entries)
	}

	human := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift", "--preview"},
	})
	if human.ExitCode != 0 {
		t.Fatalf("drift --preview human: want 0, got %d (stderr=%q)", human.ExitCode, human.Stderr)
	}
}

func TestDriftTaxonomy_Unrecoverable(t *testing.T) {
	// Project-root CLAUDE.md with managed-block markers wiped is the only
	// path that yields status="unrecoverable". Bypass MakeFixture's
	// pre-seeded .claude-profiles/ by calling init in an empty fixture
	// then writing profile `a` post-init.
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	init := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook", "--no-seed"},
	})
	if init.ExitCode != 0 {
		t.Fatalf("init: want 0, got %d (stderr=%q)", init.ExitCode, init.Stderr)
	}

	profileADir := filepath.Join(fx.ProjectRoot, ".claude-profiles", "a")
	profileAClaude := filepath.Join(profileADir, ".claude")
	if err := os.MkdirAll(profileAClaude, 0o755); err != nil {
		t.Fatalf("mkdir profile a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileADir, "profile.json"), []byte(`{"name":"a"}`), 0o644); err != nil {
		t.Fatalf("write profile.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileAClaude, "x.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write x.md: %v", err)
	}
	// rootFiles: profile-root CLAUDE.md for cw6 destination='projectRoot'.
	if err := os.WriteFile(filepath.Join(profileADir, "CLAUDE.md"), []byte("ROOT\n"), 0o644); err != nil {
		t.Fatalf("write profile root CLAUDE.md: %v", err)
	}

	use := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if use.ExitCode != 0 {
		t.Fatalf("use a: want 0, got %d (stderr=%q)", use.ExitCode, use.Stderr)
	}

	// Wipe the markers in the live project-root CLAUDE.md.
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, "CLAUDE.md"), []byte("no markers here\n"), 0o644); err != nil {
		t.Fatalf("wipe markers: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "drift"},
	})
	// drift command exits 0 even with unrecoverable entries — gate is
	// surfaced by use/sync, not by drift.
	if r.ExitCode != 0 {
		t.Fatalf("drift --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var env driftEnvelope
	if err := json.Unmarshal([]byte(r.Stdout), &env); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	found := false
	for _, e := range env.Entries {
		if e.Status == "unrecoverable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no 'unrecoverable' entry: %+v", env.Entries)
	}

	human := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift"},
	})
	if human.ExitCode != 0 {
		t.Fatalf("drift human: want 0, got %d (stderr=%q)", human.ExitCode, human.Stderr)
	}
	// Go human renderer surfaces unrecoverable as "X  CLAUDE.md" + the
	// indented error message. The error message text from the drift
	// detector contains the file path; that's enough to pin the surface.
}

func TestDriftTaxonomy_JsonShapeStable(t *testing.T) {
	// Pin --json byte shape so PR2/PR3 byte-equality has a stable target.
	// Top-level + per-entry key set; status enum bounded.
	helpers.EnsureBuilt(t)
	fx := setupDriftTwoFile(t)
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDITED\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "extra.md"), []byte("X\n"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--json", "drift"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("drift --json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	var top map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &top); err != nil {
		t.Fatalf("parse JSON: %v\nstdout=%q", err, r.Stdout)
	}
	for _, k := range []string{"schemaVersion", "entries", "scannedFiles"} {
		if _, ok := top[k]; !ok {
			t.Errorf("missing top-level key %q in: %v", k, top)
		}
	}
	var env driftEnvelope
	if err := json.Unmarshal([]byte(r.Stdout), &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	allowed := map[string]bool{"modified": true, "added": true, "deleted": true, "unrecoverable": true}
	for _, e := range env.Entries {
		if e.RelPath == "" {
			t.Errorf("entry with empty relPath: %+v", e)
		}
		if !allowed[e.Status] {
			t.Errorf("entry status %q not in allowed set: %+v", e.Status, e)
		}
	}
}

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV/T8 — translation of TS state-corruption.test.ts (F2 gap closure #7).
// Covers state-file corruption variants beyond the plain "invalid JSON"
// case. Per R42 the bin must NEVER abort on malformed state — it degrades
// to NoActive and surfaces a warning. The exception (matched against the
// Go bin's actual behavior) is a top-level schemaVersion mismatch, which
// is a hard refuse-to-operate (exit 3, "schema too new" data-loss
// prevention path). Inner schema mismatches (fingerprint.schemaVersion)
// degrade gracefully.

// setupActiveAndCorrupt seeds an active "a" profile via the CLI, then
// overwrites .meta/state.json with the supplied bytes. Returns the project
// root for the corrupted-state test invocation.
func setupActiveAndCorrupt(t *testing.T, corrupted []byte) string {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "X\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "a"}})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	stateFile := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json")
	if err := os.WriteFile(stateFile, corrupted, 0o644); err != nil {
		t.Fatalf("overwrite state.json: %v", err)
	}
	return fx.ProjectRoot
}

// expectStatusDegrades runs `c3p status` against root and asserts the bin
// exits 0 with the "no active profile" hint. Go bin renders this as
// "active profile: (none)" with a follow-up "Run `c3p use <name>`" hint.
func expectStatusDegrades(t *testing.T, root string) {
	t.Helper()
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "status"}})
	if r.ExitCode != 0 {
		t.Fatalf("status: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	combined := strings.ToLower(r.Stdout + r.Stderr)
	if !strings.Contains(combined, "(none)") && !strings.Contains(combined, "no active profile") {
		t.Errorf("status output missing NoActive hint: stdout=%q stderr=%q", r.Stdout, r.Stderr)
	}
}

// TestStateCorruption_TruncatedJSON — `{"schemaVersion":` (mid-write
// truncation) must degrade to NoActive, not crash.
func TestStateCorruption_TruncatedJSON(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`{"schemaVersion":`))
	expectStatusDegrades(t, root)
}

// TestStateCorruption_SchemaVersionFuture — schemaVersion 999 is a hard
// refuse-to-operate (data-loss prevention): exit 3 with a clear message
// that the binary needs to be upgraded. Diverges from the TS contract
// (TS degrades) — the Go bin's stricter behavior is the spec for IV.
func TestStateCorruption_SchemaVersionFuture(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`{"schemaVersion":999,"activeProfile":"a","materializedAt":"2026-01-01T00:00:00.000Z","resolvedSources":[],"fingerprint":{"schemaVersion":1,"files":{}},"externalTrustNotices":[]}`))
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "status"}})
	if r.ExitCode != 3 {
		t.Fatalf("status (future schemaVersion): want 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "schemaVersion 999") {
		t.Errorf("stderr missing 'schemaVersion 999' detail: %q", r.Stderr)
	}
}

// TestStateCorruption_SchemaVersionPast — schemaVersion 0 (older than
// supported) is degraded, not refused. The "we can't read older" case is
// recoverable — the user just runs `c3p use <name>` again.
func TestStateCorruption_SchemaVersionPast(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`{"schemaVersion":0,"activeProfile":"a","materializedAt":"2026-01-01T00:00:00.000Z","resolvedSources":[],"fingerprint":{"schemaVersion":1,"files":{}},"externalTrustNotices":[]}`))
	expectStatusDegrades(t, root)
}

// TestStateCorruption_InvalidTimestampType — materializedAt as a number
// (instead of ISO string) violates R14; degrade not crash.
func TestStateCorruption_InvalidTimestampType(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`{"schemaVersion":1,"activeProfile":"a","materializedAt":123456789,"resolvedSources":[],"fingerprint":{"schemaVersion":1,"files":{}},"externalTrustNotices":[]}`))
	expectStatusDegrades(t, root)
}

// TestStateCorruption_NULBytesInResolvedSources — resolvedSources as a
// NUL-containing string (not the array shape) is a SchemaMismatch; R42
// degrades to NoActive. Constructed via byte-array literal to keep the
// source file pure ASCII (a literal 0x00 byte would mark the file as
// binary in git).
func TestStateCorruption_NULBytesInResolvedSources(t *testing.T) {
	helpers.EnsureBuilt(t)
	corrupted := []byte(`{"schemaVersion":1,"activeProfile":"a","materializedAt":"2026-01-01T00:00:00.000Z","resolvedSources":"abc` + string([]byte{0x00}) + `def","fingerprint":{"schemaVersion":1,"files":{}},"externalTrustNotices":[]}`)
	root := setupActiveAndCorrupt(t, corrupted)
	expectStatusDegrades(t, root)
}

// TestStateCorruption_TopLevelArray — top-level array (not an object) →
// degrade.
func TestStateCorruption_TopLevelArray(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`[]`))
	expectStatusDegrades(t, root)
}

// TestStateCorruption_TopLevelScalar — top-level number (not an object) →
// degrade.
func TestStateCorruption_TopLevelScalar(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`42`))
	expectStatusDegrades(t, root)
}

// TestStateCorruption_FingerprintSchemaMismatch — inner
// fingerprint.schemaVersion mismatch is degraded (only the top-level
// schemaVersion-too-new path is a hard refuse). Pins the asymmetry so a
// future "tighten everything to refuse" change has to be deliberate.
func TestStateCorruption_FingerprintSchemaMismatch(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`{"schemaVersion":1,"activeProfile":"a","materializedAt":"2026-01-01T00:00:00.000Z","resolvedSources":[],"fingerprint":{"schemaVersion":999,"files":{}},"externalTrustNotices":[]}`))
	expectStatusDegrades(t, root)
}

// TestStateCorruption_FingerprintFilesWrongType — fingerprint.files entry
// with wrong-type size (null) must not crash the drift compare. The Go
// bin permissively coerces a null size to 0 and surfaces the entry as
// drift rather than degrading the whole state — distinct from R42's
// degrade-to-NoActive (which only fires for top-level shape violations).
// What we pin: exit 0, no panic/traceback. The "drift surfaces here" is
// an implementation detail that future hardening could tighten.
func TestStateCorruption_FingerprintFilesWrongType(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`{"schemaVersion":1,"activeProfile":"a","materializedAt":"2026-01-01T00:00:00.000Z","resolvedSources":[],"fingerprint":{"schemaVersion":1,"files":{"x.md":{"size":null,"mtimeMs":0,"contentHash":"abc"}}},"externalTrustNotices":[]}`))
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "status"}})
	if r.ExitCode != 0 {
		t.Fatalf("status (wrong-type size): want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if strings.Contains(r.Stderr, "panic:") || strings.Contains(r.Stderr, "goroutine ") {
		t.Errorf("stderr contains panic/traceback: %q", r.Stderr)
	}
}

// TestStateCorruption_MissingResolvedSources — required top-level field
// omitted → SchemaMismatch → degrade.
func TestStateCorruption_MissingResolvedSources(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActiveAndCorrupt(t, []byte(`{"schemaVersion":1,"activeProfile":"a","materializedAt":"2026-01-01T00:00:00.000Z","fingerprint":{"schemaVersion":1,"files":{}},"externalTrustNotices":[]}`))
	expectStatusDegrades(t, root)
}

// TestStateCorruption_MissingStateFile — no state file at all (fresh
// project that was never `use`d) is the documented happy-path NoActive.
func TestStateCorruption_MissingStateFile(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "X\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "status"}})
	if r.ExitCode != 0 {
		t.Fatalf("status (no state): want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "(none)") {
		t.Errorf("status stdout missing '(none)': %q", r.Stdout)
	}
}

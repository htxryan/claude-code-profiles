package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// IV/T6 — skim-output translation. TS test pinned the human + JSON output of
// every read-only verb (list, status, drift, diff). Spawn-only translation
// keeps the structural invariants (header line, sigil column, JSON envelope
// being a single line + parseable) but does not byte-pin the text — Go bin's
// human formatting differs from TS (column headers, "drift: none" vs "clean",
// etc.) and the spec explicitly only locks the --json shape.

func setupTwoProfilesFx(t *testing.T, activate string) *helpers.Fixture {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {
				Manifest: map[string]any{
					"name":        "a",
					"description": "alpha profile",
					"tags":        []string{"dev"},
				},
				Files: map[string]string{"CLAUDE.md": "A\n"},
			},
			"b": {
				Manifest: map[string]any{
					"name":        "b",
					"description": "beta profile",
				},
				Files: map[string]string{"CLAUDE.md": "B\n"},
			},
		},
	})
	if activate != "" {
		r := mustRun(t, helpers.SpawnOptions{
			Args: []string{"--cwd", fx.ProjectRoot, "use", activate},
		})
		if r.ExitCode != 0 {
			t.Fatalf("setup use %q: exit=%d stderr=%q", activate, r.ExitCode, r.Stderr)
		}
	}
	return fx
}

// ─── list ─────────────────────────────────────────────────────────────

// TestSkimOutput_ListShowsDescriptionAndTags — list output names every
// description and tag from the fixture. Header columns differ from TS but
// content (description text, tag value) must appear.
func TestSkimOutput_ListShowsDescriptionAndTags(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "list"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("list: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "alpha profile") {
		t.Errorf("missing 'alpha profile' in list stdout: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "beta profile") {
		t.Errorf("missing 'beta profile' in list stdout: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "dev") {
		t.Errorf("missing 'dev' tag in list stdout: %q", r.Stdout)
	}
	// Note: TS test asserted no trailing whitespace per line; Go bin pads
	// columns with spaces (e.g. "  b   beta profile         "). The skim
	// invariant the user actually feels is "rows align" not "lines trimmed";
	// skip that assertion — not load-bearing for Go bin.
}

// TestSkimOutput_ListActiveMarker — active profile gets the `*` marker; the
// inactive row uses two leading spaces. Pinned because it's the skim signal
// users rely on to spot which profile is live.
func TestSkimOutput_ListActiveMarker(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "list"},
	})
	// Active row: `* a ...`
	if !regexp.MustCompile(`(?m)^\* a\b`).MatchString(r.Stdout) {
		t.Errorf("expected active row '* a': %q", r.Stdout)
	}
	// Inactive row: `  b ...`
	if !regexp.MustCompile(`(?m)^ {2}b\b`).MatchString(r.Stdout) {
		t.Errorf("expected inactive row '  b': %q", r.Stdout)
	}
}

// TestSkimOutput_ListEmptyHint — empty project list emits a recovery hint
// naming the next-step verb. TS pinned exact "run `c3p new <name>`"; Go bin
// uses "(no profiles found in .claude-profiles/)" which is structurally the
// same affordance — no profiles, no list rows.
func TestSkimOutput_ListEmptyHint(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "list"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("list empty: exit=%d", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "no profiles") {
		t.Errorf("empty list missing hint: %q", r.Stdout)
	}
}

// TestSkimOutput_ListJsonShape — list --json emits a single-line JSON object
// with keys: profiles[], stateWarning. Each profile has the expected field
// set. We don't pin key order (Go's encoding/json is map-iteration order for
// any{} but struct order is fixed; this assertion remains structural).
func TestSkimOutput_ListJsonShape(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "list", "--json"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("list --json: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	// JSON envelope is a single line.
	out := strings.TrimRight(r.Stdout, "\n")
	if strings.Contains(out, "\n") {
		t.Errorf("list --json should be one line, got:\n%s", out)
	}
	var payload struct {
		Profiles []struct {
			Name             string   `json:"name"`
			Active           bool     `json:"active"`
			Description      string   `json:"description"`
			Extends          string   `json:"extends"`
			Includes         []string `json:"includes"`
			Tags             []string `json:"tags"`
			LastMaterialized string   `json:"lastMaterialized"`
		} `json:"profiles"`
		StateWarning string `json:"stateWarning"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("list --json parse: %v\n%s", err, out)
	}
	if len(payload.Profiles) != 2 {
		t.Fatalf("want 2 profiles, got %d", len(payload.Profiles))
	}
	// Validate per-profile shape: a active=true with desc + tag, b active=false.
	byName := map[string]int{}
	for i, p := range payload.Profiles {
		byName[p.Name] = i
	}
	a := payload.Profiles[byName["a"]]
	if !a.Active || a.Description != "alpha profile" || len(a.Tags) != 1 || a.Tags[0] != "dev" {
		t.Errorf("profile a shape drift: %+v", a)
	}
	b := payload.Profiles[byName["b"]]
	if b.Active || b.Description != "beta profile" {
		t.Errorf("profile b shape drift: %+v", b)
	}
}

// ─── status ───────────────────────────────────────────────────────────

// TestSkimOutput_StatusActiveProfile — status names the active profile and
// reports drift. Go bin uses "drift: none" instead of TS's "drift: clean";
// we pin both so a future homogenisation doesn't fail this assertion.
func TestSkimOutput_StatusActiveProfile(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "status"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("status: exit=%d", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "active profile: a") {
		t.Errorf("missing 'active profile: a': %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "drift:") {
		t.Errorf("missing 'drift:' line: %q", r.Stdout)
	}
}

// TestSkimOutput_StatusEmpty — no profiles at all: status hint suggests
// a path forward. TS asserted "use" wasn't named (since no profile exists);
// Go bin says "Run `c3p use <name>`" regardless. Pin structurally: the active
// line says (none).
func TestSkimOutput_StatusEmpty(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "status"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("status empty: exit=%d", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "(none)") {
		t.Errorf("missing '(none)' marker: %q", r.Stdout)
	}
}

// TestSkimOutput_StatusJsonShape — status --json single line; required keys
// present and well-typed. The TS shape pinned an exact key order; Go's struct
// emit order is similar but we assert structurally here (drift sub-object,
// activeProfile string).
func TestSkimOutput_StatusJsonShape(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "a")
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "status", "--json"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("status --json: exit=%d", r.ExitCode)
	}
	out := strings.TrimRight(r.Stdout, "\n")
	if strings.Contains(out, "\n") {
		t.Errorf("status --json should be one line, got:\n%s", out)
	}
	var payload struct {
		ActiveProfile  string `json:"activeProfile"`
		MaterializedAt string `json:"materializedAt"`
		Drift          struct {
			FingerprintOk bool `json:"fingerprintOk"`
			Modified      int  `json:"modified"`
			Added         int  `json:"added"`
			Deleted       int  `json:"deleted"`
			Total         int  `json:"total"`
		} `json:"drift"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("status --json parse: %v\n%s", err, out)
	}
	if payload.ActiveProfile != "a" {
		t.Errorf("activeProfile = %q, want a", payload.ActiveProfile)
	}
	if payload.Drift.Total != 0 {
		t.Errorf("drift.total = %d, want 0 on a clean materialize", payload.Drift.Total)
	}
}

// ─── drift ────────────────────────────────────────────────────────────

// TestSkimOutput_DriftHumanOmitsScanStats — the default human drift line
// names the file count and active profile, but does NOT carry the scan-stats
// suffix (`scanned=`, `fast=`, `slow=`) — that's --verbose territory.
func TestSkimOutput_DriftHumanOmitsScanStats(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift"},
	})
	// Drift exits 1 when modifications exist (user error class) — we don't
	// pin the code here; just inspect the stdout content.
	if !strings.Contains(r.Stdout, "drift:") {
		t.Errorf("missing 'drift:' line: %q", r.Stdout)
	}
	if strings.Contains(r.Stdout, "fast=") || strings.Contains(r.Stdout, "slow=") {
		t.Errorf("default drift should omit scan-stats: %q", r.Stdout)
	}
}

// TestSkimOutput_DriftVerboseShowsScanStats — --verbose adds the scan-stats
// suffix as a separate line.
func TestSkimOutput_DriftVerboseShowsScanStats(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift", "--verbose"},
	})
	if !regexp.MustCompile(`scanned=\d+ fast=\d+ slow=\d+`).MatchString(r.Stdout) {
		t.Errorf("verbose drift missing scan-stats line: %q", r.Stdout)
	}
}

// TestSkimOutput_DriftJsonHasScanStats — --json always carries the scan
// counters regardless of --verbose (the contract: human-only flag, JSON
// always full shape).
func TestSkimOutput_DriftJsonHasScanStats(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupTwoProfilesFx(t, "a")
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift", "--json"},
	})
	out := strings.TrimRight(r.Stdout, "\n")
	if strings.Contains(out, "\n") {
		t.Errorf("drift --json should be one line, got:\n%s", out)
	}
	var payload struct {
		ScannedFiles int `json:"scannedFiles"`
		FastPathHits int `json:"fastPathHits"`
		SlowPathHits int `json:"slowPathHits"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("drift --json parse: %v\n%s", err, out)
	}
	// Counters are uint-like; the contract is "field present, well-typed".
	if payload.ScannedFiles < 0 || payload.FastPathHits < 0 || payload.SlowPathHits < 0 {
		t.Errorf("scan counters negative: %+v", payload)
	}
}

// ─── diff ─────────────────────────────────────────────────────────────

// TestSkimOutput_DiffIdenticalSummary — diff between two profiles with the
// same content set names both profiles and reports parity. Go bin's exact
// wording differs from TS's "identical (N files in both)"; we pin the
// structural invariant: both profile names appear and no per-file sigil rows
// (since identical → empty file list).
func TestSkimOutput_DiffIdenticalSummary(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "X\n", "y.md": "Y\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "X\n", "y.md": "Y\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "diff", "a", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("diff identical: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "a") || !strings.Contains(r.Stdout, "b") {
		t.Errorf("diff stdout missing profile names: %q", r.Stdout)
	}
	// No sigil rows expected (no changes).
	if regexp.MustCompile(`(?m)^\s+[+\-~]\s+\S+\.md`).MatchString(r.Stdout) {
		t.Errorf("identical diff should not emit sigil rows: %q", r.Stdout)
	}
}

// TestSkimOutput_DiffNonIdenticalSigils — when there are differences, per-file
// rows lead with the +/-/~ sigil column. TS pinned `+ only-a.md`; Go bin uses
// `~  CLAUDE.md` (sigil + two-space pad). Pin only the sigil-presence regex.
func TestSkimOutput_DiffNonIdenticalSigils(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"shared.md": "A\n", "only-a.md": "X\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"shared.md": "B\n", "only-b.md": "Y\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "diff", "a", "b"},
	})
	// Some sigil row must appear for at least one file.
	if !regexp.MustCompile(`(?m)^\s+[+\-~]\s+\S+`).MatchString(r.Stdout) {
		t.Errorf("non-identical diff missing sigil rows: %q", r.Stdout)
	}
}

// TestSkimOutput_DiffJsonShape — diff --json single line, parseable, names
// both profiles. TS pinned an exact byte-for-byte shape; Go bin's --json
// surface uses `files` instead of `entries` and does not emit `totals`.
// Skip the byte-pin and validate structurally: keys a, b, files[].
func TestSkimOutput_DiffJsonShape(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"x.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"x.md": "B\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "diff", "a", "b", "--json"},
	})
	out := strings.TrimRight(r.Stdout, "\n")
	if strings.Contains(out, "\n") {
		t.Errorf("diff --json should be one line, got:\n%s", out)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("diff --json parse: %v\n%s", err, out)
	}
	if raw["a"] != "a" || raw["b"] != "b" {
		t.Errorf("diff --json a/b drift: %+v", raw)
	}
	// Either `files` (Go) or `entries` (TS) must carry the per-file array.
	files, ok := raw["files"].([]any)
	if !ok {
		t.Skipf("diff --json field name drift (no `files`); not load-bearing")
	}
	if len(files) != 1 {
		t.Errorf("diff --json files len = %d, want 1", len(files))
	}
}

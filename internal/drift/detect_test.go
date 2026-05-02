package drift_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/htxryan/c3p/internal/drift"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

// makeDriftPlan builds a minimal ResolvedPlan for drift tests. Two
// contributors so provenance assertions can verify the source-set
// granularity.
func makeDriftPlan() resolver.ResolvedPlan {
	return resolver.ResolvedPlan{
		SchemaVersion: resolver.ResolvedPlanSchemaVersion,
		ProfileName:   "leaf",
		Contributors: []resolver.Contributor{
			{Kind: resolver.ContributorAncestor, ID: "base", RootPath: "/abs/base"},
			{Kind: resolver.ContributorProfile, ID: "leaf", RootPath: "/abs/leaf"},
		},
		Files:         []resolver.PlanFile{},
		Warnings:      []resolver.ResolutionWarning{},
		ExternalPaths: []resolver.ExternalTrustEntry{},
	}
}

func driftMergedFile(relPath, body string) merge.MergedFile {
	return merge.MergedFile{
		Path:          relPath,
		Bytes:         []byte(body),
		Contributors:  []string{"leaf"},
		MergePolicy:   resolver.MergePolicyLastWins,
		Destination:   resolver.DestinationClaude,
		SchemaVersion: merge.MergedFileSchemaVersion,
	}
}

func materializeBaseTree(t *testing.T) (state.StatePaths, resolver.ResolvedPlan, []merge.MergedFile) {
	t.Helper()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	plan := makeDriftPlan()
	merged := []merge.MergedFile{
		driftMergedFile("CLAUDE.md", "BASE\nLEAF\n"),
		driftMergedFile("agents/a.md", "AGENT-A"),
	}
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, nil); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	return paths, plan, merged
}

func TestDetectDrift_FingerprintNotOkWhenNoActive(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if report.FingerprintOk {
		t.Errorf("FingerprintOk should be false when no active profile")
	}
	if report.Active != nil {
		t.Errorf("Active = %v, want nil", report.Active)
	}
	if len(report.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(report.Entries))
	}
	if report.ScannedFiles != 0 {
		t.Errorf("ScannedFiles = %d, want 0", report.ScannedFiles)
	}
}

func TestDetectDrift_NoEntriesWhenLiveMatchesRecorded(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if !report.FingerprintOk {
		t.Errorf("FingerprintOk = false, want true")
	}
	if report.Active == nil || *report.Active != "leaf" {
		t.Errorf("Active = %v, want leaf", report.Active)
	}
	if len(report.Entries) != 0 {
		t.Errorf("entries = %v, want []", report.Entries)
	}
	if report.ScannedFiles != 2 {
		t.Errorf("ScannedFiles = %d, want 2", report.ScannedFiles)
	}
	if report.FastPathHits != 2 {
		t.Errorf("FastPathHits = %d, want 2 (mtime+size matched)", report.FastPathHits)
	}
	if report.SlowPathHits != 0 {
		t.Errorf("SlowPathHits = %d, want 0", report.SlowPathHits)
	}
}

// R19 + PR6 #8: detect modified files (slow-path hash check).
func TestDetectDrift_R19_DetectsModified(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	target := filepath.Join(paths.ClaudeDir, "CLAUDE.md")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := os.WriteFile(target, []byte("XXXX\nYYYY\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Push mtime forward so the metadata fast path notices.
	newMtime := info.ModTime().Add(time.Second)
	if err := os.Chtimes(target, info.ModTime(), newMtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(report.Entries))
	}
	e := report.Entries[0]
	if e.RelPath != "CLAUDE.md" {
		t.Errorf("relPath = %q, want CLAUDE.md", e.RelPath)
	}
	if e.Status != drift.DriftStatusModified {
		t.Errorf("status = %q, want modified", e.Status)
	}
	if report.SlowPathHits < 1 {
		t.Errorf("SlowPathHits = %d, want >= 1", report.SlowPathHits)
	}
}

// R19: detects added files.
func TestDetectDrift_R19_DetectsAdded(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "scratch.md"), []byte("scratch"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(report.Entries))
	}
	if report.Entries[0].RelPath != "scratch.md" || report.Entries[0].Status != drift.DriftStatusAdded {
		t.Errorf("entry = %+v, want added scratch.md", report.Entries[0])
	}
}

// R19: detects deleted files.
func TestDetectDrift_R19_DetectsDeleted(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	if err := os.Remove(filepath.Join(paths.ClaudeDir, "agents/a.md")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(report.Entries))
	}
	if report.Entries[0].RelPath != "agents/a.md" || report.Entries[0].Status != drift.DriftStatusDeleted {
		t.Errorf("entry = %+v, want deleted agents/a.md", report.Entries[0])
	}
}

// R20: per-entry provenance includes recorded resolved sources.
func TestDetectDrift_R20_ProvenanceIncludesContributors(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("drifted\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(report.Entries))
	}
	ids := []string{}
	for _, p := range report.Entries[0].Provenance {
		ids = append(ids, p.ID)
	}
	sort.Strings(ids)
	want := []string{"base", "leaf"}
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Errorf("provenance ids = %v, want %v", ids, want)
	}
}

func TestDetectDrift_EntriesLexSorted(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	for _, name := range []string{"z-late.md", "a-early.md", "m-middle.md"} {
		if err := os.WriteFile(filepath.Join(paths.ClaudeDir, name), []byte(name[:1]), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	paths_ := make([]string, len(report.Entries))
	for i, e := range report.Entries {
		paths_[i] = e.RelPath
	}
	sorted := append([]string(nil), paths_...)
	sort.Strings(sorted)
	for i := range paths_ {
		if paths_[i] != sorted[i] {
			t.Errorf("entries not lex-sorted; got %v want %v", paths_, sorted)
			break
		}
	}
}

func TestDetectDrift_SchemaVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if report.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", report.SchemaVersion)
	}
}

// R42 / S17: surfaces StateReadWarning when state.json is corrupted.
func TestDetectDrift_R42_SurfacesParseWarning(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if report.FingerprintOk {
		t.Errorf("FingerprintOk = true, want false")
	}
	if report.Warning == nil {
		t.Fatalf("Warning is nil; expected ParseError")
	}
	if report.Warning.Code != state.StateReadWarningParseError {
		t.Errorf("Warning code = %q, want ParseError", report.Warning.Code)
	}
}

// FingerprintOk false when fingerprint.schemaVersion mismatches (top-level
// schemaVersion mismatch downgrades to default state).
func TestDetectDrift_FingerprintOkFalseWhenSchemaMismatches(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// schemaVersion 0 (below current 1) hits the SchemaMismatch warning
	// path — we want to verify drift still reports FingerprintOk:false
	// and surfaces the warning rather than crashing.
	contents := `{"schemaVersion":0,"activeProfile":"leaf","materializedAt":null,"resolvedSources":[],"fingerprint":{"schemaVersion":1,"files":{}},"externalTrustNotices":[]}`
	if err := os.WriteFile(paths.StateFile, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if report.FingerprintOk {
		t.Errorf("FingerprintOk = true, want false")
	}
	if report.Warning == nil {
		t.Errorf("Warning is nil; expected SchemaMismatch")
	}
}

// Documented metric invariant: fastPathHits + slowPathHits ===
// scannedFiles + (deleted count). Deletions count as slow because
// the metadata walk didn't see them.
func TestDetectDrift_MetricInvariant(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "scratch.md"), []byte("scratch"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Remove(filepath.Join(paths.ClaudeDir, "agents/a.md")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	deleted := 0
	for _, e := range report.Entries {
		if e.Status == drift.DriftStatusDeleted {
			deleted++
		}
	}
	if got := report.FastPathHits + report.SlowPathHits; got != report.ScannedFiles+deleted {
		t.Errorf("metric invariant violated: fast(%d)+slow(%d)=%d, want scanned(%d)+deleted(%d)=%d",
			report.FastPathHits, report.SlowPathHits, got,
			report.ScannedFiles, deleted, report.ScannedFiles+deleted)
	}
}

// Provenance is per-entry copy: mutating one entry's slice does not
// cross-contaminate siblings.
func TestDetectDrift_ProvenanceIsPerEntryCopy(t *testing.T) {
	t.Parallel()
	paths, _, _ := materializeBaseTree(t)
	for _, name := range []string{"f1.md", "f2.md"} {
		if err := os.WriteFile(filepath.Join(paths.ClaudeDir, name), []byte(name[:1]), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(report.Entries) < 2 {
		t.Fatalf("entries = %d, want >= 2", len(report.Entries))
	}
	a := report.Entries[0]
	b := report.Entries[1]
	// Mutate a's provenance and confirm b's is untouched.
	if len(a.Provenance) == 0 || len(b.Provenance) == 0 {
		t.Fatalf("provenance empty for at least one entry")
	}
	a.Provenance[0].ID = "MUTATED"
	if b.Provenance[0].ID == "MUTATED" {
		t.Errorf("provenance shared between entries — mutating one cross-contaminated the other")
	}
}

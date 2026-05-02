package drift_test

import (
	"os"
	"strings"
	"testing"

	"github.com/htxryan/c3p/internal/drift"
	"github.com/htxryan/c3p/internal/markers"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

// makeRootSectionPlan builds a minimal plan for projectRoot section tests.
func makeRootSectionPlan(profileName string) resolver.ResolvedPlan {
	return resolver.ResolvedPlan{
		SchemaVersion: resolver.ResolvedPlanSchemaVersion,
		ProfileName:   profileName,
		Contributors: []resolver.Contributor{
			{Kind: resolver.ContributorProfile, ID: profileName, RootPath: "/abs/" + profileName},
		},
		Files:         []resolver.PlanFile{},
		Warnings:      []resolver.ResolutionWarning{},
		ExternalPaths: []resolver.ExternalTrustEntry{},
	}
}

func mergedRoot(body string) merge.MergedFile {
	return merge.MergedFile{
		Path:          "CLAUDE.md",
		Bytes:         []byte(body),
		Contributors:  []string{"leaf"},
		MergePolicy:   resolver.MergePolicyConcat,
		Destination:   resolver.DestinationProjectRoot,
		SchemaVersion: merge.MergedFileSchemaVersion,
	}
}

// setupWithRootSection writes a project-root CLAUDE.md with markers around
// the given body, then materializes so state.RootClaudeMdSection is populated.
func setupWithRootSection(t *testing.T, sectionBody string) state.StatePaths {
	t.Helper()
	root := t.TempDir()
	// Stand up the project-root CLAUDE.md with user content above + below
	// the markers.
	before := "# My project\n\nUser content above.\n"
	after := "\n## My notes\n\nUser content below.\n"
	block := markers.RenderManagedBlock(sectionBody, 1)
	if err := os.WriteFile(root+"/CLAUDE.md", []byte(before+block+after), 0o644); err != nil {
		t.Fatalf("WriteFile root CLAUDE.md: %v", err)
	}
	paths := state.BuildStatePaths(root)
	plan := makeRootSectionPlan("leaf")
	if _, err := state.Materialize(paths, plan, []merge.MergedFile{mergedRoot(sectionBody)}, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	return paths
}

func findRootEntry(report drift.DriftReport) *drift.DriftEntry {
	for i := range report.Entries {
		if report.Entries[i].Destination == drift.DriftDestinationProjectRoot {
			return &report.Entries[i]
		}
	}
	return nil
}

// AC-7a: byte change inside the section → status 'modified'.
func TestDetectDrift_AC7a_InsideEditMarkedModified(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "ORIGINAL-SECTION")
	live, err := os.ReadFile(paths.RootClaudeMdFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	tampered := strings.Replace(string(live), "ORIGINAL-SECTION", "USER-EDITED-SECTION", 1)
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(tampered), 0o644); err != nil {
		t.Fatalf("WriteFile tampered: %v", err)
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if !report.FingerprintOk {
		t.Fatalf("FingerprintOk = false")
	}
	e := findRootEntry(report)
	if e == nil {
		t.Fatalf("no projectRoot entry; got %+v", report.Entries)
	}
	if e.RelPath != "CLAUDE.md" {
		t.Errorf("RelPath = %q, want CLAUDE.md", e.RelPath)
	}
	if e.Status != drift.DriftStatusModified {
		t.Errorf("Status = %q, want modified", e.Status)
	}
	if len(e.Provenance) != 1 || e.Provenance[0].ID != "leaf" {
		t.Errorf("Provenance = %+v, want [leaf]", e.Provenance)
	}
}

// AC-7a: section grown (length differs).
func TestDetectDrift_AC7a_GrownSectionMarkedModified(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "SHORT")
	live, _ := os.ReadFile(paths.RootClaudeMdFile)
	tampered := strings.Replace(string(live), "SHORT", "MUCH\nLONGER\nSECTION\nCONTENT", 1)
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(tampered), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	e := findRootEntry(report)
	if e == nil || e.Status != drift.DriftStatusModified {
		t.Fatalf("entry = %+v, want modified", e)
	}
}

// AC-7a: section shrunk.
func TestDetectDrift_AC7a_ShrunkSectionMarkedModified(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "LONG-ORIGINAL-SECTION-WITH-CONTENT")
	live, _ := os.ReadFile(paths.RootClaudeMdFile)
	tampered := strings.Replace(string(live), "LONG-ORIGINAL-SECTION-WITH-CONTENT", "tiny", 1)
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(tampered), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	e := findRootEntry(report)
	if e == nil || e.Status != drift.DriftStatusModified {
		t.Fatalf("entry = %+v, want modified", e)
	}
}

// AC-7b LOAD-BEARING: appending content below :end → no drift.
func TestDetectDrift_AC7b_AppendBelowMarkerIgnored(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "STABLE-SECTION")
	live, _ := os.ReadFile(paths.RootClaudeMdFile)
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(string(live)+"\n## A new heading the user added\n\nMore prose.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if e := findRootEntry(report); e != nil {
		t.Errorf("got projectRoot entry %+v; want none", e)
	}
}

// AC-7b LOAD-BEARING: prepending content above :begin → no drift.
func TestDetectDrift_AC7b_PrependAboveMarkerIgnored(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "STABLE-SECTION")
	live, _ := os.ReadFile(paths.RootClaudeMdFile)
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte("# A new top heading the user added\n\nFresh prose at the top.\n\n"+string(live)), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if e := findRootEntry(report); e != nil {
		t.Errorf("got projectRoot entry %+v; want none", e)
	}
}

// AC-7b: replacing all user-owned bytes outside markers → no drift.
func TestDetectDrift_AC7b_OutsideRewriteIgnored(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "STABLE-SECTION")
	live, _ := os.ReadFile(paths.RootClaudeMdFile)
	beginIdx := strings.Index(string(live), "<!-- c3p:v1:begin")
	endMarker := "<!-- c3p:v1:end -->"
	endIdx := strings.Index(string(live), endMarker) + len(endMarker)
	middle := string(live)[beginIdx:endIdx]
	newAbove := "% Totally different above-content\n\nLorem ipsum.\n\n"
	newBelow := "\n\n% Totally different below-content\nDolor sit amet.\n"
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(newAbove+middle+newBelow), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if e := findRootEntry(report); e != nil {
		t.Errorf("got projectRoot entry %+v; want none", e)
	}
}

// Mixed: inside + outside edit → drift detected (only inside matters).
func TestDetectDrift_MixedInsideOutsideEditDetected(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "ORIG")
	live, _ := os.ReadFile(paths.RootClaudeMdFile)
	tampered := strings.Replace(string(live), "ORIG", "INSIDE-EDIT", 1)
	tampered = "# New top heading\n\n" + tampered + "\n## New bottom heading\n"
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(tampered), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	e := findRootEntry(report)
	if e == nil || e.Status != drift.DriftStatusModified {
		t.Errorf("entry = %+v, want modified", e)
	}
}

// Markers deleted → unrecoverable status with actionable error message.
func TestDetectDrift_MarkersDeletedUnrecoverable(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "STABLE")
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte("# Plain CLAUDE.md\n\nNo markers anymore.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	e := findRootEntry(report)
	if e == nil {
		t.Fatalf("no projectRoot entry; got %+v", report.Entries)
	}
	if e.Status != drift.DriftStatusUnrecoverable {
		t.Errorf("Status = %q, want unrecoverable", e.Status)
	}
	if e.Error == "" {
		t.Errorf("Error empty; expected actionable message")
	}
	lower := strings.ToLower(e.Error)
	if !strings.Contains(lower, "init") && !strings.Contains(lower, "validate") {
		t.Errorf("Error should reference init/validate; got %q", e.Error)
	}
	if !strings.Contains(e.Error, paths.RootClaudeMdFile) {
		t.Errorf("Error should include file path; got %q", e.Error)
	}
}

// Malformed markers (lone :begin) → unrecoverable.
func TestDetectDrift_MalformedMarkersUnrecoverable(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "STABLE")
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte("# Broken\n<!-- c3p:v1:begin -->\nincomplete only\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	e := findRootEntry(report)
	if e == nil || e.Status != drift.DriftStatusUnrecoverable {
		t.Fatalf("entry = %+v, want unrecoverable", e)
	}
	lower := strings.ToLower(e.Error)
	if !strings.Contains(lower, "init") && !strings.Contains(lower, "validate") {
		t.Errorf("Error should reference init/validate; got %q", e.Error)
	}
}

// File removed entirely → unrecoverable (we recorded a section, file gone).
func TestDetectDrift_FileRemovedUnrecoverable(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "STABLE")
	if err := os.Remove(paths.RootClaudeMdFile); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	e := findRootEntry(report)
	if e == nil || e.Status != drift.DriftStatusUnrecoverable {
		t.Fatalf("entry = %+v, want unrecoverable", e)
	}
	lower := strings.ToLower(e.Error)
	if !strings.Contains(lower, "init") && !strings.Contains(lower, "validate") {
		t.Errorf("Error should reference init/validate; got %q", e.Error)
	}
}

// Smoke test: missing-file path does not panic.
func TestDetectDrift_FileRemovedDoesNotCrash(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "STABLE")
	if err := os.Remove(paths.RootClaudeMdFile); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := drift.DetectDrift(paths); err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
}

// After a clean materialize, no drift reported (section bytes match).
func TestDetectDrift_CleanMaterializeNoSectionDrift(t *testing.T) {
	t.Parallel()
	paths := setupWithRootSection(t, "CLEAN")
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if e := findRootEntry(report); e != nil {
		t.Errorf("got projectRoot entry %+v; want none", e)
	}
}

// When state has no rootClaudeMdSection (legacy / .claude-only), no
// projectRoot drift entry is produced — even if a CLAUDE.md exists.
func TestDetectDrift_NoSectionFingerprintProducesNoRootEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	plan := makeRootSectionPlan("leaf")
	merged := []merge.MergedFile{
		{
			Path:          "agents/x.md",
			Bytes:         []byte("X"),
			Contributors:  []string{"leaf"},
			MergePolicy:   resolver.MergePolicyLastWins,
			Destination:   resolver.DestinationClaude,
			SchemaVersion: merge.MergedFileSchemaVersion,
		},
	}
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte("# Random user file, never managed by us.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if e := findRootEntry(report); e != nil {
		t.Errorf("got projectRoot entry %+v; want none", e)
	}
}

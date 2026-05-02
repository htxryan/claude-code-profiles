package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	c3perr "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/markers"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

// makePlan builds a minimal ResolvedPlan suitable for materialize tests.
// Profile name is the only required field for materialize's state-write;
// Contributors/Files are filled in only when a test needs source-fingerprint
// behavior.
func makePlan(profileName string) resolver.ResolvedPlan {
	return resolver.ResolvedPlan{
		SchemaVersion: resolver.ResolvedPlanSchemaVersion,
		ProfileName:   profileName,
		Contributors: []resolver.Contributor{
			{
				Kind:     resolver.ContributorProfile,
				ID:       profileName,
				RootPath: "/abs/" + profileName,
				External: false,
			},
		},
		Files:         []resolver.PlanFile{},
		Warnings:      []resolver.ResolutionWarning{},
		ExternalPaths: []resolver.ExternalTrustEntry{},
	}
}

func mergedFile(relPath string, body string) merge.MergedFile {
	return merge.MergedFile{
		Path:          relPath,
		Bytes:         []byte(body),
		Contributors:  []string{"prof"},
		MergePolicy:   resolver.MergePolicyLastWins,
		Destination:   resolver.DestinationClaude,
		SchemaVersion: merge.MergedFileSchemaVersion,
	}
}

func mergedRootClaudeMd(body string) merge.MergedFile {
	return merge.MergedFile{
		Path:          "CLAUDE.md",
		Bytes:         []byte(body),
		Contributors:  []string{"prof"},
		MergePolicy:   resolver.MergePolicyConcat,
		Destination:   resolver.DestinationProjectRoot,
		SchemaVersion: merge.MergedFileSchemaVersion,
	}
}

// TestMaterialize_HappyPath_ClaudeOnly asserts the basic three-step protocol
// commits .claude/ contents and writes state.json with the active profile.
func TestMaterialize_HappyPath_ClaudeOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	plan := makePlan("dev")
	merged := []merge.MergedFile{
		mergedFile("settings.json", `{"a":1}`),
		mergedFile("CLAUDE.md", "hello"),
	}
	res, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if res.State.ActiveProfile == nil || *res.State.ActiveProfile != "dev" {
		t.Fatalf("activeProfile = %v, want dev", res.State.ActiveProfile)
	}
	got, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("settings.json = %q", got)
	}
	if exists, _ := state.PathExists(paths.PendingDir); exists {
		t.Fatalf("pending dir still exists after success")
	}
	if exists, _ := state.PathExists(paths.PriorDir); exists {
		t.Fatalf("prior dir still exists after success")
	}
}

// TestMaterialize_OverwritesExistingClaudeDir asserts step b moves the
// previous live tree aside and step c replaces it.
func TestMaterialize_OverwritesExistingClaudeDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "old"), []byte("OLD"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan := makePlan("dev")
	merged := []merge.MergedFile{mergedFile("new", "NEW")}
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if exists, _ := state.PathExists(filepath.Join(paths.ClaudeDir, "old")); exists {
		t.Fatalf("old file survived materialize")
	}
	got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "new"))
	if string(got) != "NEW" {
		t.Fatalf("new file contents = %q", got)
	}
}

// TestMaterialize_R45_AbortsWhenRootClaudeMdMarkersMissing covers the
// pre-flight: when the new plan contributes a projectRoot file and the live
// CLAUDE.md is missing or has malformed markers, the WHOLE materialize must
// abort — neither destination touched.
func TestMaterialize_R45_AbortsWhenRootClaudeMdMarkersMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	// Seed an existing live .claude/ — we'll assert it's untouched.
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "untouched"), []byte("OLD"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// projectRoot CLAUDE.md missing entirely (no init).
	plan := makePlan("dev")
	merged := []merge.MergedFile{
		mergedFile("settings.json", `{"a":1}`),
		mergedRootClaudeMd("section body"),
	}
	_, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, "")
	if err == nil {
		t.Fatalf("expected RootClaudeMdMarkersMissingError, got nil")
	}
	var rmErr *c3perr.RootClaudeMdMarkersMissingError
	if !errors.As(err, &rmErr) {
		t.Fatalf("error %v is not *RootClaudeMdMarkersMissingError", err)
	}
	// Live .claude/ must be untouched (atomic-across-destinations).
	if _, statErr := os.Stat(filepath.Join(paths.ClaudeDir, "untouched")); statErr != nil {
		t.Fatalf("live .claude/untouched gone: %v", statErr)
	}
	// State file must NOT have been written (no partial commit).
	if exists, _ := state.PathExists(paths.StateFile); exists {
		t.Fatalf("state.json written despite pre-flight abort")
	}
}

// TestMaterialize_RootSplice_HappyPath asserts that with valid markers in
// the live file, the splice runs after the .claude/ swap and records a
// section fingerprint.
func TestMaterialize_RootSplice_HappyPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	// Seed live root CLAUDE.md with markers (init's job in production).
	live := "User content above\n" + markers.RenderManagedBlock("", 1) + "User content below\n"
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(live), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan := makePlan("dev")
	merged := []merge.MergedFile{mergedRootClaudeMd("PROFILE BODY\n")}
	res, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	updated, err := os.ReadFile(paths.RootClaudeMdFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(updated), "PROFILE BODY") {
		t.Fatalf("splice did not land profile body; got:\n%s", updated)
	}
	if !strings.Contains(string(updated), "User content above") || !strings.Contains(string(updated), "User content below") {
		t.Fatalf("user-owned bytes outside markers were touched; got:\n%s", updated)
	}
	if res.State.RootClaudeMdSection == nil {
		t.Fatalf("rootClaudeMdSection nil; expected a fingerprint")
	}
	if res.State.RootClaudeMdSection.ContentHash == "" {
		t.Fatalf("rootClaudeMdSection.contentHash empty")
	}
}

// TestMaterialize_RootSplice_Idempotent asserts re-materializing the same
// plan twice produces byte-identical CLAUDE.md output (no extra newline
// growth at the seam).
func TestMaterialize_RootSplice_Idempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	live := "User above\n" + markers.RenderManagedBlock("", 1) + "User below\n"
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(live), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan := makePlan("dev")
	merged := []merge.MergedFile{mergedRootClaudeMd("BODY\n")}
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	first, _ := os.ReadFile(paths.RootClaudeMdFile)
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	second, _ := os.ReadFile(paths.RootClaudeMdFile)
	if string(first) != string(second) {
		t.Fatalf("non-idempotent splice:\nfirst=%q\nsecond=%q", first, second)
	}
}

// TestMaterialize_EmptySpliceClearsPriorBytes covers P1-B: when the new plan
// contributes nothing to projectRoot but the prior state had a section, the
// live file's section bytes must be cleared.
func TestMaterialize_EmptySpliceClearsPriorBytes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	live := "above\n" + markers.RenderManagedBlock("", 1) + "below\n"
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(live), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// First: a plan WITH a projectRoot contribution.
	plan1 := makePlan("withRoot")
	merged1 := []merge.MergedFile{mergedRootClaudeMd("OLDBODY\n")}
	if _, err := state.Materialize(paths, plan1, merged1, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	mid, _ := os.ReadFile(paths.RootClaudeMdFile)
	if !strings.Contains(string(mid), "OLDBODY") {
		t.Fatalf("first materialize did not land OLDBODY: %q", mid)
	}
	// Second: a plan WITHOUT a projectRoot contribution. The prior section
	// must be cleared; user bytes preserved.
	plan2 := makePlan("noRoot")
	merged2 := []merge.MergedFile{mergedFile("a.md", "x")}
	res, err := state.Materialize(paths, plan2, merged2, state.MaterializeOptions{}, "")
	if err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	final, _ := os.ReadFile(paths.RootClaudeMdFile)
	if strings.Contains(string(final), "OLDBODY") {
		t.Fatalf("OLDBODY not cleared: %q", final)
	}
	if !strings.Contains(string(final), "above") || !strings.Contains(string(final), "below") {
		t.Fatalf("user bytes mutilated: %q", final)
	}
	if res.State.RootClaudeMdSection != nil {
		t.Fatalf("rootClaudeMdSection = %+v, expected nil for no-contribution plan", res.State.RootClaudeMdSection)
	}
}

// TestMaterialize_RootSplice_StateWrittenBeforeSplice is the codex-flagged
// fitness function: when Materialize runs the root-CLAUDE.md splice, the
// state file MUST already carry the new rootSectionFp BEFORE the live root
// CLAUDE.md is mutated. Without that ordering, a fallible step between the
// splice and state-write (ComputeSourceFingerprint, WriteStateFile) leaves
// root mutated while state still points at the old plan — and reconcile
// only restores .claude/, not the root splice. The pre-splice hook lets us
// observe the on-disk state at exactly that moment.
func TestMaterialize_RootSplice_StateWrittenBeforeSplice(t *testing.T) {
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	live := "User above\n" + markers.RenderManagedBlock("", 1) + "User below\n"
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(live), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}

	type observation struct {
		stateExists       bool
		stateActiveProf   string
		stateRootFpHash   string
		rootBytesUnchanged bool
	}
	var obs observation
	originalLive, _ := os.ReadFile(paths.RootClaudeMdFile)

	restore := state.SetTestPreSpliceHook(func() {
		// state.json must already be on disk with the new fingerprint.
		res, err := state.ReadStateFile(paths)
		if err == nil && res.State.ActiveProfile != nil {
			obs.stateExists = true
			obs.stateActiveProf = *res.State.ActiveProfile
			if res.State.RootClaudeMdSection != nil {
				obs.stateRootFpHash = res.State.RootClaudeMdSection.ContentHash
			}
		}
		// Live root CLAUDE.md must NOT yet be mutated.
		current, _ := os.ReadFile(paths.RootClaudeMdFile)
		obs.rootBytesUnchanged = string(current) == string(originalLive)
	})
	defer restore()

	plan := makePlan("dev")
	merged := []merge.MergedFile{mergedRootClaudeMd("PROFILE BODY\n")}
	res, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if !obs.stateExists {
		t.Fatalf("state.json was not written before splice — ordering invariant broken")
	}
	if obs.stateActiveProf != "dev" {
		t.Fatalf("state at splice-time activeProfile = %q, want dev", obs.stateActiveProf)
	}
	if obs.stateRootFpHash == "" {
		t.Fatalf("state at splice-time has no rootClaudeMdSection fingerprint — must be pre-computed before splice")
	}
	if !obs.rootBytesUnchanged {
		t.Fatalf("root CLAUDE.md was mutated before state.json was written — ordering invariant broken")
	}
	// Sanity: the pre-computed fingerprint matches the post-splice fingerprint
	// (proving the precomputation is byte-identical to what the splice writes).
	if res.State.RootClaudeMdSection == nil || res.State.RootClaudeMdSection.ContentHash != obs.stateRootFpHash {
		t.Fatalf("post-splice fingerprint disagrees with pre-splice fingerprint baked into state.json")
	}
}

// TestMaterialize_PreservesExternalTrustNotices covers R37a: notices from
// prior state are kept; new external paths in the plan are appended.
func TestMaterialize_PreservesExternalTrustNotices(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	// Seed an existing state file with one notice.
	old := "old"
	priorState := state.DefaultState()
	priorState.MaterializedAt = &old
	priorState.ExternalTrustNotices = []state.ExternalTrustNotice{
		{Raw: "/abs/external", ResolvedPath: "/abs/external", NoticedAt: "2026-01-01T00:00:00.000Z"},
	}
	if err := state.WriteStateFile(paths, priorState); err != nil {
		t.Fatalf("seed: %v", err)
	}

	plan := makePlan("dev")
	plan.ExternalPaths = []resolver.ExternalTrustEntry{
		{Raw: "/abs/external", ResolvedPath: "/abs/external"}, // already noticed
		{Raw: "/abs/new", ResolvedPath: "/abs/new"},           // new
	}
	merged := []merge.MergedFile{mergedFile("a", "x")}
	res, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(res.State.ExternalTrustNotices) != 2 {
		t.Fatalf("notices = %d, want 2", len(res.State.ExternalTrustNotices))
	}
	if res.State.ExternalTrustNotices[0].NoticedAt != "2026-01-01T00:00:00.000Z" {
		t.Fatalf("existing notice timestamp clobbered")
	}
}

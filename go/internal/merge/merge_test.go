package merge_test

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
)

// ----- in-package fixture mirroring resolver tests -----

type profileSpec struct {
	manifest  any
	files     map[string]string
	rootFiles map[string]string
}

type componentSpec struct {
	files map[string]string
}

type fixtureSpec struct {
	profiles   map[string]profileSpec
	components map[string]componentSpec
}

type fixture struct {
	projectRoot string
}

func makeFixture(t *testing.T, spec fixtureSpec) *fixture {
	t.Helper()
	tmp := t.TempDir()
	pr := filepath.Join(tmp, "project")
	if err := os.MkdirAll(pr, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	profilesDir := filepath.Join(pr, ".claude-profiles")
	for name, p := range spec.profiles {
		dir := filepath.Join(profilesDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir profile %q: %v", name, err)
		}
		writeManifest(t, filepath.Join(dir, "profile.json"), p.manifest)
		writeTree(t, filepath.Join(dir, ".claude"), p.files)
		writeTree(t, dir, p.rootFiles)
	}
	for name, c := range spec.components {
		dir := filepath.Join(profilesDir, "_components", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir component %q: %v", name, err)
		}
		writeTree(t, filepath.Join(dir, ".claude"), c.files)
	}
	return &fixture{projectRoot: pr}
}

func writeManifest(t *testing.T, path string, m any) {
	t.Helper()
	if m == nil {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// readFromDisk is the test-side ReadFunc — D2 itself is FS-IO-free.
func readFromDisk(absPath string) ([]byte, error) {
	return os.ReadFile(absPath)
}

func resolvePlan(t *testing.T, profile, projectRoot string) *resolver.ResolvedPlan {
	t.Helper()
	plan, err := resolver.Resolve(profile, resolver.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		t.Fatalf("resolve(%q): %v", profile, err)
	}
	return plan
}

func indexByPathDest(merged []merge.MergedFile) map[string]merge.MergedFile {
	out := make(map[string]merge.MergedFile, len(merged))
	for _, m := range merged {
		out[fmt.Sprintf("%s::%s", m.Destination, m.Path)] = m
	}
	return out
}

// ─── orchestrator behavior ───────────────────────────────────────────────

func TestMerge_OneEntryPerRelPathLexSorted(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {
				manifest: map[string]any{},
				files: map[string]string{
					"CLAUDE.md":      "# leaf\n",
					"settings.json":  `{"x":1}`,
					"agents/foo.txt": "foo",
					"z.txt":          "z",
				},
			},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	paths := make([]string, len(merged))
	for i, m := range merged {
		paths[i] = m.Path
	}
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	for i := range paths {
		if paths[i] != sorted[i] {
			t.Fatalf("not lex-sorted: %v", paths)
		}
	}
	seen := map[string]struct{}{}
	for _, m := range merged {
		key := fmt.Sprintf("%s::%s", m.Destination, m.Path)
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate (dest, path): %s", key)
		}
		seen[key] = struct{}{}
	}
	want := []string{"CLAUDE.md", "agents/foo.txt", "settings.json", "z.txt"}
	for _, w := range want {
		found := false
		for _, m := range merged {
			if m.Path == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q", w)
		}
	}
}

// R8 dispatch: settings.json is deep-merged across the chain.
func TestMerge_DispatchesDeepMerge(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {
				manifest: map[string]any{},
				files:    map[string]string{"settings.json": `{"ui":{"theme":"dark"},"keep":"yes"}`},
			},
			"leaf": {
				manifest: map[string]any{"extends": "base"},
				files:    map[string]string{"settings.json": `{"ui":{"font":"mono"}}`},
			},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	settings := findOne(t, merged, "settings.json", resolver.DestinationClaude)
	if settings.MergePolicy != resolver.MergePolicyDeepMerge {
		t.Fatalf("policy: want deep-merge, got %s", settings.MergePolicy)
	}
	var got map[string]any
	if err := json.Unmarshal(settings.Bytes, &got); err != nil {
		t.Fatalf("parse merged: %v", err)
	}
	ui := got["ui"].(map[string]any)
	if ui["theme"] != "dark" || ui["font"] != "mono" {
		t.Fatalf("ui: %+v", ui)
	}
	if got["keep"] != "yes" {
		t.Fatalf("keep: %+v", got["keep"])
	}
	if !equalStrings(settings.Contributors, []string{"base", "leaf"}) {
		t.Fatalf("contributors: %v", settings.Contributors)
	}
}

// R9 worked example end-to-end.
func TestMerge_DispatchesConcatR9WorkedExample(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base":     {manifest: map[string]any{}, files: map[string]string{"CLAUDE.md": "BASE\n"}},
			"extended": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"CLAUDE.md": "EXTENDED\n"}},
			"leaf":     {manifest: map[string]any{"extends": "extended", "includes": []string{"compA", "compB"}}, files: map[string]string{"CLAUDE.md": "LEAF\n"}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"CLAUDE.md": "COMPA\n"}},
			"compB": {files: map[string]string{"CLAUDE.md": "COMPB\n"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	md := findOne(t, merged, "CLAUDE.md", resolver.DestinationClaude)
	if md.MergePolicy != resolver.MergePolicyConcat {
		t.Fatalf("policy: %s", md.MergePolicy)
	}
	if string(md.Bytes) != "BASE\nEXTENDED\nCOMPA\nCOMPB\nLEAF\n" {
		t.Fatalf("bytes: %q", string(md.Bytes))
	}
	if !equalStrings(md.Contributors, []string{"base", "extended", "compA", "compB", "leaf"}) {
		t.Fatalf("contributors: %v", md.Contributors)
	}
}

// R10: last-wins for non-mergeable files; only profile contributor wins.
func TestMerge_DispatchesLastWins(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"agents/foo.json": `{"v":"base"}`}},
			"leaf": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"agents/foo.json": `{"v":"leaf"}`}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	f := findOne(t, merged, "agents/foo.json", resolver.DestinationClaude)
	if f.MergePolicy != resolver.MergePolicyLastWins {
		t.Fatalf("policy: %s", f.MergePolicy)
	}
	if string(f.Bytes) != `{"v":"leaf"}` {
		t.Fatalf("bytes: %s", string(f.Bytes))
	}
	if !equalStrings(f.Contributors, []string{"leaf"}) {
		t.Fatalf("contributors: %v", f.Contributors)
	}
}

// Custom Read function bypasses disk completely.
func TestMerge_CustomRead(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {manifest: map[string]any{}, files: map[string]string{"settings.json": "ignored"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	injected := []byte(`{"injected":true}`)
	merged, err := merge.Merge(plan, merge.Options{
		Read: func(absPath string) ([]byte, error) { return injected, nil },
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	settings := findOne(t, merged, "settings.json", resolver.DestinationClaude)
	var got map[string]any
	if err := json.Unmarshal(settings.Bytes, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got["injected"] != true {
		t.Fatalf("injected: %+v", got["injected"])
	}
}

// Read errors surface as MergeReadFailedError carrying contributor + path.
func TestMerge_ReadErrorWrappedAsMergeReadFailed(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {manifest: map[string]any{}, files: map[string]string{"CLAUDE.md": "x"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	failingRead := func(absPath string) ([]byte, error) { return nil, fmt.Errorf("disk gremlins") }
	_, err := merge.Merge(plan, merge.Options{Read: failingRead})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var rfe *pipelineerrors.MergeReadFailedError
	if !stderrors.As(err, &rfe) {
		t.Fatalf("want MergeReadFailedError, got %T (%v)", err, err)
	}
	if rfe.Contributor != "leaf" {
		t.Fatalf("contributor: %q", rfe.Contributor)
	}
	if rfe.RelPath != "CLAUDE.md" {
		t.Fatalf("relPath: %q", rfe.RelPath)
	}
	if !strings.Contains(rfe.Error(), "disk gremlins") {
		t.Fatalf("message lacks underlying detail: %q", rfe.Error())
	}
}

// MergeReadFailedError classifies as a merge-phase pipeline error.
func TestMerge_MergeReadFailed_PhaseClassification(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {manifest: map[string]any{}, files: map[string]string{"CLAUDE.md": "x"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	failing := func(absPath string) ([]byte, error) { return nil, fmt.Errorf("boom") }
	_, err := merge.Merge(plan, merge.Options{Read: failing})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	pe := pipelineerrors.AsPipelineError(err)
	if pe == nil {
		t.Fatalf("not a pipeline error: %T", err)
	}
	if pe.Phase() != pipelineerrors.PhaseMerge {
		t.Fatalf("phase: want %s, got %s", pipelineerrors.PhaseMerge, pe.Phase())
	}
	if pe.ErrorCode() != pipelineerrors.CodeMergeReadFailed {
		t.Fatalf("code: %s", pe.ErrorCode())
	}
}

func TestMerge_EmptyPlan(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"empty": {manifest: map[string]any{}, files: map[string]string{}},
		},
	})
	plan := resolvePlan(t, "empty", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(merged) != 0 {
		t.Fatalf("want empty, got %d entries: %+v", len(merged), merged)
	}
}

// settings.json at any depth uses deep-merge.
func TestMerge_SettingsJsonAtAnyDepthIsDeepMerge(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"subdir/settings.json": `{"a":1,"c":1}`}},
			"leaf": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"subdir/settings.json": `{"a":2,"b":3}`}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	m := findOne(t, merged, "subdir/settings.json", resolver.DestinationClaude)
	if m.MergePolicy != resolver.MergePolicyDeepMerge {
		t.Fatalf("policy: %s", m.MergePolicy)
	}
	var got map[string]any
	if err := json.Unmarshal(m.Bytes, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if asInt(got["a"]) != 2 || asInt(got["b"]) != 3 || asInt(got["c"]) != 1 {
		t.Fatalf("got: %+v", got)
	}
}

// Defensive guard: conflicting mergePolicy within a (relPath, destination)
// group fails loud.
func TestMerge_ConflictingMergePolicyGroupFails(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"settings.json": `{"a":1}`}},
			"leaf": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"settings.json": `{"b":2}`}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	for i := range plan.Files {
		if i == 0 {
			plan.Files[i].MergePolicy = resolver.MergePolicyDeepMerge
		} else {
			plan.Files[i].MergePolicy = resolver.MergePolicyLastWins
		}
	}
	_, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err == nil || !strings.Contains(err.Error(), "conflicting mergePolicy") {
		t.Fatalf("want conflicting mergePolicy error, got %v", err)
	}
}

// Defensive guard: equal contributorIndex (duplicate) entries fail loud.
func TestMerge_DuplicateContributorIndexFails(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {manifest: map[string]any{}, files: map[string]string{"a.txt": "1", "b.txt": "2"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	dup := plan.Files[0]
	plan.Files = append(plan.Files, dup)
	_, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err == nil || !strings.Contains(err.Error(), "non-ascending contributorIndex") {
		t.Fatalf("want non-ascending contributorIndex error, got %v", err)
	}
}

// Defensive guard: strictly descending contributorIndex within a group
// (genuine out-of-order, not just equal) also fails loud.
func TestMerge_DescendingContributorIndexFails(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"CLAUDE.md": "B\n"}},
			"leaf": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"CLAUDE.md": "L\n"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	// Find the two CLAUDE.md group entries (both .claude destination) and
	// reverse their contributorIndex values so the second entry's index
	// is strictly less than the first.
	var idxs []int
	for i, f := range plan.Files {
		if f.RelPath == "CLAUDE.md" {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) != 2 {
		t.Fatalf("expected 2 CLAUDE.md entries, got %d", len(idxs))
	}
	plan.Files[idxs[0]].ContributorIndex, plan.Files[idxs[1]].ContributorIndex =
		plan.Files[idxs[1]].ContributorIndex, plan.Files[idxs[0]].ContributorIndex
	_, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err == nil || !strings.Contains(err.Error(), "non-ascending contributorIndex") {
		t.Fatalf("want non-ascending contributorIndex error, got %v", err)
	}
}

// Read must be required — D2 is explicitly FS-IO-free.
func TestMerge_NilReadIsAnError(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {manifest: map[string]any{}, files: map[string]string{"CLAUDE.md": "x"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	_, err := merge.Merge(plan, merge.Options{Read: nil})
	if err == nil {
		t.Fatal("want error for nil Read, got nil")
	}
}

// ─── cw6/T3: destination-aware grouping ──────────────────────────────────

func TestMerge_CW6_SplitClaudeMdAcrossDestinations(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base":     {manifest: map[string]any{}, files: map[string]string{"CLAUDE.md": "BASE-INSIDE\n"}},
			"extended": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"CLAUDE.md": "EXTENDED-INSIDE\n"}},
			"leaf":     {manifest: map[string]any{"extends": "extended"}, rootFiles: map[string]string{"CLAUDE.md": "LEAF-ROOT\n"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	idx := indexByPathDest(merged)
	inside, ok := idx[fmt.Sprintf("%s::CLAUDE.md", resolver.DestinationClaude)]
	if !ok {
		t.Fatal("missing .claude/CLAUDE.md")
	}
	root, ok := idx[fmt.Sprintf("%s::CLAUDE.md", resolver.DestinationProjectRoot)]
	if !ok {
		t.Fatal("missing projectRoot CLAUDE.md")
	}
	if inside.MergePolicy != resolver.MergePolicyConcat {
		t.Fatalf("inside policy: %s", inside.MergePolicy)
	}
	if !equalStrings(inside.Contributors, []string{"base", "extended"}) {
		t.Fatalf("inside contributors: %v", inside.Contributors)
	}
	if string(inside.Bytes) != "BASE-INSIDE\nEXTENDED-INSIDE\n" {
		t.Fatalf("inside bytes: %q", string(inside.Bytes))
	}
	if !equalStrings(root.Contributors, []string{"leaf"}) {
		t.Fatalf("root contributors: %v", root.Contributors)
	}
	if string(root.Bytes) != "LEAF-ROOT\n" {
		t.Fatalf("root bytes: %q", string(root.Bytes))
	}
}

func TestMerge_CW6_RootCLAUDEMdConcatAcrossChain(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base":     {manifest: map[string]any{}, rootFiles: map[string]string{"CLAUDE.md": "BASE-ROOT\n"}},
			"extended": {manifest: map[string]any{"extends": "base"}, rootFiles: map[string]string{"CLAUDE.md": "EXTENDED-ROOT\n"}},
			"leaf":     {manifest: map[string]any{"extends": "extended", "includes": []string{"compA", "compB"}}, rootFiles: map[string]string{"CLAUDE.md": "LEAF-ROOT\n"}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{}},
			"compB": {files: map[string]string{}},
		},
	})
	// Components don't have rootFiles (only .claude); tweak: write rootFiles
	// for components manually since fixture doesn't expose that path.
	for _, name := range []string{"compA", "compB"} {
		dir := filepath.Join(fx.projectRoot, ".claude-profiles", "_components", name)
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(strings.ToUpper(name)+"-ROOT\n"), 0o644); err != nil {
			t.Fatalf("write component root: %v", err)
		}
	}
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	root := findOne(t, merged, "CLAUDE.md", resolver.DestinationProjectRoot)
	if root.MergePolicy != resolver.MergePolicyConcat {
		t.Fatalf("policy: %s", root.MergePolicy)
	}
	wantContribs := []string{"base", "extended", "compA", "compB", "leaf"}
	if !equalStrings(root.Contributors, wantContribs) {
		t.Fatalf("contributors: want %v, got %v", wantContribs, root.Contributors)
	}
	want := "BASE-ROOT\nEXTENDED-ROOT\nCOMPA-ROOT\nCOMPB-ROOT\nLEAF-ROOT\n"
	if string(root.Bytes) != want {
		t.Fatalf("root bytes: want %q, got %q", want, string(root.Bytes))
	}
}

// Each MergedFile carries the right Destination.
func TestMerge_DestinationPreserved(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {
				manifest:  map[string]any{},
				files:     map[string]string{"CLAUDE.md": "inside\n", "settings.json": `{"a":1}`},
				rootFiles: map[string]string{"CLAUDE.md": "root\n"},
			},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	for _, m := range merged {
		if m.Destination != resolver.DestinationClaude && m.Destination != resolver.DestinationProjectRoot {
			t.Fatalf("unknown destination: %q", m.Destination)
		}
	}
	settings := findOne(t, merged, "settings.json", resolver.DestinationClaude)
	if settings.Destination != resolver.DestinationClaude {
		t.Fatalf("settings destination: %s", settings.Destination)
	}
}

// ─── R12 fitness function: hooks-precedence integration test ─────────────

// End-to-end: R12 concat wins over R8 array-replace at hooks.<EventName>
// across the canonical chain.
func TestMerge_FitnessFunction_R12OverR8(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {
				manifest: map[string]any{},
				files: map[string]string{
					"settings.json": `{"ui":{"theme":"dark"},"tools":["base-only"],"hooks":{"PreToolUse":[{"src":"base","run":"warn"}]}}`,
				},
			},
			"extended": {
				manifest: map[string]any{"extends": "base"},
				files: map[string]string{
					"settings.json": `{"hooks":{"PreToolUse":[{"src":"extended","run":"audit"}]}}`,
				},
			},
			"leaf": {
				manifest: map[string]any{"extends": "extended", "includes": []string{"compA"}},
				files: map[string]string{
					"settings.json": `{"tools":["leaf-replaces"],"hooks":{"PreToolUse":[{"src":"leaf","run":"block"}],"PostToolUse":[{"src":"leaf","run":"log"}]}}`,
				},
			},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{
				"settings.json": `{"hooks":{"PreToolUse":[{"src":"compA","run":"trace"}]}}`,
			}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	settings := findOne(t, merged, "settings.json", resolver.DestinationClaude)
	var parsed map[string]any
	if err := json.Unmarshal(settings.Bytes, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	ui := parsed["ui"].(map[string]any)
	if ui["theme"] != "dark" {
		t.Fatalf("ui.theme: %+v", ui)
	}
	tools := parsed["tools"].([]any)
	if !equalAnySlice(tools, []any{"leaf-replaces"}) {
		t.Fatalf("tools: %+v (R8 array-replace should leave only leaf)", tools)
	}
	hooks := parsed["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 4 {
		t.Fatalf("PreToolUse length: %d (%+v)", len(pre), pre)
	}
	for i, want := range []string{"base", "extended", "compA", "leaf"} {
		if got := pre[i].(map[string]any)["src"]; got != want {
			t.Fatalf("PreToolUse[%d].src: want %q, got %q", i, want, got)
		}
	}
	post := hooks["PostToolUse"].([]any)
	if len(post) != 1 || post[0].(map[string]any)["src"] != "leaf" {
		t.Fatalf("PostToolUse: %+v", post)
	}
	if !equalStrings(settings.Contributors, []string{"base", "extended", "compA", "leaf"}) {
		t.Fatalf("contributors: %v", settings.Contributors)
	}
}

// Ancestor-only conflicts on last-wins files DO NOT throw; leaf wins (R10).
func TestMerge_AncestorOnlyLastWinsResolves(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"agents/x.json": `{"v":"base"}`}},
			"mid":  {manifest: map[string]any{"extends": "base"}, files: map[string]string{"agents/x.json": `{"v":"mid"}`}},
			"leaf": {manifest: map[string]any{"extends": "mid"}, files: map[string]string{"agents/x.json": `{"v":"leaf"}`}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	a := findOne(t, merged, "agents/x.json", resolver.DestinationClaude)
	if string(a.Bytes) != `{"v":"leaf"}` {
		t.Fatalf("bytes: %q", string(a.Bytes))
	}
	if !equalStrings(a.Contributors, []string{"leaf"}) {
		t.Fatalf("contributors: %v", a.Contributors)
	}
}

// R11: include-vs-include conflict on a non-mergeable path is caught at
// resolve time and never reaches merge. Locks the boundary contract: D1
// fails closed before D2 has anything to do.
func TestMerge_R11_IncludeConflictCaughtAtResolve(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {manifest: map[string]any{"includes": []string{"compA", "compB"}}, files: map[string]string{}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"agents/x.json": "A"}},
			"compB": {files: map[string]string{"agents/x.json": "B"}},
		},
	})
	_, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err == nil {
		t.Fatal("want ConflictError, got nil — merge would silently last-wins past the conflict")
	}
	var ce *pipelineerrors.ConflictError
	if !stderrors.As(err, &ce) {
		t.Fatalf("want *ConflictError, got %T (%v)", err, err)
	}
	if ce.RelPath != "agents/x.json" {
		t.Fatalf("relPath: %q", ce.RelPath)
	}
	if !equalStrings(ce.Contributors, []string{"compA", "compB"}) {
		t.Fatalf("contributors: %v", ce.Contributors)
	}
}

// R11 carve-out: profile override silences include-conflict; merge sees
// only the profile contributor.
func TestMerge_R11_ProfileOverridesIncludeConflict(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {
				manifest: map[string]any{"includes": []string{"compA", "compB"}},
				files:    map[string]string{"agents/x.json": `{"from":"leaf"}`},
			},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"agents/x.json": "A"}},
			"compB": {files: map[string]string{"agents/x.json": "B"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	a := findOne(t, merged, "agents/x.json", resolver.DestinationClaude)
	if !equalStrings(a.Contributors, []string{"leaf"}) {
		t.Fatalf("contributors: %v", a.Contributors)
	}
	if string(a.Bytes) != `{"from":"leaf"}` {
		t.Fatalf("bytes: %q", string(a.Bytes))
	}
}

// Cross-pollinated chain: same path in includes (concat) and ancestors (concat).
func TestMerge_CrossPollinatedConcatOrder(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"CLAUDE.md": "B\n"}},
			"leaf": {
				manifest: map[string]any{"extends": "base", "includes": []string{"compA", "compB"}},
				files:    map[string]string{"CLAUDE.md": "L\n"},
			},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"CLAUDE.md": "A\n"}},
			"compB": {files: map[string]string{"CLAUDE.md": "Bcomp\n"}},
		},
	})
	plan := resolvePlan(t, "leaf", fx.projectRoot)
	merged, err := merge.Merge(plan, merge.Options{Read: readFromDisk})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	md := findOne(t, merged, "CLAUDE.md", resolver.DestinationClaude)
	if string(md.Bytes) != "B\nA\nBcomp\nL\n" {
		t.Fatalf("bytes: %q", string(md.Bytes))
	}
	if !equalStrings(md.Contributors, []string{"base", "compA", "compB", "leaf"}) {
		t.Fatalf("contributors: %v", md.Contributors)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────

func findOne(t *testing.T, merged []merge.MergedFile, path string, dest resolver.PlanFileDestination) merge.MergedFile {
	t.Helper()
	for _, m := range merged {
		if m.Path == path && m.Destination == dest {
			return m
		}
	}
	t.Fatalf("missing entry for path=%q dest=%s", path, dest)
	return merge.MergedFile{}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalAnySlice(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflectEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func reflectEqual(a, b any) bool {
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		return ok && equalAnySlice(av, bv)
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !reflectEqual(v, bv[k]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case int:
		return t
	default:
		return -1
	}
}

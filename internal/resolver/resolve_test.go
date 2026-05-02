package resolver_test

import (
	"encoding/json"
	stderrors "errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	pipelineerrors "github.com/htxryan/claude-code-config-profiles/internal/errors"
	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
)

// ----- minimal in-package fixture (mirrors tests/integration/helpers/fixture.go) -----

type profileSpec struct {
	manifest   any
	files      map[string]string
	rootFiles  map[string]string
}

type componentSpec struct {
	files map[string]string
}

type fixtureSpec struct {
	profiles   map[string]profileSpec
	components map[string]componentSpec
	external   map[string]componentSpec
}

type fixture struct {
	projectRoot, externalRoot string
}

func makeFixture(t *testing.T, spec fixtureSpec) *fixture {
	t.Helper()
	tmp := t.TempDir()
	pr := filepath.Join(tmp, "project")
	er := filepath.Join(tmp, "external")
	for _, d := range []string{pr, er} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	profilesDir := filepath.Join(pr, ".claude-profiles")
	for name, p := range spec.profiles {
		dir := filepath.Join(profilesDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir profile %q: %v", name, err)
		}
		writeFixtureManifest(t, filepath.Join(dir, "profile.json"), p.manifest)
		writeTree(t, filepath.Join(dir, ".claude"), p.files)
		writeTree(t, dir, p.rootFiles)
	}
	for name, c := range spec.components {
		dir := filepath.Join(profilesDir, "_components", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir component: %v", err)
		}
		writeTree(t, filepath.Join(dir, ".claude"), c.files)
	}
	for name, c := range spec.external {
		dir := filepath.Join(er, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir external: %v", err)
		}
		writeTree(t, filepath.Join(dir, ".claude"), c.files)
	}
	return &fixture{projectRoot: pr, externalRoot: er}
}

func writeFixtureManifest(t *testing.T, path string, m any) {
	t.Helper()
	if m == nil {
		return
	}
	switch v := m.(type) {
	case string:
		if err := os.WriteFile(path, []byte(v), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
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

// ----- ResolvedPlan invariants -----

func TestResolve_ChainEndsWithProfile(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"a.txt": "a"}},
			"mid":  {manifest: map[string]any{"extends": "base"}, files: map[string]string{"b.txt": "b"}},
			"leaf": {manifest: map[string]any{"extends": "mid"}, files: map[string]string{"c.txt": "c"}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if plan.ProfileName != "leaf" {
		t.Fatalf("profileName: want leaf, got %q", plan.ProfileName)
	}
	if !reflect.DeepEqual(plan.Chain, []string{"base", "mid", "leaf"}) {
		t.Fatalf("chain: %v", plan.Chain)
	}
	if plan.Chain[len(plan.Chain)-1] != plan.ProfileName {
		t.Fatalf("chain must end with profile")
	}
}

func TestResolve_FilesLexSorted(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {
				manifest: map[string]any{},
				files:    map[string]string{"z.txt": "z", "a.txt": "a", "m/b.txt": "b", "m/a.txt": "a"},
			},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got := make([]string, len(plan.Files))
	for i, f := range plan.Files {
		got[i] = f.RelPath
	}
	want := append([]string{}, got...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files not lex-sorted: %v vs %v", got, want)
	}
}

func TestResolve_ProfileContributorIsLast(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"a.txt": "a"}},
			"leaf": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"b.txt": "b"}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	last := plan.Contributors[len(plan.Contributors)-1]
	if last.Kind != resolver.ContributorProfile {
		t.Fatalf("last contributor kind: %q", last.Kind)
	}
	if last.ID != "leaf" {
		t.Fatalf("last contributor id: %q", last.ID)
	}
}

func TestResolve_NoDuplicateRelPathContributorPair(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"a.txt": "1"}},
			"leaf": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"a.txt": "2"}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range plan.Files {
		key := f.RelPath + "::" + string(rune(f.ContributorIndex))
		if seen[key] {
			t.Fatalf("duplicate (relPath, contributorIndex): %s", key)
		}
		seen[key] = true
	}
}

func TestResolve_ContributorIndexValid(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"a": {manifest: map[string]any{}, files: map[string]string{"x": "x"}},
			"b": {manifest: map[string]any{"extends": "a"}, files: map[string]string{"y": "y"}},
		},
	})
	plan, err := resolver.Resolve("b", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, f := range plan.Files {
		if f.ContributorIndex < 0 || f.ContributorIndex >= len(plan.Contributors) {
			t.Fatalf("bad contributorIndex: %d", f.ContributorIndex)
		}
	}
}

// ----- R3/R4/R5 — extends chain -----

func TestResolve_ExtendsChainOldestFirst(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"a": {manifest: map[string]any{}},
			"b": {manifest: map[string]any{"extends": "a"}},
			"c": {manifest: map[string]any{"extends": "b"}},
		},
	})
	plan, err := resolver.Resolve("c", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !reflect.DeepEqual(plan.Chain, []string{"a", "b", "c"}) {
		t.Fatalf("chain: %v", plan.Chain)
	}
}

func TestResolve_DetectsTwoCycle(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"a": {manifest: map[string]any{"extends": "b"}},
			"b": {manifest: map[string]any{"extends": "a"}},
		},
	})
	_, err := resolver.Resolve("a", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ce *pipelineerrors.CycleError
	if !stderrors.As(err, &ce) {
		t.Fatalf("want CycleError, got %v", err)
	}
	if !contains(ce.Cycle, "a") || !contains(ce.Cycle, "b") {
		t.Fatalf("cycle missing members: %v", ce.Cycle)
	}
}

func TestResolve_DetectsSelfLoop(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"a": {manifest: map[string]any{"extends": "a"}},
		},
	})
	_, err := resolver.Resolve("a", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ce *pipelineerrors.CycleError
	if !stderrors.As(err, &ce) {
		t.Fatalf("want CycleError, got %v", err)
	}
}

func TestResolve_DetectsThreeCycle(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"a": {manifest: map[string]any{"extends": "b"}},
			"b": {manifest: map[string]any{"extends": "c"}},
			"c": {manifest: map[string]any{"extends": "a"}},
		},
	})
	_, err := resolver.Resolve("a", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ce *pipelineerrors.CycleError
	if !stderrors.As(err, &ce) {
		t.Fatalf("want CycleError, got %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if !contains(ce.Cycle, name) {
			t.Fatalf("cycle missing %q: %v", name, ce.Cycle)
		}
	}
}

func TestResolve_MissingProfileNamesIt(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"a": {manifest: map[string]any{"extends": "ghost"}},
		},
	})
	_, err := resolver.Resolve("a", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
	if mpe.Missing != "ghost" {
		t.Fatalf("missing: %q", mpe.Missing)
	}
	if mpe.ReferencedBy != "a" {
		t.Fatalf("referencedBy: %q", mpe.ReferencedBy)
	}
}

func TestResolve_TopLevelMissingProfile(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{profiles: map[string]profileSpec{}})
	_, err := resolver.Resolve("nope", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
	if mpe.Missing != "nope" {
		t.Fatalf("missing: %q", mpe.Missing)
	}
}

// ----- R6/R7 includes -----

func TestResolve_SingleComponentInclude(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"compA"}}, files: map[string]string{"p.txt": "p"}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"ca.txt": "from compA"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	paths := make([]string, len(plan.Files))
	for i, f := range plan.Files {
		paths[i] = f.RelPath
	}
	if !contains(paths, "p.txt") {
		t.Fatalf("missing p.txt: %v", paths)
	}
	if !contains(paths, "ca.txt") {
		t.Fatalf("missing ca.txt: %v", paths)
	}
}

func TestResolve_LeafIncludesPrecedeLeaf(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base":     {manifest: map[string]any{}, files: map[string]string{"z": "z"}},
			"extended": {manifest: map[string]any{"extends": "base"}, files: map[string]string{"z": "z"}},
			"leaf":     {manifest: map[string]any{"extends": "extended", "includes": []string{"compA", "compB"}}, files: map[string]string{"z": "z"}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"z": "z"}},
			"compB": {files: map[string]string{"z": "z"}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	ids := make([]string, len(plan.Contributors))
	for i, c := range plan.Contributors {
		ids[i] = c.ID
	}
	want := []string{"base", "extended", "compA", "compB", "leaf"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("contributor ids: want %v, got %v", want, ids)
	}
}

func TestResolve_AncestorIncludesEmittedAfterAncestor(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{"includes": []string{"compBase"}}, files: map[string]string{"z": "z-base"}},
			"mid":  {manifest: map[string]any{"extends": "base"}, files: map[string]string{"z": "z-mid"}},
			"leaf": {manifest: map[string]any{"extends": "mid", "includes": []string{"compLeaf"}}, files: map[string]string{"z": "z-leaf"}},
		},
		components: map[string]componentSpec{
			"compBase": {files: map[string]string{"z": "z-compBase"}},
			"compLeaf": {files: map[string]string{"z": "z-compLeaf"}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	ids := make([]string, len(plan.Contributors))
	kinds := make([]resolver.ContributorKind, len(plan.Contributors))
	for i, c := range plan.Contributors {
		ids[i] = c.ID
		kinds[i] = c.Kind
	}
	wantIDs := []string{"base", "compBase", "mid", "compLeaf", "leaf"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("ids: want %v, got %v", wantIDs, ids)
	}
	wantKinds := []resolver.ContributorKind{
		resolver.ContributorAncestor, resolver.ContributorInclude,
		resolver.ContributorAncestor, resolver.ContributorInclude,
		resolver.ContributorProfile,
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("kinds: want %v, got %v", wantKinds, kinds)
	}
}

func TestResolve_MissingBareComponent(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"ghost"}}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mie *pipelineerrors.MissingIncludeError
	if !stderrors.As(err, &mie) {
		t.Fatalf("want MissingIncludeError, got %v", err)
	}
	if mie.Raw != "ghost" || mie.ReferencedBy != "p" {
		t.Fatalf("raw=%q referencedBy=%q", mie.Raw, mie.ReferencedBy)
	}
}

func TestResolve_MissingRelativeInclude(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"./neighbor"}}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mie *pipelineerrors.MissingIncludeError
	if !stderrors.As(err, &mie) {
		t.Fatalf("want MissingIncludeError, got %v", err)
	}
}

func TestResolve_MissingAbsoluteInclude(t *testing.T) {
	if runtime.GOOS == "windows" {
		// /this/does/not/exist/anywhere is not absolute on Windows; the
		// validator correctly rejects it before MissingIncludeError can fire.
		t.Skip("POSIX-style absolute path; not applicable on Windows")
	}
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"/this/does/not/exist/anywhere"}}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mie *pipelineerrors.MissingIncludeError
	if !stderrors.As(err, &mie) {
		t.Fatalf("want MissingIncludeError, got %v", err)
	}
}

// ----- R37 — include kinds -----

func TestResolve_ClassifiesBareComponentsAsComponent(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"compA"}}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"f": "1"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if plan.Includes[0].Kind != resolver.IncludeKindComponent {
		t.Fatalf("kind: %q", plan.Includes[0].Kind)
	}
	if plan.Includes[0].External {
		t.Fatalf("expected non-external")
	}
}

func TestResolve_ClassifiesRelativeInclude(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p":   {manifest: map[string]any{"includes": []string{"./../sib"}}},
			"sib": {manifest: map[string]any{}, files: map[string]string{"x": "x"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if plan.Includes[0].Kind != resolver.IncludeKindRelative {
		t.Fatalf("kind: %q", plan.Includes[0].Kind)
	}
	if !strings.HasSuffix(plan.Includes[0].ResolvedPath, "sib") {
		t.Fatalf("resolved: %q", plan.Includes[0].ResolvedPath)
	}
}

func TestResolve_AbsoluteInsideProjectRoot(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p":   {manifest: map[string]any{}},
			"sib": {manifest: map[string]any{}, files: map[string]string{"f": "1"}},
		},
	})
	sibAbs := filepath.Join(fx.projectRoot, ".claude-profiles", "sib")
	manifest := map[string]any{"includes": []string{sibAbs}}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(fx.projectRoot, ".claude-profiles", "p", "profile.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if plan.Includes[0].Kind != resolver.IncludeKindAbsolute {
		t.Fatalf("kind: %q", plan.Includes[0].Kind)
	}
	if plan.Includes[0].External {
		t.Fatalf("in-repo absolute should not be external")
	}
}

func TestResolve_AbsoluteExternalFlagged(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}},
		},
		external: map[string]componentSpec{
			"ext1": {files: map[string]string{"a": "1"}},
		},
	})
	extAbs := filepath.Join(fx.externalRoot, "ext1")
	manifest := map[string]any{"includes": []string{extAbs}}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(fx.projectRoot, ".claude-profiles", "p", "profile.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !plan.Includes[0].External {
		t.Fatalf("expected external=true")
	}
	if plan.Includes[0].Kind != resolver.IncludeKindAbsolute {
		t.Fatalf("kind: %q", plan.Includes[0].Kind)
	}
}

// ----- R37a — external trust -----

func TestResolve_ExternalPathsDedup(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}},
		},
		external: map[string]componentSpec{
			"ext1": {files: map[string]string{"settings.json": "{}"}},
		},
	})
	extAbs := filepath.Join(fx.externalRoot, "ext1")
	manifest := map[string]any{"includes": []string{extAbs, extAbs}}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(fx.projectRoot, ".claude-profiles", "p", "profile.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.ExternalPaths) != 1 {
		t.Fatalf("want 1 external entry, got %d (%v)", len(plan.ExternalPaths), plan.ExternalPaths)
	}
}

func TestResolve_NoExternalForInRepoIncludes(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"compA"}}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"f": "1"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.ExternalPaths) != 0 {
		t.Fatalf("want no external paths, got %v", plan.ExternalPaths)
	}
}

// ----- R11 — conflicts -----

func TestResolve_ConflictTwoIncludes(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"a", "b"}}},
		},
		components: map[string]componentSpec{
			"a": {files: map[string]string{"agents/foo.json": "{}"}},
			"b": {files: map[string]string{"agents/foo.json": "{}"}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ce *pipelineerrors.ConflictError
	if !stderrors.As(err, &ce) {
		t.Fatalf("want ConflictError, got %v", err)
	}
	if ce.RelPath != "agents/foo.json" {
		t.Fatalf("relPath: %q", ce.RelPath)
	}
	sort.Strings(ce.Contributors)
	if !reflect.DeepEqual(ce.Contributors, []string{"a", "b"}) {
		t.Fatalf("contributors: %v", ce.Contributors)
	}
}

func TestResolve_ConflictIncludeVsAncestor(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"agents/x.json": "{}"}},
			"p":    {manifest: map[string]any{"extends": "base", "includes": []string{"compA"}}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"agents/x.json": "{}"}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ce *pipelineerrors.ConflictError
	if !stderrors.As(err, &ce) {
		t.Fatalf("want ConflictError, got %v", err)
	}
}

func TestResolve_ProfileOverridesNoConflict(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {
				manifest: map[string]any{"includes": []string{"a", "b"}},
				files:    map[string]string{"agents/foo.json": "{}"},
			},
		},
		components: map[string]componentSpec{
			"a": {files: map[string]string{"agents/foo.json": "{}"}},
			"b": {files: map[string]string{"agents/foo.json": "{}"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	count := 0
	for _, f := range plan.Files {
		if f.RelPath == "agents/foo.json" {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("want 3 entries, got %d", count)
	}
}

func TestResolve_AncestorVsAncestorNoConflict(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{}, files: map[string]string{"agents/x.json": "{}"}},
			"mid":  {manifest: map[string]any{"extends": "base"}, files: map[string]string{"agents/x.json": "{}"}},
			"leaf": {manifest: map[string]any{"extends": "mid"}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	count := 0
	for _, f := range plan.Files {
		if f.RelPath == "agents/x.json" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want 2 entries, got %d", count)
	}
}

func TestResolve_MergeableNoConflict_SettingsJSON(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"a", "b"}}},
		},
		components: map[string]componentSpec{
			"a": {files: map[string]string{"settings.json": "{}"}},
			"b": {files: map[string]string{"settings.json": "{}"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	count := 0
	for _, f := range plan.Files {
		if f.RelPath == "settings.json" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want 2 settings.json entries, got %d", count)
	}
}

func TestResolve_MergeableNoConflict_CLAUDEMd(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"a", "b"}}},
		},
		components: map[string]componentSpec{
			"a": {files: map[string]string{"CLAUDE.md": "from a"}},
			"b": {files: map[string]string{"CLAUDE.md": "from b"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	count := 0
	for _, f := range plan.Files {
		if f.RelPath == "CLAUDE.md" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want 2 CLAUDE.md entries, got %d", count)
	}
}

func TestResolve_PerDestinationConflictGrouping(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"leaf": {manifest: map[string]any{"includes": []string{"a"}}, rootFiles: map[string]string{"CLAUDE.md": "from leaf root"}},
		},
		components: map[string]componentSpec{
			"a": {files: map[string]string{"CLAUDE.md": "from a inside"}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	all := []resolver.PlanFile{}
	for _, f := range plan.Files {
		if f.RelPath == "CLAUDE.md" {
			all = append(all, f)
		}
	}
	if len(all) != 2 {
		t.Fatalf("want 2 CLAUDE.md entries, got %d", len(all))
	}
	dests := []string{string(all[0].Destination), string(all[1].Destination)}
	sort.Strings(dests)
	if !reflect.DeepEqual(dests, []string{".claude", "projectRoot"}) {
		t.Fatalf("destinations: %v", dests)
	}
}

// ----- R35/R36 — manifest validation -----

func TestResolve_AcceptsAllOptionalFields(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{
				"name": "p-renamed", "description": "desc",
				"includes": []string{}, "tags": []string{"t1", "t2"},
			}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, w := range plan.Warnings {
		if w.Code == resolver.WarningUnknownManifestField {
			t.Fatalf("unexpected unknown-field warning: %v", w)
		}
	}
}

func TestResolve_UnknownFieldWarns(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"extraField": 42}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	count := 0
	for _, w := range plan.Warnings {
		if w.Code == resolver.WarningUnknownManifestField && w.Source == "p" && strings.Contains(w.Message, "extraField") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want 1 unknown-field warning naming extraField, got %d", count)
	}
}

func TestResolve_MissingManifestWarns(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: nil, files: map[string]string{"a": "1"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	count := 0
	for _, w := range plan.Warnings {
		if w.Code == resolver.WarningMissingManifest {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want 1 MissingManifest warning, got %d", count)
	}
}

func TestResolve_AbortsOnInvalidJSON(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: "{not valid json"},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("want InvalidManifestError, got %v", err)
	}
}

func TestResolve_AbortsOnTypeMismatch(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"extends": 42}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("want InvalidManifestError, got %v", err)
	}
}

func TestResolve_AbortsOnIncludesNonStringElement(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []any{"ok", 42}}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("want InvalidManifestError, got %v", err)
	}
}

// ----- R9 worked example -----

func TestResolve_R9CanonicalConcatOrder(t *testing.T) {
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
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	mdFiles := []resolver.PlanFile{}
	for _, f := range plan.Files {
		if f.RelPath == "CLAUDE.md" {
			mdFiles = append(mdFiles, f)
		}
	}
	sort.Slice(mdFiles, func(i, j int) bool { return mdFiles[i].ContributorIndex < mdFiles[j].ContributorIndex })
	got := make([]string, len(mdFiles))
	for i, f := range mdFiles {
		got[i] = plan.Contributors[f.ContributorIndex].ID
	}
	want := []string{"base", "extended", "compA", "compB", "leaf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordering: want %v, got %v", want, got)
	}
}

// ----- ResolvedPlan.SchemaVersion -----

func TestResolve_StampsSchemaVersion(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if plan.SchemaVersion != 1 {
		t.Fatalf("schemaVersion: %d", plan.SchemaVersion)
	}
}

// ----- Contributor.Manifest passthrough -----

func TestResolve_ManifestAttachedToAncestorAndProfile(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{"description": "the base", "tags": []string{"b"}}},
			"leaf": {manifest: map[string]any{"extends": "base", "description": "the leaf", "tags": []string{"l"}}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var baseC, leafC *resolver.Contributor
	for i := range plan.Contributors {
		c := &plan.Contributors[i]
		if c.ID == "base" {
			baseC = c
		}
		if c.ID == "leaf" {
			leafC = c
		}
	}
	if baseC == nil || leafC == nil {
		t.Fatalf("missing contributors: base=%v leaf=%v", baseC, leafC)
	}
	if baseC.Manifest == nil || baseC.Manifest.Description != "the base" {
		t.Fatalf("base manifest: %+v", baseC.Manifest)
	}
	if leafC.Manifest == nil || leafC.Manifest.Description != "the leaf" {
		t.Fatalf("leaf manifest: %+v", leafC.Manifest)
	}
}

func TestResolve_NoManifestOnIncludes(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"compA"}}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"f": "1"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, c := range plan.Contributors {
		if c.Kind == resolver.ContributorInclude && c.Manifest != nil {
			// ResolverError: include contributors should not carry a manifest.
			// In Go we model with *ProfileManifest; nil = absent.
			t.Fatalf("include contributor must not carry a manifest: %+v", c)
		}
	}
}

// ----- mergePolicy on each PlanFile -----

func TestResolve_MergePolicyOnPlanFiles(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}, files: map[string]string{
				"settings.json":   "{}",
				"CLAUDE.md":       "x",
				"agents/foo.json": "{}",
			}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	byPath := map[string]resolver.MergePolicy{}
	for _, f := range plan.Files {
		byPath[f.RelPath] = f.MergePolicy
	}
	if byPath["settings.json"] != resolver.MergePolicyDeepMerge {
		t.Fatalf("settings.json: %q", byPath["settings.json"])
	}
	if byPath["CLAUDE.md"] != resolver.MergePolicyConcat {
		t.Fatalf("CLAUDE.md: %q", byPath["CLAUDE.md"])
	}
	if byPath["agents/foo.json"] != resolver.MergePolicyLastWins {
		t.Fatalf("agents/foo.json: %q", byPath["agents/foo.json"])
	}
}

// ----- error messages quality -----

func TestResolve_ConflictErrorNamesPathAndContributors(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"a", "b"}}},
		},
		components: map[string]componentSpec{
			"a": {files: map[string]string{"agents/foo.json": "{}"}},
			"b": {files: map[string]string{"agents/foo.json": "{}"}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"agents/foo.json", `"a"`, `"b"`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q: %s", want, msg)
		}
	}
}

func TestResolve_MissingIncludeErrorNamesRawPathReferencer(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"./not-here"}}},
		},
	})
	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"./not-here", "not-here", "p"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q: %s", want, msg)
		}
	}
}

// ----- warnings aggregation -----

func TestResolve_AggregatesWarningsFromChain(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"base": {manifest: map[string]any{"weirdBase": 1}},
			"leaf": {manifest: map[string]any{"extends": "base", "weirdLeaf": 2}},
		},
	})
	plan, err := resolver.Resolve("leaf", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	sources := map[string]bool{}
	for _, w := range plan.Warnings {
		if w.Code == resolver.WarningUnknownManifestField {
			sources[w.Source] = true
		}
	}
	if !sources["base"] || !sources["leaf"] {
		t.Fatalf("missing source: %v", sources)
	}
}

// ----- duplicate include dedup -----

func TestResolve_DuplicateIncludeDeduped(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"compA", "compA"}}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"agents/x.json": "{}"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	includes := 0
	for _, c := range plan.Contributors {
		if c.Kind == resolver.ContributorInclude {
			includes++
		}
	}
	if includes != 1 {
		t.Fatalf("want 1 include contributor, got %d", includes)
	}
	xs := 0
	for _, f := range plan.Files {
		if f.RelPath == "agents/x.json" {
			xs++
		}
	}
	if xs != 1 {
		t.Fatalf("want 1 agents/x.json, got %d", xs)
	}
	dups := 0
	for _, w := range plan.Warnings {
		if w.Code == resolver.WarningDuplicateInclude {
			dups++
		}
	}
	if dups != 1 {
		t.Fatalf("want 1 DuplicateInclude warning, got %d", dups)
	}
}

func TestResolve_DuplicateIncludeDifferentRawForms(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"compA", "./../_components/compA"}}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"agents/x.json": "{}"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	includes := 0
	for _, c := range plan.Contributors {
		if c.Kind == resolver.ContributorInclude {
			includes++
		}
	}
	if includes != 1 {
		t.Fatalf("want 1 include contributor, got %d", includes)
	}
	dups := 0
	for _, w := range plan.Warnings {
		if w.Code == resolver.WarningDuplicateInclude {
			dups++
		}
	}
	if dups != 1 {
		t.Fatalf("want 1 DuplicateInclude warning, got %d", dups)
	}
}

// ----- destination tagging (cw6/T2) -----

func TestResolve_DestinationDefaultsClaudeForFiles(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}, files: map[string]string{
				"settings.json":   "{}",
				"CLAUDE.md":       "claude-dir md",
				"agents/foo.json": "{}",
			}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, f := range plan.Files {
		if f.Destination != resolver.DestinationClaude {
			t.Fatalf("want .claude, got %q", f.Destination)
		}
	}
}

func TestResolve_DestinationProjectRootForRootCLAUDEMd(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}, rootFiles: map[string]string{"CLAUDE.md": "ROOT"}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	rootEntries := 0
	claudeDirEntries := 0
	for _, f := range plan.Files {
		if f.RelPath != "CLAUDE.md" {
			continue
		}
		if f.Destination == resolver.DestinationProjectRoot {
			rootEntries++
			if f.MergePolicy != resolver.MergePolicyConcat {
				t.Fatalf("policy: %q", f.MergePolicy)
			}
		}
		if f.Destination == resolver.DestinationClaude {
			claudeDirEntries++
		}
	}
	if rootEntries != 1 {
		t.Fatalf("want 1 projectRoot CLAUDE.md, got %d", rootEntries)
	}
	if claudeDirEntries != 0 {
		t.Fatalf("want 0 .claude CLAUDE.md, got %d", claudeDirEntries)
	}
}

func TestResolve_BothDestinationsForCLAUDEMd(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {
				manifest:  map[string]any{},
				files:     map[string]string{"CLAUDE.md": "INSIDE"},
				rootFiles: map[string]string{"CLAUDE.md": "ROOT"},
			},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	count := 0
	dests := map[resolver.PlanFileDestination]bool{}
	for _, f := range plan.Files {
		if f.RelPath == "CLAUDE.md" {
			count++
			dests[f.Destination] = true
		}
	}
	if count != 2 {
		t.Fatalf("want 2 CLAUDE.md, got %d", count)
	}
	if !dests[resolver.DestinationClaude] || !dests[resolver.DestinationProjectRoot] {
		t.Fatalf("destinations: %v", dests)
	}
}

// ----- R2 — profile name validation (path traversal in extends/argv) -----

func TestResolve_RejectsTraversalInProfileName(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}},
		},
	})
	if err := os.MkdirAll(filepath.Join(fx.projectRoot, "outside", ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	_, err := resolver.Resolve("../outside", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
}

func TestResolve_RejectsTraversalInExtends(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"attacker": {manifest: map[string]any{"extends": "../../etc"}},
		},
	})
	_, err := resolver.Resolve("attacker", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
}

func TestResolve_RejectsSlashInExtends(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"attacker": {manifest: map[string]any{"extends": "_components/compA"}},
		},
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"f": "1"}},
		},
	})
	_, err := resolver.Resolve("attacker", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
}

func TestResolve_RejectsUnderscoreProfile(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		components: map[string]componentSpec{
			"compA": {files: map[string]string{"f": "1"}},
		},
	})
	_, err := resolver.Resolve("_components", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
}

func TestResolve_RejectsEmptyProfileName(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{})
	_, err := resolver.Resolve("", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var mpe *pipelineerrors.MissingProfileError
	if !stderrors.As(err, &mpe) {
		t.Fatalf("want MissingProfileError, got %v", err)
	}
}

// ----- empty edge cases -----

func TestResolve_EmptyProfileNoFiles(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}},
		},
	})
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !reflect.DeepEqual(plan.Chain, []string{"p"}) {
		t.Fatalf("chain: %v", plan.Chain)
	}
	if len(plan.Files) != 0 {
		t.Fatalf("want no files, got %v", plan.Files)
	}
}

// ----- helpers -----

func contains[T comparable](haystack []T, needle T) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

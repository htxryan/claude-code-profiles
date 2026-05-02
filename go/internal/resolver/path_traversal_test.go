package resolver_test

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/resolver"
)

// PR16a: a relative include with `../` segments that escapes the project
// root must produce a CONFLICT-class PathTraversalError BEFORE any
// filesystem touch. The Go resolver is stricter than TS here — TS lets the
// path resolve outside and tracks it as external, then surfaces a
// MissingInclude when the directory doesn't exist; Go rejects up-front.

func TestPR16a_RejectsRelativeEscape_Triple(t *testing.T) {
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"traversal": {
				manifest: map[string]any{"name": "traversal", "includes": []string{"../../../.ssh/config"}},
				files:    map[string]string{"x.md": "x\n"},
			},
		},
	})
	_, err := resolver.Resolve("traversal", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var pte *pipelineerrors.PathTraversalError
	if !stderrors.As(err, &pte) {
		t.Fatalf("want PathTraversalError, got %v", err)
	}
	if pte.Raw != "../../../.ssh/config" {
		t.Fatalf("raw: %q", pte.Raw)
	}
	if pte.ReferencedBy != "traversal" {
		t.Fatalf("referencedBy: %q", pte.ReferencedBy)
	}
	if pte.ErrorCode() != pipelineerrors.CodePathTraversal {
		t.Fatalf("code: %q", pte.ErrorCode())
	}
	// Resolver-phase classification.
	if pte.Phase() != pipelineerrors.PhaseResolver {
		t.Fatalf("phase: %q", pte.Phase())
	}
	// Message must name both the raw and resolved path so a user can locate
	// the offending manifest entry.
	if !strings.Contains(pte.Error(), "../../../.ssh/config") {
		t.Fatalf("message missing raw: %s", pte.Error())
	}
}

func TestPR16a_RejectsBeforeFilesystemTouch(t *testing.T) {
	// PR16a contract: the resolver rejects WITHOUT having to stat the
	// would-be target. We assert this by pointing the include at a path
	// that DOES exist on disk (so a "missing dir" check would NOT fire),
	// and verifying we still get PathTraversalError.
	//
	// referencingProfileDir = <projectRoot>/.claude-profiles/p, so we need
	// at least 3 `../` segments to escape projectRoot.
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{"includes": []string{"../../../escape-target"}}},
		},
	})
	// Create a sibling directory to projectRoot so the target actually
	// exists — only canonicalize-and-reject can stop this.
	target := filepath.Join(filepath.Dir(fx.projectRoot), "escape-target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(target) })

	_, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	var pte *pipelineerrors.PathTraversalError
	if !stderrors.As(err, &pte) {
		t.Fatalf("want PathTraversalError, got %v", err)
	}
}

func TestPR16a_RelativeInsideProjectAllowed(t *testing.T) {
	// Sanity: `./../sib` that resolves INSIDE projectRoot is fine — only
	// escapes are rejected.
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
}

func TestPR16a_AbsoluteExternalStillAllowed(t *testing.T) {
	// PR16a only rejects RELATIVE escapes. Absolute paths to external dirs
	// are an explicit, trusted form (tracked via R37a).
	fx := makeFixture(t, fixtureSpec{
		profiles: map[string]profileSpec{
			"p": {manifest: map[string]any{}},
		},
		external: map[string]componentSpec{
			"ext1": {files: map[string]string{"a": "1"}},
		},
	})
	extAbs := filepath.Join(fx.externalRoot, "ext1")
	manifest := []byte(`{"includes": [` + jsonString(extAbs) + `]}`)
	if err := os.WriteFile(filepath.Join(fx.projectRoot, ".claude-profiles", "p", "profile.json"), manifest, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan, err := resolver.Resolve("p", resolver.ResolveOptions{ProjectRoot: fx.projectRoot})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !plan.Includes[0].External {
		t.Fatalf("expected external=true")
	}
}

func TestPR16a_DirectClassifyIncludeRejects(t *testing.T) {
	// Unit-level: ClassifyInclude itself rejects without going through the
	// full resolve pipeline.
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	// referencingDir is /tmp/some-project/.claude-profiles/myprofile;
	// 4 `../` segments escape both `.claude-profiles/` and the project root.
	_, err := resolver.ClassifyInclude("../../../../etc/passwd", referencingDir, paths, "p")
	var pte *pipelineerrors.PathTraversalError
	if !stderrors.As(err, &pte) {
		t.Fatalf("want PathTraversalError, got %v", err)
	}
}

func jsonString(s string) string {
	// Minimal JSON-escape for the string forms used in tests.
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

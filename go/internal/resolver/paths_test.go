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

func TestClassifyInclude_Component(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	ref, err := resolver.ClassifyInclude("compA", referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify component: %v", err)
	}
	if ref.Kind != resolver.IncludeKindComponent {
		t.Fatalf("kind: want component, got %q", ref.Kind)
	}
	wantPath := filepath.Join(projectRoot, ".claude-profiles", "_components", "compA")
	if ref.ResolvedPath != wantPath {
		t.Fatalf("resolvedPath: want %q, got %q", wantPath, ref.ResolvedPath)
	}
	if ref.External {
		t.Fatalf("expected non-external for in-repo component")
	}
}

func TestClassifyInclude_Relative(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	ref, err := resolver.ClassifyInclude("./neighbor", referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify ./neighbor: %v", err)
	}
	if ref.Kind != resolver.IncludeKindRelative {
		t.Fatalf("kind: want relative, got %q", ref.Kind)
	}
	want := filepath.Clean(filepath.Join(referencingDir, "neighbor"))
	if ref.ResolvedPath != want {
		t.Fatalf("resolvedPath: want %q, got %q", want, ref.ResolvedPath)
	}

	ref2, err := resolver.ClassifyInclude("../sib", referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify ../sib: %v", err)
	}
	if ref2.Kind != resolver.IncludeKindRelative {
		t.Fatalf("kind: want relative, got %q", ref2.Kind)
	}
}

func TestClassifyInclude_TildeExpands(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	ref, err := resolver.ClassifyInclude("~/some/path", referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify ~: %v", err)
	}
	if ref.Kind != resolver.IncludeKindTilde {
		t.Fatalf("kind: want tilde, got %q", ref.Kind)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Clean(filepath.Join(home, "some/path"))
	if ref.ResolvedPath != want {
		t.Fatalf("resolvedPath: want %q, got %q", want, ref.ResolvedPath)
	}

	ref2, err := resolver.ClassifyInclude("~", referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify bare ~: %v", err)
	}
	if ref2.Kind != resolver.IncludeKindTilde {
		t.Fatalf("kind: want tilde, got %q", ref2.Kind)
	}
}

func TestClassifyInclude_TildeInsideProjectRoot(t *testing.T) {
	// When projectRoot itself happens to live under $HOME, a tilde include
	// whose expanded path falls inside projectRoot must be flagged
	// non-external. Coverage gap surfaced by the D1 review.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	projectRoot := filepath.Join(home)
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	ref, err := resolver.ClassifyInclude("~", referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify ~: %v", err)
	}
	if ref.External {
		t.Fatalf("expected external=false when ~ resolves inside projectRoot, got external=%v", ref.External)
	}
}

func TestIsExternal_DotDotPrefixedFilename(t *testing.T) {
	// A filename whose first component begins with `..` characters but
	// is not a path-traversal segment must not be flagged external.
	// Regression guard for the prefix-vs-segment bug.
	if resolver.IsExternal("/proj/..hidden", "/proj") {
		t.Fatalf(`"/proj/..hidden" must not be external`)
	}
}

func TestClassifyInclude_AbsoluteExternal(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	outside := "/var/tmp/external-profile"
	ref, err := resolver.ClassifyInclude(outside, referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify abs external: %v", err)
	}
	if !ref.External {
		t.Fatalf("expected external=true for /var/tmp/...")
	}
	if ref.Kind != resolver.IncludeKindAbsolute {
		t.Fatalf("kind: want absolute, got %q", ref.Kind)
	}
}

func TestClassifyInclude_AbsoluteInternal(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	inside := filepath.Join(projectRoot, ".claude-profiles", "_components", "x")
	ref, err := resolver.ClassifyInclude(inside, referencingDir, paths, "p")
	if err != nil {
		t.Fatalf("classify abs internal: %v", err)
	}
	if ref.External {
		t.Fatalf("expected external=false for in-repo absolute")
	}
	if ref.Kind != resolver.IncludeKindAbsolute {
		t.Fatalf("kind: want absolute, got %q", ref.Kind)
	}
}

func TestClassifyInclude_RejectsBareWithSlashes(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	_, err := resolver.ClassifyInclude("foo/bar", referencingDir, paths, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("expected InvalidManifestError, got %v", err)
	}
}

func TestClassifyInclude_RejectsBareWithBackslashes(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	_, err := resolver.ClassifyInclude(`foo\bar`, referencingDir, paths, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("expected InvalidManifestError, got %v", err)
	}
}

func TestClassifyInclude_RejectsTildeUserForm(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	_, err := resolver.ClassifyInclude("~bob/path", referencingDir, paths, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("expected InvalidManifestError, got %v", err)
	}
	if !strings.Contains(err.Error(), "~user") {
		t.Fatalf("error message must mention ~user: %q", err.Error())
	}
}

func TestClassifyInclude_RejectsEmpty(t *testing.T) {
	projectRoot := mustAbs(t, "/tmp/some-project")
	referencingDir := filepath.Join(projectRoot, ".claude-profiles", "myprofile")
	paths := resolver.BuildPaths(projectRoot)

	_, err := resolver.ClassifyInclude("", referencingDir, paths, "p")
	var ime *pipelineerrors.InvalidManifestError
	if !stderrors.As(err, &ime) {
		t.Fatalf("expected InvalidManifestError, got %v", err)
	}
}

func TestIsValidProfileName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"frontend", true},
		{"staging-leaf", true},
		{"foo123", true},
		{"..", false},
		{".", false},
		{"../outside", false},
		{"foo/bar", false},
		{`foo\bar`, false},
		{"", false},
		{"_components", false},
		{".hidden", false},
		{"CON", false},
		{"con.txt", false},
		{"COM1", false},
		{"LPT9", false},
		{"trailing.", false},
		{"trailing ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolver.IsValidProfileName(tc.name); got != tc.want {
				t.Fatalf("IsValidProfileName(%q): want %v, got %v", tc.name, tc.want, got)
			}
		})
	}
}

func TestIsExternal(t *testing.T) {
	cases := []struct {
		absPath, root string
		want          bool
	}{
		{"/var/data", "/Users/x/proj", true},
		{"/Users/x/proj/sub/dir", "/Users/x/proj", false},
		{"/Users/x/proj", "/Users/x/proj", false},
	}
	for _, tc := range cases {
		t.Run(tc.absPath, func(t *testing.T) {
			if got := resolver.IsExternal(tc.absPath, tc.root); got != tc.want {
				t.Fatalf("IsExternal(%q, %q): want %v, got %v", tc.absPath, tc.root, tc.want, got)
			}
		})
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

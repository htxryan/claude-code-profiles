// Package helpers provides the Go integration-test harness. F1 ships
// MakeFixture (semantic-equivalent to TS fixture.ts) and RunCli
// (semantic-equivalent to TS spawn.ts:runCli).
//
// Helper-parity audit (PR4): scripts/helper_parity_audit.sh diffs the
// public surfaces of fixture.ts and fixture.go on every PR. Adding a field
// or method here without a corresponding TS change (or vice versa) fails
// CI — the spec is "false-green tests are the worst kind of green".
//
// MH1 closure: every Fixture uses t.TempDir() for isolation; we never
// share state with TS fixtures, so a test that passes in Go but fails in
// TS reflects a real binary difference, not harness drift.
package helpers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ProfileSpec describes an in-tree profile fixture. Profiles live under
// .claude-profiles/<name>/ with profile.json and a .claude/ subtree of
// arbitrary file content.
type ProfileSpec struct {
	// Manifest may be a map (will be JSON-marshalled), a string (written
	// verbatim — the test wants invalid JSON), or nil (no profile.json
	// written — the test wants the manifest absent).
	Manifest any
	// Files keys relative posix paths under .claude/ → file content.
	Files map[string]string
	// RootFiles keys relative posix paths at the profile root (sibling of
	// .claude/) → file content. Used for cw6 to set up
	// .claude-profiles/<P>/CLAUDE.md (the destination='projectRoot' source).
	RootFiles map[string]string
}

// ComponentSpec describes an in-repo or out-of-repo component fixture.
type ComponentSpec struct {
	Files     map[string]string
	RootFiles map[string]string
}

// FixtureSpec is the input to MakeFixture: profiles, components, and
// out-of-repo paths that tests treat as absolute.
type FixtureSpec struct {
	Profiles map[string]ProfileSpec
	// Components live under .claude-profiles/_components/<name>/.
	Components map[string]ComponentSpec
	// External keys relative paths under ExternalRoot (the test treats
	// them as "absolute" because the resolver sees a fully-qualified path).
	External map[string]ComponentSpec
}

// Fixture is the handle returned by MakeFixture. ProjectRoot is the
// in-repo root the CLI runs against (the --cwd target); ExternalRoot is
// the parallel tree for out-of-repo includes. The temp tree is cleaned up
// automatically by t.Cleanup, including on test panic.
type Fixture struct {
	ProjectRoot  string
	ExternalRoot string
}

// MakeFixture materializes spec into a fresh t.TempDir() tree and returns
// a Fixture. The tree is removed at test end automatically (including on
// panic), since t.TempDir() registers its own cleanup. Errors during
// creation surface via t.Fatal — matches the TS pattern of "throw and the
// test bails".
func MakeFixture(t *testing.T, spec FixtureSpec) *Fixture {
	t.Helper()
	f, err := makeFixtureAt(t.TempDir(), spec)
	if err != nil {
		t.Fatalf("makeFixture: %v", err)
	}
	return f
}

func makeFixtureAt(tmp string, spec FixtureSpec) (*Fixture, error) {
	projectRoot := filepath.Join(tmp, "project")
	externalRoot := filepath.Join(tmp, "external")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir project: %w", err)
	}
	if err := os.MkdirAll(externalRoot, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir external: %w", err)
	}

	profilesDir := filepath.Join(projectRoot, ".claude-profiles")

	for name, p := range spec.Profiles {
		dir := filepath.Join(profilesDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir profile %q: %w", name, err)
		}
		if err := writeManifest(filepath.Join(dir, "profile.json"), p.Manifest); err != nil {
			return nil, fmt.Errorf("profile %q manifest: %w", name, err)
		}
		if err := writeTree(filepath.Join(dir, ".claude"), p.Files); err != nil {
			return nil, fmt.Errorf("profile %q files: %w", name, err)
		}
		if err := writeTree(dir, p.RootFiles); err != nil {
			return nil, fmt.Errorf("profile %q rootFiles: %w", name, err)
		}
	}

	for name, c := range spec.Components {
		dir := filepath.Join(profilesDir, "_components", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir component %q: %w", name, err)
		}
		if err := writeTree(filepath.Join(dir, ".claude"), c.Files); err != nil {
			return nil, fmt.Errorf("component %q files: %w", name, err)
		}
		if err := writeTree(dir, c.RootFiles); err != nil {
			return nil, fmt.Errorf("component %q rootFiles: %w", name, err)
		}
	}

	for absKey, c := range spec.External {
		dir := filepath.Join(externalRoot, absKey)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir external %q: %w", absKey, err)
		}
		if err := writeTree(filepath.Join(dir, ".claude"), c.Files); err != nil {
			return nil, fmt.Errorf("external %q files: %w", absKey, err)
		}
		if err := writeTree(dir, c.RootFiles); err != nil {
			return nil, fmt.Errorf("external %q rootFiles: %w", absKey, err)
		}
	}

	return &Fixture{
		ProjectRoot:  projectRoot,
		ExternalRoot: externalRoot,
	}, nil
}

// writeManifest mirrors the TS three-way switch: nil → write nothing;
// string → write verbatim (test wants invalid/exotic JSON); any other →
// JSON-marshal with two-space indent (matches TS JSON.stringify(_, null, 2)
// byte-for-byte for object literals).
func writeManifest(path string, manifest any) error {
	if manifest == nil {
		return nil
	}
	switch v := manifest.(type) {
	case string:
		return os.WriteFile(path, []byte(v), 0o644)
	default:
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, bytes, 0o644)
	}
}

func writeTree(root string, files map[string]string) error {
	for rel, content := range files {
		fp := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

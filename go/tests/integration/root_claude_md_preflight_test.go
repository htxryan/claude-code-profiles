package integration_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV translation of tests/cli/integration/root-claude-md-preflight.test.ts.
// Pins R45 strict-abort pre-flight on project-root CLAUDE.md (cw6/T4)
// through the spawned Go CLI:
//   - Exit code 1 (user error class).
//   - Error wording names CLAUDE.md and `c3p init`.
//   - Both destinations (project-root CLAUDE.md and .claude/) byte-identical
//     to pre-state on abort (R45 atomic-across-destinations).
//   - No leftover .pending / .prior / projectRoot CLAUDE.md.*.tmp staging.
//
// The lock file is a record of the last lock holder in the Go bin and is
// expected to remain (unlike the TS bin); we don't pin its absence here.

// rootClaudeMdTmpPattern mirrors state.IsRootClaudeMdTmpName; we duplicate
// the regex (rather than import the internal package) per IV constraint:
// tests/integration must talk to the bin only.
var rootClaudeMdTmpPattern = regexp.MustCompile(`^CLAUDE\.md\.\d+\.\d+-[a-z0-9]+\.tmp$`)

// setupRootContributorFx builds a fixture with profile `a` (no projectRoot
// contributor) and profile `b` (root + .claude/ contributors), then
// materializes `a` via the CLI so .claude/ has a defined pre-state.
func setupRootContributorFx(t *testing.T) *helpers.Fixture {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {
				Manifest: map[string]any{"name": "a"},
				Files:    map[string]string{"CLAUDE.md": "A\n", "settings.json": `{"v":"a"}`},
			},
			"b": {
				Manifest:  map[string]any{"name": "b"},
				Files:     map[string]string{"CLAUDE.md": "B\n", "settings.json": `{"v":"b"}`},
				RootFiles: map[string]string{"CLAUDE.md": "PROFILE-B-MANAGED-BODY\n"},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "a"}})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	return fx
}

// readTree mirrors the TS helper: relative posix path -> utf8 content.
// Returns empty map for missing root.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readTree %q: %v", root, err)
	}
	return out
}

func assertNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err == nil {
		t.Errorf("expected %q absent, but it exists", p)
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %q: unexpected err %v", p, err)
	}
}

// TestRootClaudeMdPreflight_FileAbsent — `use b` aborts when projectRoot
// CLAUDE.md is missing; .claude/ byte-identical; exit 1; no .pending/.prior;
// no leftover CLAUDE.md.*.tmp files in projectRoot.
func TestRootClaudeMdPreflight_FileAbsent(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupRootContributorFx(t)
	claudeDir := filepath.Join(fx.ProjectRoot, ".claude")
	rootClaudeMd := filepath.Join(fx.ProjectRoot, "CLAUDE.md")

	preClaude := readTree(t, claudeDir)
	assertNotExist(t, rootClaudeMd)

	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})

	if r.ExitCode != 1 {
		t.Fatalf("use b (no root): want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "c3p init") {
		t.Errorf("stderr missing 'c3p init': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "CLAUDE.md") {
		t.Errorf("stderr missing 'CLAUDE.md': %q", r.Stderr)
	}

	// Both destinations untouched.
	post := readTree(t, claudeDir)
	if !reflect.DeepEqual(post, preClaude) {
		t.Errorf("claude/ tree changed: pre=%v post=%v", preClaude, post)
	}
	assertNotExist(t, rootClaudeMd)

	// No leftover staging.
	metaDir := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta")
	assertNotExist(t, filepath.Join(metaDir, "pending"))
	assertNotExist(t, filepath.Join(metaDir, "prior"))

	// No leftover splice tmp at projectRoot.
	entries, err := os.ReadDir(fx.ProjectRoot)
	if err != nil {
		t.Fatalf("readdir projectRoot: %v", err)
	}
	for _, e := range entries {
		if rootClaudeMdTmpPattern.MatchString(e.Name()) {
			t.Errorf("leftover splice tmp: %q", e.Name())
		}
	}
}

// TestRootClaudeMdPreflight_FilePresentMarkersMissing — plain CLAUDE.md
// without c3p markers triggers strict abort; both destinations byte-identical.
func TestRootClaudeMdPreflight_FilePresentMarkersMissing(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupRootContributorFx(t)
	claudeDir := filepath.Join(fx.ProjectRoot, ".claude")
	rootClaudeMd := filepath.Join(fx.ProjectRoot, "CLAUDE.md")

	original := "# Project README\n\nNo c3p markers here yet.\n"
	if err := os.WriteFile(rootClaudeMd, []byte(original), 0o644); err != nil {
		t.Fatalf("seed CLAUDE.md: %v", err)
	}
	preClaude := readTree(t, claudeDir)

	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r.ExitCode != 1 {
		t.Fatalf("use b: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "c3p init") {
		t.Errorf("stderr missing 'c3p init': %q", r.Stderr)
	}
	if got := readTree(t, claudeDir); !reflect.DeepEqual(got, preClaude) {
		t.Errorf("claude/ tree changed")
	}
	got, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if string(got) != original {
		t.Errorf("CLAUDE.md changed: got %q want %q", string(got), original)
	}
}

// TestRootClaudeMdPreflight_MalformedLoneBegin — lone :begin without :end
// is treated as malformed → R45 abort; file unchanged.
func TestRootClaudeMdPreflight_MalformedLoneBegin(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupRootContributorFx(t)
	rootClaudeMd := filepath.Join(fx.ProjectRoot, "CLAUDE.md")
	claudeDir := filepath.Join(fx.ProjectRoot, ".claude")

	broken := "intro\n<!-- c3p:v1:begin -->\nstuff but no end\n"
	if err := os.WriteFile(rootClaudeMd, []byte(broken), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	preClaude := readTree(t, claudeDir)

	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r.ExitCode != 1 {
		t.Fatalf("use b malformed: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	got, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if string(got) != broken {
		t.Errorf("CLAUDE.md mutated on abort: got %q", string(got))
	}
	if post := readTree(t, claudeDir); !reflect.DeepEqual(post, preClaude) {
		t.Errorf("claude/ tree changed")
	}
}

// TestRootClaudeMdPreflight_RecoversAfterMarkerFix — once the user adds a
// well-formed marker pair, the next `use b` lands the splice cleanly with
// no manual cleanup of staging dirs needed. Note: the Go bin emits
// "switched to b" (lowercase verb), not "Switched to b" as TS does.
func TestRootClaudeMdPreflight_RecoversAfterMarkerFix(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupRootContributorFx(t)
	rootClaudeMd := filepath.Join(fx.ProjectRoot, "CLAUDE.md")
	claudeDir := filepath.Join(fx.ProjectRoot, ".claude")

	if err := os.WriteFile(rootClaudeMd, []byte("# Pre-existing notes.\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	r1 := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r1.ExitCode != 1 {
		t.Fatalf("first use b: want 1, got %d (stderr=%q)", r1.ExitCode, r1.Stderr)
	}

	fixed := "# Pre-existing notes.\n\n<!-- c3p:v1:begin -->\n<!-- Managed block. -->\n\n<!-- c3p:v1:end -->\n"
	if err := os.WriteFile(rootClaudeMd, []byte(fixed), 0o644); err != nil {
		t.Fatalf("write fixed: %v", err)
	}

	r2 := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r2.ExitCode != 0 {
		t.Fatalf("retry use b: want 0, got %d (stderr=%q)", r2.ExitCode, r2.Stderr)
	}
	if !strings.Contains(strings.ToLower(r2.Stdout), "switched to b") {
		t.Errorf("stdout missing 'switched to b': %q", r2.Stdout)
	}

	final, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if !strings.HasPrefix(string(final), "# Pre-existing notes.") {
		t.Errorf("user prose dropped: %q", string(final))
	}
	if !strings.Contains(string(final), "PROFILE-B-MANAGED-BODY") {
		t.Errorf("managed body not spliced: %q", string(final))
	}
	got, err := os.ReadFile(filepath.Join(claudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read .claude/CLAUDE.md: %v", err)
	}
	if string(got) != "B\n" {
		t.Errorf(".claude/CLAUDE.md = %q, want %q", string(got), "B\n")
	}
}

// TestRootClaudeMdPreflight_MultipleManagedBlocks — per spec §12.3 multiple
// managed blocks are reserved; v1 treats as malformed → abort, file unchanged.
func TestRootClaudeMdPreflight_MultipleManagedBlocks(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupRootContributorFx(t)
	rootClaudeMd := filepath.Join(fx.ProjectRoot, "CLAUDE.md")
	claudeDir := filepath.Join(fx.ProjectRoot, ".claude")

	dual := "intro\n" +
		"<!-- c3p:v1:begin -->\nblock 1\n<!-- c3p:v1:end -->\n" +
		"between\n" +
		"<!-- c3p:v1:begin -->\nblock 2\n<!-- c3p:v1:end -->\n" +
		"tail\n"
	if err := os.WriteFile(rootClaudeMd, []byte(dual), 0o644); err != nil {
		t.Fatalf("seed dual: %v", err)
	}
	preClaude := readTree(t, claudeDir)

	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "b"}})
	if r.ExitCode != 1 {
		t.Fatalf("use b dual: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	got, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != dual {
		t.Errorf("CLAUDE.md mutated: got %q", string(got))
	}
	if post := readTree(t, claudeDir); !reflect.DeepEqual(post, preClaude) {
		t.Errorf("claude/ tree changed")
	}
}

package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestVersionPrintsAndExitsZero is the IV translation of TS
// help-version.test.ts → "--version prints package version + exits 0".
// The Go bin emits "c3p X.Y.Z"; under tests the version string is the
// fixed "0.0.0-dev" tag injected by the test harness build (no ldflags).
func TestVersionPrintsAndExitsZero(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--version"}})
	if r.ExitCode != 0 {
		t.Fatalf("--version exit: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if matched, _ := regexp.MatchString(`c3p \S+`, r.Stdout); !matched {
		t.Fatalf("--version stdout missing 'c3p X.Y.Z' pattern: %q", r.Stdout)
	}
}

// TestVersionWorksThroughSymlink is the post-publish regression from TS:
// the bin must work when invoked through a symlink (npm shim path).
// Go binaries don't have the import.meta.url problem TS had, but we still
// pin the behavior so a future packaging change can't regress it.
func TestVersionWorksThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires non-Windows or developer-mode admin")
	}
	helpers.EnsureBuilt(t)
	bin := helpers.BinPath(t)
	tmp := t.TempDir()
	linked := filepath.Join(tmp, "c3p-link")
	if err := os.Symlink(bin, linked); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Spawn through the symlink directly (helpers.RunCli would resolve to
	// the cached real binary path, which would skip the symlink entirely
	// and miss the regression we want to pin).
	r := runBin(t, linked, []string{"--version"}, nil, "")
	if r.ExitCode != 0 {
		t.Fatalf("symlink --version exit: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if matched, _ := regexp.MatchString(`c3p \S+`, r.Stdout); !matched {
		t.Fatalf("symlink --version stdout missing 'c3p X.Y.Z': %q", r.Stdout)
	}
}

// TestVersionShortFlag covers -V parity with --version.
func TestVersionShortFlag(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"-V"}})
	if r.ExitCode != 0 {
		t.Fatalf("-V exit: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if matched, _ := regexp.MatchString(`c3p \S+`, r.Stdout); !matched {
		t.Fatalf("-V stdout missing version pattern: %q", r.Stdout)
	}
}

// TestHelpListsR29Verbs asserts every R29 verb appears in --help, plus
// the EXIT CODES section.
func TestHelpListsR29Verbs(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--help"}})
	if r.ExitCode != 0 {
		t.Fatalf("--help exit: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	for _, verb := range []string{"init", "list", "use", "status", "drift", "diff", "new", "validate", "sync", "hook"} {
		if !strings.Contains(r.Stdout, verb) {
			t.Errorf("--help missing verb %q", verb)
		}
	}
	if !strings.Contains(r.Stdout, "EXIT CODES") {
		t.Errorf("--help missing EXIT CODES section: %q", r.Stdout)
	}
}

// TestUseDashHelpPrintsVerbGuidance asserts "use --help" mentions
// --on-drift= (the verb-specific pivot).
func TestUseDashHelpPrintsVerbGuidance(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"use", "--help"}})
	if r.ExitCode != 0 {
		t.Fatalf("use --help exit: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "--on-drift=") {
		t.Errorf("use --help missing --on-drift= guidance: %q", r.Stdout)
	}
}

// TestEveryVerbHelpHasStandardSections is the cw6 lock-in: every verb's
// help follows the same template (USAGE / DESCRIPTION / EXAMPLES /
// EXIT CODES + a global flag reference).
func TestEveryVerbHelpHasStandardSections(t *testing.T) {
	helpers.EnsureBuilt(t)
	verbs := []string{"init", "list", "use", "status", "drift", "diff", "new", "validate", "sync", "hook", "doctor", "completions"}
	flagRE := regexp.MustCompile(`--(cwd|json)`)
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			r := mustRun(t, helpers.SpawnOptions{Args: []string{verb, "--help"}})
			if r.ExitCode != 0 {
				t.Fatalf("%s --help exit: want 0, got %d (stderr=%q)", verb, r.ExitCode, r.Stderr)
			}
			for _, sec := range []string{"USAGE", "DESCRIPTION", "EXAMPLES", "EXIT CODES"} {
				if !strings.Contains(r.Stdout, sec) {
					t.Errorf("%s --help missing %s", verb, sec)
				}
			}
			if !flagRE.MatchString(r.Stdout) {
				t.Errorf("%s --help missing global flag reference (--cwd or --json)", verb)
			}
		})
	}
}

// TestValidateHelpDocumentsR44MarkerCase covers cw6.4: validate --help
// must mention the missing-markers exit-1 case + remediation.
func TestValidateHelpDocumentsR44MarkerCase(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"validate", "--help"}})
	if r.ExitCode != 0 {
		t.Fatalf("validate --help exit: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "EXIT CODES") {
		t.Errorf("validate --help missing EXIT CODES")
	}
	lower := strings.ToLower(r.Stdout)
	if !strings.Contains(lower, "missing c3p markers") {
		t.Errorf("validate --help missing R44 marker-missing description: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "c3p init") {
		t.Errorf("validate --help missing 'c3p init' remediation: %q", r.Stdout)
	}
}

// TestTopLevelHelpDefinesGlossary asserts the spec terms in --help all
// appear in the GLOSSARY block.
func TestTopLevelHelpDefinesGlossary(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--help"}})
	if r.ExitCode != 0 {
		t.Fatalf("--help exit: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "GLOSSARY") {
		t.Errorf("--help missing GLOSSARY section: %q", r.Stdout)
	}
	for _, term := range []string{"profile", "extends", "drift", "materialize"} {
		if !strings.Contains(r.Stdout, term) {
			t.Errorf("GLOSSARY missing term %q", term)
		}
	}
}

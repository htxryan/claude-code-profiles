package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestHelpBytesStable is the help-version fitness function: the rendered
// help text must be byte-stable across runs. Tests the Go side; the
// dual-suite IV harness (separate epic) compares these bytes against the
// TS bin's output.
func TestHelpBytesStable(t *testing.T) {
	a := TopLevelHelp()
	b := TopLevelHelp()
	if a != b {
		t.Fatalf("TopLevelHelp not deterministic")
	}
	// Sanity check: known-anchor strings.
	for _, s := range []string{
		"COMMANDS",
		"GLOBAL OPTIONS",
		"GLOSSARY",
		"EXIT CODES",
		"--json",
		"--cwd=<path>",
		"init",
		"list",
		"use <name>",
		"sync",
		"completions <shell>",
	} {
		if !strings.Contains(a, s) {
			t.Errorf("top-level help missing %q", s)
		}
	}
}

// TestVerbHelpRendersAllVerbs ensures every documented verb has a help
// page. The hidden `hello` verb is intentionally absent from the help map.
func TestVerbHelpRendersAllVerbs(t *testing.T) {
	verbs := []string{
		"init", "list", "use", "status", "drift", "diff",
		"new", "validate", "sync", "hook", "doctor", "completions",
	}
	for _, v := range verbs {
		t.Run(v, func(t *testing.T) {
			text := VerbHelp(v)
			if !strings.Contains(text, "USAGE") || !strings.Contains(text, "EXIT CODES") {
				t.Fatalf("%s help missing USAGE/EXIT CODES sections: %q", v, text)
			}
		})
	}
}

// TestVersionFlagBytes asserts --version produces "c3p X.Y.Z" on non-TTY.
func TestVersionFlagBytes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, "9.9.9", &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("--version: want %d, got %d", ExitOK, code)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != "c3p 9.9.9" {
		t.Fatalf("--version: want 'c3p 9.9.9', got %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("--version unexpected stderr: %q", stderr.String())
	}
}

// TestHelpVerbReturnsHelpText is the "help <verb>" parity test.
func TestHelpVerbReturnsHelpText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"help", "use"}, "0", &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("help use: want %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout.String(), "use <name>") {
		t.Fatalf("help use missing synopsis line: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "drift gate") {
		t.Fatalf("help use missing description: %q", stdout.String())
	}
}

// TestVerbDashHelpRendersVerbHelp asserts `c3p use --help` works.
func TestVerbDashHelpRendersVerbHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"use", "--help"}, "0", &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("use --help: want %d, got %d", ExitOK, code)
	}
	if !strings.Contains(stdout.String(), "use <name>") {
		t.Fatalf("use --help missing synopsis: %q", stdout.String())
	}
}

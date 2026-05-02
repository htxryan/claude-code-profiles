// Package integration_test — IV/T4 translation of TS section-ownership-e2e.test.ts.
//
// cw6 / P1-C: end-to-end command transcript for section ownership through
// the spawned Go CLI binary:
//
//   1. init                          → root CLAUDE.md gains markers
//   2. profile setup with profile-root CLAUDE.md
//   3. use <profile>                 → section spliced; outside bytes intact
//   4. user edits between markers    → drift surfaces
//   5. use --on-drift=persist        → edited bytes persisted to profile dir
//   6. use <profile> again           → byte-identical to step 4 (round-trip)
package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestSectionOwnership_E2E_FullTranscript pins the full happy path.
func TestSectionOwnership_E2E_FullTranscript(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})

	projectRoot := fx.ProjectRoot
	rootClaudeMd := filepath.Join(projectRoot, "CLAUDE.md")
	profileDir := filepath.Join(projectRoot, ".claude-profiles", "devmode")
	profileClaudeMd := filepath.Join(profileDir, "CLAUDE.md")

	// STEP 1: init creates root CLAUDE.md with markers.
	initR := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", projectRoot, "init", "--no-hook", "--no-seed"},
	})
	if initR.ExitCode != 0 {
		t.Fatalf("init: exit=%d stderr=%q", initR.ExitCode, initR.Stderr)
	}
	afterInit, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read root after init: %v", err)
	}
	for _, want := range []string{"<!-- c3p:v1:begin", "<!-- c3p:v1:end"} {
		if !strings.Contains(string(afterInit), want) {
			t.Fatalf("root after init missing %q: %q", want, string(afterInit))
		}
	}

	// STEP 2: stand up profile with a profile-root CLAUDE.md.
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	mfBytes, _ := json.MarshalIndent(map[string]any{"name": "devmode"}, "", "  ")
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), mfBytes, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	initialProfileBody := "# Devmode rules\n\nUse strict types.\n"
	if err := os.WriteFile(profileClaudeMd, []byte(initialProfileBody), 0o644); err != nil {
		t.Fatalf("write profile CLAUDE.md: %v", err)
	}

	// Capture bytes outside markers BEFORE use to prove they survive splice.
	beginIdxBefore := strings.Index(string(afterInit), "<!-- c3p:v1:begin")
	endMarkerIdxBefore := strings.Index(string(afterInit), "<!-- c3p:v1:end")
	if beginIdxBefore < 0 || endMarkerIdxBefore < 0 {
		t.Fatalf("markers not found in init output")
	}
	endLineEndBefore := strings.Index(string(afterInit)[endMarkerIdxBefore:], "\n")
	if endLineEndBefore < 0 {
		t.Fatalf("end marker not newline-terminated")
	}
	endLineEndBefore += endMarkerIdxBefore + 1
	aboveBefore := string(afterInit)[:beginIdxBefore]
	belowBefore := string(afterInit)[endLineEndBefore:]

	// STEP 3: use devmode splices body between markers; outside preserved.
	useR := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", projectRoot, "use", "devmode"}})
	if useR.ExitCode != 0 {
		t.Fatalf("use devmode: exit=%d stderr=%q", useR.ExitCode, useR.Stderr)
	}
	if !strings.Contains(strings.ToLower(useR.Stdout), "switched to devmode") {
		t.Errorf("use stdout missing switched line: %q", useR.Stdout)
	}

	afterUse, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read root after use: %v", err)
	}
	for _, want := range []string{"Use strict types.", "# Devmode rules", "<!-- c3p:v1:begin", "<!-- c3p:v1:end"} {
		if !strings.Contains(string(afterUse), want) {
			t.Errorf("root after use missing %q: %q", want, string(afterUse))
		}
	}
	beginIdxAfter := strings.Index(string(afterUse), "<!-- c3p:v1:begin")
	endMarkerIdxAfter := strings.Index(string(afterUse), "<!-- c3p:v1:end")
	endLineEndAfter := strings.Index(string(afterUse)[endMarkerIdxAfter:], "\n")
	if endLineEndAfter < 0 {
		t.Fatalf("after-use end marker not newline-terminated")
	}
	endLineEndAfter += endMarkerIdxAfter + 1
	if got := string(afterUse)[:beginIdxAfter]; got != aboveBefore {
		t.Errorf("bytes above markers changed: want %q got %q", aboveBefore, got)
	}
	if got := string(afterUse)[endLineEndAfter:]; got != belowBefore {
		t.Errorf("bytes below markers changed: want %q got %q", belowBefore, got)
	}

	// state.json reports section fingerprint.
	stateBytes, err := os.ReadFile(filepath.Join(projectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st struct {
		ActiveProfile       string `json:"activeProfile"`
		RootClaudeMdSection *struct {
			Size int `json:"size"`
		} `json:"rootClaudeMdSection"`
	}
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if st.ActiveProfile != "devmode" {
		t.Errorf("activeProfile want devmode got %q", st.ActiveProfile)
	}
	if st.RootClaudeMdSection == nil {
		t.Errorf("state.rootClaudeMdSection want non-null")
	} else if st.RootClaudeMdSection.Size <= 0 {
		t.Errorf("state.rootClaudeMdSection.size want >0 got %d", st.RootClaudeMdSection.Size)
	}

	// STEP 4: edit section bytes; drift surfaces.
	editedFile := strings.Replace(string(afterUse), "Use strict types.", "Use strict types AND prefer immutable data.", 1)
	if editedFile == string(afterUse) {
		t.Fatalf("sanity: replacement did not happen")
	}
	if err := os.WriteFile(rootClaudeMd, []byte(editedFile), 0o644); err != nil {
		t.Fatalf("write edited: %v", err)
	}
	editedSnapshot, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("snapshot edited: %v", err)
	}

	driftR := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", projectRoot, "drift"}})
	if driftR.ExitCode != 0 {
		t.Fatalf("drift: want exit 0, got %d (stderr=%q)", driftR.ExitCode, driftR.Stderr)
	}
	if !strings.Contains(driftR.Stdout, "CLAUDE.md") {
		t.Errorf("drift stdout missing CLAUDE.md: %q", driftR.Stdout)
	}
	// Go drift uses `M  CLAUDE.md` (modified status code). TS used "modified
	// CLAUDE.md". Both surface CLAUDE.md as drifted; assert the M flag and
	// the file count summary.
	if !strings.Contains(driftR.Stdout, "M  CLAUDE.md") && !strings.Contains(driftR.Stdout, "modified") {
		t.Errorf("drift stdout missing modified marker for CLAUDE.md: %q", driftR.Stdout)
	}
	if !strings.Contains(driftR.Stdout, "drift: 1 file(s)") &&
		!strings.Contains(driftR.Stdout, "drift: 2 file(s)") {
		// Allow 1 or N — the regression guard is "non-zero", not the exact count.
		if !containsDriftCount(driftR.Stdout) {
			t.Errorf("drift stdout missing non-zero file-count summary: %q", driftR.Stdout)
		}
	}

	// STEP 5: use --on-drift=persist saves edited bytes back to profile dir.
	persistR := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", projectRoot, "--on-drift=persist", "use", "devmode"},
	})
	if persistR.ExitCode != 0 {
		t.Fatalf("use --on-drift=persist: exit=%d stderr=%q", persistR.ExitCode, persistR.Stderr)
	}
	persistedProfileBody, err := os.ReadFile(profileClaudeMd)
	if err != nil {
		t.Fatalf("read profile body after persist: %v", err)
	}
	if !strings.Contains(string(persistedProfileBody), "Use strict types AND prefer immutable data.") {
		t.Errorf("profile body missing edited line: %q", string(persistedProfileBody))
	}
	if strings.Contains(string(persistedProfileBody), "Use strict types.\n") {
		t.Errorf("profile body still contains pre-edit line: %q", string(persistedProfileBody))
	}

	// STEP 6: another use devmode is byte-identical to edited file (round-trip).
	useR2 := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", projectRoot, "use", "devmode"}})
	if useR2.ExitCode != 0 {
		t.Fatalf("use devmode (round-trip): exit=%d stderr=%q", useR2.ExitCode, useR2.Stderr)
	}
	afterRoundTrip, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read root after round-trip: %v", err)
	}
	if string(afterRoundTrip) != string(editedSnapshot) {
		t.Errorf("round-trip not byte-identical:\nwant=%q\ngot =%q", string(editedSnapshot), string(afterRoundTrip))
	}
}

// containsDriftCount loosely matches "drift: N file(s)" for any N>=1.
func containsDriftCount(s string) bool {
	idx := strings.Index(s, "drift: ")
	if idx < 0 {
		return false
	}
	rest := s[idx+len("drift: "):]
	end := strings.Index(rest, " file(s)")
	if end <= 0 {
		return false
	}
	num := rest[:end]
	if num == "0" {
		return false
	}
	for _, c := range num {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

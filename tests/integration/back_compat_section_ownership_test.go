// Package integration_test — IV/T4 translation of TS
// back-compat-section-ownership.test.ts.
//
// AC-10 silent-majority invariant: profiles laid out the v1 way (only
// .claude/CLAUDE.md, no profile-root CLAUDE.md) materialize EXACTLY as before
// section-ownership shipped. Asserted at the spawn boundary the user sees.
//
// Coverage:
//   BC-1: only .claude/CLAUDE.md → project-root CLAUDE.md byte-identical.
//   BC-2: only profile-root CLAUDE.md → .claude/CLAUDE.md NOT written.
//   BC-3: both → independent destinations, no cross-leak.
//   BC-4: legacy + init → markers added; later use of non-root profile
//         leaves project-root file unchanged from init's output.
package integration_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// BC-1: profile with only .claude/CLAUDE.md leaves project-root byte-identical.
func TestBackCompat_BC1_OnlyClaudeMd_RootUntouched(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"legacy": {
				Manifest: map[string]any{"name": "legacy"},
				Files:    map[string]string{"CLAUDE.md": "LEGACY-CLAUDE-CONTENT\n"},
			},
		},
	})
	rootClaudeMd := filepath.Join(fx.ProjectRoot, "CLAUDE.md")
	original := "# My project\n\nUser-authored content. No markers.\n"
	if err := os.WriteFile(rootClaudeMd, []byte(original), 0o644); err != nil {
		t.Fatalf("write root CLAUDE.md: %v", err)
	}
	originalStat, err := os.Stat(rootClaudeMd)
	if err != nil {
		t.Fatalf("stat root: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "legacy"}})
	if r.ExitCode != 0 {
		t.Fatalf("use legacy: exit=%d stderr=%q", r.ExitCode, r.Stderr)
	}

	after, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read root after: %v", err)
	}
	if string(after) != original {
		t.Errorf("root CLAUDE.md changed: want %q got %q", original, string(after))
	}
	afterStat, err := os.Stat(rootClaudeMd)
	if err != nil {
		t.Fatalf("stat root after: %v", err)
	}
	if !afterStat.ModTime().Equal(originalStat.ModTime()) {
		t.Errorf("root mtime changed: before=%v after=%v", originalStat.ModTime(), afterStat.ModTime())
	}

	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "LEGACY-CLAUDE-CONTENT\n" {
		t.Errorf("live CLAUDE.md want LEGACY-CLAUDE-CONTENT got %q", string(live))
	}

	stateBytes, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st struct {
		RootClaudeMdSection any `json:"rootClaudeMdSection"`
	}
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if st.RootClaudeMdSection != nil {
		t.Errorf("rootClaudeMdSection want null/absent, got %v", st.RootClaudeMdSection)
	}
}

// BC-2: profile with only profile-root CLAUDE.md does NOT write .claude/CLAUDE.md.
func TestBackCompat_BC2_OnlyProfileRootClaudeMd_NoLiveClaudeMd(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})

	initR := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook", "--no-seed"},
	})
	if initR.ExitCode != 0 {
		t.Fatalf("init: exit=%d stderr=%q", initR.ExitCode, initR.Stderr)
	}

	// Stand up the profile by hand AFTER init: manifest + profile-root
	// CLAUDE.md only, NO .claude/ subdir.
	profileDir := filepath.Join(fx.ProjectRoot, ".claude-profiles", "rooted")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	mfBytes, _ := json.MarshalIndent(map[string]any{"name": "rooted"}, "", "  ")
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), mfBytes, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "CLAUDE.md"), []byte("ROOT-MANAGED-BODY\n"), 0o644); err != nil {
		t.Fatalf("write profile-root CLAUDE.md: %v", err)
	}

	useR := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "rooted"}})
	if useR.ExitCode != 0 {
		t.Fatalf("use rooted: exit=%d stderr=%q", useR.ExitCode, useR.Stderr)
	}

	rootContent, err := os.ReadFile(filepath.Join(fx.ProjectRoot, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	for _, want := range []string{"<!-- c3p:v1:begin", "<!-- c3p:v1:end", "ROOT-MANAGED-BODY"} {
		if !strings.Contains(string(rootContent), want) {
			t.Errorf("root CLAUDE.md missing %q: %q", want, string(rootContent))
		}
	}

	// CRITICAL: .claude/CLAUDE.md was NOT written.
	claudeMdLive := filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md")
	if _, err := os.Stat(claudeMdLive); err == nil {
		t.Errorf(".claude/CLAUDE.md should not exist; created at %s", claudeMdLive)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat .claude/CLAUDE.md: unexpected error %v", err)
	}

	// No leak: nothing under .claude/ named CLAUDE.md.
	entries, err := os.ReadDir(filepath.Join(fx.ProjectRoot, ".claude"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readdir .claude: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "CLAUDE.md" {
			t.Errorf(".claude/CLAUDE.md present in dir listing — leak")
		}
	}
}

// BC-3: profile with both .claude/CLAUDE.md AND profile-root CLAUDE.md writes
// both independently — no content leak between destinations.
func TestBackCompat_BC3_BothDestinations_NoLeak(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})

	initR := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook", "--no-seed"},
	})
	if initR.ExitCode != 0 {
		t.Fatalf("init: exit=%d stderr=%q", initR.ExitCode, initR.Stderr)
	}

	profileDir := filepath.Join(fx.ProjectRoot, ".claude-profiles", "both")
	if err := os.MkdirAll(filepath.Join(profileDir, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir profile/.claude: %v", err)
	}
	mfBytes, _ := json.MarshalIndent(map[string]any{"name": "both"}, "", "  ")
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), mfBytes, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "CLAUDE.md"), []byte("PROJECT-ROOT-BODY\n"), 0o644); err != nil {
		t.Fatalf("write profile-root CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, ".claude", "CLAUDE.md"), []byte("CLAUDE-DIR-BODY\n"), 0o644); err != nil {
		t.Fatalf("write .claude/CLAUDE.md: %v", err)
	}

	useR := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "both"}})
	if useR.ExitCode != 0 {
		t.Fatalf("use both: exit=%d stderr=%q", useR.ExitCode, useR.Stderr)
	}

	claudeContent, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read .claude/CLAUDE.md: %v", err)
	}
	if string(claudeContent) != "CLAUDE-DIR-BODY\n" {
		t.Errorf(".claude/CLAUDE.md want CLAUDE-DIR-BODY got %q", string(claudeContent))
	}
	if strings.Contains(string(claudeContent), "PROJECT-ROOT-BODY") {
		t.Errorf("leak: PROJECT-ROOT-BODY in .claude/CLAUDE.md: %q", string(claudeContent))
	}

	rootContent, err := os.ReadFile(filepath.Join(fx.ProjectRoot, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if !strings.Contains(string(rootContent), "<!-- c3p:v1:begin") {
		t.Errorf("root missing begin marker: %q", string(rootContent))
	}
	if !strings.Contains(string(rootContent), "PROJECT-ROOT-BODY") {
		t.Errorf("root missing PROJECT-ROOT-BODY: %q", string(rootContent))
	}
	if strings.Contains(string(rootContent), "CLAUDE-DIR-BODY") {
		t.Errorf("leak: CLAUDE-DIR-BODY in root CLAUDE.md: %q", string(rootContent))
	}

	stateBytes, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st struct {
		Fingerprint struct {
			Files map[string]any `json:"files"`
		} `json:"fingerprint"`
		RootClaudeMdSection *struct {
			Size int `json:"size"`
		} `json:"rootClaudeMdSection"`
	}
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if _, ok := st.Fingerprint.Files["CLAUDE.md"]; !ok {
		t.Errorf("state.fingerprint.files missing CLAUDE.md entry: %+v", st.Fingerprint.Files)
	}
	if st.RootClaudeMdSection == nil {
		t.Errorf("state.rootClaudeMdSection want non-null")
	} else if st.RootClaudeMdSection.Size <= 0 {
		t.Errorf("state.rootClaudeMdSection.size want >0 got %d", st.RootClaudeMdSection.Size)
	}
}

// BC-4: legacy project + init injects markers; subsequent use of a profile
// with no projectRoot contribution preserves init's output byte-for-byte.
func TestBackCompat_BC4_LegacyInitThenLegacyUse_RootPreserved(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})

	initR := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--no-hook", "--no-seed"},
	})
	if initR.ExitCode != 0 {
		t.Fatalf("init: exit=%d stderr=%q", initR.ExitCode, initR.Stderr)
	}

	profileDir := filepath.Join(fx.ProjectRoot, ".claude-profiles", "legacy")
	if err := os.MkdirAll(filepath.Join(profileDir, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir profile/.claude: %v", err)
	}
	mfBytes, _ := json.MarshalIndent(map[string]any{"name": "legacy"}, "", "  ")
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), mfBytes, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, ".claude", "settings.json"), []byte(`{"v":"legacy"}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	rootClaudeMd := filepath.Join(fx.ProjectRoot, "CLAUDE.md")
	afterInit, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read root after init: %v", err)
	}
	for _, want := range []string{"<!-- c3p:v1:begin", "<!-- c3p:v1:end"} {
		if !strings.Contains(string(afterInit), want) {
			t.Errorf("after init missing marker %q: %q", want, string(afterInit))
		}
	}

	useR := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", "legacy"}})
	if useR.ExitCode != 0 {
		t.Fatalf("use legacy: exit=%d stderr=%q", useR.ExitCode, useR.Stderr)
	}

	afterUse, err := os.ReadFile(rootClaudeMd)
	if err != nil {
		t.Fatalf("read root after use: %v", err)
	}
	if string(afterUse) != string(afterInit) {
		t.Errorf("root CLAUDE.md changed during use:\nbefore=%q\nafter=%q", string(afterInit), string(afterUse))
	}

	settingsBytes, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if settings["v"] != "legacy" {
		t.Errorf("settings.v want legacy got %v", settings["v"])
	}

	stateBytes, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st struct {
		RootClaudeMdSection any `json:"rootClaudeMdSection"`
	}
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if st.RootClaudeMdSection != nil {
		t.Errorf("rootClaudeMdSection want null/absent, got %v", st.RootClaudeMdSection)
	}
}

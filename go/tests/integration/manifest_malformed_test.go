package integration_test

import (
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestManifestMalformed_* — IV/T7 translation of TS manifest-malformed.test.ts
// (PR6 #6, F2). Pinned to the Go bin's exit codes:
//   - JSON parse errors / wrong-type-field errors: exit 1 (InvalidManifest);
//     the TS bin used exit 3, but the Go classification is "user error"
//     because the manifest was authored wrong — recoverable by editing.
//   - PR16a path-traversal: exit 3 (CONFLICT-class, the spec promise).
//   - Missing-include via tilde resolving outside project: exit 3.
//   - Unknown manifest field: R36 warning, exit 0.

func TestManifestMalformed_InvalidJsonSyntax(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: "{not: valid json", Files: map[string]string{"x.md": "x\n"}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("invalid json: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stderr), "invalid") {
		t.Errorf("stderr missing 'invalid': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "profile.json") {
		t.Errorf("stderr missing 'profile.json' path: %q", r.Stderr)
	}
}

func TestManifestMalformed_TopLevelArray(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: "[]", Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("top-level array: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stderr), "object") {
		t.Errorf("stderr missing 'object': %q", r.Stderr)
	}
}

func TestManifestMalformed_TopLevelNull(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: "null", Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("top-level null: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

func TestManifestMalformed_TopLevelScalar(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: "42", Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("top-level scalar: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

func TestManifestMalformed_NameNotString(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: map[string]any{"name": 42}, Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("name=42: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "name") {
		t.Errorf("stderr missing 'name': %q", r.Stderr)
	}
}

func TestManifestMalformed_ExtendsArray(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: map[string]any{"extends": []string{"a"}}, Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("extends=array: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "extends") {
		t.Errorf("stderr missing 'extends': %q", r.Stderr)
	}
}

func TestManifestMalformed_IncludesNonString(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: map[string]any{"includes": []any{"valid", 42}}, Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("includes non-string: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "includes") {
		t.Errorf("stderr missing 'includes': %q", r.Stderr)
	}
}

func TestManifestMalformed_TagsNonString(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"bad": {Manifest: map[string]any{"tags": []any{"good", 42}}, Files: map[string]string{}},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "bad"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("tags non-string: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "tags") {
		t.Errorf("stderr missing 'tags': %q", r.Stderr)
	}
}

// PR16a: path-traversal is a CONFLICT-class hard reject (exit 3) in the
// Go bin — upgraded from the TS bin's MissingInclude behaviour.
func TestManifestMalformed_PathTraversal(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"traversal": {
				Manifest: map[string]any{"name": "traversal", "includes": []string{"../../../.ssh/config"}},
				Files:    map[string]string{"x.md": "x\n"},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "traversal"},
	})
	if r.ExitCode != 3 {
		t.Fatalf("path traversal: want 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "../../../.ssh/config") {
		t.Errorf("stderr missing offending raw path: %q", r.Stderr)
	}
}

func TestManifestMalformed_TildeMissingInclude(t *testing.T) {
	// Tilde-form resolves to $HOME; pointing at a guaranteed-nonexistent
	// path under $HOME produces MissingInclude (exit 3) with raw "~/..."
	// preserved. We override $HOME to a fresh t.TempDir() so the test
	// doesn't accidentally pass / fail based on whatever the runner's
	// real homedir happens to contain — without this, a developer with a
	// directory named "c3p-test-nonexistent-dir-xyzzy" in $HOME would
	// silently flip the assertion.
	helpers.EnsureBuilt(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"outside": {
				Manifest: map[string]any{
					"name":     "outside",
					"includes": []string{"~/c3p-test-nonexistent-dir-xyzzy"},
				},
				Files: map[string]string{"x.md": "x\n"},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "outside"},
	})
	if r.ExitCode != 3 {
		t.Fatalf("tilde missing-include: want 3, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "c3p-test-nonexistent-dir-xyzzy") {
		t.Errorf("stderr missing original include name: %q", r.Stderr)
	}
}

func TestManifestMalformed_UnknownFieldR36(t *testing.T) {
	// R36: unknown manifest field is a degraded-but-keep-going warning.
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"with_unknown": {
				Manifest: map[string]any{"name": "with_unknown", "futureField": "xyz"},
				Files:    map[string]string{"x.md": "x\n"},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "with_unknown"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("unknown field: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

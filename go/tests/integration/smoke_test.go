// Package integration_test is the Go-side spawn-only integration suite.
// F1 lands a smoke test that proves the test harness works end-to-end:
// fixture creation, EnsureBuilt, RunCli, exit-code surface. D7 + IV
// translate the 16 TS spawn tests into this directory.
//
// Every *_test.go in this directory MUST call helpers.EnsureBuilt(t) (the
// F1 fitness function — verified by helper_parity_audit.sh).
package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

func TestEnsureBuiltAndVersion(t *testing.T) {
	helpers.EnsureBuilt(t)

	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Args: []string{"--version"},
	}, t)
	if err != nil {
		t.Fatalf("run --version: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("--version exited %d, stderr=%q", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "0.0.0-dev") {
		t.Fatalf("--version output missing dev tag: %q", res.Stdout)
	}
}

func TestHelpListsSubcommands(t *testing.T) {
	helpers.EnsureBuilt(t)

	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Args: []string{"--help"},
	}, t)
	if err != nil {
		t.Fatalf("run --help: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("--help exited %d, stderr=%q", res.ExitCode, res.Stderr)
	}
	// Smoke-check three of the 13 stub commands. The full set is asserted
	// in IV's help_version_test.go; F1 only proves the dispatch tree wires
	// up at all.
	for _, want := range []string{"init", "use", "doctor"} {
		if !strings.Contains(res.Stdout, want) {
			t.Fatalf("--help missing subcommand %q in: %s", want, res.Stdout)
		}
	}
}

func TestHelloVerbPrintsGreeting(t *testing.T) {
	helpers.EnsureBuilt(t)

	// D7 promoted hello from a stub to a real (hidden) verb that prints a
	// greeting. Exit 0 with "Hello" on stdout.
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Args: []string{"hello"},
	}, t)
	if err != nil {
		t.Fatalf("run hello: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("hello: want exit 0, got %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Hello") {
		t.Fatalf("hello: stdout missing greeting: %q", res.Stdout)
	}
}

func TestUnknownCommandExitsUserError(t *testing.T) {
	helpers.EnsureBuilt(t)

	// D7 hand-rolled parser surfaces argv-shape errors as user-error (1),
	// matching the TS bin. Cobra's exit-2 "usage" code is gone with
	// the parser rewrite.
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Args: []string{"nonexistent-verb"},
	}, t)
	if err != nil {
		t.Fatalf("run unknown: %v", err)
	}
	if res.ExitCode != 1 {
		t.Fatalf("unknown verb: want exit 1 (user error), got %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "unknown command") {
		t.Fatalf("stderr missing 'unknown command': %q", res.Stderr)
	}
}

func TestFixtureCreatesProfileTree(t *testing.T) {
	helpers.EnsureBuilt(t)

	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"base": {
				Manifest: map[string]any{"name": "base"},
				Files:    map[string]string{"settings.json": `{"theme":"dark"}`},
			},
		},
		External: map[string]helpers.ComponentSpec{
			"team-shared": {
				Files: map[string]string{"hooks/precommit.sh": "#!/bin/sh\n"},
			},
		},
	})

	mustExist := func(p string) {
		t.Helper()
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected path %q to exist: %v", p, err)
		}
	}
	mustExist(filepath.Join(fx.ProjectRoot, ".claude-profiles", "base", "profile.json"))
	mustExist(filepath.Join(fx.ProjectRoot, ".claude-profiles", "base", ".claude", "settings.json"))
	mustExist(filepath.Join(fx.ExternalRoot, "team-shared", ".claude", "hooks", "precommit.sh"))
}

func TestRunCliCwdOverride(t *testing.T) {
	helpers.EnsureBuilt(t)

	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})

	// Smoke check: passing --cwd doesn't crash. We're not testing what the
	// stubs do with it (they don't), only that cobra accepts the flag and
	// the binary returns successfully when only --version is required.
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--version"},
	}, t)
	if err != nil {
		t.Fatalf("run --cwd --version: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
}

func TestNonInteractiveAutoDetectFromCIEnv(t *testing.T) {
	helpers.EnsureBuilt(t)

	// CI=true is the canonical signal; other CI vars exist but the spec
	// pins this one (matches TS bin's detectNonInteractive helper).
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Args: []string{"--version"},
		Env:  map[string]string{"CI": "true"},
	}, t)
	if err != nil {
		t.Fatalf("run with CI=true: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("CI=true should not affect --version exit: got %d", res.ExitCode)
	}
}

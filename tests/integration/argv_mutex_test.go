package integration_test

import (
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/tests/integration/helpers"
)

// TestArgvMutex_* — IV/T7 translation of TS argv-mutex.test.ts (PR6 #9, F2).
// Exhaustive mutex + parse-error coverage. Pinned to the Go bin's actual
// stderr text (Go uses lowercase "c3p:" prefix and slightly different
// wording from TS; the Go-side helpers/parser is the source of truth).

// ── Documented mutex pair: --quiet × --json ────────────────────────────

func TestArgvMutex_QuietJsonBothOrders(t *testing.T) {
	helpers.EnsureBuilt(t)
	r1 := mustRun(t, helpers.SpawnOptions{Args: []string{"--quiet", "--json", "status"}})
	if r1.ExitCode != 1 {
		t.Fatalf("--quiet --json: want 1, got %d (stderr=%q)", r1.ExitCode, r1.Stderr)
	}
	if !strings.Contains(r1.Stderr, "mutually exclusive") {
		t.Errorf("stderr missing 'mutually exclusive': %q", r1.Stderr)
	}
	r2 := mustRun(t, helpers.SpawnOptions{Args: []string{"--json", "--quiet", "status"}})
	if r2.ExitCode != 1 {
		t.Fatalf("--json --quiet: want 1, got %d (stderr=%q)", r2.ExitCode, r2.Stderr)
	}
	if !strings.Contains(r2.Stderr, "mutually exclusive") {
		t.Errorf("stderr missing 'mutually exclusive': %q", r2.Stderr)
	}
}

func TestArgvMutex_ShortQuietJson(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"-q", "--json", "status"}})
	if r.ExitCode != 1 {
		t.Fatalf("-q --json: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "mutually exclusive") {
		t.Errorf("stderr missing 'mutually exclusive': %q", r.Stderr)
	}
}

// ── Bad flag values ─────────────────────────────────────────────────────

func TestArgvMutex_OnDriftMissingValue(t *testing.T) {
	helpers.EnsureBuilt(t)
	// Go parser consumes the next token as value, so "--on-drift use a"
	// binds value="use" and then rejects. Either way exit 1 + names flag.
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--on-drift", "use", "a"}})
	if r.ExitCode != 1 {
		t.Fatalf("--on-drift no value: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--on-drift") {
		t.Errorf("stderr missing --on-drift: %q", r.Stderr)
	}
}

func TestArgvMutex_OnDriftInvalidValue(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--on-drift=ignore", "status"}})
	if r.ExitCode != 1 {
		t.Fatalf("--on-drift=ignore: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--on-drift") {
		t.Errorf("stderr missing --on-drift: %q", r.Stderr)
	}
	// Go bin emits "discard|persist|abort" pipe-separated.
	if !strings.Contains(r.Stderr, "discard") || !strings.Contains(r.Stderr, "persist") || !strings.Contains(r.Stderr, "abort") {
		t.Errorf("stderr missing valid value list: %q", r.Stderr)
	}
}

func TestArgvMutex_CwdEndOfArgv(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"status", "--cwd"}})
	if r.ExitCode != 1 {
		t.Fatalf("--cwd at end: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--cwd") {
		t.Errorf("stderr missing --cwd: %q", r.Stderr)
	}
}

func TestArgvMutex_CwdEmptyValue(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd=", "status"}})
	if r.ExitCode != 1 {
		t.Fatalf("--cwd=: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--cwd") {
		t.Errorf("stderr missing --cwd: %q", r.Stderr)
	}
}

func TestArgvMutex_WaitNonNumeric(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--wait=banana", "status"}})
	if r.ExitCode != 1 {
		t.Fatalf("--wait=banana: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--wait") {
		t.Errorf("stderr missing --wait: %q", r.Stderr)
	}
}

func TestArgvMutex_WaitNegative(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--wait=-5", "status"}})
	if r.ExitCode != 1 {
		t.Fatalf("--wait=-5: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--wait") {
		t.Errorf("stderr missing --wait: %q", r.Stderr)
	}
}

// ── Unknown flags / verbs ───────────────────────────────────────────────

func TestArgvMutex_UnknownGlobalFlag(t *testing.T) {
	// Go bin's parser surfaces --foo as an unknown command.
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--foo", "status"}})
	if r.ExitCode != 1 {
		t.Fatalf("--foo: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--foo") {
		t.Errorf("stderr missing --foo: %q", r.Stderr)
	}
}

func TestArgvMutex_InitUnknownFlag(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "init", "--bogus"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("init --bogus: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--bogus") {
		t.Errorf("stderr missing --bogus: %q", r.Stderr)
	}
}

func TestArgvMutex_DriftUnknownFlag(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "drift", "--bogus"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("drift --bogus: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--bogus") {
		t.Errorf("stderr missing --bogus: %q", r.Stderr)
	}
}

func TestArgvMutex_DiffUnknownFlag(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{})
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "diff", "a", "b", "--bogus"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("diff --bogus: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--bogus") {
		t.Errorf("stderr missing --bogus: %q", r.Stderr)
	}
}

func TestArgvMutex_UnknownVerb(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"bogus"}})
	if r.ExitCode != 1 {
		t.Fatalf("bogus: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "bogus") {
		t.Errorf("stderr missing 'bogus': %q", r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stderr), "unknown command") {
		t.Errorf("stderr missing 'unknown command': %q", r.Stderr)
	}
}

func TestArgvMutex_MissingArgv(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{}})
	if r.ExitCode != 1 {
		t.Fatalf("empty argv: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	low := strings.ToLower(r.Stderr)
	if !(strings.Contains(low, "missing") || strings.Contains(low, "usage") || strings.Contains(low, "command")) {
		t.Errorf("stderr missing /missing|usage|command/: %q", r.Stderr)
	}
}

// ── Argless verbs reject extra positionals ──────────────────────────────

func TestArgvMutex_StatusExtraPositional(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"status", "extra"}})
	if r.ExitCode != 1 {
		t.Fatalf("status extra: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "status") {
		t.Errorf("stderr missing 'status': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "takes no arguments") {
		t.Errorf("stderr missing 'takes no arguments': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, `"extra"`) {
		t.Errorf("stderr missing quoted 'extra': %q", r.Stderr)
	}
}

func TestArgvMutex_SyncExtraPositional(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"sync", "extra"}})
	if r.ExitCode != 1 {
		t.Fatalf("sync extra: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "sync") {
		t.Errorf("stderr missing 'sync': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "takes no arguments") {
		t.Errorf("stderr missing 'takes no arguments': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, `"extra"`) {
		t.Errorf("stderr missing quoted 'extra': %q", r.Stderr)
	}
}

func TestArgvMutex_ListExtraPositional(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"list", "extra"}})
	if r.ExitCode != 1 {
		t.Fatalf("list extra: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "list") {
		t.Errorf("stderr missing 'list': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "takes no arguments") {
		t.Errorf("stderr missing 'takes no arguments': %q", r.Stderr)
	}
	if !strings.Contains(r.Stderr, `"extra"`) {
		t.Errorf("stderr missing quoted 'extra': %q", r.Stderr)
	}
}

// ── Verbs requiring a positional reject when missing ────────────────────

func TestArgvMutex_UseNoProfile(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"use"}})
	if r.ExitCode != 1 {
		t.Fatalf("use (no name): want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "use") {
		t.Errorf("stderr missing 'use': %q", r.Stderr)
	}
}

func TestArgvMutex_DiffNoPositional(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"diff"}})
	if r.ExitCode != 1 {
		t.Fatalf("diff (no args): want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "diff") {
		t.Errorf("stderr missing 'diff': %q", r.Stderr)
	}
}

func TestArgvMutex_DiffTooManyPositionals(t *testing.T) {
	helpers.EnsureBuilt(t)
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"diff", "a", "b", "c"}})
	if r.ExitCode != 1 {
		t.Fatalf("diff a b c: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "diff") {
		t.Errorf("stderr missing 'diff': %q", r.Stderr)
	}
}

// ── Help on unknown verb is robust (no crash) ───────────────────────────

func TestArgvMutex_HelpUnknownVerb(t *testing.T) {
	helpers.EnsureBuilt(t)
	// Go bin prints a friendly "no specific help for X" then the global
	// help text and exits 0. Pinned exactly so a regression to "die with
	// exit 1 on unknown verb" surfaces here, not silently.
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"help", "bogus-verb"}})
	if r.ExitCode != 0 {
		t.Fatalf("help bogus-verb: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

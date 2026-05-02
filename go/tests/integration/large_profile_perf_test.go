package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV/T8 — translation of TS large-profile-perf.test.ts (F2 gap closure
// #11). The full R38 perf gate is already pinned in perf_test.go::
// TestLargeProfile (which builds the 1000-file fixture, asserts the time
// budget, and spot-checks materialized contents). This file covers the
// *additional* surface the TS test exercised:
//
//   1. `c3p status` against the same 1000-file fixture is also fast.
//   2. `c3p status --json` (machine-readable) on the same fixture is also
//      fast — pins the contract that --json formatting is not a perf cliff.
//
// Both reuse buildLargeProfileFixture from perf_test.go so the fixture
// shape stays in lockstep across all R38 tests.

// largeProfileSecondaryBudget is the budget for the secondary perf cells
// (status read-only). Read-only paths are strictly faster than full
// `c3p use` (no materialize, no fsync), so we hold them to the same
// budget the primary R38 gate uses — generous enough for noisy runners,
// tight enough to flag a regression that pushes status into write-path
// territory. Mirrors largeProfileBudgetCI from perf_test.go.
const largeProfileSecondaryBudget = 7 * time.Second

// TestLargeProfilePerf_StatusOnLargeProfile — `c3p status` against the
// 1000-file fixture finishes within the same budget the primary perf gate
// uses. Status is read-only (no materialize), so this is a noisier-but-
// still-meaningful regression sentinel: a status that suddenly takes 7+
// seconds on 1000 files indicates the fingerprint compare is doing
// per-file work it shouldn't.
func TestLargeProfilePerf_StatusOnLargeProfile(t *testing.T) {
	if isWindows() {
		t.Skip("R38 perf gate is platform-class; Windows fsync latency exceeds budget — covered by linux/darwin")
	}
	helpers.EnsureBuilt(t)
	root := buildLargeProfileFixture(t)

	// Materialize first so status has a fingerprint to compare against.
	useRes, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Cwd:       root,
		Args:      []string{"use", "big", "--non-interactive"},
		TimeoutMs: int(largeProfileSecondaryBudget * 2 / time.Millisecond),
	}, t)
	if err != nil {
		t.Fatalf("use big setup: %v (stderr=%q)", err, useRes.Stderr)
	}
	if useRes.ExitCode != 0 {
		t.Fatalf("use big setup: exit %d (stderr=%q)", useRes.ExitCode, useRes.Stderr)
	}

	t0 := time.Now()
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Cwd:       root,
		Args:      []string{"status"},
		TimeoutMs: int(largeProfileSecondaryBudget * 2 / time.Millisecond),
	}, t)
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("status: %v (stderr=%q)", err, res.Stderr)
	}
	t.Logf("c3p status (1000-file profile): %v budget=%v exit=%d", elapsed, largeProfileSecondaryBudget, res.ExitCode)
	if res.ExitCode != 0 {
		t.Fatalf("status: exit %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if elapsed > largeProfileSecondaryBudget {
		t.Fatalf("status regressed: %v exceeds budget %v (stdout=%q)", elapsed, largeProfileSecondaryBudget, res.Stdout)
	}
	if !strings.Contains(res.Stdout, "big") {
		t.Errorf("status stdout missing 'big': %q", res.Stdout)
	}
}

// TestLargeProfilePerf_StatusJsonOnLargeProfile — same fixture, but with
// --json. Pins the contract that the JSON envelope is not a perf cliff
// (a naive impl that re-encodes the fingerprint per-file would regress
// here long before the human-readable text path showed any signal).
func TestLargeProfilePerf_StatusJsonOnLargeProfile(t *testing.T) {
	if isWindows() {
		t.Skip("R38 perf gate is platform-class; Windows fsync latency exceeds budget — covered by linux/darwin")
	}
	helpers.EnsureBuilt(t)
	root := buildLargeProfileFixture(t)

	useRes, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Cwd:       root,
		Args:      []string{"use", "big", "--non-interactive"},
		TimeoutMs: int(largeProfileSecondaryBudget * 2 / time.Millisecond),
	}, t)
	if err != nil || useRes.ExitCode != 0 {
		t.Fatalf("use big setup: err=%v exit=%d stderr=%q", err, useRes.ExitCode, useRes.Stderr)
	}

	t0 := time.Now()
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Cwd:       root,
		Args:      []string{"--json", "status"},
		TimeoutMs: int(largeProfileSecondaryBudget * 2 / time.Millisecond),
	}, t)
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("status --json: %v (stderr=%q)", err, res.Stderr)
	}
	t.Logf("c3p status --json (1000-file profile): %v budget=%v exit=%d", elapsed, largeProfileSecondaryBudget, res.ExitCode)
	if res.ExitCode != 0 {
		t.Fatalf("status --json: exit %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if elapsed > largeProfileSecondaryBudget {
		t.Fatalf("status --json regressed: %v exceeds budget %v (stdout=%q)", elapsed, largeProfileSecondaryBudget, res.Stdout)
	}
	// Spot-check the JSON envelope mentions the active profile so we know
	// we measured a real status, not an early-exit error path.
	if !strings.Contains(res.Stdout, "big") {
		t.Errorf("status --json stdout missing 'big': %q", res.Stdout)
	}
}

// W3 (epic claude-code-profiles-94o) — performance gates.
//
// Three contract tests live in this file:
//
//   - TestColdStart   (PR18): spawn `c3p --version` 10 times, discard min
//     and max samples, assert the mean of the middle 8 ≤ 25 ms on developer
//     hardware OR ≤ 50 ms on CI runners. The platform-class threshold lets
//     the gate fire on CI despite noisy-neighbour variance, while still
//     catching real regressions on dev laptops. Gated behind
//     C3P_PERF_COLDSTART=1 — runs nightly via .github/workflows/perf.yml,
//     not on every PR. See the test's docstring for the rationale.
//
//   - TestBinarySize  (PR19): build a stripped, CGO-disabled binary and
//     assert the on-disk size ≤ 15 MB. This is the same build mode W2 ships
//     to release artifacts; growing the user-visible binary past 15 MB is
//     a P0 finding regardless of why.
//
//   - TestLargeProfile (R38, PR6 gap closure #11): generate a 1000-file
//     synthetic profile and assert `c3p use` completes within 5 s on a CI
//     runner. The fixture is built procedurally (not via MakeFixture) — at
//     1000 files the per-file content map allocations dominate cost, and we
//     are testing the materializer, not the fixture helper.
//
// All three must pass `helpers.EnsureBuilt(t)` before the first spawn so the
// shared test binary is on disk; the cold-start and binary-size tests build
// their *own* stripped binary because the EnsureBuilt artifact is not
// stripped (its symbol table inflates the size and slightly perturbs cold
// start).
package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// coldStartSamples is the sample count from PR18. Discarding min and max
// leaves an even number (8) of remaining samples — keeping the count round
// makes the "mean of middle 8" methodology read cleanly in CI logs.
const coldStartSamples = 10

// coldStartBudgetDev is the 25 ms cap on developer-class hardware. A
// regression here is a P0 finding even when CI passes (R8 risk closure).
const coldStartBudgetDev = 25 * time.Millisecond

// coldStartBudgetCI is the 50 ms cap on GitHub-hosted runners. CI runners
// see noisy-neighbour load (shared kernel, oversubscribed I/O) that can
// double an otherwise-fast spawn. Holding the dev cap on CI would flake;
// relaxing the CI cap below 50 ms would re-introduce the flake.
const coldStartBudgetCI = 50 * time.Millisecond

// binarySizeCapBytes is PR19's stripped on-disk cap (15 MB). We measure the
// stripped CGO-disabled binary because that is what users install.
const binarySizeCapBytes int64 = 15 * 1024 * 1024

// largeProfileBudget is R38 — `c3p use` on a 1000-file synthetic profile
// must complete within 5 s on a CI runner. Go's syscall-direct materialize
// holds this budget where the TS implementation (which used 10 s on CI)
// could not.
const largeProfileBudget = 5 * time.Second

// largeProfileFiles is the synthetic profile's file count. 1000 files is
// the spec's stated stress target (port spec §3.6, advisory P1-7).
const largeProfileFiles = 1000

// isCI reports whether the test is running on a CI runner. GitHub Actions
// sets CI=true; we trust that signal alone — checking GITHUB_ACTIONS too
// would let a misconfigured local environment slip into the relaxed
// budget. CI=true is the canonical convention across providers.
func isCI() bool {
	return os.Getenv("CI") == "true"
}

// TestColdStart implements PR18. We build a stripped binary, spawn
// `c3p --version` 10 times, sort the wall-clock durations, drop the min
// and max, and assert the mean of the remaining 8 samples is within
// budget. Mean-of-middle-8 is immune to a single slow spawn (e.g. cold
// page cache on the first run) and a single fast spawn (e.g. unrealistic
// best-case noise floor).
//
// Gated behind C3P_PERF_COLDSTART=1: per the W3 fitness functions, cold
// start runs nightly + on perf-impacting paths, not on every PR. Spawn
// timings on shared CI runners have enough variance that running on
// every PR would either flake (with a tight budget) or be useless (with
// a loose budget). The nightly workflow sets the env var so the gate
// fires once per day; PRs touching perf-impacting code can opt in by
// setting the env var locally or in their CI override.
func TestColdStart(t *testing.T) {
	if os.Getenv("C3P_PERF_COLDSTART") != "1" {
		t.Skip("PR18 cold-start gate runs nightly only; set C3P_PERF_COLDSTART=1 to opt in")
	}
	helpers.EnsureBuilt(t)

	bin := buildStrippedBin(t)

	samples := make([]time.Duration, 0, coldStartSamples)
	for i := 0; i < coldStartSamples; i++ {
		t0 := time.Now()
		cmd := exec.Command(bin, "--version")
		if err := cmd.Run(); err != nil {
			t.Fatalf("sample %d: spawn failed: %v", i+1, err)
		}
		samples = append(samples, time.Since(t0))
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	// Drop samples[0] (min) and samples[len-1] (max). The remaining 8
	// form the mean-of-middle-8 reported by PR18.
	middle := samples[1 : len(samples)-1]
	var total time.Duration
	for _, s := range middle {
		total += s
	}
	mean := total / time.Duration(len(middle))

	budget := coldStartBudgetDev
	hw := "developer-class"
	if isCI() {
		budget = coldStartBudgetCI
		hw = "CI runner"
	}

	// Always log the trend so a regression watcher (R8) sees the full
	// distribution, not just the mean. Missing samples mean a future
	// flake investigation has nothing to compare against.
	t.Logf("cold start (%s): mean-of-middle-8=%v budget=%v all=%v",
		hw, mean, budget, samples)

	if mean > budget {
		t.Fatalf("cold start regressed: mean-of-middle-8=%v exceeds %s budget %v (samples=%v)",
			mean, hw, budget, samples)
	}
}

// TestBinarySize implements PR19. The build mode (-s -w + CGO_ENABLED=0)
// matches the W2 release pipeline byte-for-byte; a difference here would
// invalidate the assertion.
func TestBinarySize(t *testing.T) {
	// EnsureBuilt is the F1 fitness function — every test in this
	// directory calls it even when the cached artifact isn't directly
	// used, so a broken build harness fails every perf test cleanly
	// rather than silently passing the size check on a phantom binary.
	helpers.EnsureBuilt(t)

	bin := buildStrippedBin(t)

	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat %q: %v", bin, err)
	}
	size := info.Size()
	t.Logf("stripped binary size: %d bytes (%.2f MB) cap=%d bytes (%.2f MB)",
		size, float64(size)/(1024*1024),
		binarySizeCapBytes, float64(binarySizeCapBytes)/(1024*1024))

	if size > binarySizeCapBytes {
		t.Fatalf("binary size regressed: %d bytes exceeds cap %d bytes",
			size, binarySizeCapBytes)
	}
}

// TestLargeProfile implements R38 (gap closure #11). Mirrors
// tests/cli/integration/large-profile-perf.test.ts: build a 1000-file
// profile across 10 nested directories, run `c3p use big`, assert the
// wall-clock spawn duration ≤ 5 s. The fixture is fs-direct because
// MakeFixture's per-file content map allocates ~1000 string pairs at
// JS-object scale; doing the same in Go would still be wasteful when the
// content is index-derived.
//
// Skipped on Windows: the materializer issues a per-file fsync (R23a
// durability), and Windows CI runners' shared-disk fsync latency
// (typically 2–3× a Linux runner's) regressed past the 5 s budget on
// the TS implementation of this same gate (see tests/state/perf.test.ts
// for the prior art). The perf invariant is meaningful on the platforms
// most users develop on; correctness on Windows is validated through
// the rest of the matrix. R38 closure (this gate) is intentionally
// platform-class.
func TestLargeProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("R38 perf gate is platform-class; Windows fsync latency exceeds the 5s budget — validated on linux/darwin")
	}
	helpers.EnsureBuilt(t)

	projectRoot := buildLargeProfileFixture(t)

	// Hard timeout sits above the budget so the assertion fires before
	// RunCli's deadline, not at it. Otherwise a 5.001 s spawn surfaces
	// as a harness timeout error rather than a perf-budget violation.
	hardTimeout := largeProfileBudget * 2

	t0 := time.Now()
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Cwd:       projectRoot,
		Args:      []string{"use", "big"},
		TimeoutMs: int(hardTimeout / time.Millisecond),
	}, t)
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("c3p use big: harness error: %v (stderr=%q)", err, res.Stderr)
	}

	t.Logf("c3p use 1000-file profile: %v budget=%v exit=%d",
		elapsed, largeProfileBudget, res.ExitCode)

	if res.ExitCode != 0 {
		t.Fatalf("c3p use big: exit %d, stderr=%q", res.ExitCode, res.Stderr)
	}
	if elapsed > largeProfileBudget {
		t.Fatalf("large-profile use regressed: %v exceeds budget %v (stdout=%q)",
			elapsed, largeProfileBudget, res.Stdout)
	}

	// Sanity: the live tree got materialized. A test that passes the
	// time budget but didn't actually copy files would be a false green.
	liveFile := filepath.Join(projectRoot, ".claude", "d00", "f000.md")
	body, err := os.ReadFile(liveFile)
	if err != nil {
		t.Fatalf("read materialized file %q: %v", liveFile, err)
	}
	want := "# file 0/0\nbody\n"
	if string(body) != want {
		t.Fatalf("materialized file contents wrong: got %q want %q", body, want)
	}
}

// buildStrippedBin compiles cmd/c3p with -ldflags="-s -w" and
// CGO_ENABLED=0 — the same build mode the W2 release pipeline produces.
// The binary lives in t.TempDir() so the testing runtime cleans it up
// automatically. Build cost (~1 s) is paid per-test that calls this; we
// don't share with helpers.BinPath because that artifact is unstripped.
func buildStrippedBin(t *testing.T) string {
	t.Helper()
	module := findGoModuleRoot(t)
	out := filepath.Join(t.TempDir(), "c3p-stripped"+exeSuffixForOS())
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", out, "./cmd/c3p")
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build (stripped): %v (stderr=%s)", err, stderr.String())
	}
	return out
}

// findGoModuleRoot walks up from the test source file's directory to find
// go.mod. Tests can run from any cwd so os.Getwd() is unreliable.
func findGoModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from %s", filepath.Dir(file))
		}
		dir = parent
	}
}

func exeSuffixForOS() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// buildLargeProfileFixture writes a 1000-file profile to a fresh
// t.TempDir() and returns the project root. Layout: 10 directories
// (d00..d09) each containing 100 files (f000..f099.md). Index-derived
// content guarantees uniqueness so any de-dup in the materializer would
// surface as a content mismatch, not a silent pass.
func buildLargeProfileFixture(t *testing.T) string {
	t.Helper()
	projectRoot := filepath.Join(t.TempDir(), "project")
	profileBase := filepath.Join(projectRoot, ".claude-profiles", "big")
	profileClaudeDir := filepath.Join(profileBase, ".claude")
	if err := os.MkdirAll(profileClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(profileBase, "profile.json"),
		[]byte(`{"name":"big"}`),
		0o644,
	); err != nil {
		t.Fatalf("write profile.json: %v", err)
	}

	dirCount := 10
	perDir := largeProfileFiles / dirCount
	for d := 0; d < dirCount; d++ {
		sub := filepath.Join(profileClaudeDir, fmt.Sprintf("d%02d", d))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", sub, err)
		}
		for i := 0; i < perDir; i++ {
			fp := filepath.Join(sub, fmt.Sprintf("f%03d.md", i))
			content := fmt.Sprintf("# file %d/%d\nbody\n", d, i)
			if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
				t.Fatalf("write %q: %v", fp, err)
			}
		}
	}
	return projectRoot
}

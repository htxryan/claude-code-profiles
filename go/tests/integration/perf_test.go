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
//     synthetic profile and assert `c3p use` completes within budget. Dev
//     budget is 5 s; CI runners get a 7 s platform-class budget for the
//     same reason cold-start does (shared kernel, oversubscribed I/O on
//     fsync-heavy work). The fixture is built procedurally (not via
//     MakeFixture) — at 1000 files the per-file content map allocations
//     dominate cost, and we are testing the materializer, not the fixture
//     helper.
//
// All three must pass `helpers.EnsureBuilt(t)` before the first spawn so the
// shared test binary is on disk; the cold-start and binary-size tests build
// their *own* stripped binary because the EnsureBuilt artifact is not
// stripped (its symbol table inflates the size and slightly perturbs cold
// start). The stripped build is cached via sync.Once so a single `go test
// ./tests/integration/...` invocation pays the ~1 s build cost once, not
// once per perf test.
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
	"sync"
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

// largeProfileBudgetDev is R38's developer-class budget — `c3p use` on a
// 1000-file synthetic profile must complete within 5 s on a dev laptop.
// Go's syscall-direct materialize holds this where the TS implementation
// (which used 10 s on CI) could not.
const largeProfileBudgetDev = 5 * time.Second

// largeProfileBudgetCI is the platform-class budget for GitHub-hosted
// runners. CI runners run on shared disks with higher fsync latency than
// dev hardware; the materializer issues a per-file fsync (R23a) so 1000
// files are 1000 fsyncs. Local dev hits ~4.3 s — only ~17 % headroom under
// the 5 s cap — so a per-PR CI gate at 5 s would flake. 7 s leaves enough
// margin for noisy-neighbour variance while still catching the kind of
// regression (≥1.5×) that this gate is meant to flag. Mirrors the
// dev/CI split coldStartBudgetDev/coldStartBudgetCI uses.
const largeProfileBudgetCI = 7 * time.Second

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
// budget. Mean-of-middle-8 is immune to a single slow spawn and a single
// fast spawn (unrealistic best-case noise floor).
//
// Caveat: "cold start" here is shorthand for spawn latency. After the
// first iteration the OS page cache is warm, so the mean of the middle 8
// characterizes *sustained* spawn latency, not literal cold-cache spawn.
// True cold-cache measurement isn't portable across the supported OSes
// (no cross-platform "drop caches"). The metric we hold here — repeated
// spawn → exit time of a stripped binary — is what regresses meaningfully
// when init-time work creeps in, which is the regression we care about.
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

// TestBinarySize implements PR19. The build flags (-trimpath -ldflags="-s
// -w" + CGO_ENABLED=0) are what W2's release pipeline will ship; the size
// assertion would be invalidated if the release pipeline diverged from
// this set. -trimpath strips absolute build paths from the binary (a few
// hundred KB on a typical workspace) so the on-disk size we measure here
// matches what users will actually install.
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
		t.Skip("R38 perf gate is platform-class; Windows fsync latency exceeds the budget — validated on linux/darwin")
	}
	helpers.EnsureBuilt(t)

	projectRoot := buildLargeProfileFixture(t)

	budget := largeProfileBudgetDev
	hw := "developer-class"
	if isCI() {
		budget = largeProfileBudgetCI
		hw = "CI runner"
	}

	// Hard timeout sits above the budget so the assertion fires before
	// RunCli's deadline, not at it. Otherwise a budget+0.001 s spawn
	// surfaces as a harness timeout error rather than a perf-budget
	// violation.
	hardTimeout := budget * 2

	t0 := time.Now()
	res, err := helpers.RunCli(context.Background(), helpers.SpawnOptions{
		Cwd: projectRoot,
		// --non-interactive is set explicitly (not relying solely on
		// CI=true) so the test's intent doesn't depend on the runner
		// env. Without it, a future drift that makes `c3p use` prompt
		// for confirmation would hang locally until the harness
		// timeout fires, masking the real signal.
		Args:      []string{"use", "big", "--non-interactive"},
		TimeoutMs: int(hardTimeout / time.Millisecond),
	}, t)
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("c3p use big: harness error: %v (stderr=%q)", err, res.Stderr)
	}

	t.Logf("c3p use 1000-file profile (%s): %v budget=%v exit=%d",
		hw, elapsed, budget, res.ExitCode)

	if res.ExitCode != 0 {
		t.Fatalf("c3p use big: exit %d, stderr=%q", res.ExitCode, res.Stderr)
	}
	if elapsed > budget {
		t.Fatalf("large-profile use regressed: %v exceeds %s budget %v (stdout=%q)",
			elapsed, hw, budget, res.Stdout)
	}

	// Sanity: the live tree got materialized. A test that passes the
	// time budget but didn't actually copy files would be a false green.
	// Check both the first-written file (d00/f000.md) and the
	// last-written file (d09/f099.md) so a bug that corrupts or skips
	// the tail of the materialize queue is caught — the d00 spot-check
	// alone passed even when later directories were truncated.
	liveDir := filepath.Join(projectRoot, ".claude")
	for _, c := range []struct {
		path string
		want string
	}{
		{filepath.Join(liveDir, "d00", "f000.md"), "# file 0/0\nbody\n"},
		{filepath.Join(liveDir, "d09", "f099.md"), "# file 9/99\nbody\n"},
	} {
		body, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("read materialized file %q: %v", c.path, err)
		}
		if string(body) != c.want {
			t.Fatalf("materialized file %q contents wrong: got %q want %q", c.path, body, c.want)
		}
	}
}

// strippedBin caches the result of buildStrippedBinUncached so a single
// `go test ./tests/integration/...` invocation pays the ~1 s `go build`
// cost once even when both TestColdStart and TestBinarySize run in the
// same process. The artifact lives in os.TempDir() (not t.TempDir())
// because t.TempDir() is per-test and would defeat the cache; we clean it
// up in TestMain via cleanupStrippedBin.
var (
	strippedOnce sync.Once
	strippedBin  string
	strippedErr  error
)

// buildStrippedBin returns the path to the stripped, CGO-disabled,
// -trimpath build of cmd/c3p, building it on first call. The flag set
// (-trimpath -ldflags="-s -w" + CGO_ENABLED=0) is what W2's release
// pipeline will ship; -trimpath also makes the resulting binary path-
// independent (and a few hundred KB smaller) so the size assertion is
// stable across workspaces.
func buildStrippedBin(t *testing.T) string {
	t.Helper()
	strippedOnce.Do(func() {
		strippedBin, strippedErr = buildStrippedBinUncached()
	})
	if strippedErr != nil {
		t.Fatalf("build stripped c3p: %v", strippedErr)
	}
	return strippedBin
}

func buildStrippedBinUncached() (string, error) {
	module, err := helpers.FindModuleRoot()
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("c3p-stripped-%d%s", os.Getpid(), exeSuffixForOS()))
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, "./cmd/c3p")
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build (stripped): %w (stderr=%s)", err, stderr.String())
	}
	return out, nil
}

// cleanupStrippedBin removes the cached stripped binary if one was built.
// Called from TestMain after m.Run() so the artifact doesn't accumulate
// in os.TempDir() across `go test ./...` invocations — mirrors what
// helpers.CleanupBuiltBin does for the unstripped binary.
func cleanupStrippedBin() {
	if strippedBin != "" {
		_ = os.Remove(strippedBin)
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

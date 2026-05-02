/**
 * Gap closure #11 (PR6 #11, F2 epic claude-code-profiles-yhb):
 *
 * Large-profile performance gate (R38). Generate a synthetic 1000-file
 * profile and assert `c3p use` completes within 10 s on a CI runner.
 *
 *   R38 (port spec §3.6 + advisory P1-7): ≤2 s on a developer laptop.
 *   CI variance budget allows 10 s. Failure fails the build.
 *
 * Notes:
 *   - The fixture is built directly via fs writes (not makeFixture's helper)
 *     because makeFixture takes a per-file content map, which would be
 *     expensive to build at JS-object scale for 1000 files. We construct
 *     the fixture procedurally for speed.
 *   - We measure wall-clock from spawn start to process close. The first
 *     `npm run build` cost is excluded (ensureBuilt() asserts the artifact
 *     pre-exists; the suite-level prerequisite is `npm run build`).
 *   - The 10 s budget is intentionally generous for noisy CI runners; the
 *     spec-mandated 2 s budget is a developer-machine target. We log the
 *     measured wall-clock so a future regression-tuner sees the trend.
 *     The earlier 5 s ceiling regressed intermittently on GitHub-hosted
 *     runners (2–3× slower than a dev laptop on FS-heavy work).
 */

import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { ensureBuilt, runCli } from "./spawn.js";

let cleanupRoot: string | undefined;
afterEach(async () => {
  if (cleanupRoot) {
    await fs.rm(cleanupRoot, { recursive: true, force: true });
  }
  cleanupRoot = undefined;
});

const FILE_COUNT = 1000;
// Wall-clock cap for `c3p use` against the 1000-file fixture. R38 mandates
// ≤2s on a developer laptop; we hold 10s here because GitHub-hosted CI
// runners are typically 2–3× slower than a dev laptop on FS-heavy work and
// a tighter budget flakes under shared-runner load. A local pass that
// trends toward 8–10s is the early signal to investigate before CI flakes.
const CI_BUDGET_MS = 10_000;

async function buildLargeProfileFixture(): Promise<string> {
  const tmp = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-perf-"));
  const projectRoot = path.join(tmp, "project");
  cleanupRoot = tmp;
  const profileDir = path.join(projectRoot, ".claude-profiles", "big", ".claude");
  await fs.mkdir(profileDir, { recursive: true });
  // profile.json (R35: bare object).
  await fs.writeFile(
    path.join(projectRoot, ".claude-profiles", "big", "profile.json"),
    JSON.stringify({ name: "big" }),
  );

  // Spread files across nested directories to mirror realistic profiles —
  // 10 dirs × 100 files each.
  const writes: Promise<void>[] = [];
  for (let dir = 0; dir < 10; dir++) {
    const sub = path.join(profileDir, `d${String(dir).padStart(2, "0")}`);
    await fs.mkdir(sub, { recursive: true });
    for (let i = 0; i < FILE_COUNT / 10; i++) {
      // Files are small but non-empty so the materializer copies a real
      // body. Index-based content guarantees uniqueness so any de-dup would
      // be wrong.
      const fp = path.join(sub, `f${String(i).padStart(3, "0")}.md`);
      writes.push(fs.writeFile(fp, `# file ${dir}/${i}\nbody\n`));
    }
  }
  await Promise.all(writes);
  return projectRoot;
}

describe("gap closure #11: large-profile performance R38 (PR6 #11)", () => {
  it(
    "1000-file profile: c3p use completes within 10s budget",
    async () => {
      await ensureBuilt();
      const projectRoot = await buildLargeProfileFixture();

      const t0 = Date.now();
      const r = await runCli({
        args: ["--cwd", projectRoot, "use", "big"],
        timeoutMs: CI_BUDGET_MS * 2, // hard timeout > budget so the assertion fires before timeout
      });
      const elapsed = Date.now() - t0;

      // Log the measured time so a regression-watcher can see the trend
      // without grepping out of vitest's own timing.

      console.log(`[perf] c3p use 1000-file profile: ${elapsed}ms`);

      expect(r.exitCode).toBe(0);
      expect(elapsed).toBeLessThanOrEqual(CI_BUDGET_MS);

      // Sanity: the live tree got materialized.
      const liveFile = path.join(
        projectRoot,
        ".claude",
        "d00",
        "f000.md",
      );
      const content = await fs.readFile(liveFile, "utf8");
      expect(content).toBe(`# file 0/0\nbody\n`);
    },
    // Vitest test timeout — generous so a flaky CI runner doesn't false-fail
    // the budget check (which fires inside the test, not via the timeout).
    30_000,
  );

  it(
    "1000-file profile: c3p status (read-only) is also fast (≤ 10s)",
    async () => {
      await ensureBuilt();
      const projectRoot = await buildLargeProfileFixture();

      // Materialize first so status has a fingerprint to compare.
      const useResult = await runCli({
        args: ["--cwd", projectRoot, "use", "big"],
        timeoutMs: CI_BUDGET_MS * 2,
      });
      expect(useResult.exitCode).toBe(0);

      const t0 = Date.now();
      const r = await runCli({
        args: ["--cwd", projectRoot, "status"],
        timeoutMs: CI_BUDGET_MS * 2,
      });
      const elapsed = Date.now() - t0;

      console.log(`[perf] c3p status 1000-file profile: ${elapsed}ms`);

      expect(r.exitCode).toBe(0);
      expect(elapsed).toBeLessThanOrEqual(CI_BUDGET_MS);
    },
    30_000,
  );
});

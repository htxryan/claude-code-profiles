/**
 * R38: 1000-file `use` op completes in under 2s on a developer laptop.
 *
 * Run via vitest's normal test runner; flaky on CI under load. The budget
 * is the spec's commitment, not a tight upper bound — we assert generously
 * (5s) so test infra overhead doesn't cause spurious failures while still
 * catching > 10x regressions.
 */

import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { MergedFile } from "../../src/merge/types.js";
import {
  RESOLVED_PLAN_SCHEMA_VERSION,
  type ResolvedPlan,
} from "../../src/resolver/types.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";

describe("perf budget (R38)", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-perf-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  // Windows GitHub-Actions runners are I/O-bound enough that the 5s budget
  // (already 2.5x the spec's 2s "developer laptop" budget) regularly slips
  // past 5s on cold cache. The perf invariant is meaningful on the platforms
  // most users develop on; CI on Windows already validates correctness via
  // the rest of the matrix. Skip on Windows to keep the budget tight elsewhere.
  it.skipIf(process.platform === "win32")("materializes 1000 files in under 5s (spec budget 2s, +headroom)", async () => {
    const paths = buildStatePaths(root);
    const merged: MergedFile[] = [];
    for (let i = 0; i < 1000; i++) {
      const dir = `bucket-${Math.floor(i / 100)}`;
      merged.push({
        path: `${dir}/file-${i}.md`,
        bytes: Buffer.from(`payload ${i} `.repeat(8)),
        contributors: ["leaf"],
        mergePolicy: "last-wins",
        destination: ".claude",
      });
    }
    const plan: ResolvedPlan = {
      schemaVersion: RESOLVED_PLAN_SCHEMA_VERSION,
      profileName: "leaf",
      chain: ["leaf"],
      includes: [],
      contributors: [
        {
          kind: "profile",
          id: "leaf",
          rootPath: path.join(root, ".claude-profiles/leaf"),
          claudeDir: path.join(root, ".claude-profiles/leaf/.claude"),
          external: false,
        },
      ],
      files: [],
      warnings: [],
      externalPaths: [],
    };
    const t0 = Date.now();
    await materialize(paths, plan, merged);
    const dt = Date.now() - t0;
    expect(dt).toBeLessThan(5000);
  }, 30_000);
});

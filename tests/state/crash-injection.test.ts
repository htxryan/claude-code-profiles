/**
 * E3 fitness function: crash-injection across the pending/prior protocol.
 *
 * The spec specifies five injection points:
 *   1. pre-pending — process killed before step (a) writes anything
 *   2. post-pending pre-rename-b — pending populated, .claude/ untouched
 *   3. post-rename-b pre-rename-c — .prior/ exists, .claude/ does not
 *   4. post-rename-c pre-state-write — .claude/ swapped, .state.json stale
 *   5. post-state-write — fully committed
 *
 * For each point we simulate the on-disk state, then invoke `materialize`
 * again and assert the live `.claude/` ends up as the new target with no
 * stray `.pending/` or `.prior/`.
 *
 * We don't fork child processes — we simulate the partial states directly
 * because the rename protocol is fully deterministic in terms of what
 * artifacts can exist at each injection point. This sidesteps cross-platform
 * spawn complexity while still exercising the crash-recovery code path.
 */

import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { MergedFile } from "../../src/merge/types.js";
import type { ResolvedPlan } from "../../src/resolver/types.js";
import { pathExists } from "../../src/state/atomic.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";
import { readStateFile } from "../../src/state/state-file.js";
import { RESOLVED_PLAN_SCHEMA_VERSION } from "../../src/resolver/types.js";

function makePlan(profileName: string, projectRoot: string): ResolvedPlan {
  return {
    schemaVersion: RESOLVED_PLAN_SCHEMA_VERSION,
    profileName,
    chain: [profileName],
    includes: [],
    contributors: [
      {
        kind: "profile",
        id: profileName,
        rootPath: path.join(projectRoot, ".claude-profiles", profileName),
        claudeDir: path.join(projectRoot, ".claude-profiles", profileName, ".claude"),
        external: false,
      },
    ],
    files: [],
    warnings: [],
    externalPaths: [],
  };
}

function makeMerged(content: string): MergedFile[] {
  return [
    {
      path: "CLAUDE.md",
      bytes: Buffer.from(content),
      contributors: ["leaf"],
      mergePolicy: "concat",
      destination: ".claude",
    },
    {
      path: "agents/x.md",
      bytes: Buffer.from("AGENT"),
      contributors: ["leaf"],
      mergePolicy: "last-wins",
      destination: ".claude",
    },
  ];
}

describe("E3 fitness function: crash injection across pending/prior", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-crash-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  it("recovers from injection 1: nothing on disk", async () => {
    const paths = buildStatePaths(root);
    const plan = makePlan("leaf", root);
    await materialize(paths, plan, makeMerged("LEAF-V1"));
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("LEAF-V1");
  });

  it("recovers from injection 2: .pending/ left over (no live .claude/)", async () => {
    const paths = buildStatePaths(root);
    // Stage stale .pending/ as if step (a) wrote and then we crashed.
    await fs.mkdir(paths.pendingDir, { recursive: true });
    await fs.writeFile(path.join(paths.pendingDir, "STALE.md"), "STALE");

    const plan = makePlan("leaf", root);
    await materialize(paths, plan, makeMerged("LEAF-V1"));
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("LEAF-V1");
    expect(await pathExists(path.join(paths.claudeDir, "STALE.md"))).toBe(false);
    expect(await pathExists(paths.pendingDir)).toBe(false);
    expect(await pathExists(paths.priorDir)).toBe(false);
  });

  it("recovers from injection 3: .prior/ exists, no live .claude/ (post-step-b crash)", async () => {
    const paths = buildStatePaths(root);
    // Simulate: .claude/ was renamed to .prior/, then crash.
    await fs.mkdir(paths.priorDir, { recursive: true });
    await fs.writeFile(path.join(paths.priorDir, "PRIOR.md"), "PRIOR");
    await fs.mkdir(paths.pendingDir, { recursive: true });
    await fs.writeFile(path.join(paths.pendingDir, "PENDING.md"), "PENDING");

    const plan = makePlan("leaf", root);
    await materialize(paths, plan, makeMerged("LEAF-V1"));

    // Reconciliation must restore from .prior, then materialize wins. So:
    // the recovered .claude/ should match the new merged content (LEAF-V1),
    // not PRIOR or PENDING contents.
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("LEAF-V1");
    expect(await pathExists(paths.pendingDir)).toBe(false);
    expect(await pathExists(paths.priorDir)).toBe(false);
  });

  it("recovers from injection 4: .claude/ swapped but .state.json stale", async () => {
    const paths = buildStatePaths(root);
    // Simulate full step c success, but state file points elsewhere.
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "OLD-LIVE");
    // Write a stale state pointing at a different profile.
    await fs.mkdir(paths.metaDir, { recursive: true });
    await fs.writeFile(
      paths.stateFile,
      JSON.stringify({
        schemaVersion: 1,
        activeProfile: "stale",
        materializedAt: "2026-01-01T00:00:00Z",
        resolvedSources: [],
        fingerprint: { schemaVersion: 1, files: {} },
        externalTrustNotices: [],
      }),
    );

    const plan = makePlan("leaf", root);
    await materialize(paths, plan, makeMerged("LEAF-V1"));
    const r = await readStateFile(paths);
    expect(r.state.activeProfile).toBe("leaf");
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("LEAF-V1");
  });

  it("recovers from injection 5: post state-write — re-applying is idempotent", async () => {
    const paths = buildStatePaths(root);
    const plan = makePlan("leaf", root);
    await materialize(paths, plan, makeMerged("LEAF-V1"));
    // Apply same plan again — no leftover artifacts and content stable.
    await materialize(paths, plan, makeMerged("LEAF-V1"));
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("LEAF-V1");
    expect(await pathExists(paths.pendingDir)).toBe(false);
    expect(await pathExists(paths.priorDir)).toBe(false);
  });

  it("five repeated swaps converge to the latest target with no leftover artifacts", async () => {
    const paths = buildStatePaths(root);
    for (let i = 0; i < 5; i++) {
      const plan = makePlan(`leaf${i}`, root);
      await materialize(paths, plan, makeMerged(`V${i}`));
    }
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("V4");
    expect(await pathExists(paths.pendingDir)).toBe(false);
    expect(await pathExists(paths.priorDir)).toBe(false);
    const r = await readStateFile(paths);
    expect(r.state.activeProfile).toBe("leaf4");
  });
});

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { CliUserError, EXIT_USER_ERROR } from "../../../src/cli/exit.js";
import type { GatePrompt } from "../../../src/cli/prompt.js";
import { runSwap } from "../../../src/cli/service/swap.js";
import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { readStateFile } from "../../../src/state/state-file.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

const NEVER_CALLED: GatePrompt = async () => {
  throw new Error("prompt should not have been called in this test");
};

async function setupTwoProfiles() {
  const f = await makeFixture({
    profiles: {
      a: {
        manifest: { name: "a" },
        files: { "CLAUDE.md": "A\n" },
      },
      b: {
        manifest: { name: "b" },
        files: { "CLAUDE.md": "B\n" },
      },
    },
  });
  // Activate "a" so we can swap to "b" with state in place.
  const planA = await resolve("a", { projectRoot: f.projectRoot });
  const mergedA = await merge(planA);
  await materialize(buildStatePaths(f.projectRoot), planA, mergedA);
  return f;
}

async function makeDrift(paths: ReturnType<typeof buildStatePaths>) {
  await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");
}

describe("runSwap — clean swap (no drift)", () => {
  it("interactive, no drift, no flag → materializes without prompting", async () => {
    fx = await setupTwoProfiles();
    const result = await runSwap({
      paths: buildStatePaths(fx.projectRoot),
      targetProfile: "b",
      mode: "interactive",
      onDriftFlag: null,
      prompt: NEVER_CALLED,
      signalHandlers: false,
    });
    expect(result.action).toBe("materialized");
    expect(result.choice).toBe("no-drift-proceed");
    expect(result.activeAfter).toBe("b");
  });

  it("non-interactive, no drift, no flag → materializes without prompting", async () => {
    fx = await setupTwoProfiles();
    const result = await runSwap({
      paths: buildStatePaths(fx.projectRoot),
      targetProfile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      prompt: NEVER_CALLED,
      signalHandlers: false,
    });
    expect(result.action).toBe("materialized");
  });
});

describe("runSwap — drift + non-TTY (epic invariant: never blocks)", () => {
  it("non-interactive + drift + no flag → throws CliUserError(exit 1) naming the flag", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    await makeDrift(paths);
    let thrown: unknown;
    try {
      await runSwap({
        paths,
        targetProfile: "b",
        mode: "non-interactive",
        onDriftFlag: null,
        prompt: NEVER_CALLED,
        signalHandlers: false,
      });
    } catch (err) {
      thrown = err;
    }
    expect(thrown).toBeInstanceOf(CliUserError);
    expect((thrown as CliUserError).exitCode).toBe(EXIT_USER_ERROR);
    expect((thrown as Error).message).toContain("--on-drift=");
  });

  it("non-interactive + drift + --on-drift=discard → applies without prompting", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    await makeDrift(paths);
    const result = await runSwap({
      paths,
      targetProfile: "b",
      mode: "non-interactive",
      onDriftFlag: "discard",
      prompt: NEVER_CALLED,
      signalHandlers: false,
    });
    expect(result.action).toBe("materialized");
    expect(result.choice).toBe("discard");
    expect(result.backupSnapshot).not.toBeNull();
  });

  it("non-interactive + drift + --on-drift=persist → persists then materializes", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    await makeDrift(paths);
    const result = await runSwap({
      paths,
      targetProfile: "b",
      mode: "non-interactive",
      onDriftFlag: "persist",
      prompt: NEVER_CALLED,
      signalHandlers: false,
    });
    expect(result.action).toBe("persisted-and-materialized");
    expect(result.choice).toBe("persist");
    // The active profile's CLAUDE.md should now be the EDITED bytes.
    const persisted = await fs.readFile(
      path.join(fx.projectRoot, ".claude-profiles/a/.claude/CLAUDE.md"),
      "utf8",
    );
    expect(persisted).toBe("EDITED\n");
  });

  it("non-interactive + drift + --on-drift=abort → throws CliUserError; no FS changes", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    await makeDrift(paths);
    const before = await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8");
    let thrown: unknown;
    try {
      await runSwap({
        paths,
        targetProfile: "b",
        mode: "non-interactive",
        onDriftFlag: "abort",
        prompt: NEVER_CALLED,
        signalHandlers: false,
      });
    } catch (err) {
      thrown = err;
    }
    expect(thrown).toBeInstanceOf(CliUserError);
    const after = await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8");
    expect(after).toBe(before);
    const { state } = await readStateFile(paths);
    expect(state.activeProfile).toBe("a"); // unchanged
  });
});

describe("runSwap — interactive prompt path", () => {
  it("interactive + drift + no flag → prompt called; user picks discard", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    await makeDrift(paths);
    let promptCalls = 0;
    const prompt: GatePrompt = async (input) => {
      promptCalls++;
      expect(input.activeProfile).toBe("a");
      expect(input.targetProfile).toBe("b");
      expect(input.driftedCount).toBeGreaterThan(0);
      return "discard";
    };
    const result = await runSwap({
      paths,
      targetProfile: "b",
      mode: "interactive",
      onDriftFlag: null,
      prompt,
      signalHandlers: false,
    });
    expect(promptCalls).toBe(1);
    expect(result.choice).toBe("discard");
    expect(result.action).toBe("materialized");
  });

  it("interactive + drift + flag → prompt NOT called (flag wins)", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    await makeDrift(paths);
    const result = await runSwap({
      paths,
      targetProfile: "b",
      mode: "interactive",
      onDriftFlag: "discard",
      prompt: NEVER_CALLED,
      signalHandlers: false,
    });
    expect(result.choice).toBe("discard");
  });
});

// ch5 followup: a previous materialize killed mid-step (.claude/ rolled
// aside to .prior/, .pending/ partially staged) must be reconciled by
// runSwap BEFORE the outside-lock drift detect runs. Without the entrypoint
// reconcile, the drift gate reads a missing/half-written .claude/ tree and
// reports false drift — auto-aborting the swap in non-interactive mode and
// stranding the user in an inconsistent state.
describe("runSwap — entrypoint reconcile (ch5 followup, R16/R16a)", () => {
  it("simulates a crashed materialize (.prior/ exists, .claude/ missing) and recovers", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    // Capture the live tree (the last successful state for profile "a").
    const live = path.join(paths.claudeDir);
    expect(await fs.stat(live)).toBeDefined();
    // Simulate the crash window between rename-b and rename-c: rename
    // .claude/ aside to .prior/. There is no .pending/ at this point.
    await fs.rename(live, paths.priorDir);
    // Sanity check: .claude/ is gone, .prior/ holds the bytes.
    await expect(fs.stat(live)).rejects.toThrow();
    expect(await fs.stat(paths.priorDir)).toBeDefined();

    // A non-interactive swap with NO --on-drift flag MUST succeed (not
    // auto-abort with exit 1) because the half-written state is recoverable
    // via the entrypoint reconcile and there is no real drift.
    const result = await runSwap({
      paths,
      targetProfile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      prompt: NEVER_CALLED,
      signalHandlers: false,
    });
    expect(result.action).toBe("materialized");
    expect(result.activeAfter).toBe("b");
    // The .prior/ leftover is gone: reconcile renamed it back to .claude/,
    // then the swap materialized "b" on top of it.
    await expect(fs.stat(paths.priorDir)).rejects.toThrow();
    // Live tree now reflects "b".
    const live2 = await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8");
    expect(live2).toBe("B\n");
  });

  it("simulates a leftover .pending/ (step-a crash) and recovers without false drift", async () => {
    fx = await setupTwoProfiles();
    const paths = buildStatePaths(fx.projectRoot);
    // Simulate a crash during step a: stale .pending/ left behind, .claude/
    // intact. Without reconcile, this isn't a drift signal anyway, but the
    // entrypoint reconcile must clean it up so the in-lock materialize
    // doesn't see a stale staging dir.
    await fs.mkdir(paths.pendingDir, { recursive: true });
    await fs.writeFile(path.join(paths.pendingDir, "stale.txt"), "junk");

    const result = await runSwap({
      paths,
      targetProfile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      prompt: NEVER_CALLED,
      signalHandlers: false,
    });
    expect(result.action).toBe("materialized");
    // .pending/ is no longer holding stale bytes (either gone or contains
    // only the next operation's transient files — but not "stale.txt").
    const pendingExists = await fs
      .stat(paths.pendingDir)
      .then(() => true)
      .catch(() => false);
    if (pendingExists) {
      const entries = await fs.readdir(paths.pendingDir);
      expect(entries).not.toContain("stale.txt");
    }
  });
});

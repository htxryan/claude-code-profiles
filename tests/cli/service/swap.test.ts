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

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runSync } from "../../../src/cli/commands/sync.js";
import { runUse } from "../../../src/cli/commands/use.js";
import { CliUserError, EXIT_USER_ERROR } from "../../../src/cli/exit.js";
import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";
import { captureOutput } from "../helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

async function setup() {
  const f = await makeFixture({
    profiles: {
      a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
    },
  });
  const planA = await resolve("a", { projectRoot: f.projectRoot });
  const m = await merge(planA);
  await materialize(buildStatePaths(f.projectRoot), planA, m);
  return f;
}

describe("use command (AC-11, AC-12, AC-13)", () => {
  it("clean swap: prints Switched to <name>; exit 0", async () => {
    fx = await setup();
    const cap = captureOutput(false);
    const code = await runUse({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("Switched to b");
  });

  it("non-TTY + drift + no flag: exits with CliUserError naming the flag (epic invariant)", async () => {
    fx = await setup();
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDIT\n");
    const cap = captureOutput(false);
    let thrown: unknown;
    try {
      await runUse({
        cwd: fx.projectRoot,
        output: cap.channel,
        profile: "b",
        mode: "non-interactive",
        onDriftFlag: null,
        signalHandlers: false,
      });
    } catch (err) {
      thrown = err;
    }
    expect(thrown).toBeInstanceOf(CliUserError);
    expect((thrown as CliUserError).exitCode).toBe(EXIT_USER_ERROR);
  });

  it("--json: structured payload after success", async () => {
    fx = await setup();
    const cap = captureOutput(true);
    await runUse({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
    });
    const payload = cap.jsonLines()[0] as { activeProfile: string; choice: string };
    expect(payload.activeProfile).toBe("b");
    expect(payload.choice).toBe("no-drift-proceed");
    expect(cap.stderr()).toBe("");
  });
});

describe("sync command (AC-10)", () => {
  it("clean: re-materializes active without changing it", async () => {
    fx = await setup();
    const cap = captureOutput(false);
    const code = await runSync({
      cwd: fx.projectRoot,
      output: cap.channel,
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("Synced a");
  });

  it("no active profile: throws CliUserError(exit 1)", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    let thrown: unknown;
    try {
      await runSync({
        cwd: fx.projectRoot,
        output: cap.channel,
        mode: "non-interactive",
        onDriftFlag: null,
        signalHandlers: false,
      });
    } catch (err) {
      thrown = err;
    }
    expect(thrown).toBeInstanceOf(CliUserError);
    expect((thrown as CliUserError).exitCode).toBe(EXIT_USER_ERROR);
  });

  it("drift + --on-drift=discard: backs up + applies; succeeds", async () => {
    fx = await setup();
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDIT\n");
    const cap = captureOutput(false);
    const code = await runSync({
      cwd: fx.projectRoot,
      output: cap.channel,
      mode: "non-interactive",
      onDriftFlag: "discard",
      signalHandlers: false,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("Synced a");
    expect(cap.stdout()).toContain("Backup:");
  });
});

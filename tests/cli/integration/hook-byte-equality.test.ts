/**
 * E6 fitness function: the installed pre-commit hook must be byte-identical
 * to the verbatim R25a script across releases.
 *
 * The integration test spawns the built CLI, runs `hook install`, and
 * asserts the on-disk file matches the canonical bytes. This is the
 * acceptance gate that catches a release-time spec edit (a CI job calls
 * this and fails the build if the bytes drift without a deliberate bump).
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { HOOK_SCRIPT } from "../../../src/cli/commands/hook.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

describe("R25a hook script byte equality (E6 fitness function)", () => {
  it("hook install writes the verbatim spec script", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    await fs.mkdir(path.join(fx.projectRoot, ".git", "hooks"), { recursive: true });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "hook", "install"],
    });
    expect(r.exitCode).toBe(0);
    const written = await fs.readFile(
      path.join(fx.projectRoot, ".git", "hooks", "pre-commit"),
      "utf8",
    );
    expect(written).toBe(HOOK_SCRIPT);
    // Pin against the literal string so a typo in HOOK_SCRIPT itself can't
    // pass the round-trip silently (the fitness function asserts both the
    // constant *and* the on-disk content match the spec).
    expect(written).toBe(
      "#!/bin/sh\n" +
        "command -v c3p >/dev/null 2>&1 || exit 0\n" +
        "c3p drift --pre-commit-warn 2>&1\n" +
        "exit 0\n",
    );
  });

  it("init writes the same hook bytes via the bootstrap path", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    await fs.mkdir(path.join(fx.projectRoot, ".git", "hooks"), { recursive: true });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-seed"],
    });
    expect(r.exitCode).toBe(0);
    const written = await fs.readFile(
      path.join(fx.projectRoot, ".git", "hooks", "pre-commit"),
      "utf8",
    );
    expect(written).toBe(HOOK_SCRIPT);
  });
});

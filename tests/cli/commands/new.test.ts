import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runNew } from "../../../src/cli/commands/new.js";
import { CliUserError } from "../../../src/cli/exit.js";
import { listProfiles } from "../../../src/resolver/discover.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";
import { captureOutput } from "../helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

describe("new (AC-18)", () => {
  it("creates .claude-profiles/<name>/.claude/ + profile.json", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    const code = await runNew({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "minimal",
      description: null,
    });
    expect(code).toBe(0);
    const profileDir = path.join(fx.projectRoot, ".claude-profiles/minimal");
    const stat = await fs.stat(path.join(profileDir, ".claude"));
    expect(stat.isDirectory()).toBe(true);
    const manifest = JSON.parse(
      await fs.readFile(path.join(profileDir, "profile.json"), "utf8"),
    );
    expect(manifest).toEqual({ name: "minimal" });
    expect(cap.stdout()).toContain("Created profile");
  });

  it("--description is included in manifest when present", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    await runNew({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "x",
      description: "test profile",
    });
    const m = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles/x/profile.json"),
        "utf8",
      ),
    );
    expect(m).toEqual({ name: "x", description: "test profile" });
  });

  it("refuses to overwrite an existing profile directory", async () => {
    fx = await makeFixture({
      profiles: { exists: { manifest: { name: "exists" }, files: {} } },
    });
    const cap = captureOutput(false);
    await expect(
      runNew({
        cwd: fx.projectRoot,
        output: cap.channel,
        profile: "exists",
        description: null,
      }),
    ).rejects.toBeInstanceOf(CliUserError);
  });

  it("rejects invalid profile names", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    for (const bad of ["", ".", "..", "_components", ".hidden", "a/b", "a\\b"]) {
      await expect(
        runNew({
          cwd: fx.projectRoot,
          output: cap.channel,
          profile: bad,
          description: null,
        }),
      ).rejects.toBeInstanceOf(CliUserError);
    }
  });

  it("created profile shows up in listProfiles", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    await runNew({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "added",
      description: null,
    });
    const names = await listProfiles({ projectRoot: fx.projectRoot });
    expect(names).toContain("added");
  });
});

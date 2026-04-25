import { afterEach, describe, expect, it } from "vitest";

import { runList, type ListPayload } from "../../../src/cli/commands/list.js";
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

describe("list (R30, R40)", () => {
  it("empty project: prints (no profiles)", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    const code = await runList({ cwd: fx.projectRoot, output: cap.channel });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("no profiles");
  });

  it("renders all profiles with extends/includes/active marker", async () => {
    fx = await makeFixture({
      profiles: {
        base: { manifest: { name: "base", description: "base profile" }, files: {} },
        leaf: {
          manifest: { name: "leaf", extends: "base", includes: ["compA"] },
          files: {},
        },
        other: { manifest: { name: "other" }, files: {} },
      },
      components: {
        compA: { files: {} },
      },
    });
    const cap = captureOutput(false);
    const code = await runList({ cwd: fx.projectRoot, output: cap.channel });
    expect(code).toBe(0);
    const out = cap.stdout();
    expect(out).toContain("base");
    expect(out).toContain("leaf");
    expect(out).toContain("other");
    expect(out).toContain("extends=base");
    expect(out).toContain("includes=[compA]");
    // Nothing active yet — no `*` marker present on any line content
    expect(out).not.toMatch(/^\* /m);
  });

  it("--json: structured payload that round-trips", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a", description: "first" }, files: {} },
        b: { manifest: { name: "b", extends: "a", includes: ["x"] }, files: {} },
      },
      components: { x: { files: {} } },
    });
    const cap = captureOutput(true);
    const code = await runList({ cwd: fx.projectRoot, output: cap.channel });
    expect(code).toBe(0);
    const lines = cap.jsonLines();
    expect(lines).toHaveLength(1);
    const payload = lines[0] as ListPayload;
    expect(payload.profiles).toHaveLength(2);
    expect(payload.profiles[0]).toMatchObject({
      name: "a",
      active: false,
      description: "first",
      extends: null,
      includes: [],
      tags: [],
      lastMaterialized: null,
    });
    expect(payload.profiles[1]).toMatchObject({
      name: "b",
      extends: "a",
      includes: ["x"],
    });
    // --json silences human output (epic invariant)
    expect(cap.stderr()).toBe("");
  });

  it("marks the active profile with materialized timestamp", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const merged = await merge(plan);
    const paths = buildStatePaths(fx.projectRoot);
    await materialize(paths, plan, merged);

    const cap = captureOutput(true);
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as ListPayload;
    const a = payload.profiles.find((p) => p.name === "a");
    const b = payload.profiles.find((p) => p.name === "b");
    expect(a?.active).toBe(true);
    expect(a?.lastMaterialized).toMatch(/^\d{4}-\d{2}-\d{2}T/);
    expect(b?.active).toBe(false);
    expect(b?.lastMaterialized).toBeNull();
  });

  it("Missing state file is not surfaced (fresh project)", async () => {
    fx = await makeFixture({
      profiles: { a: { manifest: { name: "a" }, files: {} } },
    });
    const cap = captureOutput(true);
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as ListPayload;
    expect(payload.stateWarning).toBeNull();
    expect(cap.stderr()).toBe("");
  });
});

import { afterEach, describe, expect, it } from "vitest";

import { runDiff, type DiffPayload } from "../../../src/cli/commands/diff.js";
import { CliUserError } from "../../../src/cli/exit.js";
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

describe("diff (R32, R40)", () => {
  it("two distinct profiles: classifies added/removed/changed", async () => {
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "shared.md": "A\n", "only-in-a.md": "X\n" },
        },
        b: {
          manifest: { name: "b" },
          files: { "shared.md": "B\n", "only-in-b.md": "Y\n" },
        },
      },
    });
    const cap = captureOutput(true);
    const code = await runDiff({
      cwd: fx.projectRoot,
      output: cap.channel,
      a: "a",
      b: "b",
    });
    expect(code).toBe(0);
    const payload = cap.jsonLines()[0] as DiffPayload;
    const byPath = Object.fromEntries(payload.entries.map((e) => [e.relPath, e.status]));
    expect(byPath["shared.md"]).toBe("changed");
    expect(byPath["only-in-a.md"]).toBe("added");
    expect(byPath["only-in-b.md"]).toBe("removed");
    expect(payload.totals).toEqual({ added: 1, removed: 1, changed: 1 });
    expect(JSON.parse(JSON.stringify(payload))).toEqual(payload);
  });

  it("identical profiles: empty diff", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "X\n" } },
        b: { manifest: { name: "b" }, files: { "x.md": "X\n" } },
      },
    });
    const cap = captureOutput(false);
    await runDiff({ cwd: fx.projectRoot, output: cap.channel, a: "a", b: "b" });
    expect(cap.stdout()).toContain("identical");
  });

  it("self-diff: no resolve needed, returns identical", async () => {
    fx = await makeFixture({
      profiles: { a: { manifest: { name: "a" }, files: { "x.md": "X\n" } } },
    });
    const cap = captureOutput(true);
    await runDiff({ cwd: fx.projectRoot, output: cap.channel, a: "a", b: "a" });
    const payload = cap.jsonLines()[0] as DiffPayload;
    expect(payload.entries).toEqual([]);
    expect(payload.totals).toEqual({ added: 0, removed: 0, changed: 0 });
  });

  it("one-arg form: compares against active profile", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "x.md": "B\n" } },
      },
    });
    // Activate b
    const planB = await resolve("b", { projectRoot: fx.projectRoot });
    const merged = await merge(planB);
    await materialize(buildStatePaths(fx.projectRoot), planB, merged);

    const cap = captureOutput(true);
    await runDiff({ cwd: fx.projectRoot, output: cap.channel, a: "a", b: null });
    const payload = cap.jsonLines()[0] as DiffPayload;
    expect(payload.b).toBe("b");
    expect(payload.entries.map((e) => e.relPath)).toContain("x.md");
  });

  it("one-arg form with no active profile: throws CliUserError", async () => {
    fx = await makeFixture({
      profiles: { a: { manifest: { name: "a" }, files: {} } },
    });
    const cap = captureOutput(false);
    await expect(
      runDiff({ cwd: fx.projectRoot, output: cap.channel, a: "a", b: null }),
    ).rejects.toBeInstanceOf(CliUserError);
  });
});

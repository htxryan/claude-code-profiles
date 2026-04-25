import { afterEach, describe, expect, it } from "vitest";

import { runValidate, type ValidatePayload } from "../../../src/cli/commands/validate.js";
import { CliUserError, EXIT_CONFLICT } from "../../../src/cli/exit.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";
import { captureOutput } from "../helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

describe("validate (R33)", () => {
  it("all-pass project: exit 0; per-profile PASS in human output", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "y.md": "B\n" } },
      },
    });
    const cap = captureOutput(false);
    const code = await runValidate({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: null,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("PASS  a");
    expect(cap.stdout()).toContain("PASS  b");
    expect(cap.stdout()).toContain("validate: 2 pass");
  });

  it("--json all-pass: structured payload with pass:true", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: {} },
      },
    });
    const cap = captureOutput(true);
    const code = await runValidate({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: null,
    });
    expect(code).toBe(0);
    const payload = cap.jsonLines()[0] as ValidatePayload;
    expect(payload.pass).toBe(true);
    expect(payload.results).toHaveLength(1);
    expect(payload.results[0]).toMatchObject({
      profile: "a",
      ok: true,
      errorCode: null,
      errorMessage: null,
    });
  });

  it("missing extends: throws CliUserError(exit 3); per-profile FAIL surfaced", async () => {
    fx = await makeFixture({
      profiles: {
        leaf: { manifest: { name: "leaf", extends: "nope" }, files: {} },
      },
    });
    const cap = captureOutput(false);
    await expect(
      runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null }),
    ).rejects.toMatchObject({ exitCode: EXIT_CONFLICT });
    expect(cap.stdout()).toContain("FAIL  leaf");
    expect(cap.stdout()).toContain("nope");
  });

  it("conflict (R11): one-include profile fails, others still pass", async () => {
    fx = await makeFixture({
      profiles: {
        ok: { manifest: { name: "ok" }, files: { "x.md": "X\n" } },
        bad: {
          manifest: { name: "bad", includes: ["c1", "c2"] },
          files: {},
        },
      },
      components: {
        c1: { files: { "settings.local.json": "{}" } },
        c2: { files: { "settings.local.json": "{}" } },
      },
    });
    const cap = captureOutput(true);
    let thrown: unknown;
    try {
      await runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null });
    } catch (err) {
      thrown = err;
    }
    expect(thrown).toBeInstanceOf(CliUserError);
    expect((thrown as CliUserError).exitCode).toBe(EXIT_CONFLICT);
    const payload = cap.jsonLines()[0] as ValidatePayload;
    const ok = payload.results.find((r) => r.profile === "ok");
    const bad = payload.results.find((r) => r.profile === "bad");
    expect(ok?.ok).toBe(true);
    expect(bad?.ok).toBe(false);
    expect(bad?.errorCode).toBe("Conflict");
  });

  it("named-profile validate skips others", async () => {
    fx = await makeFixture({
      profiles: {
        good: { manifest: { name: "good" }, files: {} },
        broken: { manifest: { name: "broken", extends: "nope" }, files: {} },
      },
    });
    const cap = captureOutput(true);
    const code = await runValidate({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "good",
    });
    expect(code).toBe(0);
    const payload = cap.jsonLines()[0] as ValidatePayload;
    expect(payload.results).toHaveLength(1);
    expect(payload.results[0]?.profile).toBe("good");
  });

  it("empty project: exit 0, prints (no profiles to validate)", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    const code = await runValidate({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: null,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("no profiles");
  });
});

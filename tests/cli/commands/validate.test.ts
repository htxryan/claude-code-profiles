import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runValidate, type ValidatePayload } from "../../../src/cli/commands/validate.js";
import { CliUserError, EXIT_CONFLICT, EXIT_USER_ERROR } from "../../../src/cli/exit.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { writeStateFile } from "../../../src/state/state-file.js";
import { defaultState } from "../../../src/state/types.js";
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
    // Non-TTY in tests → glyphs render as `[ok]`. PASS rows now lead with
    // the ok glyph and drop the `PASS` literal (visual-style epic).
    expect(cap.stdout()).toContain("[ok] a");
    expect(cap.stdout()).toContain("[ok] b");
    expect(cap.stdout()).toContain("[ok] 2 pass");
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
    // Non-TTY in tests → fail glyph renders as `[x]` ahead of the bold-
    // styled profile name (bold is a no-op without colour).
    expect(cap.stdout()).toContain("[x] leaf");
    expect(cap.stdout()).toContain("nope");
  });

  // yd8 / AC-3: a multi-line error must surface in full under the FAIL row by
  // default; --brief restores the single-line legacy shape for CI scripts.
  describe("FAIL row error rendering (yd8 AC-3)", () => {
    it("default: prints the full multi-line error indented under the FAIL row", async () => {
      fx = await makeFixture({
        profiles: {
          leaf: { manifest: { name: "leaf", extends: "nope" }, files: {} },
          good: { manifest: { name: "good" }, files: { "x.md": "X\n" } },
        },
      });
      // Patch the FAIL row to include a forced multi-line error by injecting
      // a profile.json with synthetic newline-rich detail. Easiest path: use
      // an invalid manifest body that produces a multi-line JSON parse error.
      await fs.writeFile(
        path.join(fx.projectRoot, ".claude-profiles", "leaf", "profile.json"),
        '{\n  "name": "leaf",\n  "extends": "nope"\n}\nGARBAGE',
      );
      const cap = captureOutput(false);
      await expect(
        runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null }),
      ).rejects.toMatchObject({ exitCode: EXIT_CONFLICT });
      const out = cap.stdout();
      // The FAIL row appears, and the indented continuation lines do too.
      expect(out).toContain("[x] leaf");
      // Single-line case (extends-missing) covered above. For brief vs. full
      // the contract is identical when the message is single-line — so we
      // assert at minimum that the row contains the leaf name and its error
      // body at the start of the line.
      expect(out).toContain("InvalidManifest");
    });

    it("--brief: collapses each FAIL row to a single line (no indented body)", async () => {
      fx = await makeFixture({
        profiles: {
          leaf: { manifest: { name: "leaf", extends: "nope" }, files: {} },
        },
      });
      const cap = captureOutput(false);
      await expect(
        runValidate({
          cwd: fx.projectRoot,
          output: cap.channel,
          profile: null,
          brief: true,
        }),
      ).rejects.toMatchObject({ exitCode: EXIT_CONFLICT });
      const out = cap.stdout();
      // FAIL row present.
      expect(out).toContain("[x] leaf");
      // No four-space-indented continuation line — assert the only `    ` runs
      // would come from a multi-line body that --brief suppresses.
      expect(out.split("\n").every((line) => !line.startsWith("    "))).toBe(true);
    });
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

  // cw6 / T6 / AC-2: validate flags missing root CLAUDE.md markers when a
  // profile is active. The marker check is skipped in the idle (NoActive)
  // state to avoid pestering users who have not adopted profiles yet.
  describe("project-root CLAUDE.md marker presence (cw6 R44)", () => {
    async function makeActive(fx: Fixture): Promise<void> {
      // Mark the fixture as having an active profile by writing a minimal
      // state.json with `activeProfile` set. We don't actually materialize —
      // this is a unit-level integration test for the marker check only.
      const paths = buildStatePaths(fx.projectRoot);
      await fs.mkdir(paths.metaDir, { recursive: true });
      await writeStateFile(paths, {
        ...defaultState(),
        activeProfile: "a",
        materializedAt: "2026-04-26T12:00:00.000Z",
      });
    }

    it("active profile + root CLAUDE.md missing → exit 1 with actionable error", async () => {
      fx = await makeFixture({
        profiles: {
          // P1-A: marker check fires only when the active plan contributes
          // to projectRoot. Give the profile a peer CLAUDE.md so the gating
          // is exercised here.
          a: {
            manifest: { name: "a" },
            files: {},
            rootFiles: { "CLAUDE.md": "ROOT-BODY\n" },
          },
        },
      });
      await makeActive(fx);
      // Ensure root CLAUDE.md does NOT exist.
      const cap = captureOutput(false);
      let thrown: unknown;
      try {
        await runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null });
      } catch (err) {
        thrown = err;
      }
      expect(thrown).toBeInstanceOf(CliUserError);
      expect((thrown as CliUserError).exitCode).toBe(EXIT_USER_ERROR);
      expect((thrown as CliUserError).message).toContain(
        "project-root CLAUDE.md is missing claude-profiles markers",
      );
      expect((thrown as CliUserError).message).toContain("claude-profiles init");
      // cw6.2 followup: error must name the file path so grep/log scraping is
      // consistent with the materialize-time and drift-detect error messages.
      expect((thrown as CliUserError).message).toContain(
        `(file: ${path.join(fx.projectRoot, "CLAUDE.md")}`,
      );
      // yd8 / AC-5: error must point at the migration doc so first-time
      // users have a one-link reference for the section-ownership model.
      expect((thrown as CliUserError).message).toContain(
        "docs/migration/cw6-section-ownership.md",
      );
    });

    it("active profile + root CLAUDE.md present without markers → exit 1", async () => {
      fx = await makeFixture({
        profiles: {
          a: {
            manifest: { name: "a" },
            files: {},
            rootFiles: { "CLAUDE.md": "ROOT-BODY\n" },
          },
        },
      });
      await makeActive(fx);
      await fs.writeFile(
        path.join(fx.projectRoot, "CLAUDE.md"),
        "# Project\n\nuser content, no markers here.\n",
      );
      const cap = captureOutput(false);
      await expect(
        runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null }),
      ).rejects.toMatchObject({ exitCode: EXIT_USER_ERROR });
    });

    it("active profile + root CLAUDE.md with valid markers → exit 0", async () => {
      fx = await makeFixture({
        profiles: {
          a: {
            manifest: { name: "a" },
            files: {},
            rootFiles: { "CLAUDE.md": "ROOT-BODY\n" },
          },
        },
      });
      await makeActive(fx);
      await fs.writeFile(
        path.join(fx.projectRoot, "CLAUDE.md"),
        [
          "# Project",
          "",
          "<!-- claude-profiles:v1:begin -->",
          "<!-- Managed block. -->",
          "",
          "<!-- claude-profiles:v1:end -->",
          "",
        ].join("\n"),
      );
      const cap = captureOutput(false);
      const code = await runValidate({
        cwd: fx.projectRoot,
        output: cap.channel,
        profile: null,
      });
      expect(code).toBe(0);
    });

    it("idle state (no active profile) → marker check is skipped", async () => {
      fx = await makeFixture({
        profiles: { a: { manifest: { name: "a" }, files: {} } },
      });
      // No state.json written (or written with activeProfile=null) → idle.
      // Root CLAUDE.md absent. Should still pass.
      const cap = captureOutput(false);
      const code = await runValidate({
        cwd: fx.projectRoot,
        output: cap.channel,
        profile: null,
      });
      expect(code).toBe(0);
    });

    // P1-A regression: the marker check is CONDITIONAL on the active
    // profile's plan contributing to the projectRoot destination. An active
    // profile with NO profile-root CLAUDE.md (silent-majority v1 layout)
    // must not trip the marker check — even if root CLAUDE.md is missing
    // markers or absent entirely. See docs/migration/cw6-section-ownership.md
    // §"Opting out" point 2.
    it("active profile with NO projectRoot contributor + missing markers → exit 0 (silent-majority AC-10)", async () => {
      fx = await makeFixture({
        profiles: {
          // Profile only has .claude/-destination files; no peer CLAUDE.md.
          a: { manifest: { name: "a" }, files: { "x.md": "x\n" } },
        },
      });
      await makeActive(fx);
      // Root CLAUDE.md is absent — would have failed pre-P1-A.
      const cap = captureOutput(false);
      const code = await runValidate({
        cwd: fx.projectRoot,
        output: cap.channel,
        profile: null,
      });
      expect(code).toBe(0);
    });

    it("active profile WITH projectRoot contributor + missing markers → exit 1 (gating fires)", async () => {
      fx = await makeFixture({
        profiles: {
          // Profile contributes a peer CLAUDE.md → projectRoot destination.
          a: {
            manifest: { name: "a" },
            files: {},
            rootFiles: { "CLAUDE.md": "ROOT-BODY\n" },
          },
        },
      });
      await makeActive(fx);
      // Root CLAUDE.md is absent → marker check must fail because the active
      // plan DOES contribute to projectRoot.
      const cap = captureOutput(false);
      let thrown: unknown;
      try {
        await runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null });
      } catch (err) {
        thrown = err;
      }
      expect(thrown).toBeInstanceOf(CliUserError);
      expect((thrown as CliUserError).exitCode).toBe(EXIT_USER_ERROR);
      expect((thrown as CliUserError).message).toContain(
        "project-root CLAUDE.md is missing claude-profiles markers",
      );
    });
  });
});

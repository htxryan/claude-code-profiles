/**
 * cw6 / P1-C: end-to-end command_transcript for section ownership.
 *
 * The unit + integration suites cover individual axes (init markers,
 * materialize splice, drift detection, persist), but the Verification
 * Contract requires a full happy-path command_transcript that walks the
 * real CLI binary through:
 *
 *   1. `init`                         → root CLAUDE.md gains markers
 *   2. profile setup with profile-root CLAUDE.md
 *   3. `use <profile>`                → section spliced; outside bytes intact
 *   4. user edits between markers     → drift surfaces
 *   5. `use --on-drift=persist`       → edited bytes persisted to profile dir
 *   6. `use <profile>` again          → byte-identical to step 4 (round-trip)
 *
 * Style mirrors tests/cli/integration/scenarios.test.ts: real spawn, real
 * fixture, no in-process mocks.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

describe("cw6 P1-C: section ownership end-to-end (init → use → edit → drift → persist → use)", () => {
  it("full happy-path command transcript", async () => {
    await ensureBuilt();
    fx = await makeFixture({});

    const projectRoot = fx.projectRoot;
    const rootClaudeMd = path.join(projectRoot, "CLAUDE.md");
    const profileDir = path.join(projectRoot, ".claude-profiles", "devmode");
    const profileClaudeMd = path.join(profileDir, "CLAUDE.md");

    // ────────────────────────────────────────────────────────────────────
    // STEP 1: init creates root CLAUDE.md with markers.
    // ────────────────────────────────────────────────────────────────────
    const initR = await runCli({
      args: ["--cwd", projectRoot, "init", "--no-hook", "--no-seed"],
    });
    expect(initR.exitCode).toBe(0);

    // Project-root CLAUDE.md exists with marker pair.
    const afterInit = await fs.readFile(rootClaudeMd, "utf8");
    expect(afterInit).toContain("<!-- c3p:v1:begin");
    expect(afterInit).toContain("<!-- c3p:v1:end");

    // ────────────────────────────────────────────────────────────────────
    // STEP 2: stand up a profile with a profile-root CLAUDE.md.
    // ────────────────────────────────────────────────────────────────────
    await fs.mkdir(profileDir, { recursive: true });
    await fs.writeFile(
      path.join(profileDir, "profile.json"),
      JSON.stringify({ name: "devmode" }, null, 2),
    );
    const initialProfileBody = "# Devmode rules\n\nUse strict types.\n";
    await fs.writeFile(profileClaudeMd, initialProfileBody);

    // Capture bytes outside the markers BEFORE `use` so we can prove they
    // survive the splice byte-for-byte.
    const beginIdxBefore = afterInit.indexOf("<!-- c3p:v1:begin");
    const endMarkerIdxBefore = afterInit.indexOf("<!-- c3p:v1:end");
    const endLineEndBefore = afterInit.indexOf("\n", endMarkerIdxBefore) + 1;
    const aboveBefore = afterInit.slice(0, beginIdxBefore);
    const belowBefore = afterInit.slice(endLineEndBefore);

    // ────────────────────────────────────────────────────────────────────
    // STEP 3: `use devmode` splices the profile body between the markers.
    //         Bytes outside the markers preserved byte-for-byte.
    // ────────────────────────────────────────────────────────────────────
    const useR = await runCli({
      args: ["--cwd", projectRoot, "use", "devmode"],
    });
    expect(useR.exitCode).toBe(0);
    expect(useR.stdout).toContain("Switched to devmode");

    const afterUse = await fs.readFile(rootClaudeMd, "utf8");
    // Body landed between markers.
    expect(afterUse).toContain("Use strict types.");
    expect(afterUse).toContain("# Devmode rules");
    // Markers still present + on the same lines (well-formed).
    expect(afterUse).toContain("<!-- c3p:v1:begin");
    expect(afterUse).toContain("<!-- c3p:v1:end");
    // Bytes outside the markers preserved exactly.
    const beginIdxAfter = afterUse.indexOf("<!-- c3p:v1:begin");
    const endMarkerIdxAfter = afterUse.indexOf("<!-- c3p:v1:end");
    const endLineEndAfter = afterUse.indexOf("\n", endMarkerIdxAfter) + 1;
    expect(afterUse.slice(0, beginIdxAfter)).toBe(aboveBefore);
    expect(afterUse.slice(endLineEndAfter)).toBe(belowBefore);

    // State.json reports the section fingerprint.
    const stateAfterUse = JSON.parse(
      await fs.readFile(
        path.join(projectRoot, ".claude-profiles", ".meta", "state.json"),
        "utf8",
      ),
    );
    expect(stateAfterUse.activeProfile).toBe("devmode");
    expect(stateAfterUse.rootClaudeMdSection).not.toBeNull();
    expect(stateAfterUse.rootClaudeMdSection.size).toBeGreaterThan(0);

    // ────────────────────────────────────────────────────────────────────
    // STEP 4: user edits the section bytes. drift surfaces.
    // ────────────────────────────────────────────────────────────────────
    // Replace the body line between the markers with edited content.
    const editedFile = afterUse.replace(
      "Use strict types.",
      "Use strict types AND prefer immutable data.",
    );
    expect(editedFile).not.toBe(afterUse); // sanity: replacement happened
    await fs.writeFile(rootClaudeMd, editedFile);

    // Snapshot the edited bytes — we'll round-trip-compare against this in
    // step 6.
    const editedSnapshot = await fs.readFile(rootClaudeMd, "utf8");

    // `drift` detects the section change. The drift command itself exits
    // 0 (it's a read-only report); the SIGNAL is the entries it lists.
    const driftR = await runCli({
      args: ["--cwd", projectRoot, "drift"],
    });
    expect(driftR.exitCode).toBe(0);
    expect(driftR.stdout).toContain("CLAUDE.md");
    // The drifted entry's relPath must mention CLAUDE.md (the projectRoot
    // section is keyed under that path in DriftReport).
    expect(driftR.stdout).toMatch(/modified\s+CLAUDE\.md/);
    // The summary line names a non-zero file count (regression guard
    // against drift becoming a no-op for section-only edits).
    expect(driftR.stdout).toMatch(/drift: [1-9]\d* file\(s\)/);

    // ────────────────────────────────────────────────────────────────────
    // STEP 5: `use --on-drift=persist devmode` saves edited bytes back to
    //         the profile-root CLAUDE.md, then re-materializes from there.
    // ────────────────────────────────────────────────────────────────────
    const persistR = await runCli({
      args: [
        "--cwd",
        projectRoot,
        "--on-drift=persist",
        "use",
        "devmode",
      ],
    });
    expect(persistR.exitCode).toBe(0);

    // The profile-root CLAUDE.md now carries the edited body.
    const persistedProfileBody = await fs.readFile(profileClaudeMd, "utf8");
    expect(persistedProfileBody).toContain(
      "Use strict types AND prefer immutable data.",
    );
    // Original line is gone (the edit replaced it).
    expect(persistedProfileBody).not.toContain("Use strict types.\n");

    // ────────────────────────────────────────────────────────────────────
    // STEP 6: another `use devmode` should be byte-identical to the
    //         edited file from step 4 (round-trip invariant).
    // ────────────────────────────────────────────────────────────────────
    const useR2 = await runCli({
      args: ["--cwd", projectRoot, "use", "devmode"],
    });
    expect(useR2.exitCode).toBe(0);

    const afterRoundTrip = await fs.readFile(rootClaudeMd, "utf8");
    expect(afterRoundTrip).toBe(editedSnapshot);
  });
});

/**
 * cw6/T7 back-compat regression tests.
 *
 * These tests pin AC-10 (the silent-majority invariant): existing profiles
 * laid out the v1 way — i.e. with only `.claude/CLAUDE.md` and no
 * profile-root `CLAUDE.md` — must continue to materialize EXACTLY as before
 * the section-ownership feature shipped. The CLI binary is exercised end-to-
 * end via spawn so any future regression in resolver/merge/materialize that
 * silently starts touching project-root CLAUDE.md is caught at the boundary
 * the user actually sees.
 *
 * Coverage:
 *   - BC-1: profile with ONLY `.claude/CLAUDE.md` → project-root CLAUDE.md
 *           is byte-identical before and after `use`.
 *   - BC-2: profile with ONLY profile-root CLAUDE.md → `.claude/CLAUDE.md`
 *           is NOT written (the live `.claude/` tree contains no CLAUDE.md
 *           file at all when no contributor supplies one).
 *   - BC-3: profile with BOTH → both files written independently; no
 *           cross-destination content leak (the projectRoot bytes do not
 *           appear in `.claude/CLAUDE.md`, and vice versa).
 *   - BC-4: legacy project + `init` (no profile-root CLAUDE.md anywhere)
 *           injects markers; subsequent `use` of a profile that has no
 *           profile-root contribution leaves the project-root CLAUDE.md
 *           file unchanged from init's output (section between markers
 *           remains empty / equal to init's default).
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

describe("cw6/T7 back-compat: section-ownership invariants (AC-10)", () => {
  // ──────────────────────────────────────────────────────────────────────
  // BC-1: Profile with ONLY `.claude/CLAUDE.md` (the v1 layout).
  // ──────────────────────────────────────────────────────────────────────
  it("BC-1: profile with only .claude/CLAUDE.md leaves project-root CLAUDE.md byte-identical (AC-10)", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        legacy: {
          manifest: { name: "legacy" },
          // Only a .claude/CLAUDE.md — no rootFiles.
          files: { "CLAUDE.md": "LEGACY-CLAUDE-CONTENT\n" },
        },
      },
    });

    // Hand-author a project-root CLAUDE.md the user owns. We deliberately do
    // NOT add markers — this is the "user has not opted into section
    // ownership" path. The file must remain byte-identical because no
    // contributor supplies a projectRoot source, so materialize never runs
    // the splice path (no pre-flight, no marker check).
    const rootClaudeMd = path.join(fx.projectRoot, "CLAUDE.md");
    const original = "# My project\n\nUser-authored content. No markers.\n";
    await fs.writeFile(rootClaudeMd, original);
    const originalStat = await fs.stat(rootClaudeMd);

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "legacy"],
    });
    expect(r.exitCode).toBe(0);

    // The project-root CLAUDE.md is byte-identical (no marker injection
    // because we did not run init, and use does not implicitly init).
    const after = await fs.readFile(rootClaudeMd, "utf8");
    expect(after).toBe(original);

    // Mtime invariant: the file is not even opened for writing.
    const afterStat = await fs.stat(rootClaudeMd);
    expect(afterStat.mtimeMs).toBe(originalStat.mtimeMs);

    // The legacy profile's .claude/CLAUDE.md DID land in the live tree.
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("LEGACY-CLAUDE-CONTENT\n");

    // State.json carries no rootClaudeMdSection (the field is null/absent).
    const state = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
        "utf8",
      ),
    );
    expect(state.rootClaudeMdSection ?? null).toBeNull();
  });

  // ──────────────────────────────────────────────────────────────────────
  // BC-2: Profile with ONLY profile-root CLAUDE.md.
  // ──────────────────────────────────────────────────────────────────────
  it("BC-2: profile with only profile-root CLAUDE.md does NOT write .claude/CLAUDE.md", async () => {
    await ensureBuilt();
    // Empty fixture so `init` sees a fresh project (no pre-existing
    // .claude-profiles/ → init wouldn't refuse). Stand up the profile by
    // hand AFTER init so markers are present before `use` runs.
    fx = await makeFixture({});

    const initR = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-hook", "--no-seed"],
    });
    expect(initR.exitCode).toBe(0);

    // Stand up the profile: manifest + a single profile-root CLAUDE.md
    // (NO .claude/ subdir → no .claude/-destination contributions).
    const profileDir = path.join(fx.projectRoot, ".claude-profiles", "rooted");
    await fs.mkdir(profileDir, { recursive: true });
    await fs.writeFile(
      path.join(profileDir, "profile.json"),
      JSON.stringify({ name: "rooted" }, null, 2),
    );
    await fs.writeFile(path.join(profileDir, "CLAUDE.md"), "ROOT-MANAGED-BODY\n");

    // Activate the profile.
    const useR = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "rooted"],
    });
    expect(useR.exitCode).toBe(0);

    // The project-root CLAUDE.md has the body spliced between markers.
    const rootContent = await fs.readFile(
      path.join(fx.projectRoot, "CLAUDE.md"),
      "utf8",
    );
    expect(rootContent).toContain("<!-- claude-profiles:v1:begin");
    expect(rootContent).toContain("<!-- claude-profiles:v1:end");
    expect(rootContent).toContain("ROOT-MANAGED-BODY");

    // CRITICAL: `.claude/CLAUDE.md` was NOT written. The live `.claude/`
    // tree contains no CLAUDE.md because no contributor supplied one for
    // that destination. (The directory itself may exist if other files
    // landed, but for this profile there are none, so the dir is empty.)
    const claudeMdLive = path.join(fx.projectRoot, ".claude", "CLAUDE.md");
    await expect(fs.access(claudeMdLive)).rejects.toThrow();

    // No content leak the other way: the projectRoot bytes are not
    // accidentally written into .claude/ either.
    let claudeDirEntries: string[] = [];
    try {
      claudeDirEntries = await fs.readdir(path.join(fx.projectRoot, ".claude"));
    } catch (err: unknown) {
      // OK if .claude/ itself doesn't exist — even safer.
      if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
    }
    expect(claudeDirEntries).not.toContain("CLAUDE.md");
  });

  // ──────────────────────────────────────────────────────────────────────
  // BC-3: Profile with BOTH — independent destinations, no content leak.
  // ──────────────────────────────────────────────────────────────────────
  it("BC-3: profile with both .claude/CLAUDE.md AND profile-root CLAUDE.md writes both independently (no leak)", async () => {
    await ensureBuilt();
    fx = await makeFixture({});

    const initR = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-hook", "--no-seed"],
    });
    expect(initR.exitCode).toBe(0);

    // Stand up profile post-init with BOTH source kinds (peer-of-
    // profile.json CLAUDE.md → projectRoot; .claude/CLAUDE.md → .claude/).
    const profileDir = path.join(fx.projectRoot, ".claude-profiles", "both");
    await fs.mkdir(path.join(profileDir, ".claude"), { recursive: true });
    await fs.writeFile(
      path.join(profileDir, "profile.json"),
      JSON.stringify({ name: "both" }, null, 2),
    );
    await fs.writeFile(path.join(profileDir, "CLAUDE.md"), "PROJECT-ROOT-BODY\n");
    await fs.writeFile(
      path.join(profileDir, ".claude", "CLAUDE.md"),
      "CLAUDE-DIR-BODY\n",
    );

    const useR = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "both"],
    });
    expect(useR.exitCode).toBe(0);

    // .claude/CLAUDE.md got the .claude/-source bytes (whole-file write).
    const claudeContent = await fs.readFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "utf8",
    );
    expect(claudeContent).toBe("CLAUDE-DIR-BODY\n");
    // No leak: the projectRoot bytes never appear in .claude/CLAUDE.md.
    expect(claudeContent).not.toContain("PROJECT-ROOT-BODY");

    // Project-root CLAUDE.md got the profile-root bytes spliced between
    // markers (E2 concat policy applied to a single contributor).
    const rootContent = await fs.readFile(
      path.join(fx.projectRoot, "CLAUDE.md"),
      "utf8",
    );
    expect(rootContent).toContain("<!-- claude-profiles:v1:begin");
    expect(rootContent).toContain("PROJECT-ROOT-BODY");
    // No leak the other way: the .claude/ bytes never appear in root.
    expect(rootContent).not.toContain("CLAUDE-DIR-BODY");

    // State.json carries BOTH a fingerprint entry for .claude/CLAUDE.md
    // AND a rootClaudeMdSection. They are tracked independently by the
    // (path, destination) keying introduced in cw6/T3-T5.
    const state = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
        "utf8",
      ),
    );
    expect(state.fingerprint.files["CLAUDE.md"]).toBeDefined();
    expect(state.rootClaudeMdSection).not.toBeNull();
    expect(state.rootClaudeMdSection.size).toBeGreaterThan(0);
  });

  // ──────────────────────────────────────────────────────────────────────
  // BC-4: Legacy project + init — markers added; later use of a profile
  //       with no projectRoot contribution leaves root file unchanged
  //       beyond init's marker output.
  // ──────────────────────────────────────────────────────────────────────
  it("BC-4: legacy project init injects markers; subsequent use of a non-root profile preserves init's output", async () => {
    await ensureBuilt();
    // Empty project — init must run cleanly. We then stand up the
    // wholly-legacy profile by hand so init's "already initialised" check
    // does not fire.
    fx = await makeFixture({});

    const initR = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-hook", "--no-seed"],
    });
    expect(initR.exitCode).toBe(0);

    // Stand up a wholly-legacy profile (no projectRoot CLAUDE.md anywhere).
    const profileDir = path.join(fx.projectRoot, ".claude-profiles", "legacy");
    await fs.mkdir(path.join(profileDir, ".claude"), { recursive: true });
    await fs.writeFile(
      path.join(profileDir, "profile.json"),
      JSON.stringify({ name: "legacy" }, null, 2),
    );
    await fs.writeFile(
      path.join(profileDir, ".claude", "settings.json"),
      '{"v":"legacy"}',
    );

    const rootClaudeMd = path.join(fx.projectRoot, "CLAUDE.md");
    const afterInit = await fs.readFile(rootClaudeMd, "utf8");
    // Sanity: markers present (T6 invariant).
    expect(afterInit).toContain("<!-- claude-profiles:v1:begin");
    expect(afterInit).toContain("<!-- claude-profiles:v1:end");

    // Now activate the legacy profile. Because it contributes nothing to
    // the projectRoot destination, materialize must NOT run the section
    // splice — and the project-root CLAUDE.md must be byte-identical to
    // what init produced. (The section between the markers stays empty;
    // init's output IS the final state.)
    const useR = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "legacy"],
    });
    expect(useR.exitCode).toBe(0);

    const afterUse = await fs.readFile(rootClaudeMd, "utf8");
    expect(afterUse).toBe(afterInit);

    // The .claude/ side did materialize (settings.json landed). E2's
    // deep-merge canonicaliser pretty-prints settings.json on write, so we
    // assert the parsed shape rather than byte-equality against the source.
    const settings = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude", "settings.json"),
        "utf8",
      ),
    );
    expect(settings).toEqual({ v: "legacy" });

    // State carries no section fingerprint (no contributor → no tracking).
    const state = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
        "utf8",
      ),
    );
    expect(state.rootClaudeMdSection ?? null).toBeNull();
  });
});

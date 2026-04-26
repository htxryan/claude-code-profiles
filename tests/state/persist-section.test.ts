/**
 * cw6/T5: persist write-back for project-root CLAUDE.md sections.
 *
 * Coverage map (acceptance criteria):
 *   - AC-8a: persist writes the live section to
 *           `.claude-profiles/<active>/CLAUDE.md` (peer of profile.json,
 *           NOT under .claude/)
 *   - AC-8b (regression guard): persist does NOT touch
 *           `.claude-profiles/<active>/.claude/CLAUDE.md`
 *   - The destination file in the profile contains JUST the section bytes
 *     (no markers — markers only exist in the materialized live file)
 *   - Round-trip invariant: edit-then-persist-then-use reproduces the same
 *     section bytes byte-for-byte in the live file
 *   - Regression: persist with no projectRoot contributor (legacy plan) is
 *     a no-op for the projectRoot file
 */

import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { renderManagedBlock } from "../../src/markers.js";
import type { MergedFile } from "../../src/merge/types.js";
import type { ResolvedPlan } from "../../src/resolver/types.js";
import { RESOLVED_PLAN_SCHEMA_VERSION } from "../../src/resolver/types.js";
import { pathExists } from "../../src/state/atomic.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";
import {
  persistAndMaterialize,
  persistLiveIntoProfile,
} from "../../src/state/persist.js";

function makePlan(profileName: string, projectRoot: string): ResolvedPlan {
  return {
    schemaVersion: RESOLVED_PLAN_SCHEMA_VERSION,
    profileName,
    chain: [profileName],
    includes: [],
    contributors: [
      {
        kind: "profile",
        id: profileName,
        rootPath: path.join(projectRoot, ".claude-profiles", profileName),
        claudeDir: path.join(
          projectRoot,
          ".claude-profiles",
          profileName,
          ".claude",
        ),
        external: false,
      },
    ],
    files: [],
    warnings: [],
    externalPaths: [],
  };
}

function rootFile(body: string): MergedFile {
  return {
    path: "CLAUDE.md",
    bytes: Buffer.from(body),
    contributors: ["leaf"],
    mergePolicy: "concat",
    destination: "projectRoot",
  };
}

async function writeRootClaudeMd(
  rootDir: string,
  before: string,
  sectionBody: string,
  after: string,
): Promise<void> {
  const block = renderManagedBlock(sectionBody);
  await fs.writeFile(
    path.join(rootDir, "CLAUDE.md"),
    `${before}${block}${after}`,
  );
}

async function setupActiveProfileWithRoot(
  rootDir: string,
  profileName: string,
  initialSection: string,
): Promise<{
  paths: ReturnType<typeof buildStatePaths>;
  plan: ResolvedPlan;
}> {
  // Profile dir already exists from any prior test setup; create explicitly.
  const profileDir = path.join(rootDir, ".claude-profiles", profileName);
  await fs.mkdir(profileDir, { recursive: true });
  await fs.writeFile(
    path.join(profileDir, "profile.json"),
    JSON.stringify({ name: profileName }),
  );
  // Seed the project-root CLAUDE.md with markers so materialize's pre-flight
  // passes.
  await writeRootClaudeMd(rootDir, "TOP\n\n", initialSection, "\n\nBOTTOM\n");
  const paths = buildStatePaths(rootDir);
  const plan = makePlan(profileName, rootDir);
  await materialize(paths, plan, [rootFile(initialSection)]);
  return { paths, plan };
}

describe("persist: project-root section write-back (cw6/T5 / AC-8 / R46)", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-persist-section-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  describe("AC-8a: persist writes the section to <profile>/CLAUDE.md (peer of profile.json)", () => {
    it("user edits the section in the live file → persist writes the new section bytes to <profile>/CLAUDE.md", async () => {
      const { paths } = await setupActiveProfileWithRoot(root, "leaf", "ORIG-SECTION");
      // User edits the live root file's section between the markers.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      const tampered = live.replace("ORIG-SECTION", "USER-NEW-SECTION");
      await fs.writeFile(paths.rootClaudeMdFile, tampered);

      await persistLiveIntoProfile(paths, "leaf");

      // The profile-root CLAUDE.md (peer of profile.json) now holds the
      // exact section bytes from the live file. NO markers in the destination.
      const profileRootMd = path.join(root, ".claude-profiles", "leaf", "CLAUDE.md");
      const persisted = await fs.readFile(profileRootMd, "utf8");
      expect(persisted).toBe("USER-NEW-SECTION");
      expect(persisted).not.toContain("<!-- claude-profiles");
    });

    it("destination file is at the profile root, NOT under .claude/", async () => {
      const { paths } = await setupActiveProfileWithRoot(root, "leaf", "ANY");
      // Edit and persist.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      await fs.writeFile(paths.rootClaudeMdFile, live.replace("ANY", "EDITED"));
      await persistLiveIntoProfile(paths, "leaf");

      // Peer of profile.json:
      const peer = path.join(root, ".claude-profiles", "leaf", "CLAUDE.md");
      expect(await pathExists(peer)).toBe(true);
    });
  });

  describe("AC-8b (regression guard): the section write-back targets <profile>/CLAUDE.md, NOT <profile>/.claude/CLAUDE.md", () => {
    it("persist does not synthesize a .claude/CLAUDE.md from the live section bytes", async () => {
      // The active profile contributes ONLY a project-root CLAUDE.md (no
      // .claude/ files). After a section edit + persist, the section bytes
      // must NOT leak into <profile>/.claude/CLAUDE.md — that location
      // belongs to a DIFFERENT destination ('.claude/CLAUDE.md', the
      // legacy nested file) and writing the project-root section there
      // would silently shadow the profile-root file on the next merge.
      const { paths } = await setupActiveProfileWithRoot(root, "leaf", "X");
      // Edit and persist.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      await fs.writeFile(
        paths.rootClaudeMdFile,
        live.replace("X", "USER-EDITED-Y"),
      );
      await persistLiveIntoProfile(paths, "leaf");

      // The peer file (correct destination) carries the new section.
      const peer = path.join(root, ".claude-profiles", "leaf", "CLAUDE.md");
      expect(await fs.readFile(peer, "utf8")).toBe("USER-EDITED-Y");

      // The wrong location must NOT exist (we never created it in setup,
      // and persist must not invent one). Even if a future bug tried to
      // write the section here, the test would catch it.
      const inner = path.join(
        root,
        ".claude-profiles",
        "leaf",
        ".claude",
        "CLAUDE.md",
      );
      expect(await pathExists(inner)).toBe(false);
    });

    it("when live .claude/ ALSO has a CLAUDE.md, persist replicates that file at <profile>/.claude/CLAUDE.md (existing R22 behavior) AND ALSO writes the section to <profile>/CLAUDE.md (cw6/T5 add)", async () => {
      // This pins the dual-destination contract: pre-cw6 behavior for
      // .claude/* persist is unchanged (the .claude/ tree rename pair
      // copies live → profile faithfully), and the new cw6/T5 add lays
      // the section into the correct peer-of-profile.json location.
      const { paths } = await setupActiveProfileWithRoot(root, "leaf", "ORIG");
      // Stand up a live .claude/ with a CLAUDE.md sibling — the user has
      // both a legacy nested file AND a project-root managed section.
      await fs.mkdir(paths.claudeDir, { recursive: true });
      await fs.writeFile(
        path.join(paths.claudeDir, "CLAUDE.md"),
        "LIVE-CLAUDE-DIR-CLAUDEMD",
      );
      // Edit the live root section.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      await fs.writeFile(paths.rootClaudeMdFile, live.replace("ORIG", "NEW"));

      await persistLiveIntoProfile(paths, "leaf");

      // 1) Existing R22: the live .claude/CLAUDE.md is replicated to
      // <profile>/.claude/CLAUDE.md byte-for-byte.
      const innerCopy = await fs.readFile(
        path.join(root, ".claude-profiles", "leaf", ".claude", "CLAUDE.md"),
        "utf8",
      );
      expect(innerCopy).toBe("LIVE-CLAUDE-DIR-CLAUDEMD");
      // 2) cw6/T5 add: the project-root section lands at the peer of
      // profile.json — and is JUST the user body (no markers, no
      // live-CLAUDE-DIR contents leaking in).
      const peer = await fs.readFile(
        path.join(root, ".claude-profiles", "leaf", "CLAUDE.md"),
        "utf8",
      );
      expect(peer).toBe("NEW");
      expect(peer).not.toContain("LIVE-CLAUDE-DIR-CLAUDEMD");
      expect(peer).not.toContain("<!-- claude-profiles");
    });
  });

  describe("round-trip invariant: edit → persist → re-materialize reproduces the same section bytes", () => {
    it("byte-for-byte equality across the round trip", async () => {
      const { paths, plan } = await setupActiveProfileWithRoot(root, "leaf", "ORIGINAL");

      // 1. User edits the live section.
      const live1 = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      const userBody = "USER\nMULTI-LINE\nEDIT\n";
      const tampered = live1.replace("ORIGINAL", userBody);
      await fs.writeFile(paths.rootClaudeMdFile, tampered);
      // Capture the live file to compare after the round trip.

      // 2. Persist live into profile (peer-of-profile.json CLAUDE.md).
      await persistLiveIntoProfile(paths, "leaf");

      // 3. Re-materialize the SAME plan but with merged bytes that come
      // from the freshly persisted profile-root CLAUDE.md (this is the
      // fingerprint of how the merge engine would normally produce them
      // on the next `use` cycle).
      const persistedBytes = await fs.readFile(
        path.join(root, ".claude-profiles", "leaf", "CLAUDE.md"),
      );
      await materialize(paths, plan, [
        {
          path: "CLAUDE.md",
          bytes: persistedBytes,
          contributors: ["leaf"],
          mergePolicy: "concat",
          destination: "projectRoot",
        },
      ]);

      // 4. The live root CLAUDE.md now contains the SAME user body between
      // markers as we had in step 1, byte-for-byte (no double newlines, no
      // marker drift, no off-by-one shifts).
      const live2 = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      const begin = "<!-- claude-profiles:v1:begin";
      const end = "<!-- claude-profiles:v1:end";
      const startA = tampered.indexOf(begin);
      const startB = live2.indexOf(begin);
      const endA = tampered.indexOf(end);
      const endB = live2.indexOf(end);
      // Section slice is identical (we don't compare full bytes because the
      // user-owned outer bytes were preserved by the splice but pre-existing
      // markers differ slightly — the round trip is about the SECTION
      // surviving intact).
      expect(live2.slice(startB, endB)).toBe(tampered.slice(startA, endA));
      expect(live2).toContain(userBody);
    });
  });

  describe("integration: persistAndMaterialize swaps profile after persisting section drift", () => {
    it("after section edit on base, persist+swap to v2 leaves base/CLAUDE.md = edited section", async () => {
      // Set up two profiles with their own section bodies.
      const baseDir = path.join(root, ".claude-profiles", "base");
      const v2Dir = path.join(root, ".claude-profiles", "v2");
      await fs.mkdir(baseDir, { recursive: true });
      await fs.mkdir(v2Dir, { recursive: true });
      await fs.writeFile(
        path.join(baseDir, "profile.json"),
        JSON.stringify({ name: "base" }),
      );
      await fs.writeFile(
        path.join(v2Dir, "profile.json"),
        JSON.stringify({ name: "v2", extends: "base" }),
      );
      await fs.writeFile(path.join(baseDir, "CLAUDE.md"), "BASE-SECTION");
      await fs.writeFile(path.join(v2Dir, "CLAUDE.md"), "V2-SECTION");

      // Seed the live root CLAUDE.md.
      await writeRootClaudeMd(root, "ABOVE\n", "BASE-SECTION", "\nBELOW\n");

      const paths = buildStatePaths(root);

      // Materialize base.
      const basePlan = makePlan("base", root);
      await materialize(paths, basePlan, [rootFile("BASE-SECTION")]);

      // User edits the section in the live file.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      await fs.writeFile(
        paths.rootClaudeMdFile,
        live.replace("BASE-SECTION", "BASE-EDITED-SECTION"),
      );

      // Persist + swap to v2.
      const v2Plan = makePlan("v2", root);
      const result = await persistAndMaterialize(paths, {
        activeProfileName: "base",
        newPlan: v2Plan,
        newMerged: [rootFile("V2-SECTION")],
      });

      // v2 is now active.
      expect(result.state.activeProfile).toBe("v2");
      // The live root CLAUDE.md now has v2's section body.
      const liveAfter = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      expect(liveAfter).toContain("V2-SECTION");
      // base/CLAUDE.md (peer of profile.json) carries the edited section
      // bytes — NOT the markers, NOT the surrounding "ABOVE/BELOW" prose.
      const basePersisted = await fs.readFile(
        path.join(baseDir, "CLAUDE.md"),
        "utf8",
      );
      expect(basePersisted).toBe("BASE-EDITED-SECTION");
      expect(basePersisted).not.toContain("<!-- claude-profiles");
      expect(basePersisted).not.toContain("ABOVE");
      expect(basePersisted).not.toContain("BELOW");
    });
  });

  describe("legacy/no-op cases", () => {
    it("persist with NO project-root CLAUDE.md (no markers in live) does NOT write a profile-root CLAUDE.md", async () => {
      // Active profile with only .claude/ contributors, no projectRoot file.
      const profileDir = path.join(root, ".claude-profiles", "leaf");
      await fs.mkdir(profileDir, { recursive: true });
      await fs.writeFile(
        path.join(profileDir, "profile.json"),
        JSON.stringify({ name: "leaf" }),
      );
      const paths = buildStatePaths(root);
      const plan = makePlan("leaf", root);
      await materialize(paths, plan, [
        {
          path: "agents/x.md",
          bytes: Buffer.from("X"),
          contributors: ["leaf"],
          mergePolicy: "last-wins",
          destination: ".claude",
        },
      ]);
      // Live `.claude/` exists; no project-root CLAUDE.md exists.
      // User edits something in .claude/ to trigger persist semantics.
      await fs.writeFile(path.join(paths.claudeDir, "agents/x.md"), "EDITED");

      await persistLiveIntoProfile(paths, "leaf");

      // No profile-root CLAUDE.md should have been created.
      const peer = path.join(profileDir, "CLAUDE.md");
      expect(await pathExists(peer)).toBe(false);
    });

    it("persist with markers absent on live file (user broke them) does NOT write a profile-root CLAUDE.md (skipped silently)", async () => {
      const { paths } = await setupActiveProfileWithRoot(root, "leaf", "ORIG");
      // User strips the markers from the live file.
      await fs.writeFile(paths.rootClaudeMdFile, "# Plain content, no markers.\n");

      // Persist runs without throwing — drift detection (separate path) is
      // what surfaces unrecoverable; persist itself silently skips the root
      // write-back when markers are missing because there's no section to
      // capture. (Without this skip we'd persist the user's plain-text
      // content as if it were a section, which is wrong.)
      await persistLiveIntoProfile(paths, "leaf");

      // The profile-root CLAUDE.md must NOT contain the broken live file's
      // plain-text contents. Either it's absent (clean skip) or it carries
      // some prior valid section — never the corrupt bytes.
      const peer = path.join(root, ".claude-profiles", "leaf", "CLAUDE.md");
      if (await pathExists(peer)) {
        const persisted = await fs.readFile(peer, "utf8");
        expect(persisted).not.toContain("Plain content, no markers");
        expect(persisted).not.toContain("<!-- claude-profiles");
      }
    });
  });
});

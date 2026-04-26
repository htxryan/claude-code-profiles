/**
 * cw6/T4: section-splice materialize for projectRoot CLAUDE.md.
 *
 * Coverage map (acceptance criteria):
 *   - AC-5: section-splice preserves bytes outside markers (byte-equality)
 *   - AC-6a: missing markers → exit 1 with actionable error, file unchanged
 *   - AC-6b: missing markers → .claude/ ALSO unchanged (atomic-across-
 *           destinations — this is the critical test)
 *   - idempotent re-apply leaves projectRoot file byte-identical
 *   - schema migration: legacy state.json without rootClaudeMdSection loads
 *   - reconcile sweeps leftover <projectRoot>/CLAUDE.md.*.tmp
 *
 * Marker regex / parser tests for malformed cases (multi-block, version
 * mismatch, etc.) live in tests/markers.test.ts (cw6/T6); we exercise
 * end-to-end abort behavior here to prove materialize wires through to
 * the parser correctly.
 */

import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { RootClaudeMdMarkersMissingError } from "../../src/errors/index.js";
import { renderManagedBlock } from "../../src/markers.js";
import type { MergedFile } from "../../src/merge/types.js";
import type { ResolvedPlan } from "../../src/resolver/types.js";
import { RESOLVED_PLAN_SCHEMA_VERSION } from "../../src/resolver/types.js";
import { pathExists } from "../../src/state/atomic.js";
import { materialize } from "../../src/state/materialize.js";
import {
  buildStatePaths,
  isRootClaudeMdTmpName,
  rootClaudeMdTmpPath,
} from "../../src/state/paths.js";
import { reconcileMaterialize } from "../../src/state/reconcile.js";
import { readStateFile } from "../../src/state/state-file.js";

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

function claudeFile(rel: string, body: string): MergedFile {
  return {
    path: rel,
    bytes: Buffer.from(body),
    contributors: ["leaf"],
    mergePolicy: "last-wins",
    destination: ".claude",
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

describe("materialize: projectRoot section splice (cw6/T4 / R45)", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-section-splice-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  /**
   * Helper: write a CLAUDE.md with markers around a chosen body. Mirrors
   * what `init` would produce. The "before" and "after" slices are arbitrary
   * user content that materialize must preserve byte-for-byte.
   */
  async function writeRootClaudeMdWithMarkers(opts: {
    before: string;
    sectionBody: string;
    after: string;
  }): Promise<{ filePath: string; full: string }> {
    const block = renderManagedBlock(opts.sectionBody);
    const full = `${opts.before}${block}${opts.after}`;
    const filePath = path.join(root, "CLAUDE.md");
    await fs.writeFile(filePath, full);
    return { filePath, full };
  }

  describe("AC-5: section-splice preserves bytes outside markers", () => {
    it("splices new section between markers; bytes above :begin and below :end are byte-identical", async () => {
      const before = "# My project\n\nHere is some user-owned prose.\n\n";
      const after = "\n## Notes from the user\n\nMore content below.\n";
      await writeRootClaudeMdWithMarkers({
        before,
        sectionBody: "OLD-SECTION",
        after,
      });

      const paths = buildStatePaths(root);
      const plan = makePlan("leaf", root);
      const merged: MergedFile[] = [
        claudeFile("agents/x.md", "AGENT-X"),
        rootFile("NEW-SECTION-BODY"),
      ];

      await materialize(paths, plan, merged);

      const after_ = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      // Bytes above :begin marker preserved exactly.
      expect(after_.startsWith(before)).toBe(true);
      // Bytes below :end marker preserved exactly.
      expect(after_.endsWith(after)).toBe(true);
      // The new section body appears between the markers.
      expect(after_).toContain("NEW-SECTION-BODY");
      expect(after_).not.toContain("OLD-SECTION");
      // .claude/ tree also landed.
      expect(
        await fs.readFile(path.join(paths.claudeDir, "agents/x.md"), "utf8"),
      ).toBe("AGENT-X");
    });

    it("preserves user content with edge bytes (trailing whitespace, multiple blank lines, special chars)", async () => {
      const before = "  # Leading spaces preserved\n\n\n\nλ unicode test\n";
      const after = "\n\n\nTrailing\twith\ttabs\n  ";
      await writeRootClaudeMdWithMarkers({
        before,
        sectionBody: "old",
        after,
      });

      const paths = buildStatePaths(root);
      const plan = makePlan("leaf", root);
      await materialize(paths, plan, [rootFile("NEW")]);

      const result = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      const beginIdx = result.indexOf("<!-- claude-profiles:v1:begin");
      const endIdx = result.indexOf("<!-- claude-profiles:v1:end");
      // Slice OUT the markers + body region; head and tail must equal the
      // recorded user content byte-for-byte.
      expect(result.slice(0, beginIdx)).toBe(before);
      // The end marker line ends with a trailing newline emitted by
      // renderManagedBlock; everything AFTER that newline is user-owned.
      const endMarkerLineEnd = result.indexOf("\n", endIdx) + 1;
      expect(result.slice(endMarkerLineEnd)).toBe(after);
    });

    it("records section-only fingerprint in state.json (R46)", async () => {
      await writeRootClaudeMdWithMarkers({
        before: "USER\n",
        sectionBody: "old",
        after: "\nUSER2\n",
      });
      const paths = buildStatePaths(root);
      await materialize(paths, makePlan("leaf", root), [rootFile("HELLO")]);
      const r = await readStateFile(paths);
      expect(r.warning).toBeNull();
      expect(r.state.rootClaudeMdSection).not.toBeNull();
      expect(r.state.rootClaudeMdSection!.size).toBe(Buffer.from("HELLO").length);
      // Hash should match the bytes we wrote — not the whole-file hash.
      const fullFile = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      expect(r.state.rootClaudeMdSection!.size).toBeLessThan(fullFile.length);
    });

    it("AC-9-mirror: concat semantics across two contributors (single MergedFile result)", async () => {
      // E2 already concatenates contributors into one MergedFile per
      // (path, destination); we simulate the post-merge bytes here.
      await writeRootClaudeMdWithMarkers({
        before: "PRE\n",
        sectionBody: "old",
        after: "\nPOST\n",
      });
      const paths = buildStatePaths(root);
      await materialize(paths, makePlan("leaf", root), [
        rootFile("FROM-A\nFROM-B\n"),
      ]);
      const result = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      expect(result).toContain("FROM-A\nFROM-B\n");
    });

    it("plan with NO projectRoot contributor leaves project-root CLAUDE.md untouched (R10 back-compat)", async () => {
      const original = "# My project\n\nUser-owned content.\n";
      const filePath = path.join(root, "CLAUDE.md");
      await fs.writeFile(filePath, original);

      const paths = buildStatePaths(root);
      await materialize(paths, makePlan("leaf", root), [
        claudeFile("agents/y.md", "Y"),
      ]);

      // No markers, no rootMerged → file untouched (no pre-flight runs).
      const after = await fs.readFile(filePath, "utf8");
      expect(after).toBe(original);
      // State should NOT carry a section fingerprint.
      const r = await readStateFile(paths);
      expect(r.state.rootClaudeMdSection ?? null).toBeNull();
    });
  });

  describe("AC-6a: missing markers → exit 1 + actionable error, file unchanged", () => {
    it("file absent → throws RootClaudeMdMarkersMissingError", async () => {
      // No CLAUDE.md at root.
      const paths = buildStatePaths(root);
      await expect(
        materialize(paths, makePlan("leaf", root), [rootFile("X")]),
      ).rejects.toBeInstanceOf(RootClaudeMdMarkersMissingError);
    });

    it("file present without markers → throws RootClaudeMdMarkersMissingError; file unchanged", async () => {
      const original = "# Plain CLAUDE.md, no markers.\n";
      const filePath = path.join(root, "CLAUDE.md");
      await fs.writeFile(filePath, original);

      const paths = buildStatePaths(root);
      await expect(
        materialize(paths, makePlan("leaf", root), [rootFile("X")]),
      ).rejects.toBeInstanceOf(RootClaudeMdMarkersMissingError);
      // R45 invariant: file is untouched on abort.
      expect(await fs.readFile(filePath, "utf8")).toBe(original);
    });

    it("malformed markers (lone :begin) → throws; file unchanged", async () => {
      const broken = "Some text\n<!-- claude-profiles:v1:begin -->\nincomplete\n";
      const filePath = path.join(root, "CLAUDE.md");
      await fs.writeFile(filePath, broken);

      const paths = buildStatePaths(root);
      await expect(
        materialize(paths, makePlan("leaf", root), [rootFile("X")]),
      ).rejects.toBeInstanceOf(RootClaudeMdMarkersMissingError);
      expect(await fs.readFile(filePath, "utf8")).toBe(broken);
    });

    it("error message names the file path and references init", async () => {
      const paths = buildStatePaths(root);
      try {
        await materialize(paths, makePlan("leaf", root), [rootFile("X")]);
        expect.fail("should have thrown");
      } catch (err) {
        expect(err).toBeInstanceOf(RootClaudeMdMarkersMissingError);
        const msg = (err as Error).message;
        expect(msg).toContain("claude-profiles init");
        expect(msg).toContain(paths.rootClaudeMdFile);
      }
    });
  });

  describe("AC-6b: atomic-across-destinations — .claude/ ALSO unchanged on missing-marker abort", () => {
    it("CRITICAL: pre-existing .claude/ is byte-identical after a missing-marker abort", async () => {
      // Stand up a fully materialized .claude/ from a PRIOR materialize that
      // had no projectRoot contributor. That run has no markers in root
      // CLAUDE.md (we never created one). Then run a NEW materialize with a
      // projectRoot contributor — pre-flight must abort BEFORE swapping
      // .claude/, leaving the original .claude/ tree byte-identical.
      const paths = buildStatePaths(root);

      // Initial state: hand-craft a .claude/ tree by direct fs writes.
      // No CLAUDE.md at root.
      await fs.mkdir(paths.claudeDir, { recursive: true });
      await fs.writeFile(path.join(paths.claudeDir, "ORIGINAL.md"), "ORIG");
      const originalLiveContent = await fs.readFile(
        path.join(paths.claudeDir, "ORIGINAL.md"),
        "utf8",
      );

      // Now attempt a materialize that contributes a projectRoot CLAUDE.md
      // AND a new .claude/ file. Markers are missing → must abort before
      // EITHER write lands.
      await expect(
        materialize(paths, makePlan("leaf", root), [
          claudeFile("NEW.md", "NEW"),
          rootFile("X"),
        ]),
      ).rejects.toBeInstanceOf(RootClaudeMdMarkersMissingError);

      // R45 atomic-across-destinations: .claude/ untouched.
      expect(
        await fs.readFile(path.join(paths.claudeDir, "ORIGINAL.md"), "utf8"),
      ).toBe(originalLiveContent);
      // The new .claude/ file we tried to add must NOT exist.
      expect(await pathExists(path.join(paths.claudeDir, "NEW.md"))).toBe(false);
      // No leftover staging artifacts.
      expect(await pathExists(paths.pendingDir)).toBe(false);
      expect(await pathExists(paths.priorDir)).toBe(false);
    });

    it("no leftover .tmp file in projectRoot after abort", async () => {
      const paths = buildStatePaths(root);
      // No CLAUDE.md and no markers → abort.
      await expect(
        materialize(paths, makePlan("leaf", root), [rootFile("X")]),
      ).rejects.toBeInstanceOf(RootClaudeMdMarkersMissingError);
      // Sweep the projectRoot for any leftover tmps.
      const entries = await fs.readdir(root);
      const tmps = entries.filter(isRootClaudeMdTmpName);
      expect(tmps).toHaveLength(0);
    });
  });

  describe("idempotent re-apply leaves projectRoot file byte-identical", () => {
    it("two consecutive materializes of the same plan produce identical bytes", async () => {
      await writeRootClaudeMdWithMarkers({
        before: "TOP\n",
        sectionBody: "old",
        after: "\nBOTTOM\n",
      });
      const paths = buildStatePaths(root);
      const plan = makePlan("leaf", root);

      await materialize(paths, plan, [rootFile("STABLE")]);
      const after1 = await fs.readFile(paths.rootClaudeMdFile, "utf8");

      await materialize(paths, plan, [rootFile("STABLE")]);
      const after2 = await fs.readFile(paths.rootClaudeMdFile, "utf8");

      expect(after2).toBe(after1);
    });
  });

  describe("schema migration: legacy state.json (no rootClaudeMdSection)", () => {
    it("loads legacy state.json without error and treats rootClaudeMdSection as null/absent", async () => {
      const paths = buildStatePaths(root);
      // Hand-write a legacy state.json (cw6-pre shape).
      await fs.mkdir(paths.metaDir, { recursive: true });
      const legacy = {
        schemaVersion: 1,
        activeProfile: "old",
        materializedAt: "2026-01-01T00:00:00Z",
        resolvedSources: [],
        fingerprint: { schemaVersion: 1, files: {} },
        externalTrustNotices: [],
        // intentionally NO rootClaudeMdSection
      };
      await fs.writeFile(paths.stateFile, JSON.stringify(legacy));

      const r = await readStateFile(paths);
      expect(r.warning).toBeNull();
      expect(r.state.activeProfile).toBe("old");
      expect(r.state.rootClaudeMdSection ?? null).toBeNull();
    });

    it("accepts state.json with rootClaudeMdSection=null", async () => {
      const paths = buildStatePaths(root);
      await fs.mkdir(paths.metaDir, { recursive: true });
      const legacy = {
        schemaVersion: 1,
        activeProfile: "x",
        materializedAt: "2026-01-01T00:00:00Z",
        resolvedSources: [],
        fingerprint: { schemaVersion: 1, files: {} },
        externalTrustNotices: [],
        rootClaudeMdSection: null,
      };
      await fs.writeFile(paths.stateFile, JSON.stringify(legacy));
      const r = await readStateFile(paths);
      expect(r.warning).toBeNull();
      expect(r.state.rootClaudeMdSection).toBeNull();
    });

    it("rejects state.json with malformed rootClaudeMdSection (missing contentHash)", async () => {
      const paths = buildStatePaths(root);
      await fs.mkdir(paths.metaDir, { recursive: true });
      const bad = {
        schemaVersion: 1,
        activeProfile: "x",
        materializedAt: null,
        resolvedSources: [],
        fingerprint: { schemaVersion: 1, files: {} },
        externalTrustNotices: [],
        rootClaudeMdSection: { size: 5 }, // no contentHash
      };
      await fs.writeFile(paths.stateFile, JSON.stringify(bad));
      const r = await readStateFile(paths);
      // R42: degrades to defaultState rather than throwing.
      expect(r.warning?.code).toBe("SchemaMismatch");
    });
  });

  describe("crash recovery: reconcile sweeps leftover .tmp", () => {
    it("reconcileMaterialize unlinks <projectRoot>/CLAUDE.md.<pid>.<n>.tmp", async () => {
      // Stand up a valid CLAUDE.md so the test exercises ONLY the tmp sweep.
      await writeRootClaudeMdWithMarkers({
        before: "U\n",
        sectionBody: "x",
        after: "\nV\n",
      });
      const paths = buildStatePaths(root);
      // Simulate a crashed splice: a .tmp file was written but the rename
      // never happened.
      const leftover = rootClaudeMdTmpPath(paths);
      await fs.writeFile(leftover, "PARTIAL CONTENTS");
      expect(await pathExists(leftover)).toBe(true);

      await reconcileMaterialize(paths);

      expect(await pathExists(leftover)).toBe(false);
      // Live CLAUDE.md is untouched by the sweep.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      expect(live).toContain("<!-- claude-profiles:v1:begin");
    });

    it("does NOT sweep unrelated .tmp files in projectRoot (regex anchored to CLAUDE.md prefix)", async () => {
      await writeRootClaudeMdWithMarkers({
        before: "U\n",
        sectionBody: "x",
        after: "\nV\n",
      });
      const paths = buildStatePaths(root);
      // User-created .tmp files that we MUST NOT touch.
      const userTmp1 = path.join(root, "notes.tmp");
      const userTmp2 = path.join(root, "CLAUDE.md.bak.tmp"); // close-but-not-our-pattern
      await fs.writeFile(userTmp1, "USER NOTES");
      await fs.writeFile(userTmp2, "USER BAK");

      await reconcileMaterialize(paths);

      expect(await pathExists(userTmp1)).toBe(true);
      expect(await pathExists(userTmp2)).toBe(true);
    });

    it("a fresh materialize after crash debris produces a coherent file (no garbage)", async () => {
      await writeRootClaudeMdWithMarkers({
        before: "U\n",
        sectionBody: "old",
        after: "\nV\n",
      });
      const paths = buildStatePaths(root);
      // Drop a leftover tmp.
      const leftover = rootClaudeMdTmpPath(paths);
      await fs.writeFile(leftover, "PARTIAL");

      // Now run materialize — must reconcile then write cleanly.
      await materialize(paths, makePlan("leaf", root), [rootFile("FRESH")]);

      expect(await pathExists(leftover)).toBe(false);
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      expect(live).toContain("FRESH");
      expect(live).toContain("<!-- claude-profiles:v1:begin");
      expect(live).toContain("<!-- claude-profiles:v1:end");
    });

    it("simulated mid-write fault: tmp exists but rename never ran — live file is the OLD content (not garbage)", async () => {
      // We can't actually crash the process mid-rename in a test, but we
      // can simulate the on-disk state: the rename never happened, so the
      // live CLAUDE.md still has the OLD section content. The reconcile
      // sweeps the tmp; the live file is unchanged. This is the invariant
      // the splice protocol provides.
      await writeRootClaudeMdWithMarkers({
        before: "ABOVE\n",
        sectionBody: "OLD-SECTION",
        after: "\nBELOW\n",
      });
      const paths = buildStatePaths(root);
      const liveBefore = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      // Drop a tmp with what would have been the new content.
      const leftover = rootClaudeMdTmpPath(paths);
      await fs.writeFile(
        leftover,
        liveBefore.replace("OLD-SECTION", "WOULD-HAVE-BEEN-NEW"),
      );

      await reconcileMaterialize(paths);

      // Live is the OLD content (rename never ran).
      const liveAfter = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      expect(liveAfter).toBe(liveBefore);
      expect(liveAfter).toContain("OLD-SECTION");
      expect(liveAfter).not.toContain("WOULD-HAVE-BEEN-NEW");
      // Tmp is gone.
      expect(await pathExists(leftover)).toBe(false);
    });
  });
});

/**
 * cw6/T5: section-only drift detection for project-root CLAUDE.md.
 *
 * Coverage map (acceptance criteria):
 *   - AC-7a: edits BETWEEN markers register as drift (status: 'modified')
 *   - AC-7b (the load-bearing invariant): edits OUTSIDE markers do NOT
 *           register as drift — the user owns those bytes
 *   - mixed inside+outside edit → drift detected (only the inside change matters)
 *   - missing markers (deleted/malformed) → status 'unrecoverable' with an
 *     actionable error message pointing at validate/init
 *   - file deleted entirely → unrecoverable
 *
 * These tests exercise drift via the materialize+detect pipeline so the
 * `state.rootClaudeMdSection` field is populated through the real cw6/T4
 * splice protocol rather than hand-crafted state.
 */

import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { detectDrift } from "../../src/drift/detect.js";
import { renderManagedBlock } from "../../src/markers.js";
import type { MergedFile } from "../../src/merge/types.js";
import type { ResolvedPlan } from "../../src/resolver/types.js";
import { RESOLVED_PLAN_SCHEMA_VERSION } from "../../src/resolver/types.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";

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

async function setupWithRootSection(rootDir: string, sectionBody: string): Promise<{
  paths: ReturnType<typeof buildStatePaths>;
  plan: ResolvedPlan;
}> {
  // Stand up a project-root CLAUDE.md with markers around the chosen body.
  const before = "# My project\n\nUser content above.\n";
  const after = "\n## My notes\n\nUser content below.\n";
  const block = renderManagedBlock(sectionBody);
  const filePath = path.join(rootDir, "CLAUDE.md");
  await fs.writeFile(filePath, `${before}${block}${after}`);

  const paths = buildStatePaths(rootDir);
  const plan = makePlan("leaf", rootDir);
  // Materialize: this populates state.rootClaudeMdSection with the recorded
  // section fingerprint we'll compare against in the tests below.
  await materialize(paths, plan, [rootFile(sectionBody)]);
  return { paths, plan };
}

describe("detectDrift: project-root CLAUDE.md section-only fingerprint (cw6/T5 / AC-7 / R46)", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-section-drift-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  describe("AC-7a: edits BETWEEN markers register as drift", () => {
    it("byte change inside the section → entry with status 'modified' for CLAUDE.md (destination='projectRoot')", async () => {
      const { paths } = await setupWithRootSection(root, "ORIGINAL-SECTION");
      // Read the live file, splice a new body between the markers.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      const tampered = live.replace("ORIGINAL-SECTION", "USER-EDITED-SECTION");
      await fs.writeFile(paths.rootClaudeMdFile, tampered);

      const report = await detectDrift(paths);
      expect(report.fingerprintOk).toBe(true);
      const root_ = report.entries.find(
        (e) => e.destination === "projectRoot",
      );
      expect(root_).toBeDefined();
      expect(root_!.relPath).toBe("CLAUDE.md");
      expect(root_!.status).toBe("modified");
      // Provenance is carried through.
      expect(root_!.provenance.map((p) => p.id)).toEqual(["leaf"]);
    });

    it("section grown (length differs) → drift detected as 'modified'", async () => {
      const { paths } = await setupWithRootSection(root, "SHORT");
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      // Inject a multi-line addition between the markers.
      const tampered = live.replace("SHORT", "MUCH\nLONGER\nSECTION\nCONTENT");
      await fs.writeFile(paths.rootClaudeMdFile, tampered);

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_!.status).toBe("modified");
    });

    it("section shrunk (length differs) → drift detected as 'modified'", async () => {
      const { paths } = await setupWithRootSection(root, "LONG-ORIGINAL-SECTION-WITH-CONTENT");
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      const tampered = live.replace(
        "LONG-ORIGINAL-SECTION-WITH-CONTENT",
        "tiny",
      );
      await fs.writeFile(paths.rootClaudeMdFile, tampered);

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_!.status).toBe("modified");
    });
  });

  describe("AC-7b (LOAD-BEARING): edits OUTSIDE markers do NOT register as drift", () => {
    it("appending content below the :end marker → no drift entry for CLAUDE.md", async () => {
      const { paths } = await setupWithRootSection(root, "STABLE-SECTION");
      // Append user content well below the end marker.
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      await fs.writeFile(
        paths.rootClaudeMdFile,
        `${live}\n## A new heading the user added\n\nMore prose.\n`,
      );

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeUndefined();
    });

    it("prepending content above the :begin marker → no drift entry for CLAUDE.md", async () => {
      const { paths } = await setupWithRootSection(root, "STABLE-SECTION");
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      await fs.writeFile(
        paths.rootClaudeMdFile,
        `# A new top heading the user added\n\nFresh prose at the top.\n\n${live}`,
      );

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeUndefined();
    });

    it("modifying the user-owned bytes both above AND below markers → no drift entry", async () => {
      const { paths } = await setupWithRootSection(root, "STABLE-SECTION");
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      // Replace ALL user-owned bytes outside the markers with completely
      // different content. Section bytes between markers are left intact.
      const beginIdx = live.indexOf("<!-- c3p:v1:begin");
      const endMarker = "<!-- c3p:v1:end -->";
      const endIdx = live.indexOf(endMarker) + endMarker.length;
      const middle = live.slice(beginIdx, endIdx);
      const newAbove = "% Totally different above-content\n\nLorem ipsum.\n\n";
      const newBelow = "\n\n% Totally different below-content\nDolor sit amet.\n";
      await fs.writeFile(
        paths.rootClaudeMdFile,
        `${newAbove}${middle}${newBelow}`,
      );

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeUndefined();
    });
  });

  describe("mixed: inside + outside edit → drift detected (only inside matters)", () => {
    it("inside change AND outside change → reports drift on CLAUDE.md (because of inside)", async () => {
      const { paths } = await setupWithRootSection(root, "ORIG");
      const live = await fs.readFile(paths.rootClaudeMdFile, "utf8");
      // Change BOTH the section body and the user-owned content above/below.
      let tampered = live.replace("ORIG", "INSIDE-EDIT");
      tampered = `# New top heading\n\n${tampered}\n## New bottom heading\n`;
      await fs.writeFile(paths.rootClaudeMdFile, tampered);

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeDefined();
      expect(root_!.status).toBe("modified");
    });
  });

  describe("missing-markers terminal state (the user broke the file structure)", () => {
    it("markers deleted → unrecoverable status with actionable error message", async () => {
      const { paths } = await setupWithRootSection(root, "STABLE");
      // Strip the entire managed block (markers + body) — leave plain prose.
      await fs.writeFile(
        paths.rootClaudeMdFile,
        "# Plain CLAUDE.md\n\nNo markers anymore.\n",
      );

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeDefined();
      expect(root_!.relPath).toBe("CLAUDE.md");
      expect(root_!.status).toBe("unrecoverable");
      expect(root_!.error).toBeDefined();
      // Actionable: reference init and validate.
      expect(root_!.error).toMatch(/c3p init|validate/i);
      expect(root_!.error).toContain(paths.rootClaudeMdFile);
    });

    it("malformed markers (lone :begin) → unrecoverable with actionable error", async () => {
      const { paths } = await setupWithRootSection(root, "STABLE");
      await fs.writeFile(
        paths.rootClaudeMdFile,
        "# Broken\n<!-- c3p:v1:begin -->\nincomplete only\n",
      );

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_!.status).toBe("unrecoverable");
      expect(root_!.error).toMatch(/init|validate/i);
    });

    it("file removed entirely → unrecoverable (we recorded a section, file gone)", async () => {
      const { paths } = await setupWithRootSection(root, "STABLE");
      await fs.unlink(paths.rootClaudeMdFile);

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeDefined();
      expect(root_!.status).toBe("unrecoverable");
      expect(root_!.error).toMatch(/init|validate/i);
    });

    it("does NOT crash detect — returns a report", async () => {
      const { paths } = await setupWithRootSection(root, "STABLE");
      await fs.unlink(paths.rootClaudeMdFile);
      // Smoke test: the call resolves rather than rejects.
      await expect(detectDrift(paths)).resolves.toBeDefined();
    });
  });

  describe("section-only fingerprint scope (R46)", () => {
    it("after a clean materialize, no drift reported (section bytes match)", async () => {
      const { paths } = await setupWithRootSection(root, "CLEAN");
      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeUndefined();
    });

    it("when state has no rootClaudeMdSection (legacy), no projectRoot drift entry is produced", async () => {
      // Materialize a profile with NO projectRoot contributor — state.json
      // will not carry a section fingerprint. Drift detection should not
      // attempt to fingerprint the project-root CLAUDE.md.
      const paths = buildStatePaths(root);
      const plan = makePlan("leaf", root);
      // No root file in merged set; .claude/-only contribution.
      await materialize(paths, plan, [
        {
          path: "agents/x.md",
          bytes: Buffer.from("X"),
          contributors: ["leaf"],
          mergePolicy: "last-wins",
          destination: ".claude",
        },
      ]);

      // Even if a CLAUDE.md exists at the root, no section fingerprint was
      // recorded → drift should ignore it.
      await fs.writeFile(
        path.join(root, "CLAUDE.md"),
        "# Random user file, never managed by us.\n",
      );

      const report = await detectDrift(paths);
      const root_ = report.entries.find((e) => e.destination === "projectRoot");
      expect(root_).toBeUndefined();
    });
  });
});

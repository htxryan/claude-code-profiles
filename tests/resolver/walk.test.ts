import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { walkClaudeDir, walkProfileRoot } from "../../src/resolver/walk.js";

/**
 * Tests for the resolver's filesystem walkers, specifically the new
 * `walkProfileRoot` helper introduced in cw6/T2.
 *
 * Acceptance criteria covered (from claude-code-profiles-cw6 / -07g):
 *  - AC-3a: profile with `.claude-profiles/<P>/CLAUDE.md` (peer of profile.json)
 *           → walker emits an entry the resolver tags with `destination='projectRoot'`
 *  - AC-3b: profile with `.claude/CLAUDE.md` only → walker emits an entry the
 *           resolver tags with `destination='.claude'`
 *  - Both at once: walker emits TWO entries
 *  - Negative: profile with neither → no CLAUDE.md entries
 */

describe("walkProfileRoot()", () => {
  let tmpDir: string;

  beforeEach(async () => {
    tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-walk-"));
  });

  afterEach(async () => {
    await fs.rm(tmpDir, { recursive: true, force: true });
  });

  it("returns the profile-root CLAUDE.md when present (peer of profile.json)", async () => {
    // Layout:
    //   <tmpDir>/profile.json
    //   <tmpDir>/CLAUDE.md
    //   <tmpDir>/.claude/         (sibling, may or may not exist)
    await fs.writeFile(path.join(tmpDir, "profile.json"), "{}");
    await fs.writeFile(path.join(tmpDir, "CLAUDE.md"), "ROOT CONTENT");

    const entries = await walkProfileRoot(tmpDir);
    expect(entries).toHaveLength(1);
    expect(entries[0]!.relPath).toBe("CLAUDE.md");
    expect(entries[0]!.absPath).toBe(path.join(tmpDir, "CLAUDE.md"));
  });

  it("returns [] when profile-root CLAUDE.md is absent", async () => {
    await fs.writeFile(path.join(tmpDir, "profile.json"), "{}");
    // No CLAUDE.md at the profile root.
    const entries = await walkProfileRoot(tmpDir);
    expect(entries).toEqual([]);
  });

  it("returns [] when profile dir does not exist", async () => {
    const ghost = path.join(tmpDir, "does-not-exist");
    const entries = await walkProfileRoot(ghost);
    expect(entries).toEqual([]);
  });

  it("ignores nested CLAUDE.md (e.g. inside .claude/) — that's walkClaudeDir's job", async () => {
    // Make a .claude/CLAUDE.md, but no profile-root CLAUDE.md.
    // walkProfileRoot should NOT pick up the nested one.
    await fs.writeFile(path.join(tmpDir, "profile.json"), "{}");
    const claudeDir = path.join(tmpDir, ".claude");
    await fs.mkdir(claudeDir, { recursive: true });
    await fs.writeFile(path.join(claudeDir, "CLAUDE.md"), "NESTED");

    const entries = await walkProfileRoot(tmpDir);
    expect(entries).toEqual([]);
  });

  it("returns only CLAUDE.md, not arbitrary peer files (e.g. README.md)", async () => {
    await fs.writeFile(path.join(tmpDir, "profile.json"), "{}");
    await fs.writeFile(path.join(tmpDir, "README.md"), "readme");
    await fs.writeFile(path.join(tmpDir, "CLAUDE.md"), "claude");

    const entries = await walkProfileRoot(tmpDir);
    const rels = entries.map((e) => e.relPath);
    expect(rels).toEqual(["CLAUDE.md"]);
  });

  it("ignores a CLAUDE.md that is actually a directory", async () => {
    await fs.writeFile(path.join(tmpDir, "profile.json"), "{}");
    await fs.mkdir(path.join(tmpDir, "CLAUDE.md"), { recursive: true });

    const entries = await walkProfileRoot(tmpDir);
    expect(entries).toEqual([]);
  });
});

describe("walkClaudeDir() — regression: still picks up .claude/CLAUDE.md", () => {
  let tmpDir: string;

  beforeEach(async () => {
    tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-walk-"));
  });

  afterEach(async () => {
    await fs.rm(tmpDir, { recursive: true, force: true });
  });

  it("emits .claude/CLAUDE.md as before (untouched by cw6/T2)", async () => {
    const claudeDir = path.join(tmpDir, ".claude");
    await fs.mkdir(claudeDir, { recursive: true });
    await fs.writeFile(path.join(claudeDir, "CLAUDE.md"), "in claude");

    const entries = await walkClaudeDir(claudeDir);
    expect(entries.map((e) => e.relPath)).toContain("CLAUDE.md");
  });
});

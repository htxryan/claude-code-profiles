import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  E3_GITIGNORE_ENTRIES,
  ensureGitignoreEntries,
} from "../../src/state/gitignore.js";
import { buildStatePaths } from "../../src/state/paths.js";

describe(".gitignore management", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-gitignore-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  it("creates the file with all required entries when missing", async () => {
    const paths = buildStatePaths(root);
    const result = await ensureGitignoreEntries(paths);
    expect(result.created).toBe(true);
    expect(result.added).toEqual([...E3_GITIGNORE_ENTRIES]);
    const content = await fs.readFile(paths.gitignoreFile, "utf8");
    for (const e of E3_GITIGNORE_ENTRIES) {
      expect(content).toContain(e);
    }
  });

  it("appends only missing entries when the file already has some", async () => {
    const paths = buildStatePaths(root);
    await fs.writeFile(paths.gitignoreFile, "node_modules/\n.claude/\n");
    const result = await ensureGitignoreEntries(paths);
    expect(result.created).toBe(false);
    expect(result.added).not.toContain(".claude/");
    expect(result.added).toContain(".claude-profiles/.meta/");
    const content = await fs.readFile(paths.gitignoreFile, "utf8");
    expect(content).toContain("node_modules/");
    expect(content).toContain(".claude-profiles/.meta/");
  });

  it("is idempotent on a fully-populated file", async () => {
    const paths = buildStatePaths(root);
    await ensureGitignoreEntries(paths);
    const result = await ensureGitignoreEntries(paths);
    expect(result.added).toEqual([]);
  });

  it("preserves existing user content when appending", async () => {
    const paths = buildStatePaths(root);
    const userContent = "node_modules/\ndist/\n# user comment\n.env.local\n";
    await fs.writeFile(paths.gitignoreFile, userContent);
    await ensureGitignoreEntries(paths);
    const content = await fs.readFile(paths.gitignoreFile, "utf8");
    expect(content.startsWith(userContent.trim())).toBe(true);
  });

  // Regression (Sonnet review #4, Gemini #2): the previous staging path was
  // `<root>/.gitignore.tmp`, which would be visible at the project root if a
  // crash interrupted the write. Staging now lives inside
  // `.claude-profiles/.meta/tmp/`, which is itself gitignored via the
  // `.meta/` umbrella entry.
  it("does not stage temp files at the project root", async () => {
    const paths = buildStatePaths(root);
    await ensureGitignoreEntries(paths);
    const rootEntries = await fs.readdir(root);
    const tmpAtRoot = rootEntries.filter((e) => e.endsWith(".tmp"));
    expect(tmpAtRoot).toEqual([]);
  });

  it("includes .claude-profiles/.meta/ so all bookkeeping artifacts are gitignored under one entry", () => {
    expect(E3_GITIGNORE_ENTRIES).toContain(".claude-profiles/.meta/");
  });
});

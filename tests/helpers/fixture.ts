import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

/**
 * Spec for an in-tree fixture. Profiles live under `.claude-profiles/<name>/`
 * with `profile.json` and a `.claude/` subtree of arbitrary file content.
 */
export interface ProfileSpec {
  /** profile.json content (object will be stringified). If a string, written verbatim. */
  manifest?: Record<string, unknown> | string | null;
  /** Files under `.claude/`, keyed by relative posix path → content string. */
  files?: Record<string, string>;
  /**
   * Files at the profile root (peer of profile.json, sibling of .claude/),
   * keyed by relative posix path → content string. Used for cw6 to set up
   * `.claude-profiles/<P>/CLAUDE.md` (the destination='projectRoot' source).
   */
  rootFiles?: Record<string, string>;
}

export interface ComponentSpec {
  files?: Record<string, string>;
  /**
   * Files at the component root (sibling of .claude/). For cw6 testing of
   * include contributors that supply a profile-root CLAUDE.md.
   */
  rootFiles?: Record<string, string>;
}

export interface FixtureSpec {
  profiles?: Record<string, ProfileSpec>;
  /** In-repo components (under `.claude-profiles/_components/<name>/`). */
  components?: Record<string, ComponentSpec>;
  /** Out-of-repo paths (absolute keys). */
  external?: Record<string, ComponentSpec>;
}

export interface Fixture {
  projectRoot: string;
  externalRoot: string;
  cleanup: () => Promise<void>;
}

export async function makeFixture(spec: FixtureSpec): Promise<Fixture> {
  const tmp = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-fixture-"));
  const projectRoot = path.join(tmp, "project");
  const externalRoot = path.join(tmp, "external");
  await fs.mkdir(projectRoot, { recursive: true });
  await fs.mkdir(externalRoot, { recursive: true });

  const profilesDir = path.join(projectRoot, ".claude-profiles");

  for (const [name, p] of Object.entries(spec.profiles ?? {})) {
    const dir = path.join(profilesDir, name);
    await fs.mkdir(dir, { recursive: true });
    if (p.manifest !== null && p.manifest !== undefined) {
      const content =
        typeof p.manifest === "string" ? p.manifest : JSON.stringify(p.manifest, null, 2);
      await fs.writeFile(path.join(dir, "profile.json"), content);
    }
    for (const [rel, content] of Object.entries(p.files ?? {})) {
      const fp = path.join(dir, ".claude", rel);
      await fs.mkdir(path.dirname(fp), { recursive: true });
      await fs.writeFile(fp, content);
    }
    for (const [rel, content] of Object.entries(p.rootFiles ?? {})) {
      const fp = path.join(dir, rel);
      await fs.mkdir(path.dirname(fp), { recursive: true });
      await fs.writeFile(fp, content);
    }
  }

  for (const [name, c] of Object.entries(spec.components ?? {})) {
    const dir = path.join(profilesDir, "_components", name);
    await fs.mkdir(dir, { recursive: true });
    for (const [rel, content] of Object.entries(c.files ?? {})) {
      const fp = path.join(dir, ".claude", rel);
      await fs.mkdir(path.dirname(fp), { recursive: true });
      await fs.writeFile(fp, content);
    }
    for (const [rel, content] of Object.entries(c.rootFiles ?? {})) {
      const fp = path.join(dir, rel);
      await fs.mkdir(path.dirname(fp), { recursive: true });
      await fs.writeFile(fp, content);
    }
  }

  for (const [absKey, c] of Object.entries(spec.external ?? {})) {
    // The "absolute key" is treated as a path under externalRoot to keep
    // tests hermetic. Tests pass `path.join(externalRoot, absKey)` to the
    // resolver as the includes entry to point at this directory.
    const dir = path.join(externalRoot, absKey);
    await fs.mkdir(dir, { recursive: true });
    for (const [rel, content] of Object.entries(c.files ?? {})) {
      const fp = path.join(dir, ".claude", rel);
      await fs.mkdir(path.dirname(fp), { recursive: true });
      await fs.writeFile(fp, content);
    }
  }

  return {
    projectRoot,
    externalRoot,
    cleanup: () => fs.rm(tmp, { recursive: true, force: true }),
  };
}

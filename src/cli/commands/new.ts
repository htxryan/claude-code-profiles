/**
 * `new <name>` command. Scaffolds an empty profile:
 *   .claude-profiles/<name>/profile.json   { name, description? }
 *   .claude-profiles/<name>/.claude/       (empty dir)
 *
 * Refuses if the profile dir already exists. Validates the name against the
 * resolver's `isValidProfileName` so we don't create a directory the resolver
 * can't later look up.
 *
 * Out of scope: extends/includes editing (use `new` then edit profile.json
 * by hand for v1; an `edit` verb may come in a later epic).
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";
import { assertValidProfileName } from "../suggest.js";

export interface NewOptions {
  cwd: string;
  output: OutputChannel;
  profile: string;
  description: string | null;
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
}

export async function runNew(opts: NewOptions): Promise<number> {
  // ppo: route through the shared validator so all four name-taking verbs
  // (new/use/diff/validate) emit identical wording on bad input.
  assertValidProfileName("new", opts.profile);

  const profilesRoot = path.join(opts.cwd, ".claude-profiles");
  const profileDir = path.join(profilesRoot, opts.profile);
  // Atomically refuse to overwrite: ensure .claude-profiles/ exists, then
  // create the profile dir non-recursively so a parallel `new` racing past
  // an `fs.access` check can't sneak in (mkdir-non-recursive returns EEXIST
  // and we map that to the user-facing "refusing to overwrite" error).
  await fs.mkdir(profilesRoot, { recursive: true });
  try {
    await fs.mkdir(profileDir);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "EEXIST") {
      throw new CliUserError(
        `new: ".claude-profiles/${opts.profile}/" already exists; refusing to overwrite`,
        EXIT_USER_ERROR,
      );
    }
    throw err;
  }
  await fs.mkdir(path.join(profileDir, ".claude"));

  const manifest: Record<string, unknown> = { name: opts.profile };
  if (opts.description !== null) manifest["description"] = opts.description;
  const json = `${JSON.stringify(manifest, null, 2)}\n`;
  await fs.writeFile(path.join(profileDir, "profile.json"), json);

  if (opts.output.jsonMode) {
    opts.output.json({
      profile: opts.profile,
      profileDir,
      created: true,
    });
  } else {
    const style = createStyle({
      isTty: opts.output.isTty,
      platform: process.platform,
      noColor: resolveNoColor(opts.noColor === true),
    });
    opts.output.print(style.ok(`Created profile "${opts.profile}" at ${profileDir}`));
    opts.output.print(
      style.dim(`  edit ${path.join(profileDir, "profile.json")} to set extends/includes`),
    );
  }
  return 0;
}

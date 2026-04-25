/**
 * `list` command (R30, R40). Read-only: enumerates `.claude-profiles/`,
 * loads each manifest, marks the active profile from `.state.json`, and
 * prints either a human table or a structured JSON payload.
 *
 * No lock acquired (R43). May briefly disagree with a concurrent `use`/`new`.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { listProfiles } from "../../resolver/discover.js";
import { buildPaths } from "../../resolver/paths.js";
import type { ProfileManifest } from "../../resolver/types.js";
import { buildStatePaths } from "../../state/paths.js";
import { readStateFile } from "../../state/state-file.js";
import { formatStateWarning, relativeTime, renderTable } from "../format.js";
import type { OutputChannel } from "../output.js";

export interface ListEntryPayload {
  name: string;
  active: boolean;
  description: string | null;
  extends: string | null;
  includes: string[];
  tags: string[];
  /** Last-materialized ISO timestamp — only set on the active profile. */
  lastMaterialized: string | null;
}

export interface ListPayload {
  profiles: ListEntryPayload[];
  /** Surfaced when state file degraded (Missing is silenced — fresh project). */
  stateWarning: { code: string; detail: string } | null;
}

export interface ListOptions {
  cwd: string;
  output: OutputChannel;
}

export async function runList(opts: ListOptions): Promise<number> {
  const resolverPaths = buildPaths(opts.cwd);
  const statePaths = buildStatePaths(opts.cwd);
  const names = await listProfiles({ projectRoot: opts.cwd });
  const { state, warning } = await readStateFile(statePaths);

  const entries: ListEntryPayload[] = [];
  for (const name of names) {
    const manifest = await tryLoadManifestSummary(resolverPaths.profilesDir, name);
    const isActive = state.activeProfile === name;
    entries.push({
      name,
      active: isActive,
      description: manifest?.description ?? null,
      extends: manifest?.extends ?? null,
      includes: manifest?.includes ?? [],
      tags: manifest?.tags ?? [],
      lastMaterialized: isActive ? state.materializedAt : null,
    });
  }

  // Surface only meaningful warnings — Missing is the fresh-project signal,
  // matches lesson L942d1c5b's principle: silence the "normal absence" case.
  const stateWarning =
    warning && warning.code !== "Missing"
      ? { code: warning.code, detail: warning.code === "ParseError" ? warning.detail : warning.code === "SchemaMismatch" ? warning.detail : "" }
      : null;

  if (opts.output.jsonMode) {
    const payload: ListPayload = { profiles: entries, stateWarning };
    opts.output.json(payload);
    return 0;
  }

  if (entries.length === 0) {
    opts.output.print("(no profiles — run `claude-profiles new <name>` to create one)");
  } else {
    const rows: Array<readonly [string, string]> = entries.map((e) => {
      const marker = e.active ? "*" : " ";
      const ext = e.extends ? ` extends=${e.extends}` : "";
      const inc = e.includes.length > 0 ? ` includes=[${e.includes.join(",")}]` : "";
      const lm =
        e.lastMaterialized !== null
          ? `  (materialized ${relativeTime(e.lastMaterialized)})`
          : "";
      return [`${marker} ${e.name}`, `${ext}${inc}${lm}`.trim()];
    });
    opts.output.print(renderTable(rows));
  }

  if (warning && warning.code !== "Missing") {
    opts.output.warn(formatStateWarning(warning));
  }
  return 0;
}

/**
 * Best-effort manifest read for the list/status display. We don't want a
 * malformed profile.json to blow up `list` — degrade to "no manifest fields
 * shown" rather than aborting. The full `validate` command will surface the
 * details.
 */
async function tryLoadManifestSummary(
  profilesDir: string,
  name: string,
): Promise<ProfileManifest | null> {
  try {
    const raw = await fs.readFile(path.join(profilesDir, name, "profile.json"), "utf8");
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return null;
    return parsed as ProfileManifest;
  } catch {
    return null;
  }
}

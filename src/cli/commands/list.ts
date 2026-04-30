/**
 * `list` command (R30, R40). Read-only: enumerates `.claude-profiles/`,
 * loads each manifest, marks the active profile from `.state.json`, and
 * prints either a human table or a structured JSON payload.
 *
 * No lock acquired (R43). May briefly disagree with a concurrent `use`/`new`.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import {
  buildPaths,
  listProfiles,
  type ProfileManifest,
} from "../../resolver/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import {
  formatStateWarning,
  meaningfulStateWarning,
  relativeTime,
  renderTable,
} from "../format.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";

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
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
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

  const stateWarning = meaningfulStateWarning(warning);

  if (opts.output.jsonMode) {
    const payload: ListPayload = { profiles: entries, stateWarning };
    opts.output.json(payload);
    return 0;
  }

  if (entries.length === 0) {
    opts.output.print("(no profiles — run `c3p new <name>` to create one)");
  } else {
    const style = createStyle({
      isTty: opts.output.isTty,
      platform: process.platform,
      noColor: resolveNoColor(opts.noColor === true),
    });
    // Decide which optional columns to render. A column is visible if any
    // row has content for it — keeps the layout tight in the common case
    // (no descriptions, no tags) while expanding gracefully when used.
    const showDescription = entries.some((e) => e.description !== null && e.description !== "");
    const showTags = entries.some((e) => e.tags.length > 0);

    const rows: string[][] = entries.map((e) => {
      // Active marker: glyph + bold styling per spec — both for redundancy
      // (some terminals strip ANSI; the `*` survives).
      const marker = e.active ? "*" : " ";
      const nameCell = e.active ? `${marker} ${style.bold(e.name)}` : `${marker} ${e.name}`;
      const cells: string[] = [nameCell];
      if (showDescription) cells.push(e.description ?? "");
      if (showTags) cells.push(e.tags.length > 0 ? `[${e.tags.join(", ")}]` : "");
      // Trailing meta column: extends/includes/last-materialized. Joined
      // with a single space so each piece reads as one continuous flag.
      const metaParts: string[] = [];
      if (e.extends) metaParts.push(`extends=${e.extends}`);
      if (e.includes.length > 0) metaParts.push(`includes=[${e.includes.join(",")}]`);
      if (e.lastMaterialized !== null) {
        metaParts.push(`(materialized ${relativeTime(e.lastMaterialized)})`);
      }
      cells.push(metaParts.join(" "));
      return cells;
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

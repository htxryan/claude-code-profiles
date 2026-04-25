/**
 * Human-readable formatters used by `list`, `status`, `drift`, `diff`. All
 * functions are pure (string in, string out) so the human/JSON split happens
 * once in the command handler — formatters never see the OutputChannel.
 *
 * Style conventions (per spec §7 quality bar):
 *  - Active profile marked with a leading `*` (right-padded to two cols)
 *  - Timestamps shown both ISO and humanised: "2026-04-25T12:34:56.789Z (3h ago)"
 *  - Error formatters always name file/profile/path
 *  - No ANSI colour by default — leave that to a future polish pass
 */

import type { ResolutionWarning } from "../resolver/types.js";

import type { StateReadWarning } from "../state/state-file.js";

/**
 * Render a wall-clock-relative summary for a timestamp ("3h ago", "in 2m").
 * Returns "never" for null. Tolerates unparseable ISO strings.
 */
export function relativeTime(iso: string | null, now: number = Date.now()): string {
  if (iso === null) return "never";
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  const deltaMs = now - t;
  const abs = Math.abs(deltaMs);
  const past = deltaMs >= 0;
  const seconds = Math.round(abs / 1000);
  if (seconds < 60) return past ? `${seconds}s ago` : `in ${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return past ? `${minutes}m ago` : `in ${minutes}m`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return past ? `${hours}h ago` : `in ${hours}h`;
  const days = Math.round(hours / 24);
  return past ? `${days}d ago` : `in ${days}d`;
}

/**
 * Format an ISO timestamp as "<iso> (<relative>)" — null returns "never".
 */
export function timestampWithRelative(iso: string | null, now: number = Date.now()): string {
  if (iso === null) return "never";
  return `${iso} (${relativeTime(iso, now)})`;
}

/**
 * Render a simple two-column table: pads the first column to the longest
 * width across all rows. Used for `list` and similar.
 */
export function renderTable(rows: ReadonlyArray<readonly [string, string]>): string {
  if (rows.length === 0) return "";
  const widest = Math.max(...rows.map(([k]) => k.length));
  return rows.map(([k, v]) => `${k.padEnd(widest)}  ${v}`).join("\n");
}

/**
 * Format a StateReadWarning for human display. The pre-commit hook already
 * filters Missing (lesson L942d1c5b — Missing is "fresh project", not broken),
 * but the `status` command surfaces all three variants so the user knows
 * exactly what's degraded.
 */
export function formatStateWarning(w: StateReadWarning): string {
  switch (w.code) {
    case "Missing":
      return `state file missing at ${w.path} (fresh project)`;
    case "ParseError":
      return `state file at ${w.path} is unparseable: ${w.detail}`;
    case "SchemaMismatch":
      return `state file at ${w.path} has unexpected schema: ${w.detail}`;
  }
}

/**
 * Format ResolutionWarning collection (R36 unknown manifest fields, etc.) as
 * a multi-line block. Empty array → empty string.
 */
export function formatResolutionWarnings(ws: ReadonlyArray<ResolutionWarning>): string {
  if (ws.length === 0) return "";
  return ws.map((w) => {
    const src = w.source ? ` [${w.source}]` : "";
    return `  ! ${w.code}${src}: ${w.message}`;
  }).join("\n");
}

/**
 * Format an error caught at dispatch level. Always names the file/profile/
 * path (per §7). Falls back to message-only for unknown error shapes.
 */
export function formatError(err: unknown): string {
  if (err instanceof Error) {
    // Subclass-specific fields are already baked into the message by the
    // PipelineError ctors (e.g. ConflictError mentions both contributors).
    // We just need a stable prefix.
    return `claude-profiles: ${err.message}`;
  }
  return `claude-profiles: ${String(err)}`;
}

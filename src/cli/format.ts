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
 * Render an N-column text table. Pads each column to the longest cell width
 * in that column except the LAST column, so the trailing-edge of every line
 * is the natural end of the data — no ragged trailing whitespace.
 *
 * Each row is an array of cells; missing trailing cells are treated as empty
 * strings so callers can omit columns per row when they have nothing to show.
 *
 * Cells may contain ANSI SGR escape sequences (colour, bold). Width
 * measurement uses VISIBLE length only (escapes stripped) so a bolded cell
 * does not push subsequent columns out of alignment under a real TTY.
 */
export function renderTable(rows: ReadonlyArray<ReadonlyArray<string>>): string {
  if (rows.length === 0) return "";
  const numCols = Math.max(...rows.map((r) => r.length));
  const widths: number[] = [];
  for (let c = 0; c < numCols; c++) {
    let w = 0;
    for (const r of rows) {
      const len = visibleLength(r[c] ?? "");
      if (len > w) w = len;
    }
    widths.push(w);
  }
  const lines = rows.map((r) => {
    const cells: string[] = [];
    for (let c = 0; c < numCols; c++) {
      const cell = r[c] ?? "";
      // Last column: emit raw (no padEnd) so trailing whitespace never leaks.
      // Earlier columns: pad to the column's visible width so the next
      // column starts at a stable visual offset.
      if (c === numCols - 1) cells.push(cell);
      else cells.push(padEndVisible(cell, widths[c]!));
    }
    // Two-space gap is the standard column separator across the codebase
    // (matches the prior renderTable output for the existing two-column case).
    return cells.join("  ").trimEnd();
  });
  return lines.join("\n");
}

/**
 * Strip CSI/SGR ANSI escape sequences when measuring column widths so that
 * `style.bold("name")` (which adds ~9 bytes of escape codes around 4 visible
 * chars) does not over-pad neighbouring rows. The pattern matches the
 * minimal subset we emit from `createStyle` (colour + bold + dim + reset).
 */
const ANSI_RE = /\x1b\[[0-9;]*m/g;
function visibleLength(s: string): number {
  return s.replace(ANSI_RE, "").length;
}

/**
 * `padEnd` that pads to a target VISIBLE width (i.e. accounts for embedded
 * ANSI escapes). Falls back to native padEnd for plain strings so the hot
 * path (no escapes) stays branch-light.
 */
function padEndVisible(s: string, targetVisible: number): string {
  const visible = visibleLength(s);
  if (visible >= targetVisible) return s;
  return s + " ".repeat(targetVisible - visible);
}

/**
 * Reshape a StateReadWarning into the `{code, detail}` payload commands emit
 * via --json, with `Missing` filtered out (lesson L942d1c5b: Missing is the
 * "fresh project" signal, not a degradation worth surfacing). Returns null
 * when the warning is null OR Missing — callers can treat null uniformly.
 *
 * `detail` is empty string for variants that have no detail field rather than
 * undefined so the JSON shape is stable across all rows.
 */
export function meaningfulStateWarning(
  w: StateReadWarning | null,
): { code: string; detail: string } | null {
  if (w === null || w.code === "Missing") return null;
  // ParseError and SchemaMismatch both carry a `detail` field today, and
  // exhaust the non-Missing variants — the explicit narrowing keeps the
  // function future-proof if a new variant without `detail` is added.
  return { code: w.code, detail: w.detail };
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
      return `state file missing at ${w.path} (fresh project — nothing to fret over)`;
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
    return `c3p: ${err.message}`;
  }
  return `c3p: ${String(err)}`;
}

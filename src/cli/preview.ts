/**
 * Content-preview helpers for `diff --preview` and `drift --preview` (azp).
 *
 * Two surfaces:
 *   - {@link renderUnifiedDiff} — minimal Myers-LCS-free unified diff between
 *     two text buffers, capped at a configurable max-line budget. Sufficient
 *     for human "what changed" previews; not a full git-grade diff.
 *   - {@link renderHeadPreview} — for added/deleted entries in drift, where
 *     there's no opposing buffer to diff against.
 *   - {@link isBinary} — NUL-byte sniff over the first 8KB.
 *
 * Why hand-rolled instead of an LCS library: keeps claude-profiles dependency-
 * free (matters for cold-start install). The simple line-aligned algorithm
 * here is strictly weaker than a true diff but produces correct, readable
 * output for the typical config-file edit (small, mostly-aligned changes).
 */

const BINARY_SNIFF_BYTES = 8192;
const DEFAULT_MAX_LINES = 20;
const HEAD_PREVIEW_LINES = 10;

/**
 * True iff the first 8KB of `bytes` contains a NUL byte. The conventional
 * "is this binary" heuristic — UTF-8 text never has NUL bytes outside of
 * deliberate sentinel constructs, and most binary formats (images, archives,
 * compiled code) hit one inside the first few hundred bytes.
 */
export function isBinary(bytes: Buffer): boolean {
  const limit = Math.min(bytes.length, BINARY_SNIFF_BYTES);
  for (let i = 0; i < limit; i++) {
    if (bytes[i] === 0) return true;
  }
  return false;
}

export interface PreviewOptions {
  /** Hard cap on rendered lines, INCLUDING the header but EXCLUDING the truncation footer. Default 20. */
  maxLines?: number;
}

/**
 * Render a minimal unified diff between two text buffers. Output is a multi-
 * line string with no trailing newline. Lines look like:
 *
 *   ` context line`
 *   `-removed line`
 *   `+added line`
 *
 * When the rendered output would exceed `maxLines` lines, we truncate and
 * append a single `(truncated, N more lines)` footer line. Binary inputs
 * produce a `(binary file — N bytes)` placeholder via {@link isBinary}.
 *
 * The algorithm is intentionally simple: walk both line lists, emitting
 * matching lines as context, leftover lines on the `a` side as `-`, leftover
 * lines on the `b` side as `+`. It is NOT a true diff (no LCS) — runs of
 * unchanged lines after a divergence may render as a delete-block + add-block
 * rather than a perfectly-aligned interleave. For preview purposes this is
 * fine and dependency-free.
 */
export function renderUnifiedDiff(
  a: Buffer,
  b: Buffer,
  opts: PreviewOptions = {},
): string {
  if (isBinary(a) || isBinary(b)) {
    const which = isBinary(a) ? a : b;
    return `(binary file — ${which.length} bytes)`;
  }
  const max = opts.maxLines ?? DEFAULT_MAX_LINES;
  const aLines = splitLines(a.toString("utf8"));
  const bLines = splitLines(b.toString("utf8"));

  const ops = diffLines(aLines, bLines);
  // Edge case (azp external review): when files differ ONLY in their
  // trailing newline, splitLines() drops the trailing empty element on
  // both sides (intentional — see the helper's docstring), so `ops` has
  // no `+`/`-` lines and the preview body would be silent. The caller
  // already knows the files differ (they wouldn't have made it here
  // otherwise), but a silent preview is confusing. Substitute an explicit
  // note so the user sees a body that explains "why is this preview empty".
  const hasChange = ops.some((op) => op.startsWith("+") || op.startsWith("-"));
  if (!hasChange && !a.equals(b)) {
    return "(files differ only in trailing whitespace / newline)";
  }
  const rendered: string[] = [];
  let i = 0;
  for (; i < ops.length; i++) {
    if (rendered.length >= max) break;
    const op = ops[i]!;
    rendered.push(op);
  }
  if (i < ops.length) {
    rendered.push(`(truncated, ${ops.length - i} more lines)`);
  }
  return rendered.join("\n");
}

/**
 * Render a "head" preview of one buffer — used for drift `added` entries
 * (there is no opposing buffer to diff against). Caps at `maxLines` lines
 * by default, and emits a `(truncated, N more lines)` footer when over.
 * Returns a binary placeholder when the buffer sniffs as binary.
 */
export function renderHeadPreview(
  bytes: Buffer,
  opts: PreviewOptions = {},
): string {
  if (isBinary(bytes)) {
    return `(binary file — ${bytes.length} bytes)`;
  }
  const max = opts.maxLines ?? HEAD_PREVIEW_LINES;
  const lines = splitLines(bytes.toString("utf8"));
  const out: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    if (out.length >= max) {
      out.push(`(truncated, ${lines.length - i} more lines)`);
      break;
    }
    out.push(lines[i]!);
  }
  return out.join("\n");
}

/**
 * Split a UTF-8 string into lines without trailing newlines. A trailing
 * newline produces no empty final element so `"a\nb\n"` and `"a\nb"` both
 * yield `["a", "b"]` — matches user expectations for "show me the lines".
 */
function splitLines(s: string): string[] {
  if (s === "") return [];
  const lines = s.split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/**
 * Compute a line-by-line diff between two arrays. Emits one diff line per
 * output entry, prefixed with ` ` (context), `-` (removed from a), or `+`
 * (added in b). Uses a simple two-pointer common-prefix / common-suffix
 * trim followed by a delete-then-add block for the divergent middle.
 *
 * This is NOT a Myers / LCS diff. It is the cheapest-correct approach for
 * "preview small changes inline" — for the typical config file edit (a few
 * lines changed in the middle), the prefix/suffix trim catches most of the
 * unchanged lines and the divergent block is small.
 */
function diffLines(a: ReadonlyArray<string>, b: ReadonlyArray<string>): string[] {
  const out: string[] = [];
  let prefix = 0;
  while (prefix < a.length && prefix < b.length && a[prefix] === b[prefix]) {
    prefix++;
  }
  let suffix = 0;
  while (
    suffix < a.length - prefix &&
    suffix < b.length - prefix &&
    a[a.length - 1 - suffix] === b[b.length - 1 - suffix]
  ) {
    suffix++;
  }
  // Emit prefix as context.
  for (let i = 0; i < prefix; i++) out.push(` ${a[i]!}`);
  // Emit middle: deletes from a, then adds from b.
  for (let i = prefix; i < a.length - suffix; i++) out.push(`-${a[i]!}`);
  for (let i = prefix; i < b.length - suffix; i++) out.push(`+${b[i]!}`);
  // Emit suffix as context.
  for (let i = a.length - suffix; i < a.length; i++) out.push(` ${a[i]!}`);
  return out;
}

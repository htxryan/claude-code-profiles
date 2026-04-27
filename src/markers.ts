/**
 * Single source of truth for the project-root `CLAUDE.md` managed-block
 * markers (cw6 / spec §12). Every consumer — `init` (this file), `validate`
 * (R44), `materialize` (R45), `drift detect` (R46) — reads regex + helpers
 * from here. There must be exactly one place this regex lives.
 *
 * Spec §12.3 canonical regex:
 *
 *     <!-- claude-profiles:v(\d+):begin([^>]*)-->([\s\S]*?)<!-- claude-profiles:v\1:end\2-->
 *
 * Capture groups:
 *   1. Version number (must match between :begin and :end via backref).
 *   2. Optional namespace tail (text between version and `-->`; whitespace-
 *      only in the canonical form — a single space, since the canonical
 *      marker is `<!-- claude-profiles:v1:begin -->`; reserved for future
 *      namespacing).
 *   3. The managed body — bytes the tool owns. Non-greedy so multiple managed
 *      blocks in a single file (a future possibility) do not collapse into
 *      one match.
 *
 * v1 contract: a well-formed file contains exactly one match. Zero matches
 * means markers are absent (R44 → init). More than one match is reserved and
 * rejected as malformed by parseMarkers (spec §12.3).
 */

/**
 * Canonical marker regex from spec §12.3. Use this constant, never inline a
 * copy elsewhere — that would split source-of-truth and risk skew between
 * consumers (init writes one shape, validate looks for another). The literal
 * here is verbatim from the spec; if the spec changes, this constant changes,
 * and every consumer benefits automatically.
 *
 * @internal — exported only so test introspection (`tests/markers.test.ts`)
 * can pin the regex shape without re-importing it from production code.
 * Production consumers MUST go through {@link parseMarkers} /
 * {@link renderManagedBlock} for the source-of-truth contract; treating the
 * raw regex as public API would re-create the very source-of-truth split this
 * module exists to prevent.
 */
export const MARKER_REGEX =
  /<!-- claude-profiles:v(\d+):begin([^>]*)-->([\s\S]*?)<!-- claude-profiles:v\1:end\2-->/;

/**
 * Successful parse: the file contained exactly one well-formed marker pair.
 * `before` and `after` are the bytes outside the markers (preserved on every
 * write); `section` is the body between them. Combined: `before + <begin> +
 * section + <end> + after === original content`.
 */
export interface ParseFound {
  found: true;
  before: string;
  section: string;
  after: string;
  /** Captured version integer (parsed from the `:vN:` field). */
  version: number;
}

/**
 * Failed parse. `absent` distinguishes "user has not run init yet" from
 * `malformed` — only `malformed` is reportable as a hard error in v1
 * (validate, materialize). Init treats both the same: write fresh.
 */
export interface ParseFailed {
  found: false;
  reason: "absent" | "malformed";
}

export type ParseResult = ParseFound | ParseFailed;

/**
 * Parse a CLAUDE.md (or arbitrary text) for the managed marker block.
 *
 * Why we manually verify "exactly one match" rather than rely on the regex:
 * the canonical regex without the `g` flag returns the FIRST match, which
 * makes the call site cheap, but a file with multiple `:begin`/`:end` pairs
 * would silently parse as if only the first one mattered. Spec §12.3 reserves
 * multi-block files as malformed; we check explicitly with a global re-scan
 * so that decision is made here once rather than scattered across consumers.
 *
 * Malformed cases caught:
 *   - lone `:begin` without matching `:end`
 *   - lone `:end` without matching `:begin`
 *   - version mismatch (begin v1 / end v2) — handled implicitly via the `\1`
 *     backref in the regex; the lone-marker check below catches the leftover.
 *   - more than one well-formed block.
 *
 * Line-ending policy (cw6.5 followup): parseMarkers is CRLF-tolerant. The
 * canonical regex's `[^>]*` (capture group 2) and `[\s\S]*?` (capture group
 * 3) both naturally match `\r`, so a CLAUDE.md saved with CRLF terminators
 * — Windows editors, default `git config core.autocrlf=true` checkouts —
 * parses identically to the LF form. The `before`/`section`/`after` slices
 * are returned with their on-disk bytes intact (CRLF preserved as CRLF, LF
 * as LF). The lone-marker malformed-check below also works on CRLF because
 * its regex anchors only on `<!-- claude-profiles:v\d+:(begin|end)`.
 *
 * Round-trip caveat: {@link renderManagedBlock} emits LF only. A user whose
 * pre-existing CLAUDE.md is CRLF will end up with LF *inside* the managed
 * block after the next materialize while LF/CRLF *outside* the markers is
 * preserved byte-for-byte (because before/after are copied verbatim). We
 * accept this asymmetry: the managed block is tool-owned, and forcing the
 * managed bytes to match the user's surrounding line endings would require
 * sniffing + re-encoding, which adds complexity for a vanishingly small
 * win on a tool that runs in a developer environment.
 */
export function parseMarkers(content: string): ParseResult {
  // Single-match check: the canonical regex (no `g`) returns null if no
  // well-formed block exists.
  const m = content.match(MARKER_REGEX);
  if (m === null) {
    // Distinguish absent vs malformed: a lone marker (begin OR end) means the
    // user (or a buggy tool) wrote a partial block — we should not silently
    // treat that as "absent" because re-running init would happily append a
    // second block, producing a malformed multi-block file.
    if (/<!-- claude-profiles:v\d+:(begin|end)/.test(content)) {
      return { found: false, reason: "malformed" };
    }
    return { found: false, reason: "absent" };
  }

  // Multi-block check: spec §12.3 reserves "more than one match" as malformed
  // for v1. Use a global flag to count occurrences. We could push this into
  // the regex itself, but the cost of one extra scan is trivial vs the
  // clarity of doing the check here and explaining why.
  const globalRegex = new RegExp(MARKER_REGEX.source, "g");
  let count = 0;
  while (globalRegex.exec(content) !== null) {
    count++;
    if (count > 1) {
      return { found: false, reason: "malformed" };
    }
  }

  // Splice the file: everything before the match index is `before`; from
  // match.index + matched length onward is `after`. m.index is guaranteed
  // defined for a non-null String.match result.
  const startIdx = m.index ?? 0;
  const endIdx = startIdx + m[0].length;
  const version = Number.parseInt(m[1] ?? "1", 10);
  return {
    found: true,
    before: content.slice(0, startIdx),
    section: m[3] ?? "",
    after: content.slice(endIdx),
    version,
  };
}

/**
 * Build the canonical managed block (begin marker + self-doc comment + body +
 * end marker) for the given section bytes. Default version is 1; `version` is
 * accepted explicitly so future migrations (v2+) can produce the right shape.
 *
 * The self-documenting comment line is part of the spec §12.2 "recommended
 * form" — it tells the next human reading the file what the block is and how
 * to regenerate it, which dramatically reduces the chance of someone editing
 * between the markers and watching their changes vanish on the next `use`.
 *
 * Body format: we surround the body with newlines so the result is always
 * well-formed even when `sectionBytes` is empty (the empty-section parse-test
 * checks this round-trip).
 */
export function renderManagedBlock(sectionBytes: string, version = 1): string {
  const begin = `<!-- claude-profiles:v${version}:begin -->`;
  const end = `<!-- claude-profiles:v${version}:end -->`;
  const selfDoc =
    "<!-- Managed block. Do not edit between markers — changes are overwritten on next `claude-profiles use`. -->";
  // Always emit a leading + trailing newline around the body so the markers
  // sit on their own lines regardless of whether sectionBytes is empty,
  // newline-prefixed, etc.
  const body = sectionBytes.length === 0 ? "\n" : `\n${sectionBytes}\n`;
  return `${begin}\n${selfDoc}\n${body}${end}\n`;
}

/**
 * Inverse of {@link renderManagedBlock} (cw6/T5): given the SECTION bytes
 * that {@link parseMarkers} extracted from a live file, recover the original
 * `body` parameter that was passed to renderManagedBlock. Used by `persist`
 * to write JUST the user-meaningful section body to
 * `.claude-profiles/<active>/CLAUDE.md` (the next `use` re-renders the
 * managed block from this body).
 *
 * Why this matters (round-trip invariant): the on-disk section that
 * parseMarkers returns is `\n<selfDoc>\n\n<body>\n` for a non-empty body
 * (renderManagedBlock framing). Persisting that verbatim would carry the
 * self-doc comment into the source file, and the next materialize would
 * re-wrap THAT (selfDoc + body) inside a fresh selfDoc + newlines —
 * accumulating duplicate self-doc lines on every round trip. This helper
 * undoes exactly the framing renderManagedBlock added.
 *
 * Robustness: we tolerate sections that don't have the framing we expect
 * (e.g. a future renderer variant, or a hand-edited section that lost the
 * self-doc line). In that case we return the section verbatim — the round
 * trip will still complete, just with the self-doc absent until the next
 * full materialize re-renders one.
 */
export function extractSectionBody(section: string): string {
  // Identify the self-doc prefix renderManagedBlock emits. Match it
  // tolerantly so a future cosmetic edit to the comment text doesn't break
  // round-trip — we recognize any HTML comment on the line directly after
  // the leading `\n`. If the section doesn't have a leading `\n<!-- … -->`
  // followed by `\n`, we return it verbatim (defensive).
  //
  // Expected shape for a freshly-rendered non-empty body:
  //   "\n<!-- Managed block. … -->\n\n<body>\n"
  // For a freshly-rendered empty body:
  //   "\n<!-- Managed block. … -->\n\n"
  // (renderManagedBlock uses `body = "\n"` for empty input, then concats
  // `${begin}\n${selfDoc}\n${body}${end}\n` so the section between the
  // markers becomes `\n<selfDoc>\n\n` — no third newline.)
  const m = section.match(/^\n<!--[^>]*-->\n\n([\s\S]*?)\n?$/);
  if (m === null) {
    // No recognizable framing — return as-is. Log nothing: a hand-edited
    // section is the user's prerogative, and `persist` is supposed to be
    // a faithful capture of the live state.
    return section;
  }
  return m[1] ?? "";
}

/**
 * Thrown by {@link injectMarkersIntoFile} when the input file already
 * contains a malformed claude-profiles block (lone `:begin`, lone `:end`,
 * version mismatch, or multiple blocks). Init refuses to append a second
 * fresh block on top of the broken bytes — that would leave the file
 * STILL malformed (now with two block fragments) and trip subsequent
 * `validate` / `use` calls in a confusing way. cw6.3 followup: fail closed
 * so the user knows to manually repair the file before re-running init.
 *
 * The error message is actionable: it names the exact remediation (delete
 * the partial markers and re-run init).
 */
export class MalformedMarkersError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "MalformedMarkersError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/**
 * For init: take existing CLAUDE.md content and ensure markers are present.
 *
 *   - If markers already exist (well-formed): return input unchanged (no-op).
 *   - If markers are absent: append a fresh empty managed block at end-of-
 *     file, preserving every byte above byte-for-byte. We add a single
 *     separating newline ONLY when the existing content does not already end
 *     in a newline, so we never silently mutate the user's trailing-newline
 *     convention.
 *
 * Malformed input — `parseMarkers` returns `reason: "malformed"` (lone
 * `:begin` or `:end`, version mismatch, or multi-block) — fails closed by
 * throwing {@link MalformedMarkersError} (cw6.3 followup). Appending a
 * second fresh block on top of broken bytes would leave the file still
 * malformed, just with two block fragments instead of one, and the user
 * would discover this only at the next `validate` / `use`. Failing here
 * shifts the discovery to init time and points the user at the repair
 * (delete the partial markers manually, re-run init).
 */
export function injectMarkersIntoFile(content: string): string {
  const parsed = parseMarkers(content);
  if (parsed.found) {
    // Idempotent: well-formed markers already present → no-op.
    return content;
  }
  if (parsed.reason === "malformed") {
    throw new MalformedMarkersError(
      "CLAUDE.md contains a malformed claude-profiles marker block (lone `:begin`, lone `:end`, version mismatch, or multiple blocks). Refusing to append a second block on top of broken markers — please delete the partial marker text manually and re-run `claude-profiles init`.",
    );
  }
  // Append a fresh empty managed block. Preserve the exact trailing-newline
  // state of `content`: if it ends in `\n` we don't double up; if it doesn't,
  // we insert a single separator so the begin marker lands on its own line.
  const separator = content.length === 0 || content.endsWith("\n") ? "" : "\n";
  return `${content}${separator}${renderManagedBlock("")}`;
}

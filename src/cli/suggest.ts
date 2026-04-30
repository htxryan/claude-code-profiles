/**
 * "Did you mean?" suggestions for typoed profile names (claude-code-profiles-
 * ppo). The pure helpers live here so command handlers can stay thin.
 *
 * The suggester is exclusively for unqualified CLI top-level lookups (`use
 * <typo>`, `diff <typo>`, `validate <typo>`); we deliberately do NOT enrich
 * structural-extends MissingProfileError (those have a `referencedBy` and the
 * remediation is "edit a profile.json", not "fix your typo").
 */

import { MissingProfileError } from "../errors/index.js";
import { listProfiles } from "../resolver/discover.js";
import { isValidProfileName } from "../resolver/paths.js";

import { CliUserError, EXIT_USER_ERROR } from "./exit.js";

/**
 * Levenshtein distance — minimum single-character edits (insertion, deletion,
 * substitution) required to turn `a` into `b`. Returns 0 when equal. Uses a
 * single-row DP rather than the textbook 2-D matrix to keep allocation small,
 * which matters when we run it across every profile in a project.
 */
export function levenshtein(a: string, b: string): number {
  if (a === b) return 0;
  if (a.length === 0) return b.length;
  if (b.length === 0) return a.length;

  // prev[j] = distance(a[..i-1], b[..j])
  const prev = new Array<number>(b.length + 1);
  for (let j = 0; j <= b.length; j++) prev[j] = j;

  for (let i = 1; i <= a.length; i++) {
    let prevDiag = prev[0]!;
    prev[0] = i;
    for (let j = 1; j <= b.length; j++) {
      const cost = a.charCodeAt(i - 1) === b.charCodeAt(j - 1) ? 0 : 1;
      const tmp = prev[j]!;
      prev[j] = Math.min(
        prev[j]! + 1, // deletion
        prev[j - 1]! + 1, // insertion
        prevDiag + cost, // substitution
      );
      prevDiag = tmp;
    }
  }
  return prev[b.length]!;
}

/**
 * Return up to `max` candidate names within Levenshtein distance ≤ 2 of
 * `query`, ordered by ascending distance then lex. Empty candidate list and
 * "no match within threshold" both return [].
 *
 * Threshold of 2 catches the common typo cases (one transposition,
 * adjacent missing/extra char, single-char wrong) without surfacing
 * unrelated names — for short profile names like "a"/"b" a higher threshold
 * would suggest every other profile in the project, which isn't useful.
 */
export function suggestProfiles(
  query: string,
  candidates: ReadonlyArray<string>,
  max = 3,
): string[] {
  const MAX_DISTANCE = 2;
  const scored: Array<{ name: string; d: number }> = [];
  for (const name of candidates) {
    if (name === query) continue; // exact match shouldn't end up here, but skip defensively
    const d = levenshtein(query, name);
    if (d <= MAX_DISTANCE) scored.push({ name, d });
  }
  scored.sort((x, y) => x.d - y.d || (x.name < y.name ? -1 : x.name > y.name ? 1 : 0));
  return scored.slice(0, max).map((s) => s.name);
}

/**
 * Render a comma-separated "did you mean: x?" or "did you mean: x, y, z?"
 * suffix. Empty array → empty string so callers can append unconditionally.
 *
 * Returns the bare phrase WITHOUT a leading separator; callers wrap it
 * (parenthetical or em-dash) so each error class can keep its own style.
 */
export function formatDidYouMean(suggestions: ReadonlyArray<string>): string {
  if (suggestions.length === 0) return "";
  return `I do beg your pardon. Did you perhaps mean: ${suggestions.join(", ")}?`;
}

/**
 * Standard wording for a name that fails `isValidProfileName`. Shared by every
 * verb that takes a profile name (new, use, diff, validate) so users see one
 * consistent string regardless of which command they typed.
 */
export function formatInvalidProfileNameMessage(verb: string, name: string): string {
  return (
    `${verb}: invalid profile name "${name}" ` +
    `(contains /, \\, leading . or _, NUL, trailing dot/space, or a Windows-reserved name like CON/PRN/AUX/NUL/COM1-9/LPT1-9)`
  );
}

/**
 * Pre-flight: throw a CliUserError with the standardized invalid-name wording
 * when `name` fails `isValidProfileName`. The `verb` prefix lets the user
 * see which CLI subcommand they invoked (use/diff/validate) without having
 * to scroll up.
 */
export function assertValidProfileName(verb: string, name: string): void {
  if (!isValidProfileName(name)) {
    throw new CliUserError(formatInvalidProfileNameMessage(verb, name), EXIT_USER_ERROR);
  }
}

/**
 * If `err` is a top-level (referencedBy === undefined) MissingProfileError
 * for `typedName`, return a fresh MissingProfileError carrying suggestions
 * derived from the project's profile list. Otherwise return `err` unchanged.
 *
 * We deliberately rebuild the error rather than mutating its `.message`
 * because Error.message is generally treated as immutable by tooling and we
 * want the constructor invariants (name/code/missing/referencedBy) to stay
 * locked in one place.
 */
export async function enrichMissingProfileError(
  err: unknown,
  projectRoot: string,
  typedName: string,
): Promise<unknown> {
  if (!(err instanceof MissingProfileError)) return err;
  if (err.referencedBy !== undefined) return err;
  if (err.missing !== typedName) return err;
  if (err.suggestions.length > 0) return err; // already enriched
  let candidates: string[] = [];
  try {
    candidates = await listProfiles({ projectRoot });
  } catch {
    // listProfiles tolerates a missing .claude-profiles dir already; any
    // other I/O fault here shouldn't mask the original MissingProfileError.
    return err;
  }
  const suggestions = suggestProfiles(typedName, candidates);
  if (suggestions.length === 0) return err;
  return new MissingProfileError(err.missing, undefined, suggestions);
}

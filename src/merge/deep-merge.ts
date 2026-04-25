/**
 * R8 + R12: deep-merge strategy for `settings.json`.
 *
 * Default semantics (R8):
 *   - Objects merge recursively.
 *   - Arrays at the same path are REPLACED by the later contributor.
 *   - Scalars (string/number/boolean/null) — later wins.
 *   - Type mismatches (e.g. object vs array) — later wins.
 *
 * Carve-out (R12, takes precedence over R8):
 *   - At the path `hooks.<EventName>`, action arrays are CONCATENATED in
 *     contributor order rather than replaced. The surrounding `hooks` object
 *     still deep-merges (so different events from different contributors
 *     accumulate without clobbering).
 *
 * R12 fires only when both sides at `hooks.<EventName>` are arrays. If a
 * contributor sets `hooks.<EventName>` to a non-array (an explicit unset of
 * sorts), the carve-out is bypassed and the normal R8 last-wins rule applies
 * for that event — that contributor "claims" the slot, and a later array
 * contribution will replace per R8 again. This is rare in practice but keeps
 * the rule local and predictable.
 */

import { InvalidSettingsJsonError } from "../errors/index.js";

import type { ContributorBytes, MergeStrategy, StrategyResult } from "./types.js";

type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | { [key: string]: JsonValue };

type JsonObject = { [key: string]: JsonValue };

export const deepMergeStrategy: MergeStrategy = (
  relPath: string,
  inputs: ContributorBytes[],
): StrategyResult => {
  if (inputs.length === 0) {
    throw new Error(`deep-merge invoked with no inputs for "${relPath}"`);
  }

  const parsed: Array<{ id: string; value: JsonValue }> = [];
  for (const input of inputs) {
    const text = input.bytes.toString("utf8");
    if (text.trim() === "") {
      // Treat empty file as an empty object — equivalent to "this contributor
      // has nothing to add". Avoids JSON.parse choking on whitespace.
      parsed.push({ id: input.id, value: {} });
      continue;
    }
    let value: JsonValue;
    try {
      value = JSON.parse(text) as JsonValue;
    } catch (err) {
      const detail = err instanceof Error ? err.message : String(err);
      throw new InvalidSettingsJsonError(relPath, input.id, detail);
    }
    parsed.push({ id: input.id, value });
  }

  // The merge proceeds left-to-right (oldest → newest). The result starts as
  // the first contributor's value and absorbs each subsequent contributor.
  let acc: JsonValue = clone(parsed[0]!.value);
  for (let i = 1; i < parsed.length; i++) {
    acc = mergeAt([], acc, parsed[i]!.value);
  }

  // Track which contributors actually contributed — for settings.json we
  // consider all contributors that parsed to a non-empty object as having
  // contributed. An empty object `{}` adds nothing semantically but the
  // contributor still "owns" the file in provenance, so we include it.
  const contributors = parsed.map((p) => p.id);

  const out = JSON.stringify(acc, null, 2) + "\n";
  return {
    bytes: Buffer.from(out, "utf8"),
    contributors,
  };
};

/**
 * Recursively merge `later` into `earlier`. Returns a new value (no in-place
 * mutation of inputs). `pathParts` is the dotted path used to detect the R12
 * carve-out at `hooks.<EventName>`.
 */
function mergeAt(pathParts: string[], earlier: JsonValue, later: JsonValue): JsonValue {
  // R12 carve-out: at hooks.<EventName>, concat arrays instead of replacing.
  if (
    pathParts.length === 2 &&
    pathParts[0] === "hooks" &&
    Array.isArray(earlier) &&
    Array.isArray(later)
  ) {
    return [...earlier, ...later];
  }

  // Both objects → deep merge field-by-field.
  if (isPlainObject(earlier) && isPlainObject(later)) {
    const out: JsonObject = {};
    // Copy `earlier` keys (cloned so we don't share refs with input).
    for (const [k, v] of Object.entries(earlier)) {
      out[k] = clone(v);
    }
    for (const [k, v] of Object.entries(later)) {
      if (k in out) {
        out[k] = mergeAt([...pathParts, k], out[k]!, v);
      } else {
        out[k] = clone(v);
      }
    }
    return out;
  }

  // R8 default for arrays: replace, not concat.
  // Scalar / type-mismatch: later wins.
  return clone(later);
}

// Relies on JSON.parse being the sole source of these objects, which always
// produces values with Object.prototype. Hand-constructed prototype-less
// objects (Object.create(null)) would not satisfy this check.
function isPlainObject(v: unknown): v is JsonObject {
  return (
    typeof v === "object" &&
    v !== null &&
    !Array.isArray(v) &&
    Object.getPrototypeOf(v) === Object.prototype
  );
}

function clone<T extends JsonValue>(v: T): T {
  if (v === null || typeof v !== "object") return v;
  if (Array.isArray(v)) return v.map((x) => clone(x)) as T;
  const out: JsonObject = {};
  for (const [k, val] of Object.entries(v)) {
    out[k] = clone(val);
  }
  return out as T;
}

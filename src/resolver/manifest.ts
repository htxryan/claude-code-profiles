import { promises as fs } from "node:fs";
import * as path from "node:path";

import { InvalidManifestError } from "../errors/index.js";

import type { ProfileManifest, ResolutionWarning } from "./types.js";

const KNOWN_FIELDS = new Set([
  "name",
  "description",
  "extends",
  "includes",
  "tags",
]);

export interface ManifestLoadResult {
  manifest: ProfileManifest;
  warnings: ResolutionWarning[];
}

/**
 * Load and validate a profile.json. Returns the parsed manifest plus any
 * non-fatal warnings (R36 unknown-field warnings, missing-manifest warnings).
 *
 * Behavior:
 *  - Missing profile.json → empty manifest + MissingManifest warning. R35
 *    permits all fields to be optional, so a missing manifest is treated as
 *    "default everything" rather than an abort.
 *  - Unparseable JSON → throws InvalidManifestError.
 *  - Wrong types (e.g. extends as number) → throws InvalidManifestError.
 *  - Unknown fields → kept out of the returned manifest, surfaced as warnings.
 */
export async function loadManifest(
  profileDir: string,
  source: string,
): Promise<ManifestLoadResult> {
  const manifestPath = path.join(profileDir, "profile.json");
  const warnings: ResolutionWarning[] = [];

  let raw: string;
  try {
    raw = await fs.readFile(manifestPath, "utf8");
  } catch (err) {
    const e = err as NodeJS.ErrnoException;
    if (e.code === "ENOENT") {
      warnings.push({
        code: "MissingManifest",
        message: `No profile.json at ${manifestPath}; using defaults`,
        source,
      });
      return { manifest: {}, warnings };
    }
    throw new InvalidManifestError(manifestPath, e.message);
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    const e = err as Error;
    throw new InvalidManifestError(manifestPath, `JSON parse error: ${e.message}`);
  }

  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new InvalidManifestError(
      manifestPath,
      "expected a JSON object at the root",
    );
  }

  const obj = parsed as Record<string, unknown>;
  const manifest: ProfileManifest = {};

  if (obj["name"] !== undefined) {
    if (typeof obj["name"] !== "string") {
      throw new InvalidManifestError(manifestPath, '"name" must be a string');
    }
    manifest.name = obj["name"];
  }
  if (obj["description"] !== undefined) {
    if (typeof obj["description"] !== "string") {
      throw new InvalidManifestError(manifestPath, '"description" must be a string');
    }
    manifest.description = obj["description"];
  }
  if (obj["extends"] !== undefined) {
    if (typeof obj["extends"] !== "string") {
      throw new InvalidManifestError(manifestPath, '"extends" must be a string');
    }
    manifest.extends = obj["extends"];
  }
  if (obj["includes"] !== undefined) {
    if (
      !Array.isArray(obj["includes"]) ||
      !obj["includes"].every((x): x is string => typeof x === "string")
    ) {
      throw new InvalidManifestError(
        manifestPath,
        '"includes" must be an array of strings',
      );
    }
    manifest.includes = obj["includes"];
  }
  if (obj["tags"] !== undefined) {
    if (
      !Array.isArray(obj["tags"]) ||
      !obj["tags"].every((x): x is string => typeof x === "string")
    ) {
      throw new InvalidManifestError(manifestPath, '"tags" must be an array of strings');
    }
    manifest.tags = obj["tags"];
  }

  for (const key of Object.keys(obj)) {
    if (!KNOWN_FIELDS.has(key)) {
      warnings.push({
        code: "UnknownManifestField",
        message: `Unknown field "${key}" in ${manifestPath}`,
        source,
      });
    }
  }

  return { manifest, warnings };
}

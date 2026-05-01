// Shared utilities for the IV (Integration Verification) script.
// Each module exports a `run(ctx)` function that returns
// `{ name, contract, ok, details }`. The orchestrator collects results
// and prints a summary table.

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const __filename = fileURLToPath(import.meta.url);
export const SCRIPT_DIR = dirname(__filename);
export const SITE_DIR = resolve(SCRIPT_DIR, '..', '..');
export const DIST_DIR = resolve(SITE_DIR, 'dist');
export const BASELINE_DIR = resolve(SITE_DIR, 'test-baselines');

export const PRODUCTION_URL = 'https://getc3p.dev';

export function ok(name, contract, details = '') {
  return { name, contract, ok: true, level: 'pass', details };
}

export function fail(name, contract, details) {
  return { name, contract, ok: false, level: 'fail', details };
}

// `warn` is a soft failure: surfaced in output and counted in the
// summary, but does not flip the exit code. Used for issues that have
// been filed as bugs and are tracked outside the IV pass/fail axis
// (e.g. infrastructure misconfiguration that requires CF dashboard
// access — see claude-code-profiles-rsf for an example).
export function warn(name, contract, details) {
  return { name, contract, ok: true, level: 'warn', details };
}

export function logStep(label) {
  process.stdout.write(`→ ${label}\n`);
}

// Wraps `module.run(ctx)` so a thrown error becomes a `fail()` result rather
// than aborting the whole pipeline. Keeps every contract reportable even when
// one fails — F-2 says every row gets at least one passing test, which is
// only checkable if every row gets a result.
export async function safeRun(name, contract, fn) {
  const start = Date.now();
  try {
    const result = await fn();
    const elapsed = ((Date.now() - start) / 1000).toFixed(1);
    return { ...result, elapsedSec: elapsed };
  } catch (err) {
    const elapsed = ((Date.now() - start) / 1000).toFixed(1);
    return {
      ...fail(name, contract, `threw: ${err.message}`),
      elapsedSec: elapsed,
    };
  }
}

// C2 — token snapshot. The spec assertion "Snapshot test: enumerate
// tokens.css exported names" is implemented in scripts/check-tokens.mjs
// (E2 fitness function). We invoke it as a child process so a token
// rename in tokens.css fails IV via the C2 row, not just the E2-local
// `pnpm check:tokens` script.
//
// Spawning child node is overhead-free in this context (< 200ms) and
// avoids duplicating the long token list.

import { spawn } from 'node:child_process';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fail, ok } from './util.mjs';

const SCRIPT = resolve(
  fileURLToPath(import.meta.url),
  '..',
  '..',
  'check-tokens.mjs'
);

export async function run() {
  return new Promise((resolveRun) => {
    const child = spawn(process.execPath, [SCRIPT], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (b) => (stdout += b.toString()));
    child.stderr.on('data', (b) => (stderr += b.toString()));
    child.on('close', (code) => {
      if (code !== 0) {
        const detail = (stderr || stdout)
          .split('\n')
          .filter(Boolean)
          .slice(0, 6)
          .join('; ');
        resolveRun([
          fail(
            'tokens-snapshot',
            'C2',
            `check-tokens.mjs exit ${code}: ${detail}`
          ),
        ]);
        return;
      }
      // The script prints OK lines; pull the token-count line for
      // summary readability.
      const summary = stdout
        .split('\n')
        .find((l) => l.startsWith('OK:'))
        ?.replace(/^OK: /, '');
      resolveRun([
        ok(
          'tokens-snapshot',
          'C2',
          summary ?? 'all required tokens + Starlight hooks present'
        ),
      ]);
    });
    child.on('error', (err) => {
      resolveRun([
        fail('tokens-snapshot', 'C2', `spawn failed: ${err.message}`),
      ]);
    });
  });
}

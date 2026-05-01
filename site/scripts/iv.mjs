#!/usr/bin/env node
/**
 * Integration Verification (IV) script — E6.
 *
 * Validates cross-epic interface contracts (C1–C5) end-to-end against
 * a freshly-built `dist/` and (optionally) the deployed production URL.
 *
 * Spec: docs/specs/getc3p-web-presence.md (E6 contracts table)
 *
 * Usage:
 *   pnpm iv                      # full run
 *   pnpm iv --skip-lighthouse    # fast pass (no perf gates)
 *   pnpm iv --skip-smoke         # offline (no production checks)
 *   pnpm iv --update-baselines   # rewrite test-baselines/ from current build
 *   pnpm iv --verbose            # show all results, including OK rows
 *
 * Exit codes:
 *   0 — all contracts pass
 *   1 — at least one contract failed
 *   2 — environment problem (e.g. dist/ missing and build failed)
 */

import { spawn } from 'node:child_process';
import { stat } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { startServer } from './iv/server.mjs';
import { safeRun } from './iv/util.mjs';

import * as composition from './iv/composition.mjs';
import * as smoke from './iv/smoke.mjs';
import * as links from './iv/links.mjs';
import * as pagefind from './iv/pagefind.mjs';
import * as mermaid from './iv/mermaid.mjs';
import * as tokens from './iv/tokens.mjs';
import * as theme from './iv/theme.mjs';
import * as hero from './iv/hero.mjs';
import * as axe from './iv/axe.mjs';
import * as visual from './iv/visual.mjs';
import * as lighthouse from './iv/lighthouse.mjs';

const SITE_DIR = resolve(fileURLToPath(import.meta.url), '..', '..');
const DIST_DIR = resolve(SITE_DIR, 'dist');

function parseArgs(argv) {
  const args = {
    skipLighthouse: false,
    skipSmoke: false,
    skipBuild: false,
    updateBaselines: false,
    verbose: false,
  };
  for (const a of argv) {
    if (a === '--skip-lighthouse') args.skipLighthouse = true;
    else if (a === '--skip-smoke') args.skipSmoke = true;
    else if (a === '--skip-build') args.skipBuild = true;
    else if (a === '--update-baselines') args.updateBaselines = true;
    else if (a === '--verbose') args.verbose = true;
    else if (a === '--help' || a === '-h') {
      printHelp();
      process.exit(0);
    } else {
      console.error(`Unknown arg: ${a}`);
      printHelp();
      process.exit(2);
    }
  }
  return args;
}

function printHelp() {
  console.log(`Integration Verification — E6
Usage: node scripts/iv.mjs [flags]

Flags:
  --skip-lighthouse     Skip the Lighthouse perf/a11y gates (fast pass)
  --skip-smoke          Skip the production-URL smoke checks
  --skip-build          Don't rebuild dist/ before running
  --update-baselines    Overwrite visual baselines from the current build
  --verbose             Show every result row (default: only failures)
  --help, -h            This message`);
}

async function distExists() {
  try {
    const s = await stat(DIST_DIR);
    return s.isDirectory();
  } catch {
    return false;
  }
}

async function runBuild() {
  // Re-use the existing pnpm build script so we exercise the same
  // command CF Pages runs. Using spawn (not exec) so the live build
  // log streams to the IV runner's stdout.
  return new Promise((resolveBuild, reject) => {
    const child = spawn('pnpm', ['build'], {
      cwd: SITE_DIR,
      stdio: 'inherit',
      env: { ...process.env },
    });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) resolveBuild();
      else reject(new Error(`pnpm build exited with code ${code}`));
    });
  });
}

function fmtRow(r) {
  const status = r.level === 'warn' ? 'WARN' : r.ok ? 'PASS' : 'FAIL';
  const time = r.elapsedSec ? ` (${r.elapsedSec}s)` : '';
  return `[${status}] ${r.contract.padEnd(5)} ${r.name.padEnd(28)}${time}  ${r.details}`;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const overallStart = Date.now();

  console.log('━━━ Integration Verification (E6) ━━━');

  // 1. Build (unless --skip-build).
  if (!args.skipBuild) {
    const have = await distExists();
    if (!have) {
      console.log('→ dist/ missing, building...');
    } else {
      console.log('→ Rebuilding to ensure fresh artifacts...');
    }
    try {
      await runBuild();
    } catch (err) {
      console.error(`✗ build failed: ${err.message}`);
      process.exit(2);
    }
  } else if (!(await distExists())) {
    console.error('✗ dist/ missing and --skip-build set; run `pnpm build` first.');
    process.exit(2);
  }

  // 2. Start local static server. Most modules need a live HTTP origin
  //    (axe, pagefind, theme, visual, lighthouse).
  console.log('→ Starting local static server over dist/...');
  const server = await startServer(DIST_DIR);
  console.log(`  ${server.baseUrl}`);

  const ctx = {
    localBaseUrl: server.baseUrl,
    skipLighthouse: args.skipLighthouse,
    skipSmoke: args.skipSmoke,
    updateBaselines: args.updateBaselines,
  };

  const allResults = [];
  try {
    // Order: cheap structural checks first, expensive browser-backed
    // checks last. Lets a fast-fail short-circuit Lighthouse runs when
    // the build is clearly broken.
    const phases = [
      { name: 'C1 composition', fn: () => composition.run(ctx) },
      { name: 'C2 token snapshot', fn: () => tokens.run(ctx) },
      { name: 'C4 mermaid SSR', fn: () => mermaid.run(ctx) },
      { name: 'C4 link integrity', fn: () => links.run(ctx) },
      { name: 'C4 pagefind', fn: () => pagefind.run(ctx) },
      { name: 'C2 theme + reduced-motion', fn: () => theme.run(ctx) },
      { name: 'C3 hero demo', fn: () => hero.run(ctx) },
      { name: 'C3 axe-core', fn: () => axe.run(ctx) },
      { name: 'C2 visual baselines', fn: () => visual.run(ctx) },
      { name: 'C3/C4 lighthouse', fn: () => lighthouse.run(ctx) },
    ];

    if (!args.skipSmoke) {
      // Smoke runs against the production URL — independent of local
      // server, so we add it first as a parallel-friendly check. We run
      // it sequentially anyway to keep output ordered.
      phases.unshift({ name: 'C5 production smoke', fn: () => smoke.run(ctx) });
    }

    for (const phase of phases) {
      console.log(`\n→ ${phase.name}`);
      const phaseStart = Date.now();
      const phaseResults = await safeRun(phase.name, '-', async () => ({
        results: await phase.fn(),
      }));
      const elapsed = ((Date.now() - phaseStart) / 1000).toFixed(1);
      if (phaseResults.results) {
        for (const r of phaseResults.results) {
          allResults.push(r);
          // Always log warns and failures; pass rows only in verbose.
          if (args.verbose || !r.ok || r.level === 'warn') {
            console.log(`  ${fmtRow(r)}`);
          }
        }
      } else {
        // The phase itself threw outside any sub-result.
        const synthetic = {
          name: phase.name,
          contract: '-',
          ok: false,
          details: phaseResults.details ?? 'phase threw',
          elapsedSec: elapsed,
        };
        allResults.push(synthetic);
        console.log(`  ${fmtRow(synthetic)}`);
      }
      console.log(`  (${elapsed}s)`);
    }
  } finally {
    await server.stop();
  }

  // 3. Summary.
  const fails = allResults.filter((r) => !r.ok);
  const warns = allResults.filter((r) => r.level === 'warn');
  const passes = allResults.filter((r) => r.ok && r.level !== 'warn');
  const totalSec = ((Date.now() - overallStart) / 1000).toFixed(1);

  console.log('\n━━━ Summary ━━━');
  console.log(
    `  ${passes.length} passing, ${warns.length} warn, ${fails.length} failing`
  );
  console.log(`  total runtime: ${totalSec}s`);

  // Contract coverage table: F-2 says every contract row C1–C5 has at
  // least one passing test. Surface this explicitly so coverage gaps
  // don't hide behind a green log.
  const contracts = ['C1', 'C2', 'C3', 'C4', 'C5'];
  console.log('\n  Contract coverage:');
  for (const c of contracts) {
    const cResults = allResults.filter((r) => r.contract === c || r.contract.includes(c));
    const cPass = cResults.filter((r) => r.ok).length;
    const cFail = cResults.filter((r) => !r.ok).length;
    if (cResults.length === 0) {
      console.log(`    ${c}  ⚠  no results (run skipped?)`);
    } else if (cPass > 0) {
      console.log(`    ${c}  ✓  ${cPass} pass${cFail > 0 ? `, ${cFail} fail` : ''}`);
    } else {
      console.log(`    ${c}  ✗  all ${cFail} failed`);
    }
  }

  if (warns.length > 0) {
    console.log('\n  Warnings (tracked separately, not blocking):');
    for (const r of warns) {
      console.log(`    - ${r.contract} ${r.name}: ${r.details}`);
    }
  }

  if (fails.length > 0) {
    console.log('\n  Failures:');
    for (const r of fails) console.log(`    - ${r.contract} ${r.name}: ${r.details}`);
    process.exit(1);
  }

  console.log('\n  ✓ all contracts passing');
  process.exit(0);
}

main().catch((err) => {
  console.error(`✗ IV runner crashed: ${err.stack || err.message}`);
  process.exit(2);
});

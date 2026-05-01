#!/usr/bin/env node
/**
 * Token-contract smoke test (E2 fitness function).
 *
 * The epic declares these CSS custom-property names as load-bearing —
 * downstream epics (E3 marketing, E4 docs) consume them by name. Renaming
 * any of them requires migration. This script asserts the bundled CSS
 * still ships every name; failure means we silently broke the contract.
 *
 * It also checks that the dark-theme block and the reduced-motion media
 * query are present (R-S-1 / fitness function F-5).
 *
 * Run via `pnpm check:tokens` after `pnpm build`.
 */

import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import process from 'node:process';

// `fileURLToPath` handles Windows drive letters and URL-encoded characters
// (spaces in paths) that bare `URL.pathname` mishandles.
const ASTRO_DIR = fileURLToPath(new URL('../dist/_astro/', import.meta.url));

// Load-bearing names from `docs/specs/getc3p-web-presence.md` E2 contract.
const REQUIRED_TOKENS = [
  '--color-bg-primary',
  '--color-bg-surface',
  '--color-text-primary',
  '--color-text-muted',
  '--color-accent',
  '--color-border',
  '--type-scale-1',
  '--type-scale-2',
  '--type-scale-3',
  '--type-scale-4',
  '--type-scale-5',
  '--type-scale-6',
  '--type-leading-tight',
  '--type-leading-normal',
  '--type-leading-relaxed',
  '--motion-duration-xs',
  '--motion-duration-sm',
  '--motion-duration-md',
  '--motion-duration-lg',
  '--motion-easing-standard',
  '--motion-easing-emphasis',
  '--space-1',
  '--space-2',
  '--space-3',
  '--space-4',
  '--space-5',
  '--space-6',
  '--space-7',
  '--space-8',
  '--radius-sm',
  '--radius-md',
  '--radius-lg',
];

// Starlight hooks we promise to remap (E2 "consumes Starlight CSS hooks").
const REQUIRED_STARLIGHT_HOOKS = [
  '--sl-color-bg',
  '--sl-color-text',
  '--sl-color-accent',
  '--sl-font',
];

let cssFile;
try {
  const entries = readdirSync(ASTRO_DIR);
  cssFile = entries.find((f) => f.startsWith('global.') && f.endsWith('.css'));
  if (!cssFile) {
    console.error('FAIL: no global.*.css found in dist/_astro/. Did you run `pnpm build`?');
    process.exit(1);
  }
} catch (err) {
  console.error(`FAIL: cannot read dist/_astro/ — ${err.message}`);
  console.error('Hint: run `pnpm build` first.');
  process.exit(1);
}

const css = readFileSync(join(ASTRO_DIR, cssFile), 'utf8');

const missing = [];
for (const name of [...REQUIRED_TOKENS, ...REQUIRED_STARLIGHT_HOOKS]) {
  if (!css.includes(name)) {
    missing.push(name);
  }
}

// Theme convention mirrors Starlight: dark at `:root`, light at
// `:root[data-theme="light"]`. We assert the light selector exists.
// Build pipeline minifies attribute-selector quotes — accept any form.
const hasLightBlock =
  css.includes('[data-theme="light"]') ||
  css.includes("[data-theme='light']") ||
  css.includes('[data-theme=light]');
if (!hasLightBlock) {
  missing.push('[data-theme="light"] override block');
}
if (!css.includes('prefers-reduced-motion')) {
  missing.push('@media (prefers-reduced-motion: reduce) block');
}

if (missing.length > 0) {
  console.error('FAIL: bundled CSS is missing required tokens / structures:');
  for (const m of missing) console.error(`  - ${m}`);
  process.exit(1);
}

console.log(`OK: ${REQUIRED_TOKENS.length} tokens + ${REQUIRED_STARLIGHT_HOOKS.length} Starlight hooks present in ${cssFile}`);
console.log('OK: [data-theme="light"] override block present');
console.log('OK: prefers-reduced-motion block present');

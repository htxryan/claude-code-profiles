#!/usr/bin/env node
/**
 * Generate `site/public/og.png` (1200×630) from a hand-crafted SVG.
 *
 * Run via `pnpm og` whenever the OG art needs to be refreshed. The
 * resulting PNG is checked in so production deploys don't need sharp
 * at build time on Cloudflare Pages.
 *
 * Per spec, v1 ships a single static OG card; per-route generation is
 * deferred (R-U-5, AS-1).
 */

import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

import sharp from 'sharp';

const ROOT = fileURLToPath(new URL('..', import.meta.url));
const OUT = `${ROOT}public/og.png`;

const W = 1200;
const H = 630;

// Hand-rolled SVG. Colors mirror the dark-theme tokens (kept in sync
// manually — the OG art only ships in one mode). Fonts fall back to
// SVG defaults if the system font isn't available; for v1 that's an
// acceptable trade against shipping a 100kB+ font file alongside.
const svg = `
<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#0b1120"/>
      <stop offset="100%" stop-color="#111827"/>
    </linearGradient>
    <linearGradient id="accent" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0%" stop-color="#818cf8"/>
      <stop offset="100%" stop-color="#a5b4fc"/>
    </linearGradient>
    <style>
      .brand { font: 700 56px ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; fill: #f1f5f9; letter-spacing: -0.01em; }
      .lede  { font: 500 36px ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; fill: #cbd5e1; }
      .meta  { font: 600 22px ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; fill: #94a3b8; letter-spacing: 0.06em; }
      .term  { font: 500 26px ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace; fill: #f1f5f9; }
      .term-prompt { fill: #818cf8; }
      .term-warn   { fill: #f87171; font-weight: 700; }
      .term-path   { fill: #fbbf24; }
      .term-muted  { fill: #94a3b8; }
    </style>
  </defs>

  <rect width="${W}" height="${H}" fill="url(#bg)"/>

  <!-- left rail accent bar -->
  <rect x="0" y="0" width="8" height="${H}" fill="url(#accent)"/>

  <!-- top meta line -->
  <text x="80" y="96" class="meta">GETC3P.DEV</text>

  <!-- headline -->
  <text x="80" y="190" class="brand">Swap Claude configs</text>
  <text x="80" y="258" class="brand">without losing work.</text>

  <!-- subhead -->
  <text x="80" y="322" class="lede">Profile-based config swaps for Claude Code</text>
  <text x="80" y="364" class="lede">that refuse to overwrite uncommitted edits.</text>

  <!-- terminal card -->
  <g transform="translate(80, 410)">
    <rect x="0" y="0" width="1040" height="170" rx="16" ry="16" fill="#1e293b" stroke="#334155" stroke-width="1"/>
    <circle cx="28" cy="28" r="6" fill="#ef4444"/>
    <circle cx="50" cy="28" r="6" fill="#f59e0b"/>
    <circle cx="72" cy="28" r="6" fill="#10b981"/>
    <text x="32" y="80" class="term"><tspan class="term-prompt">$</tspan> c3p use dev</text>
    <text x="32" y="124" class="term"><tspan class="term-warn">refused</tspan> uncommitted edits in <tspan class="term-path">.claude/settings.json</tspan></text>
    <text x="32" y="156" class="term term-muted">drift gate kept your work safe.</text>
  </g>
</svg>
`.trim();

mkdirSync(dirname(OUT), { recursive: true });

await sharp(Buffer.from(svg))
  .png({ compressionLevel: 9 })
  .toFile(OUT);

console.log(`OK: wrote ${OUT} (${W}×${H})`);

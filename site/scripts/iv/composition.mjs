// C1 — composition contract: site/ scaffold builds against pnpm + Node 20
// and `dist/` contains the expected route artifacts.
//
// The build is invoked by `iv.mjs` itself before this module runs. Here
// we just assert the artifacts exist — the structural contract that
// downstream epics (E2/E3/E4/E5) consume.

import { stat } from 'node:fs/promises';
import { join } from 'node:path';
import { DIST_DIR, fail, ok } from './util.mjs';

// Each entry is a path relative to dist/ that must exist after build.
// Order/grouping mirrors the spec: marketing → docs → ops assets.
const REQUIRED = [
  // Marketing surface (R-U-1, R-U-6)
  'index.html',
  '404.html',
  // OG asset (R-U-5)
  'og.png',
  // Docs surface (R-U-2)
  'docs/index.html',
  // Concepts (R-U-7 + R-U-16)
  'docs/concepts/profile/index.html',
  'docs/concepts/extends/index.html',
  'docs/concepts/includes/index.html',
  'docs/concepts/drift/index.html',
  'docs/concepts/materialize/index.html',
  // CLI reference (R-U-8) — spot-check one verb per group
  'docs/cli/init/index.html',
  'docs/cli/drift/index.html',
  'docs/cli/doctor/index.html',
  // Guides + About
  'docs/guides/quickstart/index.html',
  'docs/about/index.html',
  // Search index (R-U-3) — Pagefind emits at dist/pagefind/, not under
  // /docs/, even though it indexes /docs/** content.
  'pagefind/pagefind-entry.json',
  // Sitemap (Astro auto-generated — proves SSG run completed end-to-end)
  'sitemap-index.xml',
];

export async function run() {
  const missing = [];
  for (const rel of REQUIRED) {
    try {
      const s = await stat(join(DIST_DIR, rel));
      if (!s.isFile()) {
        missing.push(`${rel} (not a file)`);
      }
    } catch {
      missing.push(rel);
    }
  }

  if (missing.length > 0) {
    return [
      fail(
        'composition',
        'C1',
        `${missing.length} expected artifact(s) missing: ${missing.slice(0, 5).join(', ')}${missing.length > 5 ? '...' : ''}`
      ),
    ];
  }
  return [
    ok(
      'composition',
      'C1',
      `${REQUIRED.length} expected artifacts present in dist/`
    ),
  ];
}

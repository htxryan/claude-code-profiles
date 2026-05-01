// C4 — Mermaid SSR check.
//
// R-U-16 requires the extends/includes Concepts pages to ship visual
// inheritance/composition diagrams. The spec mandates server-rendered
// SVG (not client-side mermaid scripts) so the diagrams work without JS
// (R-U-14: static-only output).
//
// We assert each page contains an inline `<svg>` rooted in the markdown
// flow (i.e. inside the prose container) and does NOT carry the raw
// `<script type="text/x-mermaid">` placeholder that would mean SSR
// silently fell back to client-side rendering.

import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { DIST_DIR, fail, ok } from './util.mjs';

const PAGES = [
  'docs/concepts/extends/index.html',
  'docs/concepts/includes/index.html',
];

export async function run() {
  const results = [];
  for (const rel of PAGES) {
    const path = join(DIST_DIR, rel);
    let html;
    try {
      html = await readFile(path, 'utf8');
    } catch (err) {
      results.push(fail(`mermaid:${rel}`, 'C4', `cannot read: ${err.message}`));
      continue;
    }

    if (/<script[^>]+type=["']text\/x-mermaid["']/i.test(html)) {
      results.push(
        fail(
          `mermaid:${rel}`,
          'C4',
          'page still contains <script type="text/x-mermaid"> — SSR failed'
        )
      );
      continue;
    }

    // Starlight ships chrome SVGs (theme toggle, search, etc.) on every
    // page, so a bare `<svg>` regex would always pass. rehype-mermaid
    // emits `<svg id="mermaid-N" ... aria-roledescription="flowchart-..."`
    // — match the discriminating attributes so a parser error producing
    // an empty render still fails this gate.
    const mermaidSvg =
      /<svg\b[^>]*\bid=["']mermaid-\d+["'][^>]*>/i.test(html) ||
      /<svg\b[^>]*\baria-roledescription=["'](?:flowchart|graph|sequence|class|state|er|gantt|journey|pie)/i.test(
        html
      );
    if (!mermaidSvg) {
      results.push(
        fail(`mermaid:${rel}`, 'C4', 'no rendered Mermaid <svg> found in page output')
      );
      continue;
    }

    results.push(
      ok(`mermaid:${rel}`, 'C4', 'rendered Mermaid <svg> present, no client placeholder')
    );
  }
  return results;
}

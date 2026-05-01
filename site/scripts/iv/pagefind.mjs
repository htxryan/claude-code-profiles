// C4 — Pagefind smoke test.
//
// The spec contract is "?q=drift returns ≥ 3 results". Pagefind is a
// client-side index — there's no server endpoint to hit. We verify the
// contract two ways:
//
//   1. The compiled index exists at dist/pagefind/ (byte-level check).
//   2. Loading pagefind.js in a browser context, calling search('drift'),
//      yields at least 3 result documents.
//
// (1) is a fast structural check; (2) is the real behavioural smoke that
// matches the spec wording.

import { readdir } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium } from 'playwright';
import { DIST_DIR, fail, ok } from './util.mjs';

async function structuralCheck() {
  const pagefindDir = join(DIST_DIR, 'pagefind');
  try {
    const entries = await readdir(pagefindDir);
    if (!entries.includes('pagefind-entry.json')) {
      return fail('pagefind-index', 'C4', 'pagefind-entry.json missing');
    }
    // The fragment dir is where individual page payloads live; an empty
    // dir means Starlight indexed nothing.
    const fragDir = join(pagefindDir, 'fragment');
    let fragEntries = [];
    try {
      fragEntries = await readdir(fragDir);
    } catch {
      return fail('pagefind-index', 'C4', 'pagefind/fragment/ missing');
    }
    if (fragEntries.length === 0) {
      return fail('pagefind-index', 'C4', 'pagefind index has zero fragments');
    }
    return ok(
      'pagefind-index',
      'C4',
      `index present, ${fragEntries.length} fragments`
    );
  } catch (err) {
    return fail('pagefind-index', 'C4', `cannot read dist/pagefind: ${err.message}`);
  }
}

async function searchSmoke(baseUrl) {
  const browser = await chromium.launch();
  try {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    // Pagefind is loaded as an ES module from the docs surface. We
    // navigate to a docs page (where Starlight wires the script) and
    // import pagefind dynamically from the served bundle.
    await page.goto(`${baseUrl}/docs/`, { waitUntil: 'domcontentloaded' });

    // Pagefind exposes a global module via dynamic import. We side-load
    // pagefind.js manually so we don't depend on Starlight's UI internals.
    const result = await page.evaluate(async (origin) => {
      // @ts-ignore — pagefind has no TS types in this scope
      const mod = await import(`${origin}/pagefind/pagefind.js`);
      const search = await mod.search('drift');
      // search.results is an array of { id, data: () => Promise<...> }.
      return { count: search.results.length };
    }, baseUrl);

    if (result.count < 3) {
      return fail(
        'pagefind-search',
        'C4',
        `?q=drift returned ${result.count} results, expected ≥ 3`
      );
    }
    return ok(
      'pagefind-search',
      'C4',
      `?q=drift returned ${result.count} results`
    );
  } finally {
    await browser.close();
  }
}

export async function run(ctx) {
  const results = [];
  results.push(await structuralCheck());

  // Skip the browser-backed smoke if the structural check failed —
  // Pagefind won't function without an index.
  if (results[0].ok) {
    try {
      results.push(await searchSmoke(ctx.localBaseUrl));
    } catch (err) {
      results.push(
        fail('pagefind-search', 'C4', `playwright threw: ${err.message}`)
      );
    }
  }
  return results;
}

// C4 — link integrity over the docs surface.
//
// Crawls every `dist/**/*.html` page, extracts <a href> targets, and
// verifies internal links resolve to a file in `dist/`. lychee is the
// spec's named tool; it isn't always installed locally, so we use a
// dependency-free Node implementation that covers the same intent
// (broken internal links). External http(s) links are not crawled —
// that broadens the failure surface to "is the public internet up".

import { readFile, stat, readdir } from 'node:fs/promises';
import { join, resolve, dirname } from 'node:path';
import { DIST_DIR, fail, ok } from './util.mjs';

async function walk(dir, out = []) {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      await walk(full, out);
    } else if (entry.isFile() && full.endsWith('.html')) {
      out.push(full);
    }
  }
  return out;
}

// Cheap href extractor — Astro's HTML output is well-formed, so a
// non-greedy regex on the rendered markup is sufficient. We don't need
// a full HTML parser for IV-scope link integrity.
function extractHrefs(html) {
  const out = [];
  const re = /<a\b[^>]*?\bhref\s*=\s*("([^"]*)"|'([^']*)')/gi;
  let m;
  while ((m = re.exec(html)) !== null) {
    const href = m[2] ?? m[3] ?? '';
    out.push(href);
  }
  return out;
}

function isExternal(href) {
  return /^(https?:|mailto:|tel:|ftp:)/i.test(href);
}

function urlToFsPath(distRoot, pageFile, href) {
  // Strip query + fragment; we only resolve the path component.
  const cleaned = href.split('#')[0].split('?')[0];
  if (cleaned === '') return null;

  let target;
  if (cleaned.startsWith('/')) {
    target = join(distRoot, cleaned);
  } else {
    target = resolve(dirname(pageFile), cleaned);
  }
  return target;
}

async function exists(p) {
  try {
    const s = await stat(p);
    if (s.isDirectory()) {
      // Astro's pretty URLs map `/foo/` → `dist/foo/index.html`.
      try {
        const idx = await stat(join(p, 'index.html'));
        return idx.isFile();
      } catch {
        return false;
      }
    }
    return s.isFile();
  } catch {
    return false;
  }
}

export async function run() {
  // Restrict to /docs/** per the contracts table — marketing routes are
  // exercised by the smoke and visual checks. Scoping keeps runtime
  // predictable and matches the spec's phrasing.
  const docsRoot = join(DIST_DIR, 'docs');
  let files;
  try {
    files = await walk(docsRoot);
  } catch (err) {
    return [fail('docs-link-integrity', 'C4', `cannot walk dist/docs: ${err.message}`)];
  }

  if (files.length === 0) {
    return [fail('docs-link-integrity', 'C4', 'no HTML files under dist/docs/')];
  }

  const broken = [];
  let totalChecked = 0;

  for (const file of files) {
    const html = await readFile(file, 'utf8');
    const hrefs = extractHrefs(html);
    for (const href of hrefs) {
      if (!href || href.startsWith('#') || isExternal(href)) continue;
      // Skip Pagefind-generated query links and similar runtime targets.
      if (href.startsWith('javascript:')) continue;
      totalChecked += 1;
      const fsPath = urlToFsPath(DIST_DIR, file, href);
      if (!fsPath) continue;
      const present = await exists(fsPath);
      if (!present) {
        broken.push(`${file.replace(DIST_DIR, '')} → ${href}`);
      }
    }
  }

  if (broken.length > 0) {
    const sample = broken.slice(0, 8).join('; ');
    return [
      fail(
        'docs-link-integrity',
        'C4',
        `${broken.length}/${totalChecked} broken internal links. Sample: ${sample}`
      ),
    ];
  }

  return [
    ok(
      'docs-link-integrity',
      'C4',
      `${files.length} pages, ${totalChecked} internal links, 0 broken`
    ),
  ];
}

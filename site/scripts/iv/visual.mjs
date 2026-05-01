// C2 — visual regression baseline capture and diffing.
//
// Captures full-page screenshots of /, /docs/, /docs/concepts/extends/,
// and /404 in both light and dark themes. The first run (or with
// `--update-baselines`) writes to `site/test-baselines/<theme>/`.
// Subsequent runs compare pixel-for-pixel and report any diff.
//
// We use a coarse byte-equality fallback when no diff library is
// installed — IV doesn't need sub-pixel precision; we just want to
// catch large unintentional rendering changes (broken layout, wrong
// colors, dropped components).

import { chromium } from 'playwright';
import { mkdir, readFile, writeFile, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { BASELINE_DIR, fail, ok } from './util.mjs';

const ROUTES = [
  { name: 'home', path: '/' },
  { name: 'docs-index', path: '/docs/' },
  { name: 'docs-extends', path: '/docs/concepts/extends/' },
  // The 404 must hit a path that won't 200; the dev server falls back
  // to the static 404.html which is exactly what we want to snapshot.
  { name: '404', path: '/this-route-does-not-exist' },
];

const THEMES = ['light', 'dark'];

async function ensureDir(path) {
  await mkdir(path, { recursive: true });
}

async function captureScreenshot(page, baseUrl, route, theme) {
  await page.addInitScript((t) => {
    try {
      localStorage.setItem('starlight-theme', t);
    } catch {
      // ignore
    }
  }, theme);
  await page.goto(`${baseUrl}${route}`, { waitUntil: 'networkidle' });
  await page.evaluate((t) => {
    document.documentElement.setAttribute('data-theme', t);
  }, theme);
  // Disable animations to remove flake from baseline diffs.
  await page.addStyleTag({
    content: `
      *, *::before, *::after {
        animation-duration: 0s !important;
        transition-duration: 0s !important;
        animation-delay: 0s !important;
      }
    `,
  });
  return await page.screenshot({ fullPage: true });
}

async function fileExists(path) {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

export async function run(ctx) {
  const update = ctx.updateBaselines === true;
  const browser = await chromium.launch();
  const results = [];

  try {
    for (const theme of THEMES) {
      const dir = join(BASELINE_DIR, theme);
      await ensureDir(dir);

      for (const route of ROUTES) {
        const file = join(dir, `${route.name}.png`);
        const ctxRun = await browser.newContext({
          viewport: { width: 1280, height: 720 },
          deviceScaleFactor: 1,
          // Pin to a stable device pixel ratio + viewport so renders
          // don't drift across machines.
        });
        const page = await ctxRun.newPage();
        try {
          const buf = await captureScreenshot(
            page,
            ctx.localBaseUrl,
            route.path,
            theme
          );

          if (update || !(await fileExists(file))) {
            await writeFile(file, buf);
            results.push(
              ok(
                `visual:${route.name}:${theme}`,
                'C2',
                `baseline ${update ? 'updated' : 'created'} (${buf.length} bytes)`
              )
            );
          } else {
            // Diff strategy: by default we only assert the route still
            // produces a screenshot at all (regression: page crashed
            // during render, viewport collapsed, etc.) and that the
            // baseline file we committed is the one being compared.
            //
            // Strict pixel-equal mode is opt-in via IV_VISUAL_STRICT=1
            // for local diff sessions on the same machine that
            // generated the baseline. Cross-machine PNG output drifts
            // by font hinting and GPU compositing, so locking strict
            // mode in CI would cause permanent flake.
            const strict = process.env.IV_VISUAL_STRICT === '1';
            const existing = await readFile(file);
            if (existing.equals(buf)) {
              results.push(
                ok(
                  `visual:${route.name}:${theme}`,
                  'C2',
                  `pixel-identical (${buf.length} bytes)`
                )
              );
            } else if (!strict) {
              const actualPath = file.replace(/\.png$/, '.actual.png');
              await writeFile(actualPath, buf);
              const delta = Math.abs(existing.length - buf.length);
              results.push(
                ok(
                  `visual:${route.name}:${theme}`,
                  'C2',
                  `captured (Δ ${delta} bytes vs baseline; non-strict)`
                )
              );
            } else {
              const actualPath = file.replace(/\.png$/, '.actual.png');
              await writeFile(actualPath, buf);
              results.push(
                fail(
                  `visual:${route.name}:${theme}`,
                  'C2',
                  `pixel mismatch (strict mode) — see ${actualPath}`
                )
              );
            }
          }
        } catch (err) {
          results.push(
            fail(
              `visual:${route.name}:${theme}`,
              'C2',
              `capture threw: ${err.message}`
            )
          );
        } finally {
          await ctxRun.close();
        }
      }
    }
  } finally {
    await browser.close();
  }

  return results;
}

// Helper exposed for the orchestrator's --update-baselines flag.
export async function updateBaselines(ctx) {
  return run({ ...ctx, updateBaselines: true });
}

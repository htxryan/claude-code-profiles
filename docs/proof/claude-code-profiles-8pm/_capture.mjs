// Captures proof screenshots for the brand-logo PR.
// Run from site/ via `node ../docs/proof/claude-code-profiles-8pm/_capture.mjs`.
import { chromium } from '/Users/redhale/src/claude-code-profiles/site/node_modules/playwright/index.mjs';
import { writeFile, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const __dirname = dirname(fileURLToPath(import.meta.url));
const OUT = __dirname;
const BASE = 'http://127.0.0.1:4329';

const surfaces = [
  { name: 'marketing-dark', url: '/', theme: 'dark', clip: { x: 0, y: 0, width: 1280, height: 240 } },
  { name: 'marketing-dark-full', url: '/', theme: 'dark' },
  { name: 'marketing-light', url: '/', theme: 'light', clip: { x: 0, y: 0, width: 1280, height: 240 } },
  { name: 'marketing-light-full', url: '/', theme: 'light' },
  { name: 'docs-dark', url: '/docs/', theme: 'dark', clip: { x: 0, y: 0, width: 1280, height: 200 } },
  { name: 'docs-dark-full', url: '/docs/', theme: 'dark' },
  { name: 'docs-light', url: '/docs/', theme: 'light', clip: { x: 0, y: 0, width: 1280, height: 200 } },
  { name: 'docs-light-full', url: '/docs/', theme: 'light' },
];

const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: 1280, height: 900 },
  deviceScaleFactor: 2,
});
const page = await context.newPage();

for (const s of surfaces) {
  // Theme is persisted in localStorage["starlight-theme"]; ThemeInit + Starlight
  // both honor it. Set BEFORE the page evaluates so there is no flash.
  await page.addInitScript((theme) => {
    try { localStorage.setItem('starlight-theme', theme); } catch (_) {}
  }, s.theme);

  await page.goto(BASE + s.url, { waitUntil: 'networkidle' });
  // Belt-and-suspenders: also set the data-theme attribute directly. Starlight
  // toggles its light/dark logo by keying CSS on `[data-theme="dark"]` AND
  // `[data-theme="light"]` (both explicit). Our marketing surface treats the
  // *absence* of the attribute as dark — but in Playwright we want a stable,
  // explicit value on every surface, so always set it.
  await page.evaluate((theme) => {
    document.documentElement.dataset.theme = theme;
  }, s.theme);

  const path = join(OUT, `${s.name}.png`);
  if (s.clip) {
    await page.screenshot({ path, clip: s.clip });
  } else {
    await page.screenshot({ path, fullPage: false });
  }
  console.log(`saved ${s.name}.png`);
}

// GitHub README on the feat branch — both themes via prefers-color-scheme
// emulation (the README's <picture> element keys off this).
//
// Use the BLOB view (`/blob/.../README.md`), not the tree view. GitHub's tree
// view renders transparent PNGs with a checkerboard transparency-indicator
// overlay (handy in file previews, distracting in a brand shot). The blob view
// shows the README the same way users will see it after merge — the canonical
// rendering.
const README_URL =
  'https://github.com/htxryan/claude-code-config-profiles/blob/feat/brand-logo/README.md';
for (const scheme of ['dark', 'light']) {
  const ctx = await browser.newContext({
    viewport: { width: 1280, height: 1600 },
    deviceScaleFactor: 2,
    colorScheme: scheme,
  });
  const p = await ctx.newPage();
  // GitHub's tree page rarely reaches `networkidle` (background telemetry).
  // `load` is enough — we explicitly waitFor the README image below.
  await p.goto(README_URL, { waitUntil: 'load', timeout: 60_000 });
  // Wait for the logo image inside the rendered README to load.
  await p
    .locator('article img[alt^="C3P"]')
    .first()
    .waitFor({ state: 'visible', timeout: 30_000 });
  // Scroll the README image into the viewport before capture.
  await p.locator('article img[alt^="C3P"]').first().scrollIntoViewIfNeeded();
  const path = join(OUT, `readme-${scheme}.png`);
  await p.screenshot({ path });
  // Tighter crop around the logo for evidence.
  const box = await p.locator('article img[alt^="C3P"]').first().boundingBox();
  if (box) {
    await p.screenshot({
      path: join(OUT, `readme-${scheme}-logo.png`),
      clip: {
        x: Math.max(0, box.x - 40),
        y: Math.max(0, box.y - 40),
        width: Math.min(1280, box.width + 80),
        height: box.height + 80,
      },
    });
  }
  await ctx.close();
  console.log(`saved readme-${scheme}.png`);
}

await browser.close();

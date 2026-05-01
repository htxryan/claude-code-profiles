// C3 — accessibility critical violations via axe-core.
//
// "Zero critical violations" on landing + a representative docs page,
// in both themes. We only fail on impact === 'critical' so the check
// stays decisive — minor/moderate findings still surface in --verbose
// mode but don't block.

import { chromium } from 'playwright';
import AxeBuilder from '@axe-core/playwright';
import { fail, ok } from './util.mjs';

const PAGES = [
  { name: 'home', path: '/' },
  { name: 'docs-index', path: '/docs/' },
  { name: 'docs-extends', path: '/docs/concepts/extends/' },
  { name: 'docs-404', path: '/this-route-does-not-exist' },
];

const THEMES = ['dark', 'light'];

async function runAxeOn(page, baseUrl, route, theme) {
  // Force theme via Starlight's data attribute on <html>; this matches
  // R-S-1 (dark default) and R-O-1 (theme override). Seed localStorage
  // before navigation so Starlight's hydration picks it up; then re-apply
  // post-load as a belt-and-braces guard. No MutationObserver — that
  // creates a feedback loop where setAttribute itself re-triggers the
  // callback.
  await page.addInitScript((t) => {
    try {
      localStorage.setItem('starlight-theme', t);
    } catch {
      // localStorage unavailable in some contexts; ignore.
    }
  }, theme);

  await page.goto(`${baseUrl}${route}`, { waitUntil: 'networkidle' });
  await page.evaluate((t) => {
    document.documentElement.setAttribute('data-theme', t);
  }, theme);

  const builder = new AxeBuilder({ page })
    // Restrict rule families to the WCAG 2 set + common best-practice
    // rules; matches what a default `axe.run()` returns and keeps the
    // signal aligned with the Lighthouse a11y score.
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']);

  const result = await builder.analyze();
  const critical = result.violations.filter((v) => v.impact === 'critical');
  return { all: result.violations, critical };
}

export async function run(ctx) {
  const browser = await chromium.launch();
  const results = [];
  try {
    for (const theme of THEMES) {
      for (const route of PAGES) {
        const ctxRun = await browser.newContext();
        const page = await ctxRun.newPage();
        try {
          const { critical, all } = await runAxeOn(
            page,
            ctx.localBaseUrl,
            route.path,
            theme
          );
          if (critical.length > 0) {
            const ids = critical.map((v) => v.id).join(', ');
            results.push(
              fail(
                `axe:${route.name}:${theme}`,
                'C3',
                `${critical.length} critical violations: ${ids}`
              )
            );
          } else {
            results.push(
              ok(
                `axe:${route.name}:${theme}`,
                'C3',
                `0 critical (${all.length} other)`
              )
            );
          }
        } catch (err) {
          results.push(
            fail(
              `axe:${route.name}:${theme}`,
              'C3',
              `playwright/axe threw: ${err.message}`
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

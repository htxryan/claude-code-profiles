// C2 — theme-toggle wiring + reduced-motion contract.
//
// Two distinct checks:
//   1. Theme toggle: load /docs/, click Starlight's theme select to
//      light, verify <html data-theme="light"> sticks; toggle back to
//      dark, verify it reverts. Catches broken theme storage, missing
//      data-theme attribute, or drift in Starlight's hook names.
//   2. prefers-reduced-motion: emulate the media query, verify the
//      bundled CSS resolves --motion-duration-* to ~0s (per E2 spec).

import { chromium } from 'playwright';
import { fail, ok } from './util.mjs';

async function checkThemeToggle(baseUrl) {
  // Drive Starlight's actual `<starlight-theme-select> <select>` UI so
  // we exercise the real wiring (event handler, localStorage write,
  // data-theme propagation) — not just CSS-variable behaviour. A
  // missing handler or a broken listener would silently pass an
  // attribute-only check.
  const browser = await chromium.launch();
  try {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    await page.goto(`${baseUrl}/docs/`, { waitUntil: 'networkidle' });

    // Starlight renders the theme select twice (one in the desktop
    // header, one in the mobile menu). Either drives the same global
    // <html data-theme> state, so we pick the first match.
    const select = page.locator('starlight-theme-select select').first();
    if ((await select.count()) === 0) {
      return fail(
        'theme-toggle',
        'C2',
        '<starlight-theme-select> select not present in DOM'
      );
    }

    // Drive the select via its actual UI; selectOption fires the
    // change event Starlight listens for.
    await select.selectOption('dark');
    await page.waitForFunction(
      () => document.documentElement.getAttribute('data-theme') === 'dark',
      { timeout: 2000 }
    );
    const darkBg = await page.evaluate(
      () => getComputedStyle(document.body).backgroundColor
    );

    await select.selectOption('light');
    await page.waitForFunction(
      () => document.documentElement.getAttribute('data-theme') === 'light',
      { timeout: 2000 }
    );
    const lightBg = await page.evaluate(
      () => getComputedStyle(document.body).backgroundColor
    );

    if (darkBg === lightBg) {
      return fail(
        'theme-toggle',
        'C2',
        `dark and light produced same body bg: ${darkBg}`
      );
    }
    return ok(
      'theme-toggle',
      'C2',
      `<select> drives data-theme; bg differs (dark=${darkBg}, light=${lightBg})`
    );
  } finally {
    await browser.close();
  }
}

async function checkReducedMotion(baseUrl) {
  const browser = await chromium.launch();
  try {
    // Emulate the user setting prefers-reduced-motion: reduce.
    const ctx = await browser.newContext({
      reducedMotion: 'reduce',
    });
    const page = await ctx.newPage();
    await page.goto(`${baseUrl}/`, { waitUntil: 'networkidle' });

    // The contract is the resolved value of --motion-duration-md is 0
    // (or near-zero) under the reduce preference.
    const dur = await page.evaluate(() => {
      const v = getComputedStyle(document.documentElement).getPropertyValue(
        '--motion-duration-md'
      );
      return v.trim();
    });

    // tokens.css collapses every --motion-duration-* to 0.01ms under
    // prefers-reduced-motion: reduce. Accept 0/0s/0.01ms exactly — a
    // looser threshold (e.g. ≤1ms) would let a regression to 0.5ms or
    // 1ms slip through silently.
    const parseToMs = (s) => {
      if (!s) return null;
      const trimmed = s.trim();
      if (trimmed === '0') return 0;
      const m = /^([\d.]+)(ms|s)$/.exec(trimmed);
      if (!m) return null;
      const n = parseFloat(m[1]);
      return m[2] === 's' ? n * 1000 : n;
    };
    const ms = parseToMs(dur);
    if (ms === null) {
      return fail(
        'reduced-motion',
        'C2',
        `--motion-duration-md unparseable: "${dur}"`
      );
    }
    if (ms > 0.05) {
      return fail(
        'reduced-motion',
        'C2',
        `--motion-duration-md is ${dur} under reduce, expected ≤ 0.05ms (token value: 0.01ms)`
      );
    }
    return ok(
      'reduced-motion',
      'C2',
      `--motion-duration-md = ${dur} under prefers-reduced-motion: reduce`
    );
  } finally {
    await browser.close();
  }
}

export async function run(ctx) {
  const results = [];
  try {
    results.push(await checkThemeToggle(ctx.localBaseUrl));
  } catch (err) {
    results.push(fail('theme-toggle', 'C2', `threw: ${err.message}`));
  }
  try {
    results.push(await checkReducedMotion(ctx.localBaseUrl));
  } catch (err) {
    results.push(fail('reduced-motion', 'C2', `threw: ${err.message}`));
  }
  return results;
}

// C3 — hero / drift-gate demo presence + animation contract.
//
// Spec C3 lists "Manual: hero demo plays" as a behavioral assertion.
// We can't validate visual playback without a frame buffer, but we
// can validate the structural and behavioural contracts that make the
// demo *capable* of playing:
//
//   1. The hero section, headline (R-U-4), and CTA buttons exist.
//   2. The DriftGateDemo `.drift-demo` element renders with the
//      "refused" badge — i.e. the static SSR is intact.
//   3. The demo has CSS animations declared (i.e. the @keyframes /
//      `animation` properties survived the build) — under default
//      motion preferences. Under prefers-reduced-motion: reduce, the
//      animations should be neutralised (covered by theme.mjs).
//
// This catches: a broken DriftGateDemo component, dropped headline,
// missing install CTA, and an animation regression where the keyframe
// or animation-name was lost.

import { chromium } from 'playwright';
import { fail, ok } from './util.mjs';

export async function run(ctx) {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    await page.goto(`${ctx.localBaseUrl}/`, { waitUntil: 'networkidle' });

    // 1. Hero structural elements.
    const headline = await page.locator('h1#hero-title').first();
    const headlineCount = await headline.count();
    if (headlineCount === 0) {
      return [fail('hero-structure', 'C3', '<h1#hero-title> missing')];
    }
    const headlineText = (await headline.textContent())?.trim() ?? '';
    if (!/swap claude configs/i.test(headlineText)) {
      return [
        fail(
          'hero-structure',
          'C3',
          `headline content unexpected: "${headlineText.slice(0, 60)}"`
        ),
      ];
    }

    // 2. Drift-gate demo present + shows refusal text.
    const demo = page.locator('.drift-demo');
    if ((await demo.count()) === 0) {
      return [fail('hero-demo', 'C3', '.drift-demo not present')];
    }
    const demoText = (await demo.textContent()) ?? '';
    if (!/refused/i.test(demoText)) {
      return [
        fail(
          'hero-demo',
          'C3',
          'drift-demo missing "refused" badge — static SSR broken'
        ),
      ];
    }

    // 3. Install CTA exists with the canonical command.
    const cta = page.locator('text=npm install -g claude-code-config-profiles');
    if ((await cta.count()) === 0) {
      return [fail('hero-cta', 'C3', 'install CTA command not found in DOM')];
    }

    // 4. Animation declared on at least one demo descendant. We check
    //    the computed style — survives autoprefixer and minification.
    const hasAnim = await page.evaluate(() => {
      const els = document.querySelectorAll('.drift-demo *');
      for (const el of els) {
        const cs = getComputedStyle(el);
        if (cs.animationName && cs.animationName !== 'none') return true;
      }
      return false;
    });
    if (!hasAnim) {
      return [
        fail(
          'hero-demo-animation',
          'C3',
          'no animation-name on any .drift-demo descendant'
        ),
      ];
    }

    return [
      ok(
        'hero-structure',
        'C3',
        `headline + drift-demo + install CTA all present`
      ),
      ok('hero-demo-animation', 'C3', 'CSS animations declared on demo'),
    ];
  } finally {
    await browser.close();
  }
}

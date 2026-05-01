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
  const results = [];
  try {
    const page = await browser.newPage();
    await page.goto(`${ctx.localBaseUrl}/`, { waitUntil: 'networkidle' });

    // 1. Hero headline — assert structural shape, not literal copy. The
    //    h1 is the marketing headline; tying IV to the exact wording
    //    means every copy edit silently fails the gate.
    const headline = await page.locator('h1#hero-title').first();
    const headlineCount = await headline.count();
    if (headlineCount === 0) {
      results.push(fail('hero-headline', 'C3', '<h1#hero-title> missing'));
    } else {
      const headlineText = (await headline.textContent())?.trim() ?? '';
      if (headlineText.length === 0) {
        results.push(fail('hero-headline', 'C3', 'h1#hero-title is empty'));
      } else if (!/c3p|claude/i.test(headlineText)) {
        results.push(
          fail(
            'hero-headline',
            'C3',
            `headline missing product reference: "${headlineText.slice(0, 60)}"`
          )
        );
      } else {
        results.push(
          ok('hero-headline', 'C3', `h1 present (${headlineText.length} chars)`)
        );
      }
    }

    // 2. Drift-gate demo present + shows refusal text.
    const demo = page.locator('.drift-demo');
    if ((await demo.count()) === 0) {
      results.push(fail('hero-demo', 'C3', '.drift-demo not present'));
    } else {
      const demoText = (await demo.textContent()) ?? '';
      if (!/refused/i.test(demoText)) {
        results.push(
          fail(
            'hero-demo',
            'C3',
            'drift-demo missing "refused" badge — static SSR broken'
          )
        );
      } else {
        results.push(ok('hero-demo', 'C3', 'drift-demo SSR intact, "refused" present'));
      }
    }

    // 3. Install CTA exists with the canonical command.
    const cta = page.locator('text=npm install -g claude-code-config-profiles');
    if ((await cta.count()) === 0) {
      results.push(fail('hero-cta', 'C3', 'install CTA command not found in DOM'));
    } else {
      results.push(ok('hero-cta', 'C3', 'install CTA present'));
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
      results.push(
        fail(
          'hero-demo-animation',
          'C3',
          'no animation-name on any .drift-demo descendant'
        )
      );
    } else {
      results.push(ok('hero-demo-animation', 'C3', 'CSS animations declared on demo'));
    }
  } finally {
    await browser.close();
  }
  return results;
}

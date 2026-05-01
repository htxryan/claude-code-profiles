// C3, C4 — Lighthouse Performance / A11y / Best-Practices / SEO ≥ 90.
//
// We launch headless chrome via chrome-launcher (so lighthouse's
// expected port-scan / debug-protocol path works), run lighthouse
// programmatically, and check each of the four scores.
//
// Runtime is the dominant cost in IV (multi-second per page). We run
// against the local server, not production, to keep network noise out
// of the perf score.

import { fail, ok } from './util.mjs';

// Lazy-import lighthouse + chrome-launcher; dynamic import keeps the
// orchestrator fast when --skip-lighthouse is set.
async function loadLighthouse() {
  const [{ default: lighthouse }, chromeLauncher] = await Promise.all([
    import('lighthouse'),
    import('chrome-launcher'),
  ]);
  return { lighthouse, chromeLauncher };
}

const TARGETS = [
  { name: 'home', path: '/' },
  // Representative docs page — extends has Mermaid diagrams + heavy
  // markdown, so it's a worst-case for both perf and a11y.
  { name: 'docs-extends', path: '/docs/concepts/extends/' },
];

const CATEGORIES = ['performance', 'accessibility', 'best-practices', 'seo'];
const THRESHOLD = 90;

async function runOne(lighthouse, port, url) {
  // chromeFlags is ignored when `port` is provided — lighthouse connects
  // to the existing chrome from chrome-launcher. Flags belong on the
  // launch() call below, not here.
  const result = await lighthouse(url, {
    port,
    output: 'json',
    onlyCategories: CATEGORIES,
    logLevel: 'error',
  });
  const scores = {};
  for (const cat of CATEGORIES) {
    const c = result.lhr.categories[cat];
    scores[cat] = c ? Math.round((c.score ?? 0) * 100) : 0;
  }
  return scores;
}

export async function run(ctx) {
  if (ctx.skipLighthouse) {
    return [ok('lighthouse', 'C3/C4', 'skipped via --skip-lighthouse')];
  }

  const { lighthouse, chromeLauncher } = await loadLighthouse();
  let chrome;
  try {
    chrome = await chromeLauncher.launch({
      chromeFlags: ['--headless=new', '--no-sandbox', '--disable-gpu'],
    });
  } catch (err) {
    return [
      fail(
        'lighthouse',
        'C3/C4',
        `chrome-launcher failed: ${err.message}. Try --skip-lighthouse to bypass.`
      ),
    ];
  }

  const results = [];
  try {
    for (const target of TARGETS) {
      const url = `${ctx.localBaseUrl}${target.path}`;
      try {
        const scores = await runOne(lighthouse, chrome.port, url);
        const failing = Object.entries(scores).filter(
          ([, s]) => s < THRESHOLD
        );
        const summary = Object.entries(scores)
          .map(([k, v]) => `${k}=${v}`)
          .join(' ');
        if (failing.length > 0) {
          const fl = failing.map(([k, v]) => `${k}:${v}`).join(',');
          results.push(
            fail(
              `lighthouse:${target.name}`,
              target.name === 'home' ? 'C3' : 'C4',
              `${fl} below ${THRESHOLD} (all: ${summary})`
            )
          );
        } else {
          results.push(
            ok(
              `lighthouse:${target.name}`,
              target.name === 'home' ? 'C3' : 'C4',
              summary
            )
          );
        }
      } catch (err) {
        results.push(
          fail(
            `lighthouse:${target.name}`,
            target.name === 'home' ? 'C3' : 'C4',
            `lighthouse threw: ${err.message}`
          )
        );
      }
    }
  } finally {
    await chrome.kill();
  }

  return results;
}

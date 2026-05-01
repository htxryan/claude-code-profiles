// One-off: Lighthouse against production (m7b followup).
// Not part of IV — keep IV deterministic against local server.
import lighthouse from 'lighthouse';
import * as chromeLauncher from 'chrome-launcher';

const TARGETS = [
  { name: 'home', url: 'https://getc3p.dev/' },
  { name: 'docs-extends', url: 'https://getc3p.dev/docs/concepts/extends/' },
];
const CATEGORIES = ['performance', 'accessibility', 'best-practices', 'seo'];

const chrome = await chromeLauncher.launch({ chromeFlags: ['--headless=new', '--no-sandbox'] });
console.log(`chrome on port ${chrome.port}`);
try {
  for (const t of TARGETS) {
    const result = await lighthouse(t.url, {
      port: chrome.port,
      output: 'json',
      onlyCategories: CATEGORIES,
      logLevel: 'error',
    });
    const scores = {};
    for (const cat of CATEGORIES) {
      const c = result.lhr.categories[cat];
      scores[cat] = c ? Math.round((c.score ?? 0) * 100) : 0;
    }
    const lcp = result.lhr.audits['largest-contentful-paint']?.numericValue;
    const cls = result.lhr.audits['cumulative-layout-shift']?.numericValue;
    const tbt = result.lhr.audits['total-blocking-time']?.numericValue;
    console.log(`\n${t.name} — ${t.url}`);
    for (const [cat, score] of Object.entries(scores)) {
      const mark = score >= 90 ? '✓' : '✗';
      console.log(`  ${mark} ${cat}: ${score}`);
    }
    if (lcp != null) console.log(`    LCP: ${(lcp / 1000).toFixed(2)}s`);
    if (cls != null) console.log(`    CLS: ${cls.toFixed(3)}`);
    if (tbt != null) console.log(`    TBT: ${tbt.toFixed(0)}ms`);
  }
} finally {
  await chrome.kill();
}

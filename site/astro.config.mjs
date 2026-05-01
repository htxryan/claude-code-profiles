// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// Static-only output (R-U-14). Starlight's catch-all `[...slug]` route serves
// content from `src/content/docs/`. To mount docs under `/docs/` (R-U-2) while
// keeping the marketing landing at `/` (R-U-1), all Starlight content lives
// nested under `src/content/docs/docs/` — its own URL prefix. Explicit
// `src/pages/index.astro` wins over Starlight's catch-all at `/`.
export default defineConfig({
  site: 'https://getc3p.dev',
  output: 'static',
  integrations: [
    starlight({
      title: 'C3P',
      description: 'Profile-based config swaps for Claude Code',
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/htxryan/claude-code-config-profiles',
        },
      ],
      sidebar: [
        {
          label: 'Docs',
          autogenerate: { directory: 'docs' },
        },
      ],
    }),
  ],
});

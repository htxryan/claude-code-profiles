# site/ — getc3p.dev

Astro + Starlight app for **getc3p.dev**: marketing landing at `/` and
documentation at `/docs/*`. Self-contained — independent `package.json` and
`pnpm-lock.yaml`, **not** a pnpm workspace member of the root CLI package.

> **Spec**: [`docs/specs/getc3p-web-presence.md`](../docs/specs/getc3p-web-presence.md)

## Develop locally

```bash
cd site
pnpm install
pnpm dev          # http://localhost:4321
pnpm build        # produces site/dist/
pnpm preview      # serve site/dist/ locally
```

Requires Node 20+ and pnpm 10+.

## Layout

```
site/
├── astro.config.mjs            # Astro config — Starlight integration
├── package.json                # independent (NOT a workspace member)
├── pnpm-lock.yaml              # independent
├── tsconfig.json
├── public/                     # static assets, OG image (later)
└── src/
    ├── content.config.ts       # Starlight content collection schema (Astro v6 location)
    ├── content/
    │   └── docs/
    │       └── docs/           # ← all Starlight pages live under here so
    │                           #   their URLs resolve under /docs/
    ├── pages/
    │   ├── index.astro         # marketing landing (R-U-1)
    │   └── 404.astro           # added by E3 (R-U-6)
    ├── components/             # marketing-only components (E3)
    └── styles/
        ├── tokens.css          # design vocabulary (E2)
        └── global.css          # baseline + Starlight CSS hook overrides (E2)
```

### Design tokens (E2)

`src/styles/tokens.css` is the single source of truth for color, type, motion,
spacing, radius, and shadow. It produces tokens; it does not consume them.

`src/styles/global.css` imports the tokens, applies baseline element styles,
and remaps Starlight's `--sl-*` CSS hooks onto the token layer so the docs
surface and marketing surface share one vocabulary. It is wired into Starlight
via `customCss` in `astro.config.mjs`, and imported directly by
`src/pages/index.astro`.

**Load-bearing token names** (renaming requires migration across consumers):

- Color: `--color-bg-primary`, `--color-bg-surface`, `--color-text-primary`,
  `--color-text-muted`, `--color-accent`, `--color-border`
- Type: `--type-scale-{1..6}`, `--type-leading-{tight|normal|relaxed}`
- Motion: `--motion-duration-{xs|sm|md|lg}`,
  `--motion-easing-{standard|emphasis}`
- Spacing: `--space-{1..8}`
- Radius: `--radius-{sm|md|lg}`

Dark mode overrides the same semantic tokens under `:root[data-theme="dark"]`.
`@media (prefers-reduced-motion: reduce)` collapses every
`--motion-duration-*` to `0ms`; a belt-and-braces global rule in
`global.css` catches any animation that bypasses the tokens.

### Accessibility baseline (E2)

- Skip-to-content link (`.skip-link`) on every marketing page; targets
  `#main-content`. Starlight provides its own skip link on docs pages.
- `:focus-visible` ring (`--focus-ring-width` / `--color-focus-ring`) — keyboard
  users get the ring, mouse users don't.
- Semantic landmarks (`header[role="banner"]`, `main`, `footer[role="contentinfo"]`)
  on the marketing pages; Starlight provides equivalents for docs.
- Color tokens are picked to meet WCAG AA contrast (4.5:1 normal, 3:1 large)
  on both themes.

**Verification (manual until Lighthouse CI is wired in E5/E6):**

```bash
pnpm dev              # http://localhost:4321
# 1. Tab through the page — first Tab reveals the skip link.
# 2. Toggle prefers-reduced-motion in DevTools → animations should be instant.
# 3. Toggle the OS dark/light setting — both themes re-paint without flicker.
# 4. Run Lighthouse (DevTools) → Accessibility ≥ 90 on / and /docs/.
```

### Why are docs nested at `src/content/docs/docs/`?

Starlight's integration injects a single catch-all route at `[...slug]` and
serves content rooted at the `docs` content collection. To mount Starlight
under the URL prefix `/docs/` (R-U-2) **while** keeping a marketing page at
`/` (R-U-1), all Starlight pages live nested one level under
`src/content/docs/docs/` — that prefix becomes the URL prefix.

Astro's explicit `src/pages/index.astro` takes precedence over the catch-all,
so `/` renders the marketing page.

## Cloudflare Pages deploy configuration

Deploy is handled by **Cloudflare Pages' native GitHub integration** — there
is **no GitHub Actions workflow** for the site (per RA-5 of the spec). The
configuration below lives in the CF Pages dashboard, not in this repo. It is
documented here so the configuration is reviewable in-repo.

| Setting | Value |
|---------|-------|
| Repository | `htxryan/claude-code-config-profiles` |
| Production branch | `main` |
| Build command | `cd site && pnpm install --frozen-lockfile && pnpm build` |
| Build output directory | `site/dist` |
| Root directory (for build) | `/` |
| Node version | `20` (env var `NODE_VERSION`) |
| Preview deployments | All non-production branches + PRs |
| Production custom domain | `getc3p.dev` (apex + `www.` redirect) |

E5 (`claude-code-profiles-z0k`) wires the actual deploy + domain.

## What this directory does **not** touch

The CLI source tree at the repo root (`src/`, `tests/`, `package.json`,
`pnpm-lock.yaml`, `tsconfig*.json`, `vitest.config.ts`, `dist/`,
`.github/workflows/`, `.release-please-*`) is **unchanged** by this site.
The existing matrix CI (R-U-13) and release-please pipeline (R-U-15) keep
running untouched.

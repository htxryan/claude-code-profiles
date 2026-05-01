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

Theme convention mirrors Starlight: `:root` IS the dark default, and
`:root[data-theme="light"]` overrides the same semantic tokens for light mode.
`@media (prefers-reduced-motion: reduce)` collapses every
`--motion-duration-*` to `0.01ms`; a belt-and-braces global rule in
`global.css` catches any animation that bypasses the tokens.

### Accessibility baseline (E2)

- Skip-to-content link (`.skip-link`) on every marketing page; targets
  `#main-content`. Starlight provides its own skip link on docs pages.
- `:focus-visible` ring (`--focus-ring-width` / `--color-focus-ring`) — keyboard
  users get the ring, mouse users don't.
- Semantic landmarks (`<header>`, `<main>`, `<footer>`) on the marketing
  pages — these elements carry their `banner`/`main`/`contentinfo` roles
  implicitly per the HTML spec, no `role=` attribute needed. Starlight
  provides equivalents for docs.
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
is **no GitHub Actions workflow** for the site (per RA-5 of the spec, and
overruled at Gate 2 of E5). The configuration below lives in the CF Pages
dashboard, not in this repo. It is documented here verbatim so the
configuration is reviewable in-repo even though it's enforced out-of-repo.

### Build settings (Pages → Project → Settings → Builds & deployments)

| Setting | Value |
|---------|-------|
| Repository | `htxryan/claude-code-config-profiles` |
| Production branch | `main` |
| Build command | `cd site && pnpm install --frozen-lockfile && pnpm build` |
| Build output directory | `site/dist` |
| Root directory (for build) | `/` |
| Build system version | `2` (default) |
| Node version | `20` (set via env var `NODE_VERSION=20`) |
| Preview deployments | **Enabled for all non-production branches and PRs** |
| Build watch paths | _(default — unrestricted)_ |

### Environment variables (Pages → Project → Settings → Environment variables)

Set on **both** Production and Preview environments:

| Variable | Value | Why |
|----------|-------|-----|
| `NODE_VERSION` | `20` | Astro/Starlight require Node 20+; Pages defaults to an older runtime |

No GitHub Actions secrets are required — CF Pages talks to GitHub directly via
its OAuth app at install time, not via repo-level tokens (R-UN-3).

### Custom domains (Pages → Project → Custom domains)

| Hostname | Target | Notes |
|----------|--------|-------|
| `getc3p.dev` (apex) | Production deployment | A/AAAA records auto-managed by Pages when DNS is on the same Cloudflare account |
| `www.getc3p.dev` | 301 redirect to `https://getc3p.dev/` | Configured via a Bulk Redirect rule (Rules → Redirect Rules) or a `www` CNAME → apex; either is acceptable |

### Dashboard screenshots

Screenshots of the Build settings page and the Custom domains page are stored
under [`docs/screenshots/cf-pages/`](../docs/screenshots/cf-pages/) (added when
E5 is wired against the live dashboard). They are reference-only — the
authoritative configuration is the live dashboard.

## Rollback drill

When a bad build reaches production at `https://getc3p.dev`, roll back to the
prior good deployment. CF Pages deployments are atomic and immutable, so
rollback promotes a previous deployment in-place — no rebuild, no DNS change.

### Option A — Wrangler CLI (preferred for muscle memory)

```bash
# 1. Authenticate once per machine.
pnpm dlx wrangler login

# 2. List recent production deployments to find a prior good one.
pnpm dlx wrangler pages deployment list \
  --project-name=getc3p \
  --environment=production

# 3. Roll back to a known-good deployment ID (UUID from the list above).
pnpm dlx wrangler pages deployment rollback <DEPLOYMENT_ID> \
  --project-name=getc3p

# 4. Verify the apex now serves the rolled-back build.
curl -sI https://getc3p.dev | head -1   # expect: HTTP/2 200
```

### Option B — Dashboard (when CLI is unavailable)

1. Open Cloudflare → Pages → `getc3p` → **Deployments**.
2. Filter by environment = **Production**.
3. Find the prior good deployment (one above the current red one).
4. Click the deployment → **⋯ menu → Rollback to this deployment**.
5. Confirm. Pages flips the production alias atomically; the apex serves the
   rolled-back build immediately (atomic swap, R-S-2).

### Drill cadence

The rollback drill is run **once at E5 acceptance** against a deliberately
broken branch (verifies F-4 + F-5), and **re-run any time** the deploy pipeline
materially changes (build command, output dir, framework upgrade). Record the
drill outcome in the issue that triggered the change.

## Fitness function verification

These commands verify E5 acceptance against the live deployment. Run after the
CF Pages project + custom domain are wired.

```bash
# F-1: apex serves landing (matches the hero headline)
curl -sSf https://getc3p.dev | grep -q "Swap Claude configs without losing work" && echo "F-1 ok"

# F-2: www → apex 301
curl -sI https://www.getc3p.dev | grep -E "^HTTP/.* 301" && echo "F-2 ok"

# F-7: OG image is reachable as image/png
curl -sI https://getc3p.dev/og.png | grep -i "content-type: image/png" && echo "F-7 ok"
```

F-3 (preview deploy on PR) and F-4 (broken-build isolation) are verified by
opening a PR; CF Pages comments with the preview URL within ~5 min.

F-6 is a meta-check: `ls .github/workflows/` should show **no** new
`deploy-site.yml` (or similar) — deploys live in the CF dashboard, not in CI.

## Integration Verification (E6)

`scripts/iv.mjs` runs the cross-epic IV pass for the site. It validates
the C1–C5 contracts from the IV epic (`claude-code-profiles-rt8`)
end-to-end against a fresh build of `dist/`, and (optionally) against
the live production URL.

### Run it

```bash
pnpm iv                         # full pass (build + smoke + lighthouse)
pnpm iv:fast                    # skip Lighthouse — quickest signal
pnpm iv:offline                 # skip Lighthouse + production smoke
pnpm iv:update-baselines        # rewrite test-baselines/ from current build
```

The full pass takes ~30s on an M-series Mac (well under the F-1 budget
of 5 min). Lighthouse is the dominant cost (~13s for two pages).

### What each phase guarantees

| Phase | Contract | What it asserts |
|-------|----------|-----------------|
| C1 composition | scaffold → all downstream | `dist/` contains every expected route artifact (marketing, 404, OG, every Concept / CLI verb spot-check, search index, sitemap) |
| C2 theme + reduced-motion | tokens → marketing/docs | Body bg differs across `[data-theme="dark"]` / `[data-theme="light"]`; `--motion-duration-md` collapses to ≤1ms under `prefers-reduced-motion: reduce` |
| C2 visual baselines | tokens → marketing/docs | Pixel-stable screenshots of `/`, `/docs/`, `/docs/concepts/extends/`, `/404` in both themes (baselines under `test-baselines/`) |
| C3 axe-core | marketing → IV | Zero `impact: critical` violations on the four target routes × both themes |
| C4 link integrity | docs → IV | Every internal `<a href>` under `dist/docs/**` resolves to a real file |
| C4 mermaid SSR | docs → IV | `extends`/`includes` Concept pages contain inline `<svg>`, not the `text/x-mermaid` placeholder (R-U-16, R-U-14) |
| C4 pagefind | docs → IV | `pagefind/` index ships fragments; `?q=drift` returns ≥ 3 results |
| C5 production smoke | deploy → IV | Apex returns 200 with the C3P headline; `og.png` is `image/png`; `www` redirects (currently warn — see `claude-code-profiles-rsf`) |
| C3/C4 lighthouse | marketing+docs → IV | Performance / Accessibility / Best-Practices / SEO scores ≥ 90 on landing + `docs/concepts/extends/` |

### Visual baselines

`test-baselines/{light,dark}/*.png` is the snapshot store. By default
the visual phase only verifies a screenshot was captured and writes
any mismatch to a sibling `*.actual.png` for inspection — cross-machine
renders drift by font hinting and GPU compositing, so a pixel-equal
gate would flake permanently in CI.

Re-record intentionally with `pnpm iv:update-baselines` and review the
resulting PNGs in the diff. Set `IV_VISUAL_STRICT=1` to enforce
pixel-equality (only useful on the same machine that captured the
baseline — typically when iterating on a tokens / layout change).

### Environment variables

| Var | Default | Purpose |
|-----|---------|---------|
| `IV_VISUAL_STRICT` | unset | Enforce pixel-equal visual baselines (local dev only) |
| `SMOKE_WWW_STRICT` | unset | Fail (not warn) when `www.getc3p.dev` doesn't 301 to apex. Flip this to `1` once `claude-code-profiles-rsf` is closed so the contract is locked in. |

### Failure-injection drill (C5)

The contracts table calls for a "push broken commit on a branch →
production unaffected" drill. Run this once after a deploy-pipeline
change:

1. Branch off `main`, push a commit that breaks `pnpm build` (e.g.
   delete `astro.config.mjs`).
2. Verify CF Pages reports the build as failed in the PR's deployment
   comment.
3. Verify `https://getc3p.dev` continues to serve the prior production
   deployment (`curl -sI https://getc3p.dev | head -1` → `HTTP/2 200`).
4. Discard the branch.

The drill is documented in detail under [Rollback drill](#rollback-drill)
above.

### Bug protocol

Per the IV-5 protocol, integration failures spawn bug beads with
`bd dep add … --type related` to the originating epic. Open bugs at
the time of writing:

| Bug | Contract | Status |
|-----|----------|--------|
| `claude-code-profiles-rsf` | C5 (www-redirect) | open — DNS gap; surfaced as warn |
| `claude-code-profiles-3jd` | C3 (axe color-contrast) | open — serious-level on install CTA `<code>`; not blocking IV (we gate on critical only) |

## What this directory does **not** touch

The CLI source tree at the repo root (`src/`, `tests/`, `package.json`,
`pnpm-lock.yaml`, `tsconfig*.json`, `vitest.config.ts`, `dist/`,
`.github/workflows/`, `.release-please-*`) is **unchanged** by this site.
The existing matrix CI (R-U-13) and release-please pipeline (R-U-15) keep
running untouched.

# Proof — `claude-code-profiles-8pm` (brand logo)

Captured 2026-05-01 with `_capture.mjs` (Playwright). Reproducible: start
the C3P site dev server on `127.0.0.1:4329`, then `node _capture.mjs`.

## Three surfaces × two themes

| Surface          | Dark theme                    | Light theme                    |
| ---------------- | ----------------------------- | ------------------------------ |
| Marketing header | `marketing-dark.png`          | `marketing-light.png`          |
| Marketing (full) | `marketing-dark-full.png`    | `marketing-light-full.png`    |
| Starlight docs   | `docs-dark.png`               | `docs-light.png`               |
| Docs (full)      | `docs-dark-full.png`          | `docs-light-full.png`          |
| GitHub README    | `readme-dark.png`             | `readme-light.png`             |
| README crop      | `readme-dark-logo.png`        | `readme-light-logo.png`        |

## What to look for

- **Dark theme** — `c3p-logo-dark.png` (white "C3P" wordmark) renders.
- **Light theme** — `c3p-logo-light.png` (dark "C3P" wordmark) renders.
- Marketing surface flips via `:root[data-theme="light"]` (matches
  `tokens.css` + `ThemeInit.astro` convention).
- Starlight docs surface flips via Starlight's built-in `logo.light/dark`.
- README on GitHub flips via `<picture>` + `prefers-color-scheme`.

The README screenshots use Playwright's `colorScheme` emulation, which
GitHub honors via `prefers-color-scheme` for unauthenticated viewers.

**README capture target.** The script captures the **blob view**
(`/blob/feat/brand-logo/README.md`), not the **tree view**
(`/tree/feat/brand-logo`). They render the same README with two different
treatments: the tree view overlays a checkerboard "transparency indicator"
on transparent PNGs, while the blob view shows the image cleanly over the
page background. The blob view is what users see when they actually open
the README; that is the canonical rendering, so that is what we captured.

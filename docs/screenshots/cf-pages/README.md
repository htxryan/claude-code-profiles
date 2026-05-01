# Cloudflare Pages dashboard screenshots

Reference screenshots of the live CF Pages configuration for `getc3p.dev`.
The authoritative configuration is the live dashboard; these are kept in-repo
so the build settings and custom-domain wiring are reviewable in a PR diff
even though they live outside git.

Expected files (added when E5 is wired against the dashboard):

- `build-settings.png` — Pages → Project → Settings → Builds & deployments
- `custom-domains.png` — Pages → Project → Custom domains
- `env-vars.png` — Pages → Project → Settings → Environment variables

When dashboard configuration changes (build command, output dir, Node version,
domain wiring), refresh the corresponding screenshot in the same PR that
records the change in [`site/README.md`](../../../site/README.md).

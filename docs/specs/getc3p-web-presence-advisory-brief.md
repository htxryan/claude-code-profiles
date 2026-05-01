# getc3p.dev — advisory brief

**Spec under review**: `docs/specs/getc3p-web-presence.md`
**Lenses**: simplicity & alternatives (claude), security & reliability (claude), design & IA (gemini)
**Source reports**: `/tmp/advisory/getc3p-{simplicity,security,design}-report.md`

This brief synthesises the three external reviews. It is **non-blocking** — it
informs the Gate 2 decision but does not veto it. The human chooses what to
accept, defer, or reject.

## Top-line verdicts

| Lens | Verdict |
|------|---------|
| Simplicity | **Scope down, then go.** Site stack is correct; monorepo restructure + custom CD workflow is gold-plating. |
| Security | **Not secure-by-default as written.** Three must-fix items before shipping. Reliability framing is good. |
| Design / IA | **"Will visitors get it in 10 seconds?" — Not yet.** Tech foundation solid; pitch buried in jargon; docs IA needs a recut. |

## CRITICAL — must-fix before any deploy workflow ships

**[security 1] Pin the PR event to `pull_request`, never `pull_request_target`.**
Fork PRs running with `pull_request_target` get the base ref's secrets *and* a
checkout of attacker-controlled code — a malicious `astro.config.mjs` or
postinstall script can exfiltrate `CLOUDFLARE_API_TOKEN` at build time. Add an
explicit EARS constraint forbidding `pull_request_target` in `deploy-site.yml`.
*Action*: amend R-E-3 to specify the event.

**[security 2] Environment-gate the Cloudflare token.** The token's scope is
account-wide (CF can't scope to a single Pages project). Mitigations: (a) GitHub
Environment with `main`-only deploy protection on the production deploy step,
(b) 90-day rotation cadence, (c) optionally move the site to its own CF
account for the cleanest blast-radius cut. *Action*: amend the secrets table
and add an EARS requirement for environment gating.

**[security 3] PR comment injection guard.** The sticky comment must be a
fixed template with the preview URL as the only interpolated variable, sourced
from `wrangler` output — *never* from PR title/branch metadata. *Action*: add
EARS constraint to R-E-3.

## STRONG RECOMMENDATIONS — direct conflict with your earlier Gate 1 answers

The simplicity reviewer makes two arguments that directly contradict choices
you locked in at Gate 1. You should decide whether to overrule the reviewer
or revise the spec.

**[simplicity A] Drop the monorepo restructure (R-U-9 to R-U-13).** You chose
"Full restructure" over "Keep package at root" at Gate 1. The reviewer's case:

- The site doesn't import a single line from `packages/c3p/`. There is no
  shared code, no shared types, no shared build tooling.
- The cost is real: regenerate lockfile (RA-3), rewrite release-please config
  (R-U-12), put the npm OIDC trusted-publisher entry at risk (RA-1).
- The benefit is hypothetical: "scaling to more packages later" — but C3P is
  one package and there's no roadmap for a second one.
- Lighter alternative: keep CLI at root, add `site/` as a sibling. No pnpm
  workspace. ~70% of the spec's mechanical risk disappears.

**[simplicity B] Drop the custom `deploy-site.yml` workflow.** Your original
ask was "CD workflows in gh, auto-deploying on every main push; stored in
this repo". The reviewer's case:

- Cloudflare Pages' native GitHub integration auto-deploys on push and posts
  preview URLs to PRs — that's R-E-2 and R-E-3 with zero YAML.
- Build command `pnpm --filter site build` (or `cd site && pnpm build` under
  the lighter restructure) is configurable in the CF Pages dashboard.
- The custom workflow re-implements what CF Pages already does for free, and
  introduces the secret-handling surface that drives the security-1/-2 risks.

**Note**: option B forfeits "workflow stored in the repo" — your original ask.
If repo-storage is load-bearing for you (audit trail, version control, peer
review of deploy logic), keep the custom workflow and apply security-1/-2/-3.
If repo-storage was just "I want CD on every main push", CF native integration
gives you that for free and is strictly safer.

## SHOULD-FIX — fold into the spec, not blocking

**[security 4] R-UN-1 is half-true.** `wrangler pages deploy` is atomic at
promote-time, but a successful deploy of broken JS still ships. Add a smoke-
check step (curl `/`, assert 200 + a known string) before considering a
deploy successful. Document manual rollback via `wrangler pages deployment
rollback`.

**[security 5] RA-1 rollback plan is vague.** Name the recovery path: re-add
the trusted publisher entry on npmjs.com, or fall back to `NPM_TOKEN` for one
release. Pre-stage the fallback token in a sealed secret.

**[security 6] Supply-chain gating is weak for a public site.** `ci.yml`'s
`audit` job is `continue-on-error: true`. Adding Astro + Starlight roughly
triples the dep tree. Add a non-advisory `pnpm audit --audit-level=critical`
gate on `apps/site` (or `site/`), and require `npm provenance` verification
on dependency installs.

**[design A] The 10-second pitch is buried in jargon.** Visitors won't
understand "drift gate" or "materialize" cold. The reviewer's recommendation:
replace the static hero with a "show, don't tell" demo — animated terminal or
high-fidelity SVG showing `c3p use dev` being *blocked* by the drift gate.
Land the value of the safety mechanism instantly.

**[design B] Demote "Migration" in the docs IA.** R-U-7 lists Migration as a
top-level pillar (25% of nav). New users don't need it. Move it under Guides.

**[design C] Group CLI verbs by intent.** 12 flat verbs is a wall of text.
Recommended grouping:
- **Core loop**: `init`, `new`, `use`, `status`
- **Inspection**: `list`, `drift`, `diff`, `validate`
- **Maintenance**: `sync`, `hook`, `doctor`, `completions`

**[design D] Persona-based feature cards** instead of README concept cards:
solo dev, team lead, CI/CD engineer.

**[design E] Visual inheritance diagram** for `extends` / `includes`. Layered
files in prose are high-friction.

## NITS — acknowledge but don't block

- pnpm cache shared across workflows via `setup-node`'s `cache: pnpm` —
  low-risk with `pull_request`, worth a one-line note.
- R-O-2 (`CF_WEB_ANALYTICS_TOKEN`) is fine; analytics beacons are low blast
  radius.
- Spec doesn't mention `concurrency:` on `deploy-site.yml` — without it, two
  PR pushes race for the same preview slot.
- "Subtle flourishes" voice risks reading as half-committed. Tie the C-3PO
  narration to actual UX moments (drift gate prompt, doctor output, 404)
  rather than dropping a stray line in the footer.
- One static OG image first; revisit per-page OG cards (R-U-5) if traffic
  warrants it.

## What the brief does *not* dispute

- Astro + Starlight as the stack
- Cloudflare Pages as the host
- `getc3p.dev` as the domain
- Single-app shape (marketing at `/`, docs at `/docs/*`)
- Subtle C-3PO voice direction
- SSG-only foreclosure (R-U-14)
- Existing matrix CI continues unmodified (R-U-13)
- Release-please path preservation (R-U-12) — explicitly endorsed by security

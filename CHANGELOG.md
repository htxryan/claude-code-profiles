# Changelog

## [0.6.0](https://github.com/htxryan/claude-code-config-profiles/compare/v0.5.0...v0.6.0) (2026-05-01)


### Features

* **brand:** add C3P logo to README, marketing header, and docs sidebar ([#20](https://github.com/htxryan/claude-code-config-profiles/issues/20)) ([9f1d444](https://github.com/htxryan/claude-code-config-profiles/commit/9f1d4446100fbf46ff0c8c1b782b010c1787e58c))


### Bug Fixes

* **site:** make active sidebar link readable in both themes ([#18](https://github.com/htxryan/claude-code-config-profiles/issues/18)) ([49f60c9](https://github.com/htxryan/claude-code-config-profiles/commit/49f60c9de7bf005e3bc6864cd8fa75579d5735e9))

## [0.5.0](https://github.com/htxryan/claude-code-config-profiles/compare/v0.4.0...v0.5.0) (2026-05-01)


### Features

* **site:** design tokens + a11y baseline (E2) ([9fb99c8](https://github.com/htxryan/claude-code-config-profiles/commit/9fb99c84356c801beb7655d47532b67c5b1d2d4e))
* **site:** E3 marketing landing + 404 + drift-gate demo ([708e3d5](https://github.com/htxryan/claude-code-config-profiles/commit/708e3d52b27b1c3dd1698380e9db6a70f03b0d54))
* **site:** E4 docs IA — concepts, CLI reference, guides, about ([e4e71df](https://github.com/htxryan/claude-code-config-profiles/commit/e4e71dfa2ef963c30bba565b283575fe9240ba7d))
* **site:** E6 Integration Verification — IV script + visual baselines ([d6872b2](https://github.com/htxryan/claude-code-config-profiles/commit/d6872b2f0de3b5b8a48e476d3f1d47c4c2714651))
* **site:** scaffold Astro + Starlight at site/ (E1) ([cc661fb](https://github.com/htxryan/claude-code-config-profiles/commit/cc661fbd9d1ea9f8baa9799061efb5d46e294e77))


### Bug Fixes

* **cli:** handle async EPIPE on stdout/stderr (closes claude-code-profiles-qga) ([e11c37b](https://github.com/htxryan/claude-code-config-profiles/commit/e11c37b0d5afa85c142a12780df8ae5450a7f91a))
* **site:** address E2 review feedback (a11y, portability, theme cascade) ([da55db8](https://github.com/htxryan/claude-code-config-profiles/commit/da55db8cdacec157e72c9efa03ef8714b49e8600))
* **site:** address E3 review feedback round 2 ([5d9d5b7](https://github.com/htxryan/claude-code-config-profiles/commit/5d9d5b737fa6604b7b80fb3fff834f1240d9ded0))
* **site:** address E4 review feedback ([db2e5c9](https://github.com/htxryan/claude-code-config-profiles/commit/db2e5c920d23091004551283bc658005f9de6a56))
* **site:** address E4 review feedback round 2 ([a720741](https://github.com/htxryan/claude-code-config-profiles/commit/a720741234355b96ce66e2422ed81b2579822f7b))
* **site:** address E6 IV review feedback round 3 ([36562c8](https://github.com/htxryan/claude-code-config-profiles/commit/36562c83b70a264e1d60cabc094ad4360c7b1598))
* **site:** bump Node requirement to 22.12+ (Astro 6 minimum) ([da5ea69](https://github.com/htxryan/claude-code-config-profiles/commit/da5ea699c6246ffeef0deeef156dee443899df91))
* **site:** close P3 followups from E6 IV (CTA contrast, 404 collision, Mermaid polish) ([979893f](https://github.com/htxryan/claude-code-config-profiles/commit/979893fb2a460ba0ee067df6a3c5429b2ea4f180))
* **site:** drift-gate demo timing + selector robustness ([390904e](https://github.com/htxryan/claude-code-config-profiles/commit/390904e97bb3e7a0dc543a8856594841b61b0e32))
* **site:** use Mermaid for extends/includes diagrams (R-U-16) ([ec73e7d](https://github.com/htxryan/claude-code-config-profiles/commit/ec73e7d9cd5afe7b32f2b0063eb2c10f522709ac))

## [0.4.0](https://github.com/htxryan/claude-code-config-profiles/compare/v0.3.0...v0.4.0) (2026-05-01)


### Features

* **cli:** turn up the C3P thematic flourishes (round 2) + add hidden hello command ([c41d15d](https://github.com/htxryan/claude-code-config-profiles/commit/c41d15d8755966d821747c29a556ada32814af76))

## [0.3.0](https://github.com/htxryan/claude-code-config-profiles/compare/v0.2.4...v0.3.0) (2026-04-30)


### ⚠ BREAKING CHANGES

* CLI binary `claude-profiles` is renamed to `c3p`. The old binary is no longer installed. Existing pre-commit hooks, gitignore sections, and CLAUDE.md markers in user repos are not auto-recognized.

### Features

* rename CLI bin from claude-profiles to c3p ([#10](https://github.com/htxryan/claude-code-config-profiles/issues/10)) ([c163d08](https://github.com/htxryan/claude-code-config-profiles/commit/c163d08748c4251416f6132b988846a77b453441))

## [0.3.0](https://github.com/htxryan/claude-code-config-profiles/compare/v0.2.4...v0.3.0) (2026-04-30)


### ⚠ BREAKING CHANGES

* **cli:** the CLI binary is renamed from `claude-profiles` to `c3p`. The npm package name (`claude-code-config-profiles`) and the on-disk profile-store directory (`.claude-profiles/`) are unchanged. Five user-visible surfaces rename together — there is no automatic migration:
  1. **npm bin entry** — `package.json` `bin` is now `{ "c3p": "dist/cli/bin.js" }`. The legacy `claude-profiles` bin key is removed.
  2. **In-CLI text** — help, errors, and progress hints all hardcode `c3p`.
  3. **Gitignore section header** — `# Added by claude-profiles` → `# Added by c3p`.
  4. **CLAUDE.md managed-block markers** — `<!-- claude-profiles:v1:begin -->` / `<!-- claude-profiles:v1:end -->` → `<!-- c3p:v1:begin -->` / `<!-- c3p:v1:end -->`. The marker reader does **not** recognize legacy `claude-profiles:v1` blocks.
  5. **Pre-commit hook** — the installed hook now invokes `c3p drift --pre-commit-warn` (with `command -v c3p` as the fail-open guard). Existing user hooks reference a binary that no longer exists and will silently fail-open.

### How to upgrade

1. Install the new binary:
   ```bash
   npm install -g claude-code-config-profiles@0.3.0
   ```
2. In each project that previously ran `claude-profiles init`:
   - Manually delete the legacy `# Added by claude-profiles` section from `.gitignore`.
   - Manually delete the legacy `<!-- claude-profiles:v1:begin -->` … `<!-- claude-profiles:v1:end -->` markers (and the bytes between them) from project-root `CLAUDE.md`.
   - Re-run `c3p init` to write a fresh gitignore section and CLAUDE.md markers.
   - Run `c3p hook install --force` to overwrite the legacy pre-commit hook.
3. Re-source shell completions: `c3p completions zsh > …` (and equivalent for `bash`/`fish`).

## [0.2.4](https://github.com/htxryan/claude-code-config-profiles/compare/v0.2.3...v0.2.4) (2026-04-30)


### Bug Fixes

* **cli:** canonicalise argv[1] before isDirect comparison ([2f863c4](https://github.com/htxryan/claude-code-config-profiles/commit/2f863c47a17967d7a7381b965a561bc889ba2edc))

## [0.2.3](https://github.com/htxryan/claude-code-config-profiles/compare/v0.2.2...v0.2.3) (2026-04-30)


### Bug Fixes

* **ci:** use Node 24 (ships npm &gt;= 11.5.1) for OIDC publish ([bd284f5](https://github.com/htxryan/claude-code-config-profiles/commit/bd284f571c1cce21b9e62e6a9f1a3fcfe3b813d9))

## [0.2.2](https://github.com/htxryan/claude-code-config-profiles/compare/v0.2.1...v0.2.2) (2026-04-30)


### Bug Fixes

* **ci:** bump publish step to Node 22 + npm@latest for OIDC support ([c76f1f6](https://github.com/htxryan/claude-code-config-profiles/commit/c76f1f64451165c0cd77e6c5d9dae62dce64898a))

## [0.2.1](https://github.com/htxryan/claude-code-config-profiles/compare/v0.2.0...v0.2.1) (2026-04-30)


### Bug Fixes

* **ci,release:** chain publish into release-please + Windows fingerprint flake ([d4c4291](https://github.com/htxryan/claude-code-config-profiles/commit/d4c42919f29e7f169bd55ee79f7e9971e8c95efe))
* **docs:** correct test count + duration in CONTRIBUTING ([86d6f2d](https://github.com/htxryan/claude-code-config-profiles/commit/86d6f2d3a0ff6370065b8009d556dddefe381750))
* **publish:** restore NODE_AUTH_TOKEN for first-publish bootstrap ([ba50a0b](https://github.com/htxryan/claude-code-config-profiles/commit/ba50a0b8312abf68b153df8e48af9b7592beeabb))
* **publish:** switch npm auth to tokenless OIDC trusted publisher ([441afdf](https://github.com/htxryan/claude-code-config-profiles/commit/441afdfbf518e1d530abb07bba64dba4fc5ff051))

## [0.2.0](https://github.com/htxryan/claude-code-profiles/compare/v0.1.0...v0.2.0) (2026-04-29)


### Features

* **cli/0zn:** doctor + completions + discoverability docs ([0c36113](https://github.com/htxryan/claude-code-profiles/commit/0c3611317abfaa00f62ffc47318231d5fb0648ce))
* **cli/3yy:** charm-style visual hierarchy — colour, byte intensity, phase progress ([b2517f7](https://github.com/htxryan/claude-code-profiles/commit/b2517f7bc05d6c7939f4d62a21850ad6f97baa31))
* **cli/azp:** power-user affordances — quiet, stale-source, previews, byte counts ([0eda8ff](https://github.com/htxryan/claude-code-profiles/commit/0eda8ffa74f96cf5c38d688b344cdc045355718b))
* **cli/bhq:** visual style consistency across mutating verbs ([8000051](https://github.com/htxryan/claude-code-profiles/commit/8000051bb598efc5d39c9edfd2eb04a1181e45d0))
* **cli/ppo:** every error names the next step ([c88b418](https://github.com/htxryan/claude-code-profiles/commit/c88b418adaff1557340516590ee912685d776e80))
* **cli/yd8:** polish decision-point UX (gate, lock, validate, marker errors) ([ae36227](https://github.com/htxryan/claude-code-profiles/commit/ae362270316570474d40dbb1ec46ac416339df6f))
* **cli:** E6 — init flow + hook install/uninstall ([f861b21](https://github.com/htxryan/claude-code-profiles/commit/f861b213a1d1157cb1e048b4fa9127a66a6edb9e))
* **cli:** markers module + init injection + validate check (cw6/T6) ([60ec73c](https://github.com/htxryan/claude-code-profiles/commit/60ec73c67919fdf711ad414897696350072d1c94))
* **cli:** skimmable read-only commands (list/status/drift/diff) ([0d5ed0a](https://github.com/htxryan/claude-code-profiles/commit/0d5ed0a84d69fe11324d27a874b58f26b0a2880b))
* **cli:** T1+T2 — argv parser, output channel, exit-code policy ([4f65dff](https://github.com/htxryan/claude-code-profiles/commit/4f65dff0a83b70f9b1e43af047f33c7abb818c30))
* **cli:** T3 — list, status, drift read commands ([6a6362b](https://github.com/htxryan/claude-code-profiles/commit/6a6362ba0a9815c3f7a87577334730b98c163467))
* **cli:** T4 — diff + validate commands ([eba2dab](https://github.com/htxryan/claude-code-profiles/commit/eba2dabf5dcbcad9a37a25ecf94ca0bdd1873a35))
* **cli:** T5 — swap orchestrator + use/sync commands + interactive prompt ([4327bac](https://github.com/htxryan/claude-code-profiles/commit/4327bac455daa210e11a7b4ca92e2f708bfcde01))
* **cli:** T6+T7+T8 — bin entry, dispatcher, new command, integration tests ([2a05f5c](https://github.com/htxryan/claude-code-profiles/commit/2a05f5c3f7c4fd1c900128256ef2f0c89c8e8846))
* **drift,persist:** section-only fingerprint and profile-root write-back (cw6/T5) ([66727f7](https://github.com/htxryan/claude-code-profiles/commit/66727f7147e7da9210a46b605901919e7441dd1f))
* **drift:** implement E4 — detect, gate, apply, pre-commit warn ([b0a901c](https://github.com/htxryan/claude-code-profiles/commit/b0a901c26a0051a5d889a5cef3842f2974da1e19))
* init UX polish, GitHub Actions CI/CD, npm publish metadata ([449cf4e](https://github.com/htxryan/claude-code-profiles/commit/449cf4e17a7e9e14d88699ddeada64fb322e15ff))
* **merge:** destination-aware grouping for project-root CLAUDE.md (cw6/T3) ([044ed69](https://github.com/htxryan/claude-code-profiles/commit/044ed6988b6ae5562e683f3a54f09b3f7b890b09))
* **merge:** implement E2 — strategy-pattern merge engine over ResolvedPlan ([b667497](https://github.com/htxryan/claude-code-profiles/commit/b66749750e3362709f6b8573e1368709d1cf3aba))
* **resolver:** destination-aware PlanFile for project-root CLAUDE.md (cw6/T2) ([cdf2516](https://github.com/htxryan/claude-code-profiles/commit/cdf251620987a6086f5a9cb46e0d3c22f154f3f9))
* **resolver:** implement E1 — manifest parsing, extends/includes resolution ([62654ff](https://github.com/htxryan/claude-code-profiles/commit/62654ff9faec0ef0b8fd3d22487a708cdf5d4f29))
* **state:** atomic section splice for projectRoot CLAUDE.md (cw6/T4) ([2df33e1](https://github.com/htxryan/claude-code-profiles/commit/2df33e186946ce41e446bf13e9031c3bf1521662))
* **state:** implement E3 — materialization, state, concurrency ([289bc05](https://github.com/htxryan/claude-code-profiles/commit/289bc05a5c6172cea309ef68a2f904cc97301a3c))


### Bug Fixes

* **ci:** platform-gate Windows-incompatible tests + dependabot + audit ([799c3d0](https://github.com/htxryan/claude-code-profiles/commit/799c3d04107fb5ed57c116f309d44c29ecf88201))
* **cli/azp:** apply external review feedback ([5c04335](https://github.com/htxryan/claude-code-profiles/commit/5c04335c5023d7c75e64db74c8b81224fb6d53be))
* **cli/bhq:** apply external review feedback ([c83ac44](https://github.com/htxryan/claude-code-profiles/commit/c83ac445afcee9d4e50f828394007a463ca2e49e))
* **cli/pcs:** ANSI-aware column padding + help-text refresh ([cccb7ec](https://github.com/htxryan/claude-code-profiles/commit/cccb7ec63f5ded67e336614427525d8865a38f69))
* **cli:** address E5 multi-reviewer feedback (P1/P2) ([a0393f4](https://github.com/htxryan/claude-code-profiles/commit/a0393f452f4b568e8db8f947dd2bf07c338d02d7))
* **cli:** address E5 third-pass review feedback (P1/P2/P3) ([c28d6fc](https://github.com/htxryan/claude-code-profiles/commit/c28d6fc8e7460fdb704d2efdf67edb29c9432a7f))
* **cli:** address E6 third-pass review feedback (P2/P3) ([ce506ef](https://github.com/htxryan/claude-code-profiles/commit/ce506ef54f47d58c48dc9d7b65d72fbf66421389))
* **cw6:** address P1 review findings — validate gating, stale section, e2e scenario, spec wording ([e69dea4](https://github.com/htxryan/claude-code-profiles/commit/e69dea4f70d3e64f2834e71d174087d4843f434b))
* **drift:** address E4 multi-reviewer feedback (P0/P1) ([c3a7ef3](https://github.com/htxryan/claude-code-profiles/commit/c3a7ef348b8388e1311ce0425b3b941a3d3555ce))
* **drift:** second-pass review — surface state warning in pre-commit ([04254ea](https://github.com/htxryan/claude-code-profiles/commit/04254eafd4a7741c4e246909438ec6a8a9b1c368))
* **merge:** address E2 multi-reviewer feedback (P2/P3) ([6d89d4a](https://github.com/htxryan/claude-code-profiles/commit/6d89d4abe68c1b250d4df9e9aa54d165d9c82a65))
* **n0u:** clear correctness debt — cw6 followups + ch5/bj0/j44 ([9d3ba85](https://github.com/htxryan/claude-code-profiles/commit/9d3ba85a6ed1f7b61e815a5c79a5aee3ca2d5c5a))
* **publish:** wire NPM_TOKEN secret for first publish ([26f5bf1](https://github.com/htxryan/claude-code-profiles/commit/26f5bf15f3a858f4e61bb23819026fd30c38f09d))
* **resolver:** address E1 multi-reviewer feedback (P1/P2/P3) ([e1895ba](https://github.com/htxryan/claude-code-profiles/commit/e1895baf69f22b0fce84cb9808fff425c4d733ff))
* **state:** address E3 multi-reviewer feedback (P1/P2/P3) ([4b699dc](https://github.com/htxryan/claude-code-profiles/commit/4b699dcc03a2acdaa494c29672af0d49807949b4))
* **state:** second-pass E3 review fixes (lock TOCTOU, tmp-paths, ENOENT) ([b3733f7](https://github.com/htxryan/claude-code-profiles/commit/b3733f7f680fb61a97a5bcfca2488275693607ab))

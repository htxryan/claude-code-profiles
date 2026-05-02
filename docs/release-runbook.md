# c3p Release Runbook

How to cut a c3p release, rotate the release tokens, and deprecate the
legacy npm package.

Source spec: [c3p-go-migration.md](specs/c3p-go-migration.md) PR8 / PR9
/ PR10 / PR27.

Pipeline definition: [`.goreleaser.yaml`](../.goreleaser.yaml) +
[`release.yml`](../.github/workflows/release.yml).

---

## One-shot prerequisites (human-managed)

These are NOT inside the W2 epic deliverables; they must exist before the
first goreleaser-driven release.

### 1. Homebrew tap repo — `htxryan/homebrew-tap`

- Create the repo (any layout; goreleaser writes `Casks/c3p.rb` because
  the config uses `homebrew_casks:`, not the deprecated `brews:`).
- Settings → Branches → branch protection on `main`:
  - Require pull request review (≥ 1 approver)
  - Require status checks (none configured initially is OK)
- Settings → General → Pull Requests → "Allow auto-merge" — **DISABLED**.
  PR8.1 mandates human approval; auto-merge is the override path that
  must not exist.

Users will install via:
```
brew install htxryan/tap/c3p
```

### 2. WinGet fork — `htxryan/winget-pkgs`

Fork [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs)
into `htxryan/winget-pkgs`. Goreleaser pushes a release branch into the
fork and opens a PR upstream against `microsoft/winget-pkgs:master`.
Manual upstream review applies (msft policy — outside our control).

### 3. GitHub Action secrets

Settings → Secrets and variables → Actions → New repository secret:

| Secret               | Scope (fine-grained PAT)                              | Permissions                                           | Expiration |
|----------------------|-------------------------------------------------------|-------------------------------------------------------|------------|
| `HOMEBREW_TAP_TOKEN` | Selected repository: `htxryan/homebrew-tap`           | Contents: Read+Write, Pull requests: Read+Write       | ≤ 90 days  |
| `WINGET_PKGS_TOKEN`  | Selected repository: `htxryan/winget-pkgs`            | Contents: Read+Write, Pull requests: Read+Write       | ≤ 90 days  |

PR8.1 + PR27 contracts:
- Tokens are fine-grained PATs, **not** classic PATs.
- Each token is scoped to a single repo (the tap or the winget fork).
- Expiration is ≤ 90 days. Rotation procedure below.

The default `GITHUB_TOKEN` is reserved for GitHub Releases write on
*this* repo only. It MUST NOT be reused for the tap or winget fork.

---

## Cutting a release

### Pre-flight (local)

From a clean checkout of the release commit:

```
goreleaser release --snapshot --clean --skip=publish,sign,announce
```

Inspect `dist/`. Confirm:

- One archive per `{linux,darwin,windows} × {amd64,arm64}` named
  `c3p_<version>_<os>_<arch>.{tar.gz,zip}`.
- A `c3p_<version>_checksums.txt` covering all archives.
- `dist/c3p_<version>.json` shape sane.

A failure here blocks the tag — fix the goreleaser config before pushing.

### Tag and push

```
git checkout main
git pull --rebase
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

The push triggers `.github/workflows/release.yml`:

1. **smoke** matrix — builds `c3p` for each native target on its native
   runner (linux/arm64 via qemu-user-static on ubuntu-latest) and runs
   `c3p --version`. PR8.5 gate: any failure aborts the release before
   any artifact is signed or uploaded.
2. **release** — goreleaser cross-compiles, archives, checksums,
   cosign-signs (keyless OIDC), uploads to GH Releases, opens PR to
   `htxryan/homebrew-tap`, opens PR to `htxryan/winget-pkgs`.

Cosign signing failure aborts the workflow (PR8.3 SHALL — no silent
skip).

**Recovery from partial-publish failure**: bump the patch version and
re-tag (e.g. v1.2.3 failed → push v1.2.4 with the fix). Do **NOT** delete
and re-push the same tag — re-tagging requires `git push --force` and
mutates the audit trail for any consumer who already pulled the failed
tag. The sole exception is a transient infra failure (e.g. tap repo 5xx)
where the goreleaser run can simply be re-run from the Actions UI; the
`mode: replace` setting in `.goreleaser.yaml` allows a re-run to overwrite
the partial GitHub Release without manual cleanup.

If the release workflow becomes stuck (no progress, no failure), cancel
it manually via the Actions UI. The `concurrency.cancel-in-progress: false`
setting blocks new workflow runs on the same tag until the stuck run is
resolved one way or the other.

### Post-flight

1. **Approve the Homebrew tap PR**. Merge by hand. Auto-merge is
   disabled per PR8.1.
2. **Approve the WinGet PR** in `htxryan/winget-pkgs` and forward it to
   `microsoft/winget-pkgs`. Manual upstream review applies.
3. **Verify a fresh install** on at least one platform:
   ```
   brew install htxryan/tap/c3p && c3p --version
   ```

### Cosign verification (for auditors and downstream consumers)

```
cosign verify-blob \
  --certificate                c3p_1.2.3_checksums.txt.pem \
  --signature                  c3p_1.2.3_checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/htxryan/claude-code-config-profiles/\.github/workflows/release\.yml@.+' \
  --certificate-oidc-issuer    https://token.actions.githubusercontent.com \
  c3p_1.2.3_checksums.txt
sha256sum -c c3p_1.2.3_checksums.txt
```

The certificate-identity-regexp pins the signature to the release
workflow path in this repo. A signature minted by a different workflow
or a different repo will not verify.

---

## PR27 — Token rotation cadence (≤ 90 days)

Set a recurring calendar reminder for **80 days** after each token's
issue date — rotate before expiration to avoid release-day breakage.

For each of `HOMEBREW_TAP_TOKEN` and `WINGET_PKGS_TOKEN`:

1. **Generate a new token.** GitHub user settings → Developer settings →
   Personal access tokens → **Fine-grained tokens** → "Generate new token".
   - Token name: `c3p-release-<repo>-<YYYYMMDD>` (e.g.
     `c3p-release-tap-20260801`).
   - Resource owner: `htxryan`.
   - Repository access: **Only select repositories** → choose just one
     (`homebrew-tap` OR `winget-pkgs`). Never "All repositories".
   - Expiration: **90 days**.
   - Repository permissions:
     - Contents: **Read and write**
     - Pull requests: **Read and write**
     - Metadata: Read-only (auto-included)
     - All other permissions: **No access**.
2. **Copy the token value** (it is shown once).
3. **Update the repo secret** in this repo:
   `htxryan/claude-code-config-profiles` → Settings → Secrets and
   variables → Actions → click the secret → Update value → paste.
4. **Revoke the previous token** on the same fine-grained tokens page.
5. **Record** the rotation in the release log (commit message in this
   repo or release-notes appendix) so the next rotation due-date is
   discoverable from git history.

If the token expires before rotation, a release will fail at the brew
or winget publish step with a 401/403 from the target repo. Recovery
is identical to step 1–3 above; the GH Release for the failed run is
already uploaded and signed (signing is upstream of brew/winget
publish), so once the token is refreshed re-run the release workflow.

---

## PR9 — npm deprecation (one-shot at v1.0.0)

Once the v1.0.0 Go release ships and `brew install htxryan/tap/c3p`
verifies on macOS + Linux:

```
npm deprecate "claude-code-config-profiles@<=0.99.99" \
  "Replaced by Go binary 'c3p'. Install via: brew install htxryan/tap/c3p, winget install htxryan.c3p, or download from https://github.com/htxryan/claude-code-config-profiles/releases"
```

Run from a machine logged into npm as a maintainer of
`claude-code-config-profiles`. No final npm publish is required —
existing 0.x versions remain installable but `npm install` surfaces the
deprecation warning, pointing users at the Go install paths.

After deprecation:
- Archive (do NOT unpublish) the package on npm. Unpublishing breaks
  any existing lockfile that pinned a 0.x version; the deprecation
  warning is the migration channel.
- Trusted-publisher entry for this package on npm can be left in place
  or removed; the `release-please.yml` workflow was deleted in epic Z
  cutover, so no new npm releases will be cut from this repo.

---

## Re-decomposition trigger (W2 epic)

If the WinGet review process requires per-release manual changes that
cannot be templated into the goreleaser winget config (e.g., msft adds
a required field that varies by release), W2 distribution must split
into:
- W2a — automated channels (Homebrew + GH Releases)
- W2b — manually-curated channel (WinGet)

Until then, this single runbook covers all three channels.

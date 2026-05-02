---
title: c3p hook
description: Install or uninstall the git pre-commit drift hook.
---

`c3p hook install|uninstall` manages the project's git pre-commit hook. The
hook script is **fixed and minimal**, and is **fail-open** — a missing or
broken `c3p` binary never blocks a commit. Its only job is to surface a
warning if you're about to commit drifted `.claude/` files.

## Help

```text
c3p hook — manage the git pre-commit hook

USAGE
  c3p hook install|uninstall [options]

DESCRIPTION
  The hook script content is fixed and minimal (R25a) and fail-open: a
  missing or broken c3p binary never blocks commits.
  
  install:    writes .git/hooks/pre-commit (preserves existing hook unless --force)
  uninstall:  removes the hook only if its content matches the canonical
              script (a user-edited or third-party hook is left untouched)

OPTIONS
  --force            (install only) overwrite an existing pre-commit hook

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p hook install
  c3p hook install --force
  c3p hook uninstall
  c3p hook install --json

EXIT CODES
  0  success (or no-op if hook is already in the desired state)
  1  bad argv (missing install|uninstall, install with conflicting hook + no --force)
  2  IO/permission fault, missing .git/ directory
```

## Example

```bash
# Install (or no-op if already installed)
c3p hook install

# Replace an existing hook with the canonical c3p one
c3p hook install --force

# Remove the hook (only if it's the canonical script — user-edited hooks left alone)
c3p hook uninstall
```

## Windows: `pre-commit.bat` companion

When you run `c3p hook install` on Windows, c3p writes **both** files into the
hooks directory:

- `pre-commit` — the same POSIX shell script installed on Linux/macOS
- `pre-commit.bat` — a native cmd companion with the same fail-open behavior

Both are managed (install/uninstall) symmetrically: install writes both, and
uninstall removes both **only if their bytes match the canonical content**.
A user-edited `pre-commit.bat` is left alone, mirroring the POSIX rule.

### Which hook does git actually run?

Git for Windows installations vary, and **`core.hooksPath`** is the
authoritative answer:

- Default install (no `core.hooksPath` set): git looks for `.git/hooks/pre-commit`
  and runs it through the bundled MSYS2 `sh.exe`. The POSIX script wins.
- Custom `core.hooksPath` pointing at a directory whose `pre-commit.bat`
  resolves before `pre-commit` (per the user's `PATHEXT` and shell): the
  `.bat` runs instead.
- IDE-bundled git (some Windows editors): may invoke the `.bat` directly,
  bypassing `sh.exe` entirely.

You can override either side without touching c3p — the bytes are pinned and
both code paths share the same fail-open contract, so either choice keeps
the "drift warning, never block a commit" guarantee.

### Verifying which one git uses

```sh
git config --get core.hooksPath
git -C <repo> rev-parse --git-path hooks
```

c3p always writes to `.git/hooks/` (the conventional location). If you've
set `core.hooksPath` to point elsewhere, git will read from your custom
path and **not** see c3p's hooks — you'll need to copy or symlink the
files into your `core.hooksPath` directory yourself, or unset
`core.hooksPath` to use c3p's managed hooks. Either way, the bytes are
pinned and the same fail-open contract applies wherever the file lands.

To disable one of the two, delete it. Re-running `c3p hook install`
restores both. Uninstall removes c3p's managed copy in either direction.

## See also

- [`c3p init`](/docs/cli/init/) — installs the hook by default; `--no-hook` opts out
- [`c3p drift --pre-commit-warn`](/docs/cli/drift/) — the fail-open hook entry point
- [Drift concept](/docs/concepts/drift/)

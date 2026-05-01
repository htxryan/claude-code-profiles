---
title: c3p completions
description: Emit a bash, zsh, or fish completion script.
---

`c3p completions <shell>` prints a static shell-completion script to stdout.
Source it in your shell's startup file (or eval it inline) to enable
tab-complete for verbs, `--flags`, and profile names on commands like `use`,
`diff`, `validate`, and `sync`.

Profile names are read from `.claude-profiles/` at tab-time — no daemon,
no state-file reads.

## Help

```text
c3p completions — emit a shell completion script (bash | zsh | fish)

USAGE
  c3p completions <shell>

DESCRIPTION
  Prints a static completion script to stdout. Source the output in
  your shell's startup file (or eval it inline) to enable tab-complete
  for verbs, --flags, and profile names on `use`/`diff`/`validate`/
  `sync`. Profile names are read from `.claude-profiles/` at tab time;
  no daemon, no state-file reads.

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p completions zsh > ~/.zfunc/_c3p
  eval "$(c3p completions bash)"
  c3p completions fish > ~/.config/fish/completions/c3p.fish

EXIT CODES
  0  success
  1  bad argv (missing shell, unsupported shell)
```

## Example

```bash
# bash — eval inline
eval "$(c3p completions bash)"

# zsh — install into your fpath
c3p completions zsh > ~/.zfunc/_c3p

# fish — install into the system completion dir
c3p completions fish > ~/.config/fish/completions/c3p.fish
```

## See also

- [Quickstart](/docs/guides/quickstart/) — typical first-time setup

/**
 * Help text generator. Centralised so the dispatcher and the parser's
 * `--help`-after-verb path both render the same content.
 *
 * The text is intentionally terse — the README/docs cover deep usage. Help
 * here exists to remind the user of the verb shapes and exit-code policy.
 */

const TOP = `claude-profiles — swappable .claude/ configurations

USAGE
  claude-profiles <command> [args] [options]

COMMANDS
  init                     bootstrap .claude-profiles/ in this project
  list                     show all profiles + active marker
  use <name>               switch to <name>; runs the drift gate
  status                   show active profile + drift summary
  drift                    per-file drift report (read-only)
  diff <a> [<b>]           file-level diff of two profiles' resolved trees
  new <name>               scaffold an empty profile
  validate [<name>]        dry-run resolve+merge over one or all profiles
  sync                     re-materialize the active profile (drift-gated)
  hook install|uninstall   install / remove the git pre-commit hook

GLOBAL OPTIONS
  --json                   emit one JSON object per command (silences human output)
  --cwd=<path>             project root (default: current working dir)
  --on-drift=<choice>      discard|persist|abort — required for non-TTY swap with drift
  --no-color               disable ANSI colour
  --help, -h               this message; "claude-profiles <verb> --help" for verb-specific help
  --version, -V            print version

EXIT CODES
  0  success
  1  user error (drift abort, bad argv, validation fail)
  2  system error (IO/permission/internal fault)
  3  conflict, cycle, missing profile/include, lock held by peer process
`;

const VERBS: Record<string, string> = {
  init: `claude-profiles init — bootstrap .claude-profiles/ (E6, not yet implemented)`,
  list: `claude-profiles list — list all profiles with active marker, extends, includes,
last-materialized timestamp.

Options: --json (machine-readable)`,
  use: `claude-profiles use <name> — switch to profile <name>.

Runs the drift gate: if .claude/ has uncommitted edits, prompts you to
discard / persist / abort. Non-TTY sessions MUST pass --on-drift=<choice>;
otherwise the command exits 1 immediately so CI scripts never block.

Options:
  --on-drift=discard|persist|abort   pre-answer the gate
  --json                             emit a structured outcome payload`,
  status: `claude-profiles status — print active profile, drift summary, warnings.

Options: --json`,
  drift: `claude-profiles drift — per-file drift report with provenance.

Options:
  --json
  --pre-commit-warn   fail-open hook entry point (always exits 0)`,
  diff: `claude-profiles diff <a> [<b>] — file-level diff of resolved+merged file lists.

If <b> is omitted, compares <a> to the active profile.

Options: --json`,
  new: `claude-profiles new <name> [--description=<text>] — scaffold an empty profile.

Refuses if .claude-profiles/<name>/ already exists. Edit the generated
profile.json to set extends/includes.`,
  validate: `claude-profiles validate [<name>] — dry-run resolve+merge.

With no name, validates every profile in the project. Exits 3 on any failure.

Options: --json`,
  sync: `claude-profiles sync — re-materialize the active profile.

Same drift-gate flow as 'use'. Exits 1 if no profile is active.`,
  hook: `claude-profiles hook install|uninstall — manage the git pre-commit hook (E6, not yet implemented)`,
};

export function topLevelHelp(): string {
  return TOP.trimEnd();
}

export function verbHelp(verb: string): string {
  return VERBS[verb] ?? `claude-profiles: no specific help for "${verb}"\n\n` + TOP.trimEnd();
}

export function versionString(version: string): string {
  return `claude-profiles ${version}`;
}

/**
 * Help text generator. Centralised so the dispatcher and the parser's
 * `--help`-after-verb path both render the same content.
 *
 * Per-verb help follows a single template:
 *   tagline → USAGE → DESCRIPTION → OPTIONS → GLOBAL OPTIONS → EXAMPLES → EXIT CODES.
 * The tests/cli/integration/help-version.test.ts suite snapshots key strings
 * so format drift is caught in CI (claude-code-profiles-xd2).
 */

const TOP = `claude-profiles — swappable .claude/ configurations

USAGE
  claude-profiles <command> [args] [options]

COMMANDS
  init                     bootstrap .claude-profiles/ in this project
  list                     show all profiles + active marker
  use <name>               switch to <name>; runs the drift gate
  status                   show active profile + drift summary
  drift                    per-file drift report (read-only); --preview shows inline unified diffs
  diff <a> [<b>]           file-level diff of two profiles' resolved trees; --preview shows inline content
  new <name>               scaffold an empty profile
  validate [<name>]        dry-run resolve+merge over one or all profiles
  sync                     re-materialize the active profile (drift-gated)
  hook install|uninstall   install / remove the git pre-commit hook
  doctor                   read-only health check (state, lock, gitignore, hook, markers)
  completions <shell>      emit a bash|zsh|fish completion script (eval to install)

GLOBAL OPTIONS
  --json                   emit one JSON object per command (silences human output)
  --quiet, -q              silence human output; preserves errors + exit codes (mutually exclusive with --json)
  --cwd=<path>             project root (default: current working dir)
  --on-drift=<choice>      discard|persist|abort — required for non-TTY swap with drift
  --wait[=<seconds>]       poll a held lock instead of failing fast (default 30s)
  --no-color               disable colour output (additive with NO_COLOR env)
  --help, -h               this message; "claude-profiles <verb> --help" for verb-specific help
  --version, -V            print version

GLOSSARY
  profile        a named configuration in .claude-profiles/<name>/
  extends        single-parent inheritance (child layers over parent files)
  includes       additive components spliced into a profile
  drift          byte-level differences between live .claude/ and the active profile
  materialize    copy resolved+merged profile into .claude/ via atomic rename

EXIT CODES
  0  success
  1  user error (bad argv, drift abort, validation fail, profile-name typo)
  2  system error (IO/permission/internal fault)
  3  structural conflict — cycle in extends, missing include, missing extends
     parent in a manifest, peer process holds the lock

See README.md for full documentation.
`;

interface VerbHelp {
  tagline: string;
  /** Synopsis line (without the leading "claude-profiles "). */
  synopsis: string;
  description: string;
  /** Verb-local options (one per line, "--name=<val>  description"). */
  options: string[];
  /** Global options visible in this verb's help (subset of top-level globals). */
  globals: string[];
  /** Examples ("  claude-profiles ...  # purpose"). */
  examples: string[];
  /** Exit codes a successful run of this verb may produce. */
  exitCodes: string[];
}

const COMMON_GLOBALS = [
  "--cwd=<path>     project root (default: cwd)",
  "--json           machine-readable output (silences human output)",
  "--quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json",
];
const SWAP_GLOBALS = [
  ...COMMON_GLOBALS,
  "--on-drift=<v>   discard|persist|abort (required in non-TTY when drift exists)",
  "--wait[=<sec>]   poll a held lock with backoff instead of failing fast (default 30s)",
];

const VERBS: Record<string, VerbHelp> = {
  init: {
    tagline: "bootstrap .claude-profiles/ in this project",
    synopsis: "init [options]",
    description:
      "Creates .claude-profiles/, optionally seeds a starter profile from an\n" +
      "existing .claude/ tree, updates .gitignore, and installs the pre-commit\n" +
      "hook. Also injects claude-profiles markers into project-root CLAUDE.md\n" +
      "(preserves existing content) so profiles can manage a section of it.\n" +
      "Refuses to overwrite an already-initialised .claude-profiles/.",
    options: [
      "--starter=<name>   starter profile name (default: \"default\")",
      "--no-seed          skip seeding from .claude/ even if present",
      "--no-hook          skip installing the pre-commit hook",
    ],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles init                    # bootstrap with defaults",
      "claude-profiles init --no-hook          # skip pre-commit hook (CI / non-git)",
      "claude-profiles init --starter=dev      # name the starter profile \"dev\"",
    ],
    exitCodes: [
      "0  success",
      "1  already initialised; bad argv",
      "2  IO/permission fault (e.g. unwritable .git/hooks/)",
    ],
  },
  list: {
    tagline: "list all profiles with active marker, description, tags",
    synopsis: "list [options]",
    description:
      "Prints one row per profile: name (active is marked `*` and bold),\n" +
      "description (column shown only when at least one profile has one),\n" +
      "tags (column shown only when at least one profile has any), and a\n" +
      "trailing meta column with extends/includes/last-materialized.",
    options: [],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles list",
      "claude-profiles list --json | jq '.profiles[].name'",
    ],
    exitCodes: ["0  success", "2  IO fault reading .claude-profiles/"],
  },
  status: {
    tagline: "print active profile, description, drift summary, warnings",
    synopsis: "status [options]",
    description:
      "Reports the active profile name (with its description, when present),\n" +
      "the count of drifted files in the live .claude/ tree, any resolver\n" +
      "warnings carried over from the last swap, AND a stale-source signal\n" +
      "when the active profile's source files have changed since the last\n" +
      "materialize (a teammate's `git pull` brings in new bytes that .claude/\n" +
      "hasn't picked up yet — run `claude-profiles sync` to apply them).",
    options: [],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles status",
      "claude-profiles status --json",
    ],
    exitCodes: ["0  success", "2  IO fault"],
  },
  drift: {
    tagline: "per-file drift report with provenance",
    synopsis: "drift [options]",
    description:
      "Lists each file in .claude/ that differs from the active profile's\n" +
      "resolved+merged tree, naming the contributor each file came from.\n" +
      "Read-only — does not change anything on disk. The summary line shows\n" +
      "byte deltas: `(+added -removed ~changed bytes)`.",
    options: [
      "--pre-commit-warn  fail-open hook entry point (always exits 0)",
      "--verbose          include scan stats (scanned N, fast=X, slow=Y) in the summary",
      "--preview          render unified-diff content for modified entries (capped at 20 lines per file)",
    ],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles drift",
      "claude-profiles drift --json",
      "claude-profiles drift --preview            # show what changed inside each drifted file",
      "claude-profiles drift --pre-commit-warn   # used by the git hook; never blocks",
    ],
    exitCodes: ["0  success (drift present or absent)", "2  IO fault"],
  },
  diff: {
    tagline: "file-level diff of resolved+merged file lists",
    synopsis: "diff <a> [<b>] [options]",
    description:
      "Compares two profiles' resolved+merged file lists. If <b> is omitted,\n" +
      "compares <a> to the currently active profile. The summary line shows\n" +
      "byte deltas: `(+added -removed ~changed bytes)`.",
    options: [
      "--preview          render unified-diff content for changed entries (capped at 20 lines per file)",
    ],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles diff dev ci          # compare two profiles",
      "claude-profiles diff dev             # compare dev to the active profile",
      "claude-profiles diff dev ci --preview # also show what changed inside each file",
    ],
    exitCodes: [
      "0  success",
      "1  bad argv (missing required <a>)",
      "3  cycle / missing include / missing extends parent in either profile",
    ],
  },
  new: {
    tagline: "scaffold an empty profile",
    synopsis: "new <name> [--description=<text>] [options]",
    description:
      "Creates .claude-profiles/<name>/ with a minimal profile.json. Refuses if\n" +
      "the directory already exists. Edit the generated profile.json to set\n" +
      "extends/includes, then add files under .claude-profiles/<name>/.claude/.",
    options: [
      "--description=<text>   one-line description recorded in profile.json",
    ],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles new dev",
      "claude-profiles new dev --description=\"local dev with verbose agents\"",
      "claude-profiles new ci --json",
    ],
    exitCodes: [
      "0  success",
      "1  bad argv, invalid name, or profile already exists",
      "2  IO/permission fault",
    ],
  },
  use: {
    tagline: "switch to profile <name>; runs the drift gate",
    synopsis: "use <name> [options]",
    description:
      "Materializes <name> into .claude/. If <name> (or any contributor) has a\n" +
      "profile-root CLAUDE.md, also splices its content between the markers in\n" +
      "project-root CLAUDE.md (user content above/below preserved). If .claude/\n" +
      "has uncommitted edits (drift), prompts you to discard / persist / abort.\n" +
      "Non-TTY sessions MUST pass --on-drift=<choice>; otherwise the command\n" +
      "exits 1 immediately so CI scripts never block on a hidden prompt.",
    options: [],
    globals: SWAP_GLOBALS,
    examples: [
      "claude-profiles use dev                       # interactive (prompts on drift)",
      "claude-profiles use ci --on-drift=discard     # CI: drop drifted edits",
      "claude-profiles use dev --on-drift=persist    # write drift back to active first",
      "claude-profiles use dev --json --on-drift=abort",
    ],
    exitCodes: [
      "0  success",
      "1  drift abort, missing --on-drift in non-TTY, profile-name typo",
      "2  IO fault during materialize/backup",
      "3  cycle / missing include / missing extends parent / lock held by peer",
    ],
  },
  validate: {
    tagline: "dry-run resolve+merge over one or all profiles",
    synopsis: "validate [<name>] [options]",
    description:
      "Walks the resolver and merger without writing anything. With no name,\n" +
      "validates every profile in the project and reports pass/fail per profile.\n" +
      "When a profile is active, also checks that project-root CLAUDE.md has the\n" +
      "claude-profiles markers (run `init` to add them).",
    options: [
      "--brief            collapse FAIL rows to one line each (CI-friendly)",
    ],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles validate              # validate all profiles (full error per FAIL)",
      "claude-profiles validate dev          # validate just dev",
      "claude-profiles validate --brief      # one-line FAIL rows (CI scripts)",
      "claude-profiles validate --json",
    ],
    exitCodes: [
      "0  all profiles validated cleanly",
      "1  unparseable profile.json (invalid manifest); project-root CLAUDE.md is missing claude-profiles markers (run `claude-profiles init`)",
      "3  any profile failed (cycle, missing include, missing extends parent, conflict)",
    ],
  },
  sync: {
    tagline: "re-materialize the active profile (drift-gated)",
    synopsis: "sync [options]",
    description:
      "Picks up edits made directly to the active profile's source tree and\n" +
      "writes them into .claude/. Same drift gate as 'use' — uncommitted edits\n" +
      "to .claude/ trigger the discard/persist/abort prompt.",
    options: [],
    globals: SWAP_GLOBALS,
    examples: [
      "claude-profiles sync",
      "claude-profiles sync --on-drift=discard",
    ],
    exitCodes: [
      "0  success",
      "1  no profile is currently active; drift abort; missing --on-drift in non-TTY",
      "2  IO fault",
      "3  cycle / missing include / lock held by peer",
    ],
  },
  doctor: {
    tagline: "read-only health check across state, lock, gitignore, hook, markers",
    synopsis: "doctor [options]",
    description:
      "Runs the same checks as `validate` plus environment diagnostics:\n" +
      "state-file schema (R42), lock liveness (R41), gitignore correctness\n" +
      "(R15), pre-commit hook byte-equality (R25a), backup retention count\n" +
      "(R23a), external-path reachability (R37a), and managed-block markers\n" +
      "in project-root CLAUDE.md (R44/R45). Read-only — never writes.\n" +
      "Returns 0 when every check passes and 1 on any actionable warning so\n" +
      "CI scripts can `claude-profiles doctor || exit 0` for soft checks.",
    options: [],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles doctor                # human-readable status table",
      "claude-profiles doctor --json         # machine-readable summary for CI",
      "claude-profiles doctor || echo \"check failed\"  # gate a script on health",
    ],
    exitCodes: [
      "0  all checks passed",
      "1  one or more checks reported a warning or failure",
      "2  IO/permission fault while running checks",
    ],
  },
  completions: {
    tagline: "emit a shell completion script (bash | zsh | fish)",
    synopsis: "completions <shell>",
    description:
      "Prints a static completion script to stdout. Source the output in\n" +
      "your shell's startup file (or eval it inline) to enable tab-complete\n" +
      "for verbs, --flags, and profile names on `use`/`diff`/`validate`/\n" +
      "`sync`. Profile names are read from `.claude-profiles/` at tab time;\n" +
      "no daemon, no state-file reads.",
    options: [],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles completions zsh > ~/.zfunc/_claude-profiles",
      "eval \"$(claude-profiles completions bash)\"",
      "claude-profiles completions fish > ~/.config/fish/completions/claude-profiles.fish",
    ],
    exitCodes: [
      "0  success",
      "1  bad argv (missing shell, unsupported shell)",
    ],
  },
  hook: {
    tagline: "manage the git pre-commit hook",
    synopsis: "hook install|uninstall [options]",
    description:
      "The hook script content is fixed and minimal (R25a) and fail-open: a\n" +
      "missing or broken claude-profiles binary never blocks commits.\n" +
      "\n" +
      "install:    writes .git/hooks/pre-commit (preserves existing hook unless --force)\n" +
      "uninstall:  removes the hook only if its content matches the canonical\n" +
      "            script (a user-edited or third-party hook is left untouched)",
    options: [
      "--force            (install only) overwrite an existing pre-commit hook",
    ],
    globals: COMMON_GLOBALS,
    examples: [
      "claude-profiles hook install",
      "claude-profiles hook install --force",
      "claude-profiles hook uninstall",
      "claude-profiles hook install --json",
    ],
    exitCodes: [
      "0  success (or no-op if hook is already in the desired state)",
      "1  bad argv (missing install|uninstall, install with conflicting hook + no --force)",
      "2  IO/permission fault, missing .git/ directory",
    ],
  },
};

function renderVerb(verb: string, h: VerbHelp): string {
  const sections: string[] = [];
  sections.push(`claude-profiles ${verb} — ${h.tagline}`);
  sections.push(`USAGE\n  claude-profiles ${h.synopsis}`);
  sections.push(`DESCRIPTION\n  ${h.description.replace(/\n/g, "\n  ")}`);
  if (h.options.length > 0) {
    sections.push(`OPTIONS\n  ${h.options.join("\n  ")}`);
  }
  if (h.globals.length > 0) {
    sections.push(`GLOBAL OPTIONS\n  ${h.globals.join("\n  ")}`);
  }
  if (h.examples.length > 0) {
    sections.push(`EXAMPLES\n  ${h.examples.join("\n  ")}`);
  }
  if (h.exitCodes.length > 0) {
    sections.push(`EXIT CODES\n  ${h.exitCodes.join("\n  ")}`);
  }
  return sections.join("\n\n");
}

export function topLevelHelp(): string {
  return TOP.trimEnd();
}

export function verbHelp(verb: string): string {
  const h = VERBS[verb];
  if (h === undefined) {
    return `claude-profiles: no specific help for "${verb}"\n\n${TOP.trimEnd()}`;
  }
  return renderVerb(verb, h);
}

export function versionString(version: string): string {
  return `claude-profiles ${version}`;
}

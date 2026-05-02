// Help text generator. Mirrors src/cli/help.ts byte-for-byte so the dual-
// suite IV harness can compare both binaries' --help output.
package cli

import "strings"

const topHelp = `c3p — fluent in over six million forms of .claude/ configuration

USAGE
  c3p <command> [args] [options]

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
  --help, -h               this message; "c3p <verb> --help" for verb-specific help
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

See README.md for full documentation.`

type verbHelpEntry struct {
	tagline     string
	synopsis    string
	description string
	options     []string
	globals     []string
	examples    []string
	exitCodes   []string
}

var commonGlobals = []string{
	"--cwd=<path>     project root (default: cwd)",
	"--json           machine-readable output (silences human output)",
	"--quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json",
}

var swapGlobals = append([]string{}, append(commonGlobals,
	"--on-drift=<v>   discard|persist|abort (required in non-TTY when drift exists)",
	"--wait[=<sec>]   poll a held lock with backoff instead of failing fast (default 30s)",
)...)

var verbHelpMap = map[string]verbHelpEntry{
	"init": {
		tagline:  "bootstrap .claude-profiles/ in this project; shall I prepare the way?",
		synopsis: "init [options]",
		description: "Creates .claude-profiles/, optionally seeds a starter profile from an\n" +
			"existing .claude/ tree, updates .gitignore, and installs the pre-commit\n" +
			"hook. Also injects c3p markers into project-root CLAUDE.md\n" +
			"(preserves existing content) so profiles can manage a section of it.\n" +
			"Refuses to overwrite an already-initialised .claude-profiles/.",
		options: []string{
			`--starter=<name>   starter profile name (default: "default")`,
			"--no-seed          skip seeding from .claude/ even if present",
			"--no-hook          skip installing the pre-commit hook",
		},
		globals: commonGlobals,
		examples: []string{
			"c3p init                    # bootstrap with defaults",
			"c3p init --no-hook          # skip pre-commit hook (CI / non-git)",
			`c3p init --starter=dev      # name the starter profile "dev"`,
		},
		exitCodes: []string{
			"0  success",
			"1  already initialised; bad argv",
			"2  IO/permission fault (e.g. unwritable .git/hooks/)",
		},
	},
	"list": {
		tagline:  "allow me to introduce all profiles, with active marker, description, tags",
		synopsis: "list [options]",
		description: "Prints one row per profile: name (active is marked `*` and bold),\n" +
			"description (column shown only when at least one profile has one),\n" +
			"tags (column shown only when at least one profile has any), and a\n" +
			"trailing meta column with extends/includes/last-materialized.",
		globals: commonGlobals,
		examples: []string{
			"c3p list",
			"c3p list --json | jq '.profiles[].name'",
		},
		exitCodes: []string{"0  success", "2  IO fault reading .claude-profiles/"},
	},
	"status": {
		tagline:  "print active profile, description, drift summary, warnings",
		synopsis: "status [options]",
		description: "Reports the active profile name (with its description, when present),\n" +
			"the count of drifted files in the live .claude/ tree, any resolver\n" +
			"warnings carried over from the last swap, AND a stale-source signal\n" +
			"when the active profile's source files have changed since the last\n" +
			"materialize (a teammate's `git pull` brings in new bytes that .claude/\n" +
			"hasn't picked up yet — run `c3p sync` to apply them).",
		globals: commonGlobals,
		examples: []string{
			"c3p status",
			"c3p status --json",
		},
		exitCodes: []string{"0  success", "2  IO fault"},
	},
	"drift": {
		tagline:  "per-file drift report with provenance",
		synopsis: "drift [options]",
		description: "Lists each file in .claude/ that differs from the active profile's\n" +
			"resolved+merged tree, naming the contributor each file came from.\n" +
			"Read-only — does not change anything on disk. The summary line shows\n" +
			"byte deltas: `(+added -removed ~changed bytes)`.",
		options: []string{
			"--pre-commit-warn  fail-open hook entry point (always exits 0)",
			"--verbose          include scan stats (scanned N, fast=X, slow=Y) in the summary",
			"--preview          render unified-diff content for modified entries (capped at 20 lines per file)",
		},
		globals: commonGlobals,
		examples: []string{
			"c3p drift",
			"c3p drift --json",
			"c3p drift --preview            # show what changed inside each drifted file",
			"c3p drift --pre-commit-warn   # used by the git hook; never blocks",
		},
		exitCodes: []string{"0  success (drift present or absent)", "2  IO fault"},
	},
	"diff": {
		tagline:  "file-level diff — I do believe these two differ",
		synopsis: "diff <a> [<b>] [options]",
		description: "Compares two profiles' resolved+merged file lists. If <b> is omitted,\n" +
			"compares <a> to the currently active profile. The summary line shows\n" +
			"byte deltas: `(+added -removed ~changed bytes)`.",
		options: []string{
			"--preview          render unified-diff content for changed entries (capped at 20 lines per file)",
		},
		globals: commonGlobals,
		examples: []string{
			"c3p diff dev ci          # compare two profiles",
			"c3p diff dev             # compare dev to the active profile",
			"c3p diff dev ci --preview # also show what changed inside each file",
		},
		exitCodes: []string{
			"0  success",
			"1  bad argv (missing required <a>)",
			"3  cycle / missing include / missing extends parent in either profile",
		},
	},
	"new": {
		tagline:  "scaffold an empty profile — splendid!",
		synopsis: "new <name> [--description=<text>] [options]",
		description: "Creates .claude-profiles/<name>/ with a minimal profile.json. Refuses if\n" +
			"the directory already exists. Edit the generated profile.json to set\n" +
			"extends/includes, then add files under .claude-profiles/<name>/.claude/.",
		options: []string{
			"--description=<text>   one-line description recorded in profile.json",
		},
		globals: commonGlobals,
		examples: []string{
			"c3p new dev",
			`c3p new dev --description="local dev with verbose agents"`,
			"c3p new ci --json",
		},
		exitCodes: []string{
			"0  success",
			"1  bad argv, invalid name, or profile already exists",
			"2  IO/permission fault",
		},
	},
	"use": {
		tagline:  "switch to profile <name>; do mind the drift gate, if I may",
		synopsis: "use <name> [options]",
		description: "Materializes <name> into .claude/. If <name> (or any contributor) has a\n" +
			"profile-root CLAUDE.md, also splices its content between the markers in\n" +
			"project-root CLAUDE.md (user content above/below preserved). If .claude/\n" +
			"has uncommitted edits (drift), prompts you to discard / persist / abort.\n" +
			"Non-TTY sessions MUST pass --on-drift=<choice>; otherwise the command\n" +
			"exits 1 immediately so CI scripts never block on a hidden prompt.",
		globals: swapGlobals,
		examples: []string{
			"c3p use dev                       # interactive (prompts on drift)",
			"c3p use ci --on-drift=discard     # CI: drop drifted edits",
			"c3p use dev --on-drift=persist    # write drift back to active first",
			"c3p use dev --json --on-drift=abort",
		},
		exitCodes: []string{
			"0  success",
			"1  drift abort, missing --on-drift in non-TTY, profile-name typo",
			"2  IO fault during materialize/backup",
			"3  cycle / missing include / missing extends parent / lock held by peer",
		},
	},
	"validate": {
		tagline:  "dry-run resolve+merge — I do try to be thorough",
		synopsis: "validate [<name>] [options]",
		description: "Walks the resolver and merger without writing anything. With no name,\n" +
			"validates every profile in the project and reports pass/fail per profile.\n" +
			"When a profile is active, also checks that project-root CLAUDE.md has the\n" +
			"c3p markers (run `init` to add them).",
		options: []string{
			"--brief            collapse FAIL rows to one line each (CI-friendly)",
		},
		globals: commonGlobals,
		examples: []string{
			"c3p validate              # validate all profiles (full error per FAIL)",
			"c3p validate dev          # validate just dev",
			"c3p validate --brief      # one-line FAIL rows (CI scripts)",
			"c3p validate --json",
		},
		exitCodes: []string{
			"0  all profiles validated cleanly",
			"1  unparseable profile.json (invalid manifest); project-root CLAUDE.md is missing c3p markers (run `c3p init`)",
			"3  any profile failed (cycle, missing include, missing extends parent, conflict)",
		},
	},
	"sync": {
		tagline:  "re-materialize the active profile — at your service",
		synopsis: "sync [options]",
		description: "Picks up edits made directly to the active profile's source tree and\n" +
			"writes them into .claude/. Same drift gate as 'use' — uncommitted edits\n" +
			"to .claude/ trigger the discard/persist/abort prompt.",
		globals: swapGlobals,
		examples: []string{
			"c3p sync",
			"c3p sync --on-drift=discard",
		},
		exitCodes: []string{
			"0  success",
			"1  no profile is currently active; drift abort; missing --on-drift in non-TTY",
			"2  IO fault",
			"3  cycle / missing include / lock held by peer",
		},
	},
	"doctor": {
		tagline:  "if anything is amiss, I shall fret about it for you",
		synopsis: "doctor [options]",
		description: "Runs the same checks as `validate` plus environment diagnostics:\n" +
			"state-file schema (R42), lock liveness (R41), gitignore correctness\n" +
			"(R15), pre-commit hook byte-equality (R25a), backup retention count\n" +
			"(R23a), external-path reachability (R37a), and managed-block markers\n" +
			"in project-root CLAUDE.md (R44/R45). Read-only — never writes.\n" +
			"Returns 0 when every check passes and 1 on any actionable warning so\n" +
			"CI scripts can `c3p doctor || exit 0` for soft checks.",
		globals: commonGlobals,
		examples: []string{
			"c3p doctor                # human-readable status table",
			"c3p doctor --json         # machine-readable summary for CI",
			`c3p doctor || echo "check failed"  # gate a script on health`,
		},
		exitCodes: []string{
			"0  all checks passed",
			"1  one or more checks reported a warning or failure",
			"2  IO/permission fault while running checks",
		},
	},
	"completions": {
		tagline:  "emit a shell completion script (bash | zsh | fish)",
		synopsis: "completions <shell>",
		description: "Prints a static completion script to stdout. Source the output in\n" +
			"your shell's startup file (or eval it inline) to enable tab-complete\n" +
			"for verbs, --flags, and profile names on `use`/`diff`/`validate`/\n" +
			"`sync`. Profile names are read from `.claude-profiles/` at tab time;\n" +
			"no daemon, no state-file reads.",
		globals: commonGlobals,
		examples: []string{
			"c3p completions zsh > ~/.zfunc/_c3p",
			`eval "$(c3p completions bash)"`,
			"c3p completions fish > ~/.config/fish/completions/c3p.fish",
		},
		exitCodes: []string{
			"0  success",
			"1  bad argv (missing shell, unsupported shell)",
		},
	},
	"hook": {
		tagline:  "manage the git pre-commit hook",
		synopsis: "hook install|uninstall [options]",
		description: "The hook script content is fixed and minimal (R25a) and fail-open: a\n" +
			"missing or broken c3p binary never blocks commits.\n" +
			"\n" +
			"install:    writes .git/hooks/pre-commit (preserves existing hook unless --force)\n" +
			"uninstall:  removes the hook only if its content matches the canonical\n" +
			"            script (a user-edited or third-party hook is left untouched)",
		options: []string{
			"--force            (install only) overwrite an existing pre-commit hook",
		},
		globals: commonGlobals,
		examples: []string{
			"c3p hook install",
			"c3p hook install --force",
			"c3p hook uninstall",
			"c3p hook install --json",
		},
		exitCodes: []string{
			"0  success (or no-op if hook is already in the desired state)",
			"1  bad argv (missing install|uninstall, install with conflicting hook + no --force)",
			"2  IO/permission fault, missing .git/ directory",
		},
	},
}

// TopLevelHelp returns the top-level help text (no trailing newline).
func TopLevelHelp() string {
	return topHelp
}

// VerbHelp returns the help text for verb. Returns a "no specific help"
// fallback when the verb isn't documented (e.g. the hidden hello).
func VerbHelp(verb string) string {
	h, ok := verbHelpMap[verb]
	if !ok {
		return "c3p: I do beg your pardon — no specific help for \"" + verb + "\".\n\n" + topHelp
	}
	return renderVerbHelp(verb, h)
}

func renderVerbHelp(verb string, h verbHelpEntry) string {
	var sections []string
	sections = append(sections, "c3p "+verb+" — "+h.tagline)
	sections = append(sections, "USAGE\n  c3p "+h.synopsis)
	sections = append(sections, "DESCRIPTION\n  "+strings.ReplaceAll(h.description, "\n", "\n  "))
	if len(h.options) > 0 {
		sections = append(sections, "OPTIONS\n  "+strings.Join(h.options, "\n  "))
	}
	if len(h.globals) > 0 {
		sections = append(sections, "GLOBAL OPTIONS\n  "+strings.Join(h.globals, "\n  "))
	}
	if len(h.examples) > 0 {
		sections = append(sections, "EXAMPLES\n  "+strings.Join(h.examples, "\n  "))
	}
	if len(h.exitCodes) > 0 {
		sections = append(sections, "EXIT CODES\n  "+strings.Join(h.exitCodes, "\n  "))
	}
	return strings.Join(sections, "\n\n")
}

// VersionString formats the version banner ("c3p X.Y.Z").
func VersionString(version string) string {
	return "c3p " + version
}

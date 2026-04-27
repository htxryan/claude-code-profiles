/**
 * E5 cross-module contracts: Command discriminated union and GlobalOptions.
 *
 * The argv parser produces a typed Command; the dispatcher pattern-matches on
 * `kind` and routes to the right handler. Adding a new verb means adding a
 * new variant here, a parser branch, a dispatcher case, and (usually) a
 * command-module handler.
 *
 * Per R29, the public verb surface is fixed:
 *   init | list | use | status | drift | diff | new | validate | sync | hook
 *
 * Out of E5 scope (deferred to E6): the actual implementations of `init` and
 * `hook install|uninstall` — but the dispatcher must understand them so users
 * see consistent help/parse behaviour from day one.
 */

import type { GateChoice } from "../drift/types.js";

/**
 * Choices the user may pass via `--on-drift=<choice>`. Strict subset of
 * GateChoice that excludes the orchestration-internal "no-drift-proceed".
 */
export type OnDriftFlag = Exclude<GateChoice, "no-drift-proceed">;

/**
 * Global flags accepted on every verb. `cwd` is the project root the CLI
 * operates against — defaults to process.cwd(); tests inject a fixture path.
 *
 * `--no-color` is treated as additive over the NO_COLOR env var: either one
 * being set disables colour. The flag exists so users can disable colour ad
 * hoc without exporting an env var (e.g. piping into a pager).
 */
export interface GlobalOptions {
  /** When true, all human-readable output is suppressed; only JSON to stdout. */
  json: boolean;
  /** Project root override (defaults to process.cwd()). */
  cwd: string;
  /** Honored gate flag for `use`/`sync` non-interactive paths. */
  onDrift: OnDriftFlag | null;
  /** When true, force colour off (additive with NO_COLOR env). */
  noColor: boolean;
  /**
   * When true, silence human print()/warn() output but keep error() and the
   * exit code semantics. Designed for shell chains: `claude-profiles use ci -q
   * && ./run`. Mutually exclusive with --json (the parser rejects both).
   */
  quiet: boolean;
  /**
   * yd8 / AC-4: when set, mutating verbs (`use`, `sync`, `init`, `new`)
   * poll a held lock with exponential backoff for up to `waitMs`
   * milliseconds before failing with LockHeldError. Off by default — opt-in
   * only so non-interactive scripts that forgot to pass --wait still fail
   * fast at the first conflict. The CLI surface is `--wait` (defaults to a
   * sensible 30s) or `--wait=<seconds>`.
   */
  waitMs: number | null;
}

export type CommandKind =
  | "init"
  | "list"
  | "use"
  | "status"
  | "drift"
  | "diff"
  | "new"
  | "validate"
  | "sync"
  | "hook"
  | "help"
  | "version";

export type HookAction = "install" | "uninstall";

/**
 * Command discriminated union. Each variant carries verb-specific args;
 * GlobalOptions are bundled separately by the parser to keep verbs minimal.
 */
export type Command =
  | {
      kind: "init";
      /** Name to give a seeded starter profile (default "default"). */
      starter: string;
      /** When false, skip seeding even if `.claude/` exists. */
      seed: boolean;
      /** When false, skip the pre-commit hook install step. */
      hook: boolean;
    }
  | { kind: "list" }
  | { kind: "use"; profile: string }
  | { kind: "status" }
  | { kind: "drift"; preCommitWarn: boolean; verbose: boolean; preview: boolean }
  | { kind: "diff"; a: string; b: string | null; preview: boolean }
  | { kind: "new"; profile: string; description: string | null }
  | { kind: "validate"; profile: string | null; brief: boolean }
  | { kind: "sync" }
  | { kind: "hook"; action: HookAction; force: boolean }
  | { kind: "help"; verb: string | null }
  | { kind: "version" };

export interface ParsedInvocation {
  command: Command;
  global: GlobalOptions;
}

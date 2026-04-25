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
 */
export interface GlobalOptions {
  /** When true, all human-readable output is suppressed; only JSON to stdout. */
  json: boolean;
  /** Project root override (defaults to process.cwd()). */
  cwd: string;
  /** Honored gate flag for `use`/`sync` non-interactive paths. */
  onDrift: OnDriftFlag | null;
  /** Disable ANSI colour even when stderr is a TTY. */
  noColor: boolean;
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
  | { kind: "init" }
  | { kind: "list" }
  | { kind: "use"; profile: string }
  | { kind: "status" }
  | { kind: "drift"; preCommitWarn: boolean }
  | { kind: "diff"; a: string; b: string | null }
  | { kind: "new"; profile: string; description: string | null }
  | { kind: "validate"; profile: string | null }
  | { kind: "sync" }
  | { kind: "hook"; action: HookAction }
  | { kind: "help"; verb: string | null }
  | { kind: "version" };

export interface ParsedInvocation {
  command: Command;
  global: GlobalOptions;
}

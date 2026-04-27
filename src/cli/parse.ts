/**
 * Hand-rolled argv parser. Avoids external deps so the CLI is dependency-free
 * (matters for `npx claude-profiles`-style cold starts) and so the parsing
 * surface is fully reviewable in one file.
 *
 * Conventions:
 *  - First non-flag token is the verb (R29). `--help`/`--version` short-
 *    circuit verb dispatch.
 *  - Global flags (`--json`, `--cwd=<path>`, `--on-drift=<choice>`,
 *    `--no-color`, `--help`, `--version`) may appear anywhere in argv —
 *    before or after the verb.
 *  - Verb-specific flags (`--description=<txt>` for `new`, `--pre-commit-warn`
 *    for `drift`) are scoped per verb.
 *  - Positional args after the verb are required (e.g. `use <name>`); we
 *    return ParseError naming the missing arg, never throw.
 *  - Unknown verbs / flags / values are ParseErrors with exit-code-1 semantics.
 */

import type { GlobalOptions, OnDriftFlag, ParsedInvocation } from "./types.js";

export interface ParseError {
  ok: false;
  /** Human-readable error suitable for stderr. */
  message: string;
  /** True iff the user typed `--help` or `--version` (no args required). */
  helpRequested: boolean;
}

export type ParseResult =
  | { ok: true; invocation: ParsedInvocation }
  | ParseError;

const VERBS = new Set([
  "init",
  "list",
  "use",
  "status",
  "drift",
  "diff",
  "new",
  "validate",
  "sync",
  "hook",
  "help",
]);

const ON_DRIFT_VALUES: ReadonlySet<OnDriftFlag> = new Set<OnDriftFlag>([
  "discard",
  "persist",
  "abort",
]);

/**
 * Parse argv (without the leading `node` + script path — caller slices off
 * `process.argv.slice(2)`). Returns either a typed ParsedInvocation or a
 * structured error.
 *
 * `defaultCwd` is the cwd to use when `--cwd=` is not passed; the bin entry
 * supplies `process.cwd()`.
 */
export function parseArgs(argv: ReadonlyArray<string>, defaultCwd: string): ParseResult {
  // Collect global flags + extract verb + remaining tokens in a single pass.
  const tokens = [...argv];
  const global: GlobalOptions = {
    json: false,
    cwd: defaultCwd,
    onDrift: null,
    noColor: false,
    quiet: false,
    waitMs: null,
  };

  // Side-channel for help/version short-circuit. We still want to parse the
  // verb (so `claude-profiles use --help` shows use-specific help), but we
  // surface the request via the Command itself.
  let helpFlagSeen = false;
  let versionFlagSeen = false;

  // First pass: pull global flags out of the token stream. We do this BEFORE
  // verb dispatch so `--cwd=/tmp use foo` and `use foo --cwd=/tmp` both work.
  const verbAndArgs: string[] = [];
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i]!;
    if (t === "--json") {
      global.json = true;
    } else if (t === "--help" || t === "-h") {
      helpFlagSeen = true;
    } else if (t === "--version" || t === "-V") {
      versionFlagSeen = true;
    } else if (t === "--cwd") {
      const next = tokens[i + 1];
      if (next === undefined || next.startsWith("-")) {
        return parseError(`--cwd requires a path argument`);
      }
      // Empty-string is rejected here as well as in the `--cwd=` branch — a
      // bare `""` arg silently running against cwd="" would later look up
      // `.claude-profiles/` in whatever directory the OS normalises "" to.
      if (next === "") return parseError(`--cwd requires a non-empty path`);
      global.cwd = next;
      i++;
    } else if (t.startsWith("--cwd=")) {
      global.cwd = t.slice("--cwd=".length);
      if (global.cwd === "") return parseError(`--cwd requires a non-empty path`);
    } else if (t === "--on-drift") {
      const next = tokens[i + 1];
      if (next === undefined) return parseError(`--on-drift requires a value (discard|persist|abort)`);
      const flag = parseOnDrift(next);
      if (!flag) return parseError(`--on-drift must be discard|persist|abort, got "${next}"`);
      global.onDrift = flag;
      i++;
    } else if (t.startsWith("--on-drift=")) {
      const v = t.slice("--on-drift=".length);
      const flag = parseOnDrift(v);
      if (!flag) return parseError(`--on-drift must be discard|persist|abort, got "${v}"`);
      global.onDrift = flag;
    } else if (t === "--no-color") {
      // Additive with NO_COLOR env: the flag turns colour off even when env
      // is unset. Threaded through dispatch into createStyle.
      global.noColor = true;
    } else if (t === "--quiet" || t === "-q") {
      // Silences human print()/warn() while preserving error() and exit code.
      // Mutually-exclusive enforcement happens after the full pass so users
      // see one consolidated error regardless of arg order.
      global.quiet = true;
    } else if (t === "--wait") {
      // yd8 / AC-4: bare --wait → 30 second default budget. Picked to be
      // long enough that a typical short swap (sub-second) clears, while
      // capping accidental hangs at well under the impatient-user threshold.
      global.waitMs = 30_000;
    } else if (t.startsWith("--wait=")) {
      const v = t.slice("--wait=".length);
      const seconds = Number.parseFloat(v);
      if (!Number.isFinite(seconds) || seconds < 0) {
        return parseError(`--wait must be a non-negative number of seconds; got "${v}"`);
      }
      global.waitMs = Math.round(seconds * 1000);
    } else {
      verbAndArgs.push(t);
    }
  }

  // --quiet and --json are mutually exclusive: --json already silences human
  // output, and a script that asks for both is signalling unclear intent —
  // surface the conflict at parse time rather than silently picking one.
  if (global.quiet && global.json) {
    return parseError(`--quiet and --json are mutually exclusive`);
  }

  // Version short-circuit beats verb dispatch (R29 doesn't list `--version`
  // as a verb but it's a universal CLI affordance). Fires regardless of
  // whether a verb is also present — silently consuming `--version` when a
  // verb is supplied (`claude-profiles list --version`) hides the user's
  // intent. The verb form `--help` keeps the verb (renders verb-specific
  // help), but `--version` always means "print version and exit".
  if (versionFlagSeen) {
    return ok({ command: { kind: "version" }, global });
  }

  // Help with no verb -> top-level help.
  if (verbAndArgs.length === 0) {
    if (helpFlagSeen) return ok({ command: { kind: "help", verb: null }, global });
    return parseError(
      `missing command; run "claude-profiles --help" for usage`,
      true,
    );
  }

  const verb = verbAndArgs[0]!;
  const rest = verbAndArgs.slice(1);

  if (!VERBS.has(verb)) {
    return parseError(`unknown command "${verb}"; run "claude-profiles --help" for usage`);
  }

  // `help <verb>` is equivalent to `<verb> --help`. Both produce a help
  // command for the given verb.
  if (verb === "help") {
    if (rest.length === 0) return ok({ command: { kind: "help", verb: null }, global });
    if (rest.length > 1) return parseError(`help takes at most one argument; got "${rest.join(" ")}"`);
    return ok({ command: { kind: "help", verb: rest[0]! }, global });
  }

  if (helpFlagSeen) {
    return ok({ command: { kind: "help", verb }, global });
  }

  // Per-verb dispatch.
  switch (verb) {
    case "init": {
      let starter = "default";
      let seed = true;
      let hook = true;
      const positional: string[] = [];
      for (let i = 0; i < rest.length; i++) {
        const t = rest[i]!;
        if (t === "--no-seed") seed = false;
        else if (t === "--no-hook") hook = false;
        else if (t === "--starter") {
          const next = rest[i + 1];
          // Treat any leading `-` as a missing value: profile names never
          // start with `-`, so `init --starter -force` (a typo) would
          // otherwise silently bind `starter = "-force"`.
          if (next === undefined || next.startsWith("-")) {
            return parseError(`--starter requires a value`);
          }
          starter = next;
          i++;
        } else if (t.startsWith("--starter=")) {
          starter = t.slice("--starter=".length);
          if (starter === "") return parseError(`--starter requires a non-empty value`);
        } else if (t.startsWith("--")) {
          return parseError(`init: unknown flag "${t}"`);
        } else {
          positional.push(t);
        }
      }
      if (positional.length > 0) {
        return parseError(`init takes no positional arguments; got "${positional.join(" ")}"`);
      }
      return ok({ command: { kind: "init", starter, seed, hook }, global });
    }

    case "list":
      if (rest.length > 0) return parseError(`list takes no arguments; got "${rest.join(" ")}"`);
      return ok({ command: { kind: "list" }, global });

    case "status":
      if (rest.length > 0) return parseError(`status takes no arguments; got "${rest.join(" ")}"`);
      return ok({ command: { kind: "status" }, global });

    case "sync":
      if (rest.length > 0) return parseError(`sync takes no arguments; got "${rest.join(" ")}"`);
      return ok({ command: { kind: "sync" }, global });

    case "use": {
      if (rest.length === 0) return parseError(`use requires a profile name`);
      if (rest.length > 1) return parseError(`use takes one argument; got "${rest.join(" ")}"`);
      return ok({ command: { kind: "use", profile: rest[0]! }, global });
    }

    case "drift": {
      let preCommitWarn = false;
      let verbose = false;
      let preview = false;
      const positional: string[] = [];
      for (const t of rest) {
        if (t === "--pre-commit-warn") preCommitWarn = true;
        else if (t === "--verbose") verbose = true;
        else if (t === "--preview") preview = true;
        else if (t.startsWith("--")) return parseError(`drift: unknown flag "${t}"`);
        else positional.push(t);
      }
      if (positional.length > 0) return parseError(`drift takes no positional arguments; got "${positional.join(" ")}"`);
      return ok({ command: { kind: "drift", preCommitWarn, verbose, preview }, global });
    }

    case "diff": {
      let preview = false;
      const positional: string[] = [];
      for (const t of rest) {
        if (t === "--preview") preview = true;
        else if (t.startsWith("--")) return parseError(`diff: unknown flag "${t}"`);
        else positional.push(t);
      }
      if (positional.length === 0) return parseError(`diff requires at least one profile name`);
      if (positional.length > 2) return parseError(`diff takes one or two profile names; got "${positional.join(" ")}"`);
      return ok({
        command: { kind: "diff", a: positional[0]!, b: positional[1] ?? null, preview },
        global,
      });
    }

    case "new": {
      let description: string | null = null;
      const positional: string[] = [];
      for (let i = 0; i < rest.length; i++) {
        const t = rest[i]!;
        if (t === "--description") {
          const next = rest[i + 1];
          if (next === undefined || next.startsWith("--")) {
            return parseError(`--description requires a value`);
          }
          description = next;
          i++;
        } else if (t.startsWith("--description=")) {
          description = t.slice("--description=".length);
        } else if (t.startsWith("--")) {
          return parseError(`new: unknown flag "${t}"`);
        } else {
          positional.push(t);
        }
      }
      if (positional.length === 0) return parseError(`new requires a profile name`);
      if (positional.length > 1) return parseError(`new takes one positional argument; got "${positional.join(" ")}"`);
      return ok({
        command: { kind: "new", profile: positional[0]!, description },
        global,
      });
    }

    case "validate": {
      let brief = false;
      const positional: string[] = [];
      for (const t of rest) {
        if (t === "--brief") brief = true;
        else if (t.startsWith("--")) return parseError(`validate: unknown flag "${t}"`);
        else positional.push(t);
      }
      if (positional.length > 1) return parseError(`validate takes at most one profile name; got "${positional.join(" ")}"`);
      return ok({
        command: { kind: "validate", profile: positional[0] ?? null, brief },
        global,
      });
    }

    case "hook": {
      let force = false;
      const positional: string[] = [];
      for (const t of rest) {
        if (t === "--force") force = true;
        else if (t.startsWith("--")) return parseError(`hook: unknown flag "${t}"`);
        else positional.push(t);
      }
      if (positional.length === 0) return parseError(`hook requires an action (install|uninstall)`);
      if (positional.length > 1) return parseError(`hook takes one positional argument; got "${positional.join(" ")}"`);
      const action = positional[0]!;
      if (action !== "install" && action !== "uninstall") {
        return parseError(`hook action must be install|uninstall, got "${action}"`);
      }
      // `--force` is only meaningful for `install` (overwrite a foreign
      // hook). Silently accepting it on `uninstall` invites scripts that
      // think they're forcing removal — but our policy is to NEVER remove
      // a non-matching hook. Reject explicitly so the contract is clear.
      if (action === "uninstall" && force) {
        return parseError(`hook uninstall does not accept --force (a non-matching hook is never removed)`);
      }
      return ok({ command: { kind: "hook", action, force }, global });
    }

    default: {
      // Should be unreachable thanks to VERBS.has() guard above.
      const _exhaustive: string = verb;
      return parseError(`unknown command "${_exhaustive}"`);
    }
  }
}

function parseOnDrift(value: string): OnDriftFlag | null {
  if (ON_DRIFT_VALUES.has(value as OnDriftFlag)) return value as OnDriftFlag;
  return null;
}

function ok(invocation: ParsedInvocation): ParseResult {
  return { ok: true, invocation };
}

function parseError(message: string, helpRequested = false): ParseError {
  return { ok: false, message, helpRequested };
}

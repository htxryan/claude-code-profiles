/**
 * Command dispatcher. Pattern-matches on Command.kind, calls the right
 * handler, and returns an exit code. Errors thrown by handlers propagate so
 * the bin entry's central catch can format them via formatError + exitCodeFor.
 */

import type { GateMode } from "../drift/types.js";

import { runDrift } from "./commands/drift.js";
import { runDiff } from "./commands/diff.js";
import { runHook } from "./commands/hook.js";
import { runInit } from "./commands/init.js";
import { runList } from "./commands/list.js";
import { runNew } from "./commands/new.js";
import { runStatus } from "./commands/status.js";
import { runSync } from "./commands/sync.js";
import { runUse } from "./commands/use.js";
import { runValidate } from "./commands/validate.js";
import { EXIT_OK } from "./exit.js";
import { topLevelHelp, verbHelp, versionString } from "./help.js";
import type { OutputChannel } from "./output.js";
import type { Command, GlobalOptions } from "./types.js";

export interface DispatchContext {
  output: OutputChannel;
  /** Determines whether the gate may prompt. */
  mode: GateMode;
  /** Package version for --version. */
  version: string;
  /** Plumbed through to the lock; tests pass false. */
  signalHandlers: boolean;
}

/**
 * Dispatch a parsed Command. Returns an exit code; throws on failure (the
 * caller's central catch handles error formatting).
 */
export async function dispatch(
  command: Command,
  global: GlobalOptions,
  ctx: DispatchContext,
): Promise<number> {
  switch (command.kind) {
    case "version":
      // print() is silenced under --json (per the OutputChannel contract). For
      // consumers running `claude-profiles --version --json` we still emit a
      // structured payload so the output is non-empty and machine-parseable.
      if (ctx.output.jsonMode) ctx.output.json({ version: ctx.version });
      else ctx.output.print(versionString(ctx.version));
      return EXIT_OK;

    case "help": {
      const text = command.verb !== null ? verbHelp(command.verb) : topLevelHelp();
      // Same rationale as version: emit JSON shape under --json so the channel
      // never silently produces an empty stdout for a successful command.
      if (ctx.output.jsonMode) {
        ctx.output.json(command.verb !== null ? { help: text, verb: command.verb } : { help: text });
      } else {
        ctx.output.print(text);
      }
      return EXIT_OK;
    }

    case "list":
      return runList({ cwd: global.cwd, output: ctx.output, noColor: global.noColor });

    case "status":
      return runStatus({ cwd: global.cwd, output: ctx.output, noColor: global.noColor });

    case "drift":
      return runDrift({
        cwd: global.cwd,
        output: ctx.output,
        preCommitWarn: command.preCommitWarn,
        verbose: command.verbose,
      });

    case "diff":
      return runDiff({
        cwd: global.cwd,
        output: ctx.output,
        a: command.a,
        b: command.b,
      });

    case "validate":
      return runValidate({
        cwd: global.cwd,
        output: ctx.output,
        profile: command.profile,
        noColor: global.noColor,
      });

    case "new":
      return runNew({
        cwd: global.cwd,
        output: ctx.output,
        profile: command.profile,
        description: command.description,
        noColor: global.noColor,
      });

    case "use":
      return runUse({
        cwd: global.cwd,
        output: ctx.output,
        profile: command.profile,
        mode: ctx.mode,
        onDriftFlag: global.onDrift,
        signalHandlers: ctx.signalHandlers,
        noColor: global.noColor,
      });

    case "sync":
      return runSync({
        cwd: global.cwd,
        output: ctx.output,
        mode: ctx.mode,
        onDriftFlag: global.onDrift,
        signalHandlers: ctx.signalHandlers,
        noColor: global.noColor,
      });

    case "init":
      return runInit({
        cwd: global.cwd,
        output: ctx.output,
        starterName: command.starter,
        seedFromClaudeDir: command.seed,
        installHook: command.hook,
        signalHandlers: ctx.signalHandlers,
        noColor: global.noColor,
      });

    case "hook":
      return runHook({
        cwd: global.cwd,
        output: ctx.output,
        action: command.action,
        force: command.force,
        noColor: global.noColor,
      });

    default: {
      const _exhaustive: never = command;
      throw new Error(`unreachable: unknown command kind ${String(_exhaustive)}`);
    }
  }
}

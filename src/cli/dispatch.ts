/**
 * Command dispatcher. Pattern-matches on Command.kind, calls the right
 * handler, and returns an exit code. Errors thrown by handlers propagate so
 * the bin entry's central catch can format them via formatError + exitCodeFor.
 *
 * Init/hook stubs (epic E5 → E6 handoff): we register placeholder handlers
 * that throw CliNotImplementedError. The dispatcher never silently no-ops;
 * the user always gets a clear "not yet implemented (E6)" message.
 */

import type { GateMode } from "../drift/types.js";

import { runDrift } from "./commands/drift.js";
import { runDiff } from "./commands/diff.js";
import { runList } from "./commands/list.js";
import { runNew } from "./commands/new.js";
import { runStatus } from "./commands/status.js";
import { runSync } from "./commands/sync.js";
import { runUse } from "./commands/use.js";
import { runValidate } from "./commands/validate.js";
import { CliNotImplementedError, EXIT_OK } from "./exit.js";
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
      ctx.output.print(versionString(ctx.version));
      return EXIT_OK;

    case "help":
      if (command.verb !== null) ctx.output.print(verbHelp(command.verb));
      else ctx.output.print(topLevelHelp());
      return EXIT_OK;

    case "list":
      return runList({ cwd: global.cwd, output: ctx.output });

    case "status":
      return runStatus({ cwd: global.cwd, output: ctx.output });

    case "drift":
      return runDrift({
        cwd: global.cwd,
        output: ctx.output,
        preCommitWarn: command.preCommitWarn,
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
      });

    case "new":
      return runNew({
        cwd: global.cwd,
        output: ctx.output,
        profile: command.profile,
        description: command.description,
      });

    case "use":
      return runUse({
        cwd: global.cwd,
        output: ctx.output,
        profile: command.profile,
        mode: ctx.mode,
        onDriftFlag: global.onDrift,
        signalHandlers: ctx.signalHandlers,
      });

    case "sync":
      return runSync({
        cwd: global.cwd,
        output: ctx.output,
        mode: ctx.mode,
        onDriftFlag: global.onDrift,
        signalHandlers: ctx.signalHandlers,
      });

    case "init":
      throw new CliNotImplementedError("init", "E6");

    case "hook":
      throw new CliNotImplementedError(`hook ${command.action}`, "E6");

    default: {
      const _exhaustive: never = command;
      throw new Error(`unreachable: unknown command kind ${String(_exhaustive)}`);
    }
  }
}

/**
 * Public surface of the E5 CLI module. Downstream callers (E6 init/hook,
 * embedding contexts that want to invoke commands programmatically) consume
 * only this surface.
 */

export { dispatch, type DispatchContext } from "./dispatch.js";
export { main } from "./bin.js";
export { parseArgs, type ParseError, type ParseResult } from "./parse.js";
export {
  createOutput,
  type OutputChannel,
  type OutputChannelOptions,
} from "./output.js";
export {
  CliNotImplementedError,
  CliUserError,
  EXIT_CONFLICT,
  EXIT_OK,
  EXIT_SYSTEM_ERROR,
  EXIT_USER_ERROR,
  exitCodeFor,
  type ExitCode,
} from "./exit.js";
export type {
  Command,
  CommandKind,
  GlobalOptions,
  HookAction,
  OnDriftFlag,
  ParsedInvocation,
} from "./types.js";

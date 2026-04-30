/**
 * `hello` command (claude-code-profiles-7x4). Hidden easter-egg verb.
 *
 * Intentionally undocumented: NO entry in src/cli/help.ts VERBS map, NO mention
 * in topLevelHelp(). Discovering the verb is the whole point. `c3p hello --help`
 * therefore falls through to the generic "no specific help" path — that's the
 * desired behaviour, not an oversight.
 *
 * Output shape mirrors `version`/`completions`: human path under default mode,
 * structured payload under `--json`. Compact (non-pretty) JSON matches the
 * project convention — every other command emits `JSON.stringify(payload)`
 * without indentation so `| jq -s 'add'` and one-object-per-line streaming
 * stay consistent.
 */

import { EXIT_OK } from "../exit.js";
import type { OutputChannel } from "../output.js";

const GREETING = "Hello there! At your service.";

export interface HelloOptions {
  output: OutputChannel;
}

export interface HelloPayload {
  greeting: string;
}

export function runHello(opts: HelloOptions): Promise<number> {
  if (opts.output.jsonMode) {
    const payload: HelloPayload = { greeting: GREETING };
    opts.output.json(payload);
  } else {
    opts.output.print(GREETING);
  }
  return Promise.resolve(EXIT_OK);
}

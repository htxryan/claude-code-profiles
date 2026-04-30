#!/usr/bin/env node
// c3p — protocol module, configuration division
/**
 * `c3p` CLI binary entry. Wires:
 *   argv → parseArgs → dispatch → exit code
 *
 * Top-level concerns OWNED HERE (and nowhere else):
 *   - process.argv slicing
 *   - process.cwd() default for --cwd=
 *   - TTY detection for gate mode (drives non-TTY auto-abort invariant)
 *   - Final error formatting → stderr + exitCodeFor
 *   - process.exit with the resolved code
 *
 * Note: SIGINT-to-lock-release is wired by the `withLock` call inside the
 * swap orchestrator (lesson L29affb99 + lock module's signalHandlers opt).
 * The bin doesn't need its own SIGINT handler — letting the per-acquire
 * handlers do their job keeps the cleanup tightly scoped to "release the
 * lock you actually hold" rather than a global "release any lock".
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
// Default import (NOT namespace) so EventEmitter methods (`process.on`) and
// TTY props (`process.stdin.isTTY`) bind correctly. The namespace form
// surfaces internals like `_events` but loses prototype methods.
import process from "node:process";
import { fileURLToPath, pathToFileURL } from "node:url";
import { realpathSync } from "node:fs";

import { dispatch } from "./dispatch.js";
import { exitCodeFor } from "./exit.js";
import { formatError } from "./format.js";
import { createOutput } from "./output.js";
import { parseArgs } from "./parse.js";

/**
 * Read the package version from package.json, falling back to "0.0.0" if the
 * file isn't reachable (npm-installed binary in a weird layout etc.).
 */
async function readPackageVersion(): Promise<string> {
  // The compiled binary lives at dist/cli/bin.js; package.json is the parent
  // of dist/. Resolve relative to import.meta.url so we don't rely on cwd.
  const here = path.dirname(fileURLToPath(import.meta.url));
  const candidates = [
    path.join(here, "..", "..", "package.json"), // dist/cli/bin.js → repo root
    path.join(here, "..", "package.json"), // src/cli/bin.ts in tests
  ];
  for (const c of candidates) {
    try {
      const raw = await fs.readFile(c, "utf8");
      const parsed = JSON.parse(raw);
      if (typeof parsed?.version === "string") return parsed.version;
    } catch {
      // try the next candidate
    }
  }
  return "0.0.0";
}

export async function main(argv: ReadonlyArray<string>): Promise<number> {
  const parsed = parseArgs(argv, process.cwd());
  const output = createOutput({
    json: parsed.ok ? parsed.invocation.global.json : false,
    quiet: parsed.ok ? parsed.invocation.global.quiet : false,
    // OutputChannel.isTty drives every command's colour decision. Reading
    // process.stdout.isTTY here (and only here) keeps the rest of the CLI
    // testable without monkey-patching the global stdout.
    isTty: Boolean((process.stdout as { isTTY?: boolean }).isTTY),
  });

  if (!parsed.ok) {
    output.error(formatError(new Error(parsed.message)));
    return 1; // argv error class is always user error
  }

  // TTY detection: if either stdin or stdout is not a TTY, we're in
  // non-interactive mode (e.g. piped, CI, scripted). The gate uses this to
  // auto-abort or honor --on-drift= rather than attempting a read.
  const isInteractive = Boolean(process.stdin.isTTY && process.stdout.isTTY);

  const version = await readPackageVersion();

  try {
    return await dispatch(parsed.invocation.command, parsed.invocation.global, {
      output,
      mode: isInteractive ? "interactive" : "non-interactive",
      version,
      signalHandlers: true,
    });
  } catch (err) {
    output.error(formatError(err));
    return exitCodeFor(err);
  }
}

// Run when invoked as a script. The conditional avoids running on import (the
// dispatch module is also exported so embedding contexts can call it
// directly).
//
// Compare via pathToFileURL so the equality is correct on Windows (where
// `file:///C:/...` doesn't match a naive `file://${argv[1]}` template), and
// drop the basename `endsWith("bin.js")` fallback — that fallback used to
// fire on any importer file ending in `bin.js`/`bin.ts`, which would auto-
// run main() inside an embedder named e.g. `mybin.js`.
//
// realpathSync canonicalises the argv path so symlinked installs (npm's
// node_modules/.bin shims, Homebrew prefixes, macOS `/var → /private/var`
// in tmp dirs) compare equal. import.meta.url is already the canonical
// real-path file URL, so the two only match after realpath resolution.
// Falls back to the un-canonicalised path if the file is unreadable —
// that's the embedder/test case where argv1 is something we don't own.
const isDirect = (() => {
  const argv1 = process.argv[1];
  if (typeof argv1 !== "string" || argv1 === "") return false;
  try {
    let resolved: string;
    try {
      resolved = realpathSync(argv1);
    } catch {
      resolved = argv1;
    }
    return import.meta.url === pathToFileURL(resolved).href;
  } catch {
    return false;
  }
})();

if (isDirect) {
  main(process.argv.slice(2)).then(
    (code) => process.exit(code),
    (err) => {
      // Defense-in-depth: dispatch's central catch handles app errors; this
      // catch only fires for unexpected failures (e.g. parser bugs). Stderr
      // bypasses OutputChannel here because we can't trust it post-failure.
      process.stderr.write(`c3p: internal error: ${String(err)}\n`);
      process.exit(2);
    },
  );
}

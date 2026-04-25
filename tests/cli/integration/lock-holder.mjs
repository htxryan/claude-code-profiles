/**
 * Test helper: holds the project lock indefinitely and exits when killed.
 *
 * Used by sigint.test.ts to verify SIGINT releases the lock cleanly (AC-15).
 * Runs against the BUILT artifact (dist/) so we exercise the same code path
 * the bin uses during a real swap. Communicates "lock acquired" by writing
 * the literal string "LOCKED\n" to stdout — parent reads that to know it's
 * safe to send SIGINT.
 */

import { withLock } from "../../../dist/state/lock.js";
import { buildStatePaths } from "../../../dist/state/paths.js";

const projectRoot = process.argv[2];
if (!projectRoot) {
  process.stderr.write("usage: lock-holder.mjs <project-root>\n");
  process.exit(2);
}
const paths = buildStatePaths(projectRoot);

await withLock(paths, async () => {
  process.stdout.write("LOCKED\n");
  // Wait forever — the test's SIGINT triggers the lock module's synchronous
  // unlink + exit(130) signal handler. The heartbeat interval keeps the
  // event loop alive; without it Node 20+ detects the unsettled top-level
  // await and exits early ("Warning: Detected unsettled top-level await").
  await new Promise(() => {
    setInterval(() => {}, 60_000);
  });
});

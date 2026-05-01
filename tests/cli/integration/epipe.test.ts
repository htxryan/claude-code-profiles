/**
 * EPIPE pipe-close regression — claude-code-profiles-qga.
 *
 * Read-only commands can be piped into short-circuiting consumers like
 * `head -1`, `grep -q`, `tail -1`. When the consumer closes the read end
 * before c3p has finished writing, EPIPE can fire asynchronously on the
 * underlying socket and bypass `output.writeSafe`'s synchronous try/catch.
 *
 * Without the fix in `bin.ts`'s `setupPipeSafeStdio`, that surfaces as a
 * `node:events` traceback on stderr and a non-zero exit. With the fix,
 * the listener handles EPIPE → process.exit(0) cleanly.
 *
 * The race is timing-dependent (couldn't reproduce in 30 follow-up tries
 * during the original sighting). To make this test deterministic, we
 * pipe `--help` (long, multi-section output) through `head -1` so the
 * consumer reliably closes its read end mid-write. We loop a handful of
 * times to catch any flake.
 */
import { spawn } from "node:child_process";

import { describe, expect, it } from "vitest";

import { BIN_PATH, ensureBuilt } from "./spawn.js";

interface PipelineResult {
  /** Pipeline exit code under `set -o pipefail` — non-zero iff c3p OR head failed. */
  exitCode: number;
  stdout: string;
  /** Combined stderr from the whole pipeline; head doesn't write to stderr,
   *  so realistically this is c3p's stderr. */
  stderr: string;
}

/**
 * Run `c3p <args> | head -1` via bash with `pipefail`. With pipefail set,
 * the pipeline's exit code is the first non-zero stage's exit code (or 0
 * if every stage succeeded). Since head -1 succeeds whenever it reads
 * any input, exit code 0 here means c3p also exited cleanly — the EPIPE
 * was either (a) handled by the listener, or (b) didn't fire this run.
 *
 * If exit != 0 with stderr matching node:events / EPIPE, the regression
 * is back.
 */
function runPipeline(args: string[]): Promise<PipelineResult> {
  return new Promise((resolve, reject) => {
    const argString = args.map((a) => `'${a}'`).join(" ");
    const cmd = `set -o pipefail; ${process.execPath} ${BIN_PATH} ${argString} | head -1`;

    const child = spawn("bash", ["-c", cmd], { stdio: ["ignore", "pipe", "pipe"] });

    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (d) => {
      stdout += d.toString();
    });
    child.stderr.on("data", (d) => {
      stderr += d.toString();
    });

    const timer = setTimeout(() => {
      child.kill("SIGTERM");
      reject(new Error("pipeline timed out after 10s"));
    }, 10_000);

    child.on("error", (err) => {
      clearTimeout(timer);
      reject(err);
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      resolve({ exitCode: code ?? -1, stdout, stderr });
    });
  });
}

describe("EPIPE on early pipe close (claude-code-profiles-qga)", () => {
  // Loop the test a few times because the underlying race is timing-
  // dependent — the original sighting only fired once in ~30 invocations.
  // Five iterations is enough to catch a regression that flakes 1-in-3.
  for (let i = 0; i < 5; i++) {
    it(`--help piped into head -1 exits cleanly (run ${i + 1}/5)`, async () => {
      await ensureBuilt();
      const r = await runPipeline(["--help"]);
      // pipefail: 0 means c3p AND head both exited 0. Without the EPIPE
      // listener, c3p crashes with a node:events traceback on stderr and
      // exit 1 (uncaught exception), which pipefail surfaces here.
      expect(r.exitCode, `pipeline stderr: ${r.stderr}`).toBe(0);
      expect(r.stderr).not.toMatch(/EPIPE/);
      expect(r.stderr).not.toMatch(/node:events/);
      expect(r.stderr).not.toMatch(/Error: write/);
      // head -1 should have captured at least one line of usage output.
      expect(r.stdout.length).toBeGreaterThan(0);
    });
  }

  it("--version piped into head -1 exits cleanly (short output stress case)", async () => {
    await ensureBuilt();
    const r = await runPipeline(["--version"]);
    expect(r.exitCode, `pipeline stderr: ${r.stderr}`).toBe(0);
    expect(r.stderr).not.toMatch(/EPIPE|node:events|Error: write/);
  });
});

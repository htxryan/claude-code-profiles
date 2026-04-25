/**
 * Spawn the built CLI binary as a subprocess so the integration tests
 * exercise the real exit-code + stdout/stderr surface (no in-process mocks).
 *
 * Tests must `npm run build` first — handled at suite setup level by an
 * `expect.stat` of dist/cli/bin.js. We deliberately do NOT auto-build per
 * test (it'd dominate runtime); contributors run `npm run build && npm test`
 * or rely on a CI step.
 */

import { spawn } from "node:child_process";
import * as path from "node:path";

import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const BIN_PATH = path.resolve(HERE, "..", "..", "..", "dist", "cli", "bin.js");

export interface SpawnResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  signal: NodeJS.Signals | null;
}

export interface SpawnOptions {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  /** Additional argv to append after our injected --cwd. */
  args: string[];
  /** Optional stdin payload. */
  stdin?: string;
  /** Timeout in ms; default 10s. */
  timeoutMs?: number;
}

export async function runCli(opts: SpawnOptions): Promise<SpawnResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [BIN_PATH, ...opts.args], {
      cwd: opts.cwd ?? process.cwd(),
      env: { ...process.env, ...opts.env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (d) => {
      stdout += d.toString();
    });
    child.stderr?.on("data", (d) => {
      stderr += d.toString();
    });
    if (opts.stdin) child.stdin?.write(opts.stdin);
    child.stdin?.end();

    const timer = setTimeout(() => {
      child.kill("SIGTERM");
      reject(new Error(`spawn timed out after ${opts.timeoutMs ?? 10000}ms`));
    }, opts.timeoutMs ?? 10000);

    child.on("close", (code, signal) => {
      clearTimeout(timer);
      resolve({
        stdout,
        stderr,
        exitCode: code ?? 0,
        signal,
      });
    });
    child.on("error", (err) => {
      clearTimeout(timer);
      reject(err);
    });
  });
}

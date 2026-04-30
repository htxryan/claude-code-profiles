/**
 * Integration tests for `c3p completions <shell>` (claude-code-profiles-0zn).
 *
 * The contract: the emitted script must source cleanly in the target shell
 * (bash/zsh always; fish only when the binary is on PATH). Tab-completion
 * behaviour itself is shell-tested in the smoke runs below — we validate
 * that the function/widget gets registered without errors.
 */

import { execSync, spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { ensureBuilt, runCli } from "./spawn.js";

interface ShellResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

function which(cmd: string): boolean {
  try {
    const r = execSync(`command -v ${cmd}`, { stdio: "pipe" });
    return r.toString().trim().length > 0;
  } catch {
    return false;
  }
}

function runShell(shell: string, scriptBody: string): Promise<ShellResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(shell, ["-c", scriptBody], { stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (d) => (stdout += d.toString()));
    child.stderr.on("data", (d) => (stderr += d.toString()));
    const timer = setTimeout(() => {
      child.kill("SIGTERM");
      reject(new Error(`shell timed out`));
    }, 5000);
    child.on("close", (code) => {
      clearTimeout(timer);
      resolve({ stdout, stderr, exitCode: code ?? -1 });
    });
    child.on("error", reject);
  });
}

const tmpFiles: string[] = [];
afterEach(async () => {
  for (const f of tmpFiles.splice(0)) {
    await fs.rm(f, { force: true });
  }
});

async function writeTmp(prefix: string, content: string): Promise<string> {
  const p = path.join(os.tmpdir(), `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}.sh`);
  await fs.writeFile(p, content);
  tmpFiles.push(p);
  return p;
}

describe("completions <shell> (claude-code-profiles-0zn)", () => {
  it("emits non-empty script for each supported shell", async () => {
    await ensureBuilt();
    for (const shell of ["bash", "zsh", "fish"]) {
      const r = await runCli({ args: ["completions", shell] });
      expect(r.exitCode, `${shell} exit`).toBe(0);
      expect(r.stdout.length, `${shell} script empty`).toBeGreaterThan(100);
    }
  });

  it("rejects unsupported shell with exit 1", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["completions", "powershell"] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("bash|zsh|fish");
  });

  it("rejects missing shell argument with exit 1", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["completions"] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("requires a shell");
  });

  it("bash script sources cleanly and registers _c3p", async () => {
    if (!which("bash")) return;
    await ensureBuilt();
    const r = await runCli({ args: ["completions", "bash"] });
    expect(r.exitCode).toBe(0);
    const scriptPath = await writeTmp("ccp-bash", r.stdout);
    const result = await runShell("bash", `source ${scriptPath} && type _c3p`);
    expect(result.exitCode, `stderr: ${result.stderr}`).toBe(0);
    expect(result.stdout).toContain("_c3p");
  });

  it("zsh script sources cleanly and registers _c3p", async () => {
    if (!which("zsh")) return;
    await ensureBuilt();
    const r = await runCli({ args: ["completions", "zsh"] });
    expect(r.exitCode).toBe(0);
    const scriptPath = await writeTmp("ccp-zsh", r.stdout);
    // Initialize compinit before sourcing the function definition, then check
    // the function is callable. -u skips owner checks (tmp files have $USER as owner anyway).
    const result = await runShell(
      "zsh",
      `autoload -Uz compinit && compinit -u && source ${scriptPath} && type _c3p`,
    );
    expect(result.exitCode, `stderr: ${result.stderr}`).toBe(0);
    expect(result.stdout).toContain("_c3p");
  });

  it("fish script sources cleanly when fish is available", async () => {
    if (!which("fish")) return;
    await ensureBuilt();
    const r = await runCli({ args: ["completions", "fish"] });
    expect(r.exitCode).toBe(0);
    const scriptPath = await writeTmp("ccp-fish", r.stdout);
    const result = await runShell(
      "fish",
      `source ${scriptPath} && complete -c c3p | head -1`,
    );
    expect(result.exitCode, `stderr: ${result.stderr}`).toBe(0);
  });

  it("--json wraps the script in a payload (machine consumers)", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["completions", "bash", "--json"] });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout) as { shell: string; script: string };
    expect(payload.shell).toBe("bash");
    expect(payload.script).toContain("_c3p");
  });
});

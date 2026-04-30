import { spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { describe, expect, it } from "vitest";

import { BIN_PATH, ensureBuilt, runCli } from "./spawn.js";

describe("--help / --version (AC-20)", () => {
  it("--version prints package version + exits 0", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["--version"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/claude-profiles \d+\.\d+\.\d+/);
  });

  // Regression: isDirect must canonicalise argv[1] before comparing it to
  // import.meta.url. Without realpathSync, a symlinked bin path (npm's
  // node_modules/.bin shim, macOS /var→/private/var, Homebrew prefixes,
  // /usr/local/bin → /opt/homebrew/...) compares unequal so main() never
  // runs and the binary silently exits 0 with no output. Found via
  // post-publish install validation of 0.2.3.
  it("--version works when invoked through a symlink (real npm install path)", async () => {
    await ensureBuilt();
    const tmp = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-symlink-bin-"));
    try {
      const linked = path.join(tmp, "claude-profiles");
      await fs.symlink(BIN_PATH, linked);
      const r = await new Promise<{ stdout: string; stderr: string; code: number }>((resolve, reject) => {
        const child = spawn(process.execPath, [linked, "--version"], { stdio: ["ignore", "pipe", "pipe"] });
        let stdout = "";
        let stderr = "";
        child.stdout.on("data", (d) => (stdout += d.toString()));
        child.stderr.on("data", (d) => (stderr += d.toString()));
        child.on("close", (code) => resolve({ stdout, stderr, code: code ?? -1 }));
        child.on("error", reject);
      });
      expect(r.code, `stderr: ${r.stderr}`).toBe(0);
      expect(r.stdout).toMatch(/claude-profiles \d+\.\d+\.\d+/);
    } finally {
      await fs.rm(tmp, { recursive: true, force: true });
    }
  });

  it("-V short form also works", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["-V"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/claude-profiles \d+\.\d+\.\d+/);
  });

  it("--help prints usage with all R29 verbs", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["--help"] });
    expect(r.exitCode).toBe(0);
    for (const verb of ["init", "list", "use", "status", "drift", "diff", "new", "validate", "sync", "hook"]) {
      expect(r.stdout).toContain(verb);
    }
    expect(r.stdout).toContain("EXIT CODES");
  });

  it("use --help prints verb-specific guidance", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["use", "--help"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("--on-drift=");
  });

  // claude-code-profiles-xd2: every verb's help follows the same template —
  // tagline, USAGE, DESCRIPTION, EXAMPLES, EXIT CODES. Lock that in so a
  // future help-text edit can't quietly drop a section.
  it("every verb's help has the standard sections", async () => {
    await ensureBuilt();
    const verbs = ["init", "list", "use", "status", "drift", "diff", "new", "validate", "sync", "hook", "doctor", "completions"];
    for (const verb of verbs) {
      const r = await runCli({ args: [verb, "--help"] });
      expect(r.exitCode, `${verb} --help exited non-zero`).toBe(0);
      expect(r.stdout, `${verb} --help missing USAGE`).toContain("USAGE");
      expect(r.stdout, `${verb} --help missing DESCRIPTION`).toContain("DESCRIPTION");
      expect(r.stdout, `${verb} --help missing EXAMPLES`).toContain("EXAMPLES");
      expect(r.stdout, `${verb} --help missing EXIT CODES`).toContain("EXIT CODES");
      // Every verb mentions --cwd or --json (the common globals) so users
      // running just `claude-profiles <verb> --help` don't have to bounce out
      // to the top-level help to learn about them.
      expect(r.stdout, `${verb} --help missing global option reference`).toMatch(/--(cwd|json)/);
    }
  });

  // cw6.4 followup: validate's exit-codes block must document the R44
  // marker-failure case so users grepping --help know why validate may
  // exit 1 instead of 0/3.
  it("validate --help documents the R44 marker-missing exit-1 case", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["validate", "--help"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("EXIT CODES");
    expect(r.stdout).toMatch(/missing claude-profiles markers/i);
    expect(r.stdout).toContain("claude-profiles init");
  });

  it("top-level --help defines the spec terms it uses", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["--help"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("GLOSSARY");
    // The four terms the help text references should each appear in the glossary.
    for (const term of ["profile", "extends", "drift", "materialize"]) {
      expect(r.stdout).toContain(term);
    }
  });
});

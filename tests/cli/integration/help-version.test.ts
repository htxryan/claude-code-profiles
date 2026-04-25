import { promises as fs } from "node:fs";

import { describe, expect, it } from "vitest";

import { BIN_PATH, runCli } from "./spawn.js";

async function ensureBuilt() {
  try {
    await fs.access(BIN_PATH);
  } catch {
    throw new Error(
      `dist/cli/bin.js not found at ${BIN_PATH} — run \`npm run build\` before integration tests`,
    );
  }
}

describe("--help / --version (AC-20)", () => {
  it("--version prints package version + exits 0", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["--version"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/claude-profiles \d+\.\d+\.\d+/);
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
});

import { describe, expect, it } from "vitest";

import { ensureBuilt, runCli } from "./spawn.js";

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

  // claude-code-profiles-xd2: every verb's help follows the same template —
  // tagline, USAGE, DESCRIPTION, EXAMPLES, EXIT CODES. Lock that in so a
  // future help-text edit can't quietly drop a section.
  it("every verb's help has the standard sections", async () => {
    await ensureBuilt();
    const verbs = ["init", "list", "use", "status", "drift", "diff", "new", "validate", "sync", "hook"];
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

import { describe, expect, it } from "vitest";

import { parseArgs } from "../../src/cli/parse.js";
import type { Command, GlobalOptions } from "../../src/cli/types.js";

const CWD = "/cwd";

function run(argv: string[]): { command: Command; global: GlobalOptions } {
  const r = parseArgs(argv, CWD);
  if (!r.ok) throw new Error(`expected parse ok, got error: ${r.message}`);
  return r.invocation;
}

function err(argv: string[]): string {
  const r = parseArgs(argv, CWD);
  if (r.ok) throw new Error(`expected parse error, got ok`);
  return r.message;
}

describe("parseArgs — verbs (R29)", () => {
  it("parses every R29 verb into a typed Command", () => {
    expect(run(["init"]).command).toEqual({
      kind: "init",
      starter: "default",
      seed: true,
      hook: true,
    });
    expect(run(["init", "--no-seed", "--no-hook"]).command).toEqual({
      kind: "init",
      starter: "default",
      seed: false,
      hook: false,
    });
    expect(run(["init", "--starter=base"]).command).toEqual({
      kind: "init",
      starter: "base",
      seed: true,
      hook: true,
    });
    expect(run(["init", "--starter", "main"]).command).toEqual({
      kind: "init",
      starter: "main",
      seed: true,
      hook: true,
    });
    expect(run(["list"]).command).toEqual({ kind: "list" });
    expect(run(["use", "minimal"]).command).toEqual({ kind: "use", profile: "minimal" });
    expect(run(["status"]).command).toEqual({ kind: "status" });
    expect(run(["drift"]).command).toEqual({ kind: "drift", preCommitWarn: false });
    expect(run(["drift", "--pre-commit-warn"]).command).toEqual({
      kind: "drift",
      preCommitWarn: true,
    });
    expect(run(["diff", "a"]).command).toEqual({ kind: "diff", a: "a", b: null });
    expect(run(["diff", "a", "b"]).command).toEqual({ kind: "diff", a: "a", b: "b" });
    expect(run(["new", "minimal"]).command).toEqual({
      kind: "new",
      profile: "minimal",
      description: null,
    });
    expect(run(["new", "x", "--description=foo"]).command).toEqual({
      kind: "new",
      profile: "x",
      description: "foo",
    });
    expect(run(["validate"]).command).toEqual({ kind: "validate", profile: null });
    expect(run(["validate", "x"]).command).toEqual({ kind: "validate", profile: "x" });
    expect(run(["sync"]).command).toEqual({ kind: "sync" });
    expect(run(["hook", "install"]).command).toEqual({
      kind: "hook",
      action: "install",
      force: false,
    });
    expect(run(["hook", "install", "--force"]).command).toEqual({
      kind: "hook",
      action: "install",
      force: true,
    });
    expect(run(["hook", "uninstall"]).command).toEqual({
      kind: "hook",
      action: "uninstall",
      force: false,
    });
  });

  it("rejects unknown verbs with helpful message naming the offending word", () => {
    const m = err(["bogus"]);
    expect(m).toContain("unknown command");
    expect(m).toContain("bogus");
  });

  it("rejects unknown sub-action for hook", () => {
    expect(err(["hook", "reload"])).toContain("install|uninstall");
  });

  it("rejects unknown flag per verb", () => {
    expect(err(["drift", "--xyz"])).toContain("--xyz");
    expect(err(["diff", "--xyz", "a"])).toContain("--xyz");
    expect(err(["new", "x", "--bogus"])).toContain("--bogus");
    expect(err(["validate", "--bogus"])).toContain("--bogus");
  });
});

describe("parseArgs — global flags", () => {
  it("--json sets json mode regardless of position", () => {
    expect(run(["--json", "list"]).global.json).toBe(true);
    expect(run(["list", "--json"]).global.json).toBe(true);
  });

  it("--cwd accepts both = and space forms", () => {
    expect(run(["--cwd=/tmp/x", "list"]).global.cwd).toBe("/tmp/x");
    expect(run(["--cwd", "/tmp/y", "list"]).global.cwd).toBe("/tmp/y");
  });

  it("--cwd requires a non-empty path", () => {
    expect(err(["--cwd=", "list"])).toContain("non-empty");
    expect(err(["--cwd", "--json"])).toContain("requires a path");
    // Regression: the space-form previously accepted "" silently because the
    // check was `next === undefined || next.startsWith("-")`.
    expect(err(["--cwd", "", "list"])).toContain("non-empty");
  });

  it("--on-drift accepts discard|persist|abort and rejects others", () => {
    expect(run(["--on-drift=discard", "use", "x"]).global.onDrift).toBe("discard");
    expect(run(["--on-drift=persist", "use", "x"]).global.onDrift).toBe("persist");
    expect(run(["--on-drift=abort", "use", "x"]).global.onDrift).toBe("abort");
    expect(run(["--on-drift", "abort", "use", "x"]).global.onDrift).toBe("abort");
    expect(err(["--on-drift=keep", "use", "x"])).toContain("discard|persist|abort");
  });

  it("--no-color is rejected explicitly with a flag-shaped diagnostic", () => {
    // Until colour is wired through formatters, --no-color is rejected at the
    // parser level so the message names the flag — earlier the flag fell
    // through to the verb dispatcher and produced "list takes no arguments",
    // which made it look like the flag was a positional.
    const m = err(["list", "--no-color"]);
    expect(m).toContain("--no-color");
    expect(m).toContain("unknown flag");
  });

  it("--help with no verb returns help command", () => {
    expect(run(["--help"]).command).toEqual({ kind: "help", verb: null });
  });

  it("--help after a verb returns help command for that verb", () => {
    expect(run(["use", "--help"]).command).toEqual({ kind: "help", verb: "use" });
  });

  it("`help <verb>` is equivalent to `<verb> --help`", () => {
    expect(run(["help", "use"]).command).toEqual({ kind: "help", verb: "use" });
    expect(run(["help"]).command).toEqual({ kind: "help", verb: null });
  });

  it("--version emits version command", () => {
    expect(run(["--version"]).command).toEqual({ kind: "version" });
    expect(run(["-V"]).command).toEqual({ kind: "version" });
  });

  it("--version short-circuits even when a verb is also present", () => {
    // Regression: previously `claude-profiles list --version` ran `list` and
    // silently dropped the flag; users who type --version always want version.
    expect(run(["list", "--version"]).command).toEqual({ kind: "version" });
    expect(run(["--version", "use", "x"]).command).toEqual({ kind: "version" });
  });
});

describe("parseArgs — positional arity", () => {
  it("use requires exactly one positional", () => {
    expect(err(["use"])).toContain("requires");
    expect(err(["use", "a", "b"])).toContain("one argument");
  });

  it("diff requires 1 or 2 positionals", () => {
    expect(err(["diff"])).toContain("requires");
    expect(err(["diff", "a", "b", "c"])).toContain("one or two");
  });

  it("new requires exactly one positional", () => {
    expect(err(["new"])).toContain("requires");
    expect(err(["new", "a", "b"])).toContain("one positional");
  });

  it("init/list/status/sync take no positionals", () => {
    expect(err(["init", "x"])).toContain("no positional arguments");
    expect(err(["list", "x"])).toContain("no arguments");
    expect(err(["status", "x"])).toContain("no arguments");
    expect(err(["sync", "x"])).toContain("no arguments");
  });
});

describe("parseArgs — empty and missing-command", () => {
  it("empty argv returns missing-command error pointing at --help", () => {
    const m = err([]);
    expect(m).toContain("missing command");
    expect(m).toContain("--help");
  });
});

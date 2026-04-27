import { afterEach, describe, expect, it } from "vitest";

import { dispatch } from "../../src/cli/dispatch.js";
import type { Command, GlobalOptions } from "../../src/cli/types.js";
import { makeFixture, type Fixture } from "../helpers/fixture.js";
import { captureOutput } from "./helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

function ctxFor(json = false) {
  const cap = captureOutput(json);
  return {
    cap,
    ctx: {
      output: cap.channel,
      mode: "non-interactive" as const,
      version: "9.9.9",
      signalHandlers: false,
    },
  };
}

function global(json = false): GlobalOptions {
  return { json, cwd: ".", onDrift: null, noColor: false, quiet: false, waitMs: null };
}

describe("dispatch — top-level routes", () => {
  it("version → prints version string", async () => {
    const { cap, ctx } = ctxFor();
    const code = await dispatch({ kind: "version" }, global(), ctx);
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("claude-profiles 9.9.9");
  });

  it("version under --json → emits {version} payload (not silenced)", async () => {
    // Regression: ctx.output.print is silenced in --json mode; without an
    // explicit JSON branch, `--version --json` produced empty stdout.
    const { cap, ctx } = ctxFor(true);
    const code = await dispatch({ kind: "version" }, global(true), ctx);
    expect(code).toBe(0);
    const out = cap.stdout().trim();
    expect(JSON.parse(out)).toEqual({ version: "9.9.9" });
  });

  it("help (no verb) → prints top-level help with all R29 verbs", async () => {
    const { cap, ctx } = ctxFor();
    await dispatch({ kind: "help", verb: null }, global(), ctx);
    const out = cap.stdout();
    for (const verb of ["init", "list", "use", "status", "drift", "diff", "new", "validate", "sync", "hook"]) {
      expect(out).toContain(verb);
    }
  });

  it("help under --json → emits {help} payload (not silenced)", async () => {
    const { cap, ctx } = ctxFor(true);
    await dispatch({ kind: "help", verb: null }, global(true), ctx);
    const parsed = JSON.parse(cap.stdout().trim());
    expect(typeof parsed.help).toBe("string");
    expect(parsed.help).toContain("USAGE");
  });

  it("help <verb> → prints verb-specific help", async () => {
    const { cap, ctx } = ctxFor();
    await dispatch({ kind: "help", verb: "use" }, global(), ctx);
    expect(cap.stdout()).toContain("use <name>");
    expect(cap.stdout()).toContain("--on-drift=");
  });
});

describe("dispatch — every command kind dispatches without throwing on baseline fixture", () => {
  it("list / status / drift / diff / validate / new on a happy fixture", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "X\n" } },
        b: { manifest: { name: "b" }, files: { "y.md": "Y\n" } },
      },
    });
    const cwd = fx.projectRoot;
    const cases: Command[] = [
      { kind: "list" },
      { kind: "status" },
      { kind: "drift", preCommitWarn: false, verbose: false, preview: false },
      { kind: "diff", a: "a", b: "b", preview: false },
      { kind: "validate", profile: null, brief: false },
      { kind: "new", profile: "scratch", description: "test" },
    ];
    for (const cmd of cases) {
      const { ctx } = ctxFor();
      const code = await dispatch(cmd, { ...global(), cwd }, ctx);
      expect(code).toBe(0);
    }
  });
});

describe("dispatch — E6 init/hook routes", () => {
  it("init in a fresh project → 0", async () => {
    fx = await makeFixture({});
    const { ctx } = ctxFor();
    const code = await dispatch(
      { kind: "init", starter: "default", seed: false, hook: false },
      { ...global(), cwd: fx.projectRoot },
      ctx,
    );
    expect(code).toBe(0);
  });

  it("hook uninstall in a fresh project (no .git) → 0 with absent classification", async () => {
    fx = await makeFixture({});
    const { cap, ctx } = ctxFor();
    const code = await dispatch(
      { kind: "hook", action: "uninstall", force: false },
      { ...global(), cwd: fx.projectRoot },
      ctx,
    );
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("No pre-commit hook to remove");
  });
});
